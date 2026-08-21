package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/cocola-project/cocola/apps/gateway/internal/agentprofile"
	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	feishuconnector "github.com/cocola-project/cocola/apps/gateway/internal/channel/feishu"
)

const maxFeishuConnectorBody = int64(8 << 10)

type feishuManualRequest struct {
	Domain    string `json:"domain"`
	AppID     string `json:"app_id"`
	AppSecret string `json:"app_secret"`
}

func feishuIdentity(r *http.Request) (feishuconnector.Identity, bool) {
	id, ok := auth.IdentityOf(r)
	return feishuconnector.Identity{
		TenantID: id.TenantID, UserID: id.UserID, Email: id.Email,
		Name: id.Name, Username: id.Username,
	}, ok
}

func (a *API) feishuWorkspaceRequest(
	w http.ResponseWriter,
	r *http.Request,
	requireService bool,
) (feishuconnector.Identity, bool) {
	id, ok := feishuIdentity(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return feishuconnector.Identity{}, false
	}
	if requireService && a.feishu == nil {
		writeErr(w, http.StatusServiceUnavailable, "FEISHU_UNAVAILABLE", "Feishu connector is unavailable")
		return feishuconnector.Identity{}, false
	}
	return id, true
}

func (a *API) feishuWorkspaceConnection(w http.ResponseWriter, r *http.Request) {
	id, ok := a.feishuWorkspaceRequest(w, r, false)
	if !ok {
		return
	}
	if a.feishu == nil {
		writeJSON(w, http.StatusOK, feishuconnector.ConnectorView{
			Status: "disabled", Enabled: false,
		})
		return
	}
	view, err := a.feishu.Connection(r.Context(), id, "")
	if a.writeFeishuError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (a *API) feishuWorkspaceRegistrationStart(w http.ResponseWriter, r *http.Request) {
	id, ok := a.feishuWorkspaceRequest(w, r, true)
	if !ok || !decodeOptionalEmptyObject(w, r) {
		return
	}
	flow, err := a.feishu.StartWorkspaceRegistration(r.Context(), id)
	if a.writeFeishuError(w, err) {
		return
	}
	writeJSON(w, http.StatusAccepted, flow)
}

func (a *API) feishuWorkspaceRegistration(w http.ResponseWriter, r *http.Request) {
	id, ok := a.feishuWorkspaceRequest(w, r, true)
	if !ok {
		return
	}
	flowID := strings.TrimSpace(r.PathValue("flow_id"))
	if _, err := uuid.Parse(flowID); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "registration id must be a UUID")
		return
	}
	flow, err := a.feishu.Registration(r.Context(), id, "", flowID)
	if a.writeFeishuError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, flow)
}

func (a *API) feishuWorkspaceRegistrationCancel(w http.ResponseWriter, r *http.Request) {
	id, ok := a.feishuWorkspaceRequest(w, r, true)
	if !ok {
		return
	}
	flowID := strings.TrimSpace(r.PathValue("flow_id"))
	if _, err := uuid.Parse(flowID); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "registration id must be a UUID")
		return
	}
	if err := a.feishu.CancelRegistration(r.Context(), id, "", flowID); a.writeFeishuError(w, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) feishuWorkspaceManual(w http.ResponseWriter, r *http.Request) {
	id, ok := a.feishuWorkspaceRequest(w, r, true)
	if !ok {
		return
	}
	var input feishuManualRequest
	if !decodeFeishuJSON(w, r, &input) {
		return
	}
	view, err := a.feishu.ConfigureWorkspaceManual(
		r.Context(), id, input.Domain, input.AppID, input.AppSecret,
	)
	if a.writeFeishuError(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func (a *API) feishuWorkspaceEnable(w http.ResponseWriter, r *http.Request) {
	a.feishuWorkspaceToggle(w, r, true)
}

func (a *API) feishuWorkspaceDisable(w http.ResponseWriter, r *http.Request) {
	a.feishuWorkspaceToggle(w, r, false)
}

func (a *API) feishuWorkspaceToggle(w http.ResponseWriter, r *http.Request, enabled bool) {
	id, ok := a.feishuWorkspaceRequest(w, r, true)
	if !ok || !decodeOptionalEmptyObject(w, r) {
		return
	}
	var (
		view feishuconnector.ConnectorView
		err  error
	)
	if enabled {
		view, err = a.feishu.EnableWorkspace(r.Context(), id)
	} else {
		view, err = a.feishu.Disable(r.Context(), id, "")
	}
	if a.writeFeishuError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (a *API) feishuWorkspaceDisconnect(w http.ResponseWriter, r *http.Request) {
	id, ok := a.feishuWorkspaceRequest(w, r, true)
	if !ok || !decodeOptionalEmptyObject(w, r) {
		return
	}
	if err := a.feishu.Disconnect(r.Context(), id, ""); a.writeFeishuError(w, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) feishuAgentRequest(
	w http.ResponseWriter,
	r *http.Request,
	requireService bool,
) (feishuconnector.Identity, agentprofile.Agent, bool) {
	id, ok := feishuIdentity(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return feishuconnector.Identity{}, agentprofile.Agent{}, false
	}
	if a.agents == nil {
		writeErr(w, http.StatusServiceUnavailable, "AGENTS_UNAVAILABLE", "Agent service unavailable")
		return feishuconnector.Identity{}, agentprofile.Agent{}, false
	}
	agent, err := a.agents.GetActive(r.Context(), agentprofile.Identity{
		TenantID: id.TenantID, UserID: id.UserID,
	}, strings.TrimSpace(r.PathValue("id")))
	if a.writeAgentError(w, err) {
		return feishuconnector.Identity{}, agentprofile.Agent{}, false
	}
	if requireService && a.feishu == nil {
		writeErr(w, http.StatusServiceUnavailable, "FEISHU_UNAVAILABLE", "Feishu connector is unavailable")
		return feishuconnector.Identity{}, agentprofile.Agent{}, false
	}
	return id, agent, true
}

func (a *API) feishuConnection(w http.ResponseWriter, r *http.Request) {
	id, agent, ok := a.feishuAgentRequest(w, r, false)
	if !ok {
		return
	}
	if a.feishu == nil {
		writeJSON(w, http.StatusOK, feishuconnector.ConnectorView{
			AgentID: agent.ID, Status: "disabled", Enabled: false,
		})
		return
	}
	view, err := a.feishu.Connection(r.Context(), id, agent.ID)
	if a.writeFeishuError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (a *API) feishuRegistrationStart(w http.ResponseWriter, r *http.Request) {
	id, agent, ok := a.feishuAgentRequest(w, r, true)
	if !ok {
		return
	}
	if !decodeOptionalEmptyObject(w, r) {
		return
	}
	flow, err := a.feishu.StartRegistration(r.Context(), id, feishuconnector.AgentRegistration{
		ID: agent.ID, Name: agent.Name, Description: agent.Description,
	})
	if a.writeFeishuError(w, err) {
		return
	}
	writeJSON(w, http.StatusAccepted, flow)
}

func (a *API) feishuRegistration(w http.ResponseWriter, r *http.Request) {
	id, agent, ok := a.feishuAgentRequest(w, r, true)
	if !ok {
		return
	}
	flowID := strings.TrimSpace(r.PathValue("flow_id"))
	if _, err := uuid.Parse(flowID); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "registration id must be a UUID")
		return
	}
	flow, err := a.feishu.Registration(r.Context(), id, agent.ID, flowID)
	if a.writeFeishuError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, flow)
}

func (a *API) feishuRegistrationCancel(w http.ResponseWriter, r *http.Request) {
	id, agent, ok := a.feishuAgentRequest(w, r, true)
	if !ok {
		return
	}
	flowID := strings.TrimSpace(r.PathValue("flow_id"))
	if _, err := uuid.Parse(flowID); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "registration id must be a UUID")
		return
	}
	if err := a.feishu.CancelRegistration(r.Context(), id, agent.ID, flowID); a.writeFeishuError(w, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) feishuManual(w http.ResponseWriter, r *http.Request) {
	id, agent, ok := a.feishuAgentRequest(w, r, true)
	if !ok {
		return
	}
	var input feishuManualRequest
	if !decodeFeishuJSON(w, r, &input) {
		return
	}
	result, err := a.feishu.ConfigureManual(
		r.Context(),
		id,
		agent.ID,
		input.Domain,
		input.AppID,
		input.AppSecret,
	)
	if a.writeFeishuError(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (a *API) feishuEnable(w http.ResponseWriter, r *http.Request) {
	a.feishuToggle(w, r, true)
}

func (a *API) feishuDisable(w http.ResponseWriter, r *http.Request) {
	a.feishuToggle(w, r, false)
}

func (a *API) feishuToggle(w http.ResponseWriter, r *http.Request, enabled bool) {
	id, agent, ok := a.feishuAgentRequest(w, r, true)
	if !ok {
		return
	}
	if !decodeOptionalEmptyObject(w, r) {
		return
	}
	var (
		view feishuconnector.ConnectorView
		err  error
	)
	if enabled {
		view, err = a.feishu.Enable(r.Context(), id, agent.ID)
	} else {
		view, err = a.feishu.Disable(r.Context(), id, agent.ID)
	}
	if a.writeFeishuError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (a *API) feishuDisconnect(w http.ResponseWriter, r *http.Request) {
	id, agent, ok := a.feishuAgentRequest(w, r, true)
	if !ok {
		return
	}
	if !decodeOptionalEmptyObject(w, r) {
		return
	}
	if err := a.feishu.Disconnect(r.Context(), id, agent.ID); a.writeFeishuError(w, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeOptionalEmptyObject(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxFeishuConnectorBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input struct{}
	err := decoder.Decode(&input)
	if errors.Is(err, io.EOF) {
		return true
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "malformed JSON body")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "malformed JSON body")
		return false
	}
	return true
}

func decodeFeishuJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxFeishuConnectorBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeErr(w, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "request body is too large")
			return false
		}
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "malformed JSON body")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "malformed JSON body")
		return false
	}
	return true
}

func (a *API) writeFeishuError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	var registrationErr *feishuconnector.RegistrationError
	switch {
	case errors.As(err, &registrationErr) && registrationErr.Code == "credentials_invalid":
		writeErr(w, http.StatusBadRequest, "FEISHU_CREDENTIALS_INVALID", "Feishu application credentials were rejected")
	case errors.Is(err, feishuconnector.ErrInvalid):
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid Feishu connector request")
	case errors.Is(err, feishuconnector.ErrNotFound):
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "Feishu connector or registration was not found")
	case errors.Is(err, feishuconnector.ErrAppConflict):
		writeErr(w, http.StatusConflict, "FEISHU_APP_IN_USE", "this Feishu application is already connected to another Cocola connector")
	case errors.Is(err, feishuconnector.ErrConflict):
		writeErr(w, http.StatusConflict, "CONFLICT", "a Feishu connection or registration is already active")
	case errors.Is(err, feishuconnector.ErrFlowTerminated):
		writeErr(w, http.StatusConflict, "REGISTRATION_FINISHED", "registration is no longer active")
	case errors.Is(err, feishuconnector.ErrAgentArchived):
		writeErr(w, http.StatusConflict, "AGENT_ARCHIVED", "Agent is archived")
	default:
		a.log.Warn("Feishu connector request failed: " + err.Error())
		writeErr(w, http.StatusServiceUnavailable, "FEISHU_UNAVAILABLE", "Feishu connector is temporarily unavailable")
	}
	return true
}
