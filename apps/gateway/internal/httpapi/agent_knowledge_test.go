package httpapi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/cocola-project/cocola/apps/gateway/internal/agentprofile"
	feishuconnector "github.com/cocola-project/cocola/apps/gateway/internal/channel/feishu"
	"github.com/cocola-project/cocola/apps/gateway/internal/wiki"
)

type knowledgeRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn knowledgeRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestFeishuKnowledgeEndpointUsesOnlyFixedOpenPlatformHosts(t *testing.T) {
	source, ok := agentprofile.NormalizeKnowledgeSource(agentprofile.KnowledgeSource{
		Type: agentprofile.KnowledgeTypeFeishuDoc,
		URL:  "https://tenant.feishu.cn/docx/Abc_123?redirect=https://attacker.invalid",
	})
	if !ok {
		t.Fatal("source did not normalize")
	}
	endpoint, ok := feishuKnowledgeEndpoint(feishuconnector.DomainFeishu, source)
	if !ok {
		t.Fatal("endpoint was rejected")
	}
	if endpoint != "https://open.feishu.cn/open-apis/docx/v1/documents/Abc_123/raw_content" {
		t.Fatalf("endpoint = %q", endpoint)
	}
	if strings.Contains(endpoint, "tenant.feishu.cn") ||
		strings.Contains(endpoint, "attacker.invalid") {
		t.Fatalf("user-controlled host leaked into endpoint: %s", endpoint)
	}
}

func TestFeishuKnowledgeStatusMapping(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{name: "ready", status: 200, body: `{"code":0,"data":{}}`, want: knowledgeReady},
		{name: "permission http", status: 403, body: `{}`, want: knowledgePermissionRequired},
		{name: "not found code", status: 200, body: `{"code":1770002,"msg":"missing"}`, want: knowledgeNotFound},
		{name: "wiki permission", status: 200, body: `{"code":131006,"msg":"permission denied"}`, want: knowledgePermissionRequired},
		{name: "sheet missing", status: 200, body: `{"code":1310214,"msg":"spreadsheet not found"}`, want: knowledgeNotFound},
		{name: "base missing", status: 200, body: `{"code":1254040,"msg":"base token not found"}`, want: knowledgeNotFound},
		{name: "invalid parameter", status: 200, body: `{"code":1770001,"msg":"invalid param"}`, want: knowledgeTemporarilyUnavailable},
		{name: "temporary", status: 503, body: `{}`, want: knowledgeTemporarilyUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			api := &API{agentKnowledgeHTTPClient: &http.Client{
				Transport: knowledgeRoundTripFunc(func(request *http.Request) (*http.Response, error) {
					if request.URL.Host != "open.feishu.cn" {
						t.Fatalf("unexpected host %q", request.URL.Host)
					}
					if request.Header.Get("Authorization") != "Bearer test-token" {
						t.Fatal("missing tenant token")
					}
					return &http.Response{
						StatusCode: test.status,
						Body:       io.NopCloser(strings.NewReader(test.body)),
						Header:     make(http.Header),
					}, nil
				}),
			}}
			got := api.checkFeishuKnowledgeSource(
				context.Background(),
				feishuconnector.RuntimeCredential{
					Brand: feishuconnector.DomainFeishu, TenantAccessToken: "test-token",
				},
				agentprofile.KnowledgeSource{
					Type: agentprofile.KnowledgeTypeFeishuDoc,
					URL:  "https://tenant.feishu.cn/docx/Abc_123",
				},
			)
			if got != test.want {
				t.Fatalf("status = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCocolaWikiKnowledgeStatusMapping(t *testing.T) {
	nodeID := "8eea8a2b-9491-49b7-84c5-a37d1d0ede90"
	source := agentprofile.KnowledgeSource{
		Type:  agentprofile.KnowledgeTypeCocolaWiki,
		Label: "Handbook", NodeID: nodeID,
	}
	store := &wikiStoreStub{
		currentNode: wiki.Node{ID: nodeID, Kind: "file", Name: "handbook.md"},
		current:     wiki.Version{ID: "version", NodeID: nodeID},
	}
	api := &API{wiki: store, store: &cleanupObjectStore{}}
	id := agentprofile.Identity{TenantID: "tenant", UserID: "user"}

	if got := api.checkCocolaWikiKnowledge(context.Background(), id, source); got != knowledgeReady {
		t.Fatalf("ready status = %q", got)
	}
	if store.currentID != (wiki.Identity{TenantID: "tenant", UserID: "user"}) ||
		store.currentNodeID != nodeID {
		t.Fatalf("Wiki identity = %+v, node = %q", store.currentID, store.currentNodeID)
	}
	store.currentErr = wiki.ErrNotFound
	if got := api.checkCocolaWikiKnowledge(context.Background(), id, source); got != knowledgeNotFound {
		t.Fatalf("not found status = %q", got)
	}
	store.currentErr = context.DeadlineExceeded
	if got := api.checkCocolaWikiKnowledge(context.Background(), id, source); got != knowledgeTemporarilyUnavailable {
		t.Fatalf("temporary status = %q", got)
	}
}
