package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/cocola-project/cocola/apps/gateway/internal/chatrun"
	"github.com/cocola-project/cocola/apps/gateway/internal/skillbroker"
	traceevents "github.com/cocola-project/cocola/apps/gateway/internal/traceevent"
)

const (
	skillArchiveMaxBytes = 64 << 20
	skillRuntimeTokenTTL = 5 * time.Minute
)

func (a *API) skillBrokerRunActive(ctx context.Context, claims skillbroker.Claims) bool {
	if a.runs == nil || a.runs.store == nil {
		return false
	}
	run, err := a.runs.store.GetOwned(ctx, claims.RunID, claims.UserID)
	return err == nil && run.Status == chatrun.StatusRunning &&
		run.ConversationID == claims.ConversationID
}

func (a *API) scanRunSkill(w http.ResponseWriter, r *http.Request) {
	a.proxyRunSkill(w, r, "scan")
}

func (a *API) importRunSkill(w http.ResponseWriter, r *http.Request) {
	a.proxyRunSkill(w, r, "import")
}

func (a *API) proxyRunSkill(w http.ResponseWriter, r *http.Request, action string) {
	started := time.Now()
	if a.skillBroker == nil || a.skillHTTPClient == nil || a.skillAdminURL == "" ||
		a.sandboxTokenIssuer == nil {
		writeErr(w, http.StatusServiceUnavailable, "SKILL_BROKER_UNAVAILABLE", "Skill broker is unavailable")
		return
	}
	claims, err := a.skillBroker.Verify(brokerBearer(r))
	if err != nil || !a.skillBrokerRunActive(r.Context(), claims) {
		writeErr(w, http.StatusUnauthorized, "BROKER_CREDENTIAL_INVALID", "Skill run credential is invalid or expired")
		return
	}
	archive, err := readRunSkillArchive(w, r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "SKILL_INVALID", err.Error())
		return
	}
	runtimeToken, _, err := a.sandboxTokenIssuer.Issue(
		claims.UserID, claims.TenantID, skillRuntimeTokenTTL, 0,
	)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "SKILL_BROKER_UNAVAILABLE", "Skill broker could not authorize the request")
		return
	}
	responseStatus, responseBody, err := a.forwardRunSkill(
		r.Context(), action, runtimeToken, archive,
	)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "SKILL_BROKER_UNAVAILABLE", "Skill broker could not reach the catalog")
		return
	}
	skillID := skillIDFromAdminResponse(responseBody)
	a.log.Info(fmt.Sprintf(
		"skill broker request completed action=%s user_id=%s skill_id=%s bytes=%d duration=%s",
		action, claims.UserID, skillID, len(archive), time.Since(started),
	))
	a.upsertTraceSpan(r.Context(), traceevents.Span{
		TraceID: claims.RunID, SpanID: traceevents.NewSpanID(),
		SchemaVersion: 1, Service: "gateway", Name: "skill." + action,
		Category: "skill", StartedAt: started.UTC(),
		DurationUS: time.Since(started).Microseconds(), Status: responseTraceStatus(responseStatus),
		Attributes: map[string]any{
			"action": action, "skill_count": boolInt(skillID != ""), "content_length": len(archive),
		},
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(responseStatus)
	_, _ = w.Write(responseBody)
}

func readRunSkillArchive(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, skillArchiveMaxBytes+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		return nil, err
	}
	defer r.MultipartForm.RemoveAll()
	if len(r.MultipartForm.Value) != 0 || len(r.MultipartForm.File) != 1 ||
		len(r.MultipartForm.File["file"]) != 1 {
		return nil, io.ErrUnexpectedEOF
	}
	file, err := r.MultipartForm.File["file"][0].Open()
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, skillArchiveMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > skillArchiveMaxBytes {
		return nil, io.ErrUnexpectedEOF
	}
	return data, nil
}

func (a *API) forwardRunSkill(
	ctx context.Context, action, runtimeToken string, archive []byte,
) (int, []byte, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "skill.zip")
	if err != nil {
		return 0, nil, err
	}
	if _, err = part.Write(archive); err != nil {
		return 0, nil, err
	}
	if err = writer.Close(); err != nil {
		return 0, nil, err
	}
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, a.skillAdminURL+"/me/skills/"+action+"/archive", &body,
	)
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Authorization", "Bearer "+runtimeToken)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Accept", "application/json")
	response, err := a.skillHTTPClient.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return 0, nil, err
	}
	if len(responseBody) == 0 {
		responseBody = []byte(`{"error":{"code":"SKILL_PUBLISH_FAILED","message":"empty catalog response"}}`)
	}
	return response.StatusCode, responseBody, nil
}

func skillIDFromAdminResponse(data []byte) string {
	var payload struct {
		Skills []struct {
			ID string `json:"id"`
		} `json:"skills"`
	}
	if json.Unmarshal(data, &payload) == nil && len(payload.Skills) == 1 {
		return strings.TrimSpace(payload.Skills[0].ID)
	}
	return ""
}

func responseTraceStatus(status int) string {
	if status >= 200 && status < 300 {
		return "success"
	}
	return "error"
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
