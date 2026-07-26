package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cocola-project/cocola/apps/gateway/internal/chatrun"
	"github.com/cocola-project/cocola/apps/gateway/internal/skillbroker"
	"github.com/cocola-project/cocola/packages/go-common/logger"
	"github.com/cocola-project/cocola/packages/go-common/token"
)

func TestSkillBrokerImportsAsRunUserWithShortLivedToken(t *testing.T) {
	const secret = "skill-broker-test-secret"
	admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/me/skills/import/archive" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		claims, err := token.Decode(r.Header.Get("Authorization")[len("Bearer "):], secret, 0)
		if err != nil || claims.Subject != "user-1" || claims.Tenant != "tenant-1" {
			t.Fatalf("runtime claims = %#v, %v", claims, err)
		}
		if claims.Expires-claims.IssuedAt != int64((5 * time.Minute).Seconds()) {
			t.Fatalf("runtime token TTL = %d", claims.Expires-claims.IssuedAt)
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"skills":[{"id":"user:demo"}]}`))
	}))
	defer admin.Close()

	broker, err := skillbroker.New(secret)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := broker.Issue("tenant-1", "user-1", "conversation-1", "run-1")
	if err != nil {
		t.Fatal(err)
	}
	api := &API{
		log: logger.Must(),
		runs: &runController{
			store: &brokerRunStore{run: chatrun.Run{
				ID: "run-1", UserID: "user-1", ConversationID: "conversation-1",
				Status: chatrun.StatusRunning,
			}},
			live: map[string]*liveRun{},
		},
		sandboxTokenIssuer: token.NewIssuer(secret, "cocola", time.Hour),
	}
	api.WithSkillBroker(broker, admin.URL, admin.Client())

	request := skillBrokerRequest(t, "/internal/skills/import", credential, []byte("normalized-zip"))
	response := httptest.NewRecorder()
	api.importRunSkill(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
}

func TestSkillBrokerRejectsEndedRun(t *testing.T) {
	broker, _ := skillbroker.New("secret")
	credential, _ := broker.Issue("tenant-1", "user-1", "conversation-1", "run-1")
	api := &API{
		log: logger.Must(),
		runs: &runController{
			store: &brokerRunStore{run: chatrun.Run{
				ID: "run-1", UserID: "user-1", ConversationID: "conversation-1",
				Status: chatrun.StatusSuccess,
			}},
			live: map[string]*liveRun{},
		},
		sandboxTokenIssuer: token.NewIssuer("secret", "cocola", time.Hour),
	}
	api.WithSkillBroker(broker, "http://admin.invalid", http.DefaultClient)

	response := httptest.NewRecorder()
	api.scanRunSkill(
		response,
		skillBrokerRequest(t, "/internal/skills/scan", credential, []byte("zip")),
	)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func skillBrokerRequest(
	t *testing.T, path, credential string, archive []byte,
) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "skill.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(archive); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, path, &body).WithContext(context.Background())
	request.Header.Set("Authorization", "Bearer "+credential)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}
