package agent

import (
	"context"
	"net"
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
		WikiReferences: []WikiReference{{
			NodeID: "node", VersionID: "version", LogicalPath: "Team/brief.docx",
			Filename: "brief.docx", Mime: "application/docx",
			ObjectKey: "wiki/node/version", Size: 123, SHA256: "abc",
		}},
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
	if got := (<-recording.metadata).Get("x-cocola-skill-broker-credential"); len(got) != 1 ||
		got[0] != "skill-run-credential" {
		t.Fatalf("Skill broker metadata = %#v", got)
	}
}
