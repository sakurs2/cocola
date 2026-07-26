package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkchannel "github.com/larksuite/oapi-sdk-go/v3/channel"
	"github.com/larksuite/oapi-sdk-go/v3/channel/normalize"
	"github.com/larksuite/oapi-sdk-go/v3/channel/outbound"
	"github.com/larksuite/oapi-sdk-go/v3/channel/safety"
	larktypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type RuntimeMessage struct {
	EventID      string
	MessageID    string
	ChatID       string
	ChatType     string
	SenderOpenID string
	Text         string
	ContentType  string
	Resources    []Resource
	CreateTimeMS int64
}

type BotIdentity struct {
	OpenID         string
	Name           string
	ActivateStatus int
}

type MessageStream interface {
	Append(context.Context, string) error
	Close(context.Context) error
}

type RuntimeChannel interface {
	OnMessage(func(context.Context, RuntimeMessage) error)
	OnReady(func(BotIdentity))
	OnError(func(error))
	Start(context.Context) error
	Stop(context.Context) error
	SendMarkdown(context.Context, string, string, string) error
	StreamMarkdown(context.Context, string, string, string) (MessageStream, error)
	AddReaction(context.Context, string, string) (string, error)
	DeleteReaction(context.Context, string, string) error
}

type ChannelFactory interface {
	New(Connector, string) (RuntimeChannel, error)
}

type SDKChannelFactory struct{}

func (SDKChannelFactory) New(connector Connector, appSecret string) (RuntimeChannel, error) {
	if connector.AppID == "" || appSecret == "" {
		return nil, ErrInvalid
	}
	baseURL := lark.FeishuBaseUrl
	if connector.Domain == DomainLark {
		baseURL = lark.LarkBaseUrl
	}
	client := lark.NewClient(
		connector.AppID,
		appSecret,
		lark.WithOpenBaseUrl(baseURL),
		lark.WithLogLevel(larkcore.LogLevelWarn),
	)
	channelConfig := larktypes.DefaultChannelConfig()
	eventDispatcher := dispatcher.NewEventDispatcher("", "")
	runtime := &sdkRuntimeChannel{
		client: client,
		config: channelConfig,
		inbound: newManagedWSClient(
			connector.AppID,
			appSecret,
			baseURL,
			eventDispatcher,
		),
	}
	eventDispatcher.OnP2MessageReceiveV1(runtime.receiveMessage)
	return runtime, nil
}

type sdkRuntimeChannel struct {
	client  *lark.Client
	config  larktypes.ChannelConfig
	inbound *managedWSClient

	mu        sync.Mutex
	onMessage func(context.Context, RuntimeMessage) error
	botOpenID string
}

func (c *sdkRuntimeChannel) OnMessage(handler func(context.Context, RuntimeMessage) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onMessage = handler
}

func (c *sdkRuntimeChannel) OnReady(handler func(BotIdentity)) {
	c.inbound.SetOnReady(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		identity, err := c.getBotIdentity(ctx)
		if err != nil {
			handler(BotIdentity{})
			return
		}
		c.mu.Lock()
		c.botOpenID = identity.OpenID
		c.mu.Unlock()
		handler(BotIdentity{
			OpenID: identity.OpenID, Name: identity.Name,
			ActivateStatus: identity.ActivateStatus,
		})
	})
}

func (c *sdkRuntimeChannel) OnError(handler func(error)) {
	c.inbound.SetOnError(handler)
}

func (c *sdkRuntimeChannel) Start(ctx context.Context) error {
	return c.inbound.Start(ctx)
}

func (c *sdkRuntimeChannel) Stop(ctx context.Context) error {
	return c.inbound.Stop(ctx)
}

func (c *sdkRuntimeChannel) receiveMessage(
	ctx context.Context,
	event *larkim.P2MessageReceiveV1,
) error {
	message := normalize.ParseMessage(event)
	if message == nil || safety.IsStale(
		message.CreateTimeMs,
		larktypes.DefaultStaleWindow,
	) {
		return nil
	}
	c.mu.Lock()
	handler := c.onMessage
	botOpenID := c.botOpenID
	c.mu.Unlock()
	if handler == nil || (botOpenID != "" && message.UserID == botOpenID) {
		return nil
	}
	resources := make([]Resource, 0, len(message.Resources))
	for _, resource := range message.Resources {
		resources = append(resources, Resource{
			Type: resource.Type, FileKey: resource.FileKey, Filename: resource.FileName,
		})
	}
	return handler(ctx, RuntimeMessage{
		EventID: message.EventID, MessageID: message.MessageID,
		ChatID: message.ChatID, ChatType: message.ChatType,
		SenderOpenID: message.UserID, Text: message.Content,
		ContentType: message.RawContentType, Resources: resources,
		CreateTimeMS: message.CreateTimeMs,
	})
}

func (c *sdkRuntimeChannel) SendMarkdown(
	ctx context.Context,
	chatID string,
	replyMessageID string,
	text string,
) error {
	if chatID == "" || text == "" {
		return errors.New("Feishu outbound message is empty")
	}
	_, err := c.sendMarkdown(ctx, chatID, replyMessageID, text)
	return err
}

func (c *sdkRuntimeChannel) StreamMarkdown(
	ctx context.Context,
	chatID string,
	replyMessageID string,
	initialText string,
) (MessageStream, error) {
	if chatID == "" || initialText == "" {
		return nil, errors.New("Feishu outbound stream is empty")
	}
	messageID, err := c.sendMarkdown(ctx, chatID, replyMessageID, initialText)
	if err != nil {
		return nil, err
	}
	stream := larkchannel.NewMarkdownStreamController(
		c.client,
		c.config,
		messageID,
		initialText,
		"",
	)
	return &sdkMessageStream{stream: stream}, nil
}

func (c *sdkRuntimeChannel) getBotIdentity(ctx context.Context) (BotIdentity, error) {
	response, err := c.client.Get(
		ctx,
		"/open-apis/bot/v3/info",
		nil,
		larkcore.AccessTokenTypeTenant,
	)
	if err != nil {
		return BotIdentity{}, err
	}
	if response == nil || response.StatusCode != http.StatusOK {
		return BotIdentity{}, errors.New("Feishu bot identity request failed")
	}
	var payload struct {
		Code int `json:"code"`
		Bot  struct {
			OpenID         string `json:"open_id"`
			AppName        string `json:"app_name"`
			ActivateStatus int    `json:"activate_status"`
		} `json:"bot"`
	}
	if err := json.Unmarshal(response.RawBody, &payload); err != nil {
		return BotIdentity{}, fmt.Errorf("decode Feishu bot identity: %w", err)
	}
	if payload.Code != 0 {
		return BotIdentity{}, fmt.Errorf("Feishu bot identity failed: code=%d", payload.Code)
	}
	return BotIdentity{
		OpenID: payload.Bot.OpenID, Name: payload.Bot.AppName,
		ActivateStatus: payload.Bot.ActivateStatus,
	}, nil
}

func (c *sdkRuntimeChannel) sendMarkdown(
	ctx context.Context,
	chatID string,
	replyMessageID string,
	text string,
) (string, error) {
	content, err := normalize.SimpleMarkdownToPost("", text, nil)
	if err != nil {
		return "", fmt.Errorf("format Feishu markdown: %w", err)
	}
	if replyMessageID != "" {
		request := larkim.NewReplyMessageReqBuilder().
			MessageId(replyMessageID).
			Body(larkim.NewReplyMessageReqBodyBuilder().
				MsgType("post").
				Content(content).
				Build()).
			Build()
		result, err := outbound.Retry(
			ctx,
			func(int) (any, error) {
				response, err := c.client.Im.V1.Message.Reply(ctx, request)
				if err != nil {
					return nil, err
				}
				if !response.Success() {
					return nil, &larkcore.CodeError{Code: response.Code, Msg: response.Msg}
				}
				return response, nil
			},
			&outbound.RetryOptions{
				MaxAttempts: c.config.Outbound.Retry.MaxAttempts,
				BaseDelay:   c.config.Outbound.Retry.BaseDelayMs,
			},
		)
		if err != nil {
			return "", err
		}
		response := result.(*larkim.ReplyMessageResp)
		if response.Data == nil || response.Data.MessageId == nil {
			return "", errors.New("Feishu reply response is missing message_id")
		}
		return *response.Data.MessageId, nil
	}
	request := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType("chat_id").
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(chatID).
			MsgType("post").
			Content(content).
			Build()).
		Build()
	result, err := outbound.Retry(
		ctx,
		func(int) (any, error) {
			response, err := c.client.Im.V1.Message.Create(ctx, request)
			if err != nil {
				return nil, err
			}
			if !response.Success() {
				return nil, &larkcore.CodeError{Code: response.Code, Msg: response.Msg}
			}
			return response, nil
		},
		&outbound.RetryOptions{
			MaxAttempts: c.config.Outbound.Retry.MaxAttempts,
			BaseDelay:   c.config.Outbound.Retry.BaseDelayMs,
		},
	)
	if err != nil {
		return "", err
	}
	response := result.(*larkim.CreateMessageResp)
	if response.Data == nil || response.Data.MessageId == nil {
		return "", errors.New("Feishu create response is missing message_id")
	}
	return *response.Data.MessageId, nil
}

type sdkMessageStream struct{ stream larktypes.StreamController }

func (s *sdkMessageStream) Append(ctx context.Context, text string) error {
	return s.stream.Append(ctx, text)
}

func (s *sdkMessageStream) Close(ctx context.Context) error {
	return s.stream.Close(ctx)
}
