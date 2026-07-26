package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

const (
	managedWSMaxBootstrapBytes = 1 << 20
	managedWSMaxMessageBytes   = 16 << 20
	managedWSMaxFragments      = 64
	managedWSMaxFragmentSets   = 128
)

var managedWSRetryDelays = []time.Duration{
	time.Second,
	5 * time.Second,
	30 * time.Second,
	2 * time.Minute,
	5 * time.Minute,
}

type managedWSDialer interface {
	DialContext(
		context.Context,
		string,
		http.Header,
	) (*websocket.Conn, *http.Response, error)
}

// managedWSClient keeps the official Feishu wire protocol and event dispatcher,
// while making connection lifetime follow context cancellation. The SDK v3.9.9
// client blocks forever after Start and its reconnect sleeps ignore cancellation.
type managedWSClient struct {
	appID      string
	appSecret  string
	domain     string
	http       *http.Client
	dialer     managedWSDialer
	dispatcher *dispatcher.EventDispatcher

	mu           sync.Mutex
	conn         *websocket.Conn
	cancel       context.CancelFunc
	running      bool
	pingInterval time.Duration
	onReady      func()
	onError      func(error)

	writeMu sync.Mutex
}

func newManagedWSClient(
	appID string,
	appSecret string,
	domain string,
	eventDispatcher *dispatcher.EventDispatcher,
) *managedWSClient {
	return &managedWSClient{
		appID: appID, appSecret: appSecret, domain: strings.TrimRight(domain, "/"),
		http: http.DefaultClient, dialer: websocket.DefaultDialer,
		dispatcher: eventDispatcher, pingInterval: 2 * time.Minute,
	}
}

func (c *managedWSClient) SetOnReady(handler func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onReady = handler
}

func (c *managedWSClient) SetOnError(handler func(error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onError = handler
}

func (c *managedWSClient) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return errors.New("Feishu WebSocket client is already running")
	}
	runCtx, cancel := context.WithCancel(ctx)
	c.running = true
	c.cancel = cancel
	c.mu.Unlock()
	defer func() {
		cancel()
		c.mu.Lock()
		c.running = false
		c.cancel = nil
		c.conn = nil
		c.mu.Unlock()
	}()

	failureCount := 0
	for {
		connected, err := c.runConnection(runCtx)
		if runCtx.Err() != nil {
			return runCtx.Err()
		}
		if err == nil {
			err = errors.New("Feishu WebSocket connection closed")
		}
		c.reportError(err)
		if connected {
			failureCount = 0
		}
		delay := managedWSRetryDelays[min(failureCount, len(managedWSRetryDelays)-1)]
		failureCount++
		timer := time.NewTimer(delay)
		select {
		case <-runCtx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return runCtx.Err()
		case <-timer.C:
		}
	}
}

func (c *managedWSClient) Stop(context.Context) error {
	c.mu.Lock()
	cancel := c.cancel
	conn := c.conn
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if conn != nil {
		_ = conn.Close()
	}
	return nil
}

func (c *managedWSClient) runConnection(ctx context.Context) (bool, error) {
	endpoint, serviceID, config, err := c.bootstrap(ctx)
	if err != nil {
		return false, err
	}
	conn, resp, err := c.dialer.DialContext(ctx, endpoint, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return false, fmt.Errorf("connect Feishu WebSocket: %w", err)
	}
	conn.SetReadLimit(managedWSMaxMessageBytes)
	c.applyConfig(config)
	c.mu.Lock()
	c.conn = conn
	onReady := c.onReady
	c.mu.Unlock()
	if onReady != nil {
		onReady()
	}

	connectionCtx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		<-connectionCtx.Done()
		_ = conn.Close()
	}()
	go func() {
		defer workers.Done()
		c.pingLoop(connectionCtx, conn, serviceID)
	}()
	err = c.readLoop(connectionCtx, conn)
	cancel()
	_ = conn.Close()
	workers.Wait()
	c.mu.Lock()
	if c.conn == conn {
		c.conn = nil
	}
	c.mu.Unlock()
	return true, err
}

func (c *managedWSClient) bootstrap(
	ctx context.Context,
) (string, int32, *larkws.ClientConfig, error) {
	payload, err := json.Marshal(larkws.BootstrapRequest{
		AppID: c.appID, AppSecret: c.appSecret,
	})
	if err != nil {
		return "", 0, nil, err
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.domain+larkws.GenEndpointUri,
		bytes.NewReader(payload),
	)
	if err != nil {
		return "", 0, nil, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("locale", "zh")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", 0, nil, fmt.Errorf("bootstrap Feishu WebSocket: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, managedWSMaxBootstrapBytes+1))
	if err != nil {
		return "", 0, nil, fmt.Errorf("read Feishu WebSocket bootstrap: %w", err)
	}
	if len(body) > managedWSMaxBootstrapBytes {
		return "", 0, nil, errors.New("Feishu WebSocket bootstrap response is too large")
	}
	if resp.StatusCode != http.StatusOK {
		return "", 0, nil, fmt.Errorf(
			"Feishu WebSocket bootstrap returned HTTP %d",
			resp.StatusCode,
		)
	}
	var endpoint larkws.EndpointResp
	if err := json.Unmarshal(body, &endpoint); err != nil {
		return "", 0, nil, fmt.Errorf("decode Feishu WebSocket bootstrap: %w", err)
	}
	if endpoint.Code != larkws.OK {
		return "", 0, nil, fmt.Errorf(
			"Feishu WebSocket bootstrap failed: code=%d",
			endpoint.Code,
		)
	}
	if endpoint.Data == nil || endpoint.Data.Url == "" {
		return "", 0, nil, errors.New(
			"Feishu WebSocket bootstrap did not return an endpoint",
		)
	}
	parsed, err := url.Parse(endpoint.Data.Url)
	if err != nil || (parsed.Scheme != "ws" && parsed.Scheme != "wss") {
		return "", 0, nil, errors.New(
			"Feishu WebSocket bootstrap returned an invalid endpoint",
		)
	}
	serviceID, _ := strconv.ParseInt(parsed.Query().Get(larkws.ServiceID), 10, 32)
	return endpoint.Data.Url, int32(serviceID), endpoint.Data.ClientConfig, nil
}

func (c *managedWSClient) readLoop(
	ctx context.Context,
	conn *websocket.Conn,
) error {
	fragments := make(map[string][][]byte)
	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("read Feishu WebSocket: %w", err)
		}
		if messageType != websocket.BinaryMessage {
			continue
		}
		if err := c.handleFrame(ctx, conn, fragments, payload); err != nil {
			return err
		}
	}
}

func (c *managedWSClient) handleFrame(
	ctx context.Context,
	conn *websocket.Conn,
	fragments map[string][][]byte,
	payload []byte,
) error {
	var frame larkws.Frame
	if err := frame.Unmarshal(payload); err != nil {
		return fmt.Errorf("decode Feishu WebSocket frame: %w", err)
	}
	headers := larkws.Headers(frame.Headers)
	switch larkws.FrameType(frame.Method) {
	case larkws.FrameTypeControl:
		if larkws.MessageType(headers.GetString(larkws.HeaderType)) == larkws.MessageTypePong &&
			len(frame.Payload) > 0 {
			var config larkws.ClientConfig
			if json.Unmarshal(frame.Payload, &config) == nil {
				c.applyConfig(&config)
			}
		}
		return nil
	case larkws.FrameTypeData:
	default:
		return nil
	}
	if larkws.MessageType(headers.GetString(larkws.HeaderType)) != larkws.MessageTypeEvent {
		return nil
	}
	messageID := headers.GetString(larkws.HeaderMessageID)
	data, err := combineWSFragments(
		fragments,
		messageID,
		headers.GetInt(larkws.HeaderSum),
		headers.GetInt(larkws.HeaderSeq),
		frame.Payload,
	)
	if err != nil || data == nil {
		return err
	}
	startedAt := time.Now()
	var responseData any
	var handlerErr error
	if c.dispatcher != nil {
		responseData, handlerErr = c.dispatcher.Do(ctx, data)
	}
	headers.Add(larkws.HeaderBizRt, strconv.FormatInt(time.Since(startedAt).Milliseconds(), 10))
	response := larkws.NewResponseByCode(http.StatusOK)
	if handlerErr != nil {
		response = larkws.NewResponseByCode(http.StatusInternalServerError)
	} else if responseData != nil {
		response.Data, handlerErr = json.Marshal(responseData)
		if handlerErr != nil {
			response = larkws.NewResponseByCode(http.StatusInternalServerError)
		}
	}
	frame.Payload, _ = json.Marshal(response)
	frame.Headers = headers
	encoded, marshalErr := frame.Marshal()
	if marshalErr != nil {
		return fmt.Errorf("encode Feishu WebSocket response: %w", marshalErr)
	}
	if writeErr := c.write(conn, websocket.BinaryMessage, encoded); writeErr != nil {
		return fmt.Errorf("write Feishu WebSocket response: %w", writeErr)
	}
	return nil
}

func combineWSFragments(
	fragments map[string][][]byte,
	messageID string,
	sum int,
	sequence int,
	payload []byte,
) ([]byte, error) {
	if sum <= 1 {
		return payload, nil
	}
	if messageID == "" || sum > managedWSMaxFragments || sequence < 0 || sequence >= sum {
		return nil, errors.New("invalid fragmented Feishu WebSocket message")
	}
	parts, exists := fragments[messageID]
	if !exists {
		if len(fragments) >= managedWSMaxFragmentSets {
			return nil, errors.New("too many incomplete Feishu WebSocket messages")
		}
		parts = make([][]byte, sum)
		fragments[messageID] = parts
	}
	if len(parts) != sum {
		delete(fragments, messageID)
		return nil, errors.New("inconsistent fragmented Feishu WebSocket message")
	}
	parts[sequence] = append([]byte(nil), payload...)
	size := 0
	for _, part := range parts {
		if part == nil {
			return nil, nil
		}
		size += len(part)
		if size > managedWSMaxMessageBytes {
			delete(fragments, messageID)
			return nil, errors.New("fragmented Feishu WebSocket message is too large")
		}
	}
	combined := make([]byte, 0, size)
	for _, part := range parts {
		combined = append(combined, part...)
	}
	delete(fragments, messageID)
	return combined, nil
}

func (c *managedWSClient) pingLoop(
	ctx context.Context,
	conn *websocket.Conn,
	serviceID int32,
) {
	for {
		timer := time.NewTimer(c.currentPingInterval())
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
		frame := larkws.NewPingFrame(serviceID)
		encoded, err := frame.Marshal()
		if err != nil || c.write(conn, websocket.BinaryMessage, encoded) != nil {
			_ = conn.Close()
			return
		}
	}
}

func (c *managedWSClient) write(
	conn *websocket.Conn,
	messageType int,
	payload []byte,
) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return conn.WriteMessage(messageType, payload)
}

func (c *managedWSClient) applyConfig(config *larkws.ClientConfig) {
	if config == nil || config.PingInterval <= 0 {
		return
	}
	interval := time.Duration(config.PingInterval) * time.Second
	if interval < time.Second || interval > 10*time.Minute {
		return
	}
	c.mu.Lock()
	c.pingInterval = interval
	c.mu.Unlock()
}

func (c *managedWSClient) currentPingInterval() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pingInterval
}

func (c *managedWSClient) reportError(err error) {
	c.mu.Lock()
	handler := c.onError
	c.mu.Unlock()
	if handler != nil {
		handler(err)
	}
}
