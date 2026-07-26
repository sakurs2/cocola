package feishu

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

func TestManagedWSReconnectsAndStops(t *testing.T) {
	originalDelays := managedWSRetryDelays
	managedWSRetryDelays = []time.Duration{time.Millisecond}
	defer func() { managedWSRetryDelays = originalDelays }()

	var connections atomic.Int32
	var server *httptest.Server
	upgrader := websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case larkws.GenEndpointUri:
			endpoint := "ws" + strings.TrimPrefix(server.URL, "http") +
				"/ws?service_id=1"
			_ = json.NewEncoder(w).Encode(larkws.EndpointResp{
				Code: larkws.OK,
				Data: &larkws.Endpoint{Url: endpoint},
			})
		case "/ws":
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			if connections.Add(1) == 1 {
				_ = conn.Close()
				return
			}
			defer conn.Close()
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newManagedWSClient(
		"app-id",
		"app-secret",
		server.URL,
		dispatcher.NewEventDispatcher("", ""),
	)
	ready := make(chan struct{}, 2)
	client.SetOnReady(func() { ready <- struct{}{} })
	client.SetOnError(func(error) {})
	started := make(chan error, 1)
	go func() { started <- client.Start(context.Background()) }()

	for index := 0; index < 2; index++ {
		select {
		case <-ready:
		case <-time.After(2 * time.Second):
			t.Fatalf("ready callback %d was not delivered", index+1)
		}
	}
	if err := client.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	select {
	case err := <-started:
		if err == nil {
			t.Fatal("Start returned nil after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("Start did not return after Stop")
	}
}

func TestCombineWSFragmentsBoundsIncompleteMessages(t *testing.T) {
	fragments := make(map[string][][]byte)
	for index := 0; index < managedWSMaxFragmentSets; index++ {
		combined, err := combineWSFragments(
			fragments,
			string(rune(index+1)),
			2,
			0,
			[]byte("partial"),
		)
		if err != nil || combined != nil {
			t.Fatalf("fragment %d = %q, %v", index, combined, err)
		}
	}
	if _, err := combineWSFragments(
		fragments,
		"overflow",
		2,
		0,
		[]byte("partial"),
	); err == nil {
		t.Fatal("incomplete fragment set limit was not enforced")
	}
}
