package agent

import (
	"context"
	"net"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	agentv1 "github.com/cocola-project/cocola/packages/proto/gen/go/cocola/agent/v1"
)

type recordingAgentServer struct {
	agentv1.UnimplementedAgentRuntimeServiceServer
	requests chan *agentv1.QueryRequest
	metadata chan metadata.MD
}

func (s *recordingAgentServer) Query(
	request *agentv1.QueryRequest,
	stream agentv1.AgentRuntimeService_QueryServer,
) error {
	s.requests <- request
	if s.metadata != nil {
		value, _ := metadata.FromIncomingContext(stream.Context())
		s.metadata <- value
	}
	return stream.Send(&agentv1.AgentEvent{Kind: "done"})
}

func TestClientStreamMapsWikiReferences(t *testing.T) {
	t.Parallel()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	recording := &recordingAgentServer{
		requests: make(chan *agentv1.QueryRequest, 1),
		metadata: make(chan metadata.MD, 1),
	}
	agentv1.RegisterAgentRuntimeServiceServer(server, recording)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	connection, err := grpc.DialContext(
		context.Background(),
		"bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	client := NewClient(connection)

	err = client.Stream(context.Background(), Query{
		UserID: "user", SessionID: "session", Prompt: "read it",
		SkillBrokerCredential: "skill-run-credential",
		LarkCredential: LarkRuntimeCredential{
			Status: RuntimeCredentialReadyForTest, AppID: "app-id", Brand: "feishu",
			TenantAccessToken: "tenant-token",
		},
		WikiReferences: []WikiReference{{
			NodeID: "node", VersionID: "version", LogicalPath: "Team/brief.docx",
			Filename: "brief.docx", Mime: "application/docx",
			ObjectKey: "wiki/node/version", Size: 123, SHA256: "abc",
		}},
		Agent: &AgentContext{
			ID: "agent-1", Version: 3, Name: "Research",
			Instructions:    "Cite primary sources.",
			SkillCatalogIDs: []string{"catalog-search"},
			KnowledgeSources: []AgentKnowledgeSource{{
				Type: "feishu_doc", Label: "Plan",
				URL: "https://docs.feishu.cn/docx/Abc_123",
			}, {
				Type: "cocola_wiki", Label: "Handbook",
				NodeID: "8eea8a2b-9491-49b7-84c5-a37d1d0ede90",
			}},
		},
	}, func(Event) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	request := <-recording.requests
	if len(request.WikiReferences) != 1 {
		t.Fatalf("request = %#v", request)
	}
	got := request.WikiReferences[0]
	if got.NodeId != "node" || got.VersionId != "version" ||
		got.LogicalPath != "Team/brief.docx" ||
		got.OssKey != "wiki/node/version" ||
		got.Size != 123 || got.Sha256 != "abc" {
		t.Fatalf("WikiReference = %#v", got)
	}
	if request.AgentContext == nil ||
		request.AgentContext.Id != "agent-1" ||
		request.AgentContext.Version != 3 ||
		request.AgentContext.Name != "Research" ||
		request.AgentContext.Instructions != "Cite primary sources." ||
		len(request.AgentContext.SkillCatalogIds) != 1 ||
		request.AgentContext.SkillCatalogIds[0] != "catalog-search" ||
		len(request.AgentContext.KnowledgeSources) != 2 ||
		request.AgentContext.KnowledgeSources[0].Url !=
			"https://docs.feishu.cn/docx/Abc_123" ||
		request.AgentContext.KnowledgeSources[1].NodeId !=
			"8eea8a2b-9491-49b7-84c5-a37d1d0ede90" {
		t.Fatalf("AgentContext = %#v", request.AgentContext)
	}
	incomingMetadata := <-recording.metadata
	if got := incomingMetadata.Get("x-cocola-skill-broker-credential"); len(got) != 1 ||
		got[0] != "skill-run-credential" {
		t.Fatalf("Skill broker metadata = %#v", got)
	}
	for key, want := range map[string]string{
		"x-cocola-lark-status":              RuntimeCredentialReadyForTest,
		"x-cocola-lark-app-id":              "app-id",
		"x-cocola-lark-brand":               "feishu",
		"x-cocola-lark-tenant-access-token": "tenant-token",
	} {
		if got := incomingMetadata.Get(key); len(got) != 1 || got[0] != want {
			t.Fatalf("%s metadata = %#v", key, got)
		}
	}
	if strings.Contains(request.String(), "tenant-token") {
		t.Fatal("tenant access token leaked into QueryRequest protobuf")
	}
}

const RuntimeCredentialReadyForTest = "ready"
