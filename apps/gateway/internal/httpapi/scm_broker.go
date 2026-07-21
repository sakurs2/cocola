package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cocola-project/cocola/apps/gateway/internal/agent"
	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	"github.com/cocola-project/cocola/apps/gateway/internal/chatrun"
	"github.com/cocola-project/cocola/apps/gateway/internal/project"
)

func brokerBearer(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(value) < 8 || !strings.EqualFold(value[:7], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(value[7:])
}

func (a *API) brokerRunActive(ctx context.Context, claims project.BrokerCredentialClaims) bool {
	if a.runs == nil || a.runs.store == nil {
		return false
	}
	run, err := a.runs.store.GetOwned(ctx, claims.RunID, claims.UserID)
	return err == nil && run.Status == chatrun.StatusRunning &&
		run.ConversationID == claims.ConversationID
}

func (a *API) createGitHubTokenLease(w http.ResponseWriter, r *http.Request) {
	if a.projects == nil {
		writeErr(w, http.StatusServiceUnavailable, "SCM_BROKER_UNAVAILABLE", "GitHub broker is unavailable")
		return
	}
	credential := brokerBearer(r)
	claims, err := a.projects.VerifyBrokerCredential(credential)
	if err != nil || !a.brokerRunActive(r.Context(), claims) {
		writeErr(w, http.StatusUnauthorized, "BROKER_CREDENTIAL_INVALID", "Project run credential is invalid or expired")
		return
	}
	var input project.BrokerCommand
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid broker request")
		return
	}
	result, err := a.projects.AcquireTokenLease(r.Context(), credential, input)
	if errors.Is(err, project.ErrApprovalRequired) {
		a.publishSCMApproval(
			claims.RunID, result.ApprovalID, "pending", result.Category, result.Label,
		)
		writeJSON(w, http.StatusAccepted, result)
		return
	}
	if errors.Is(err, project.ErrApprovalDenied) {
		if result.Status == "expired" {
			a.publishSCMApproval(
				claims.RunID, result.ApprovalID, "expired", result.Category, result.Label,
			)
		}
		writeJSON(w, http.StatusForbidden, result)
		return
	}
	if err != nil {
		a.writeProjectError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) revokeGitHubTokenLease(w http.ResponseWriter, r *http.Request) {
	if a.projects == nil {
		writeErr(w, http.StatusServiceUnavailable, "SCM_BROKER_UNAVAILABLE", "GitHub broker is unavailable")
		return
	}
	credential := brokerBearer(r)
	claims, err := a.projects.VerifyBrokerCredential(credential)
	if err != nil || !a.brokerRunActive(r.Context(), claims) {
		writeErr(w, http.StatusUnauthorized, "BROKER_CREDENTIAL_INVALID", "Project run credential is invalid or expired")
		return
	}
	var completion project.LeaseCompletion
	if r.Body != nil {
		err = json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&completion)
		if err != nil && !errors.Is(err, io.EOF) {
			writeErr(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid lease completion")
			return
		}
	}
	if err := a.projects.RevokeTokenLease(
		r.Context(), credential, r.PathValue("id"), completion,
	); err != nil {
		a.writeProjectError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) waitSCMApproval(w http.ResponseWriter, r *http.Request) {
	if a.projects == nil {
		writeErr(w, http.StatusServiceUnavailable, "SCM_BROKER_UNAVAILABLE", "GitHub broker is unavailable")
		return
	}
	credential, approvalID := brokerBearer(r), r.PathValue("id")
	claims, err := a.projects.VerifyBrokerCredential(credential)
	if err != nil || !a.brokerRunActive(r.Context(), claims) {
		writeErr(w, http.StatusUnauthorized, "BROKER_CREDENTIAL_INVALID", "Project run credential is invalid or expired")
		return
	}
	value, err := a.projects.ApprovalStatus(r.Context(), credential, approvalID)
	if err != nil {
		a.writeProjectError(w, err)
		return
	}
	if value.Status == "pending" && value.ExpiresAt.After(time.Now().UTC()) {
		ready, cancel := a.subscribeBrokerApproval(approvalID)
		defer cancel()
		// Recheck after subscription closes the create/subscribe race.
		value, err = a.projects.ApprovalStatus(r.Context(), credential, approvalID)
		if err == nil && value.Status == "pending" {
			timer := time.NewTimer(25 * time.Second)
			defer timer.Stop()
			select {
			case <-r.Context().Done():
				return
			case <-ready:
			case <-timer.C:
			}
			value, err = a.projects.ApprovalStatus(r.Context(), credential, approvalID)
		}
	}
	if err != nil {
		a.writeProjectError(w, err)
		return
	}
	status := value.Status
	if status == "expired" {
		a.publishSCMApproval(
			claims.RunID, value.ID, status, value.CommandCategory, value.CommandLabel,
		)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"approval_id": value.ID, "status": status, "expires_at": value.ExpiresAt,
	})
}

func (a *API) decideSCMApproval(w http.ResponseWriter, r *http.Request) {
	if a.projects == nil {
		writeErr(w, http.StatusServiceUnavailable, "SCM_BROKER_UNAVAILABLE", "GitHub broker is unavailable")
		return
	}
	identity, ok := auth.IdentityOf(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	var input struct {
		Decision string `json:"decision"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&input) != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid approval decision")
		return
	}
	value, err := a.projects.DecideApproval(r.Context(), project.Identity{
		TenantID: identity.TenantID, UserID: identity.UserID,
	}, r.PathValue("id"), strings.TrimSpace(input.Decision))
	if err != nil {
		a.writeProjectError(w, err)
		return
	}
	a.notifyBrokerApproval(value.ID)
	a.publishSCMApproval(
		value.RunID, value.ID, value.Status, value.CommandCategory, value.CommandLabel,
	)
	writeJSON(w, http.StatusOK, map[string]any{
		"approval_id": value.ID, "status": value.Status,
	})
}

func (a *API) publishSCMApproval(runID, approvalID, status, category, label string) {
	if a.runs == nil {
		return
	}
	live := a.runs.getLive(runID)
	if live == nil {
		return
	}
	event := agent.Event{Kind: "scm_approval", Data: map[string]string{
		"id": approvalID, "status": status, "category": category, "label": label,
	}}
	live.apply(event)
	live.publish(event)
}

func (a *API) subscribeBrokerApproval(id string) (<-chan struct{}, func()) {
	channel := make(chan struct{})
	a.brokerWaitMu.Lock()
	if a.brokerWaiters == nil {
		a.brokerWaiters = make(map[string]map[chan struct{}]struct{})
	}
	if a.brokerWaiters[id] == nil {
		a.brokerWaiters[id] = make(map[chan struct{}]struct{})
	}
	a.brokerWaiters[id][channel] = struct{}{}
	a.brokerWaitMu.Unlock()
	return channel, func() {
		a.brokerWaitMu.Lock()
		if waiters := a.brokerWaiters[id]; waiters != nil {
			delete(waiters, channel)
			if len(waiters) == 0 {
				delete(a.brokerWaiters, id)
			}
		}
		a.brokerWaitMu.Unlock()
	}
}

func (a *API) notifyBrokerApproval(id string) {
	a.brokerWaitMu.Lock()
	waiters := a.brokerWaiters[id]
	delete(a.brokerWaiters, id)
	for channel := range waiters {
		close(channel)
	}
	a.brokerWaitMu.Unlock()
}
