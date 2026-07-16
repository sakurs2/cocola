package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	"github.com/cocola-project/cocola/apps/gateway/internal/chatrun"
	"github.com/cocola-project/cocola/apps/gateway/internal/convo"
)

const (
	maxFolderNameRunes          = 80
	folderSessionCleanupTimeout = 10 * time.Second
)

type folderNameRequest struct {
	Name string `json:"name"`
}

type moveConversationRequest struct {
	FolderID json.RawMessage `json:"folder_id"`
}

func (a *API) listFolders(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityOf(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	if a.convo == nil {
		writeJSON(w, http.StatusOK, []convo.Folder{})
		return
	}
	folders, err := a.convo.ListFolders(r.Context(), identity.UserID)
	if err != nil {
		a.log.Warn("list folders failed: " + err.Error())
		writeErr(w, http.StatusInternalServerError, "INTERNAL", "could not list folders")
		return
	}
	writeJSON(w, http.StatusOK, folders)
}

func (a *API) createFolder(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityOf(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	name, ok := decodeFolderName(w, r)
	if !ok {
		return
	}
	if a.convo == nil {
		writeErr(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "folder store is not configured")
		return
	}
	now := time.Now().UTC()
	folder, err := a.convo.CreateFolder(r.Context(), convo.Folder{
		ID: uuid.NewString(), UserID: identity.UserID, Name: name, CreatedAt: now, UpdatedAt: now,
	})
	if a.writeFolderStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, folder)
}

func (a *API) renameFolder(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityOf(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	name, ok := decodeFolderName(w, r)
	if !ok {
		return
	}
	if a.convo == nil {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "folder not found")
		return
	}
	folder, err := a.convo.RenameFolder(r.Context(), r.PathValue("id"), identity.UserID, name, time.Now().UTC())
	if a.writeFolderStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, folder)
}

func (a *API) moveConversationToFolder(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityOf(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	folderID, ok := decodeMoveFolderID(w, r)
	if !ok {
		return
	}
	if a.convo == nil {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "conversation not found")
		return
	}
	convID := r.PathValue("id")
	unlock := func() {}
	if a.runs != nil {
		a.runs.mutationMu.Lock()
		unlock = a.runs.mutationMu.Unlock
		if active, err := a.runs.store.Active(r.Context(), convID, identity.UserID); err == nil {
			unlock()
			writeRunInProgress(w, active, convID, "stop the running answer before moving this conversation")
			return
		} else if !errors.Is(err, chatrun.ErrNotFound) {
			unlock()
			a.runs.databaseUnavailable.Store(true)
			writeErr(w, http.StatusServiceUnavailable, "RUN_STORE_UNAVAILABLE", "could not verify conversation run state")
			return
		}
		a.runs.databaseUnavailable.Store(false)
	}
	conversation, err := a.convo.MoveConversation(r.Context(), convID, identity.UserID, folderID, time.Now().UTC())
	unlock()
	if errors.Is(err, convo.ErrUnsupportedChatType) {
		writeErr(w, http.StatusConflict, "FOLDER_UNSUPPORTED_CONVERSATION_TYPE", "scheduled task conversations cannot be moved into folders")
		return
	}
	if errors.Is(err, convo.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "conversation or folder not found")
		return
	}
	if err != nil {
		a.log.Warn("move conversation failed: " + err.Error())
		writeErr(w, http.StatusInternalServerError, "INTERNAL", "could not move conversation")
		return
	}
	writeJSON(w, http.StatusOK, conversation)
}

func (a *API) deleteFolder(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityOf(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return
	}
	if a.convo == nil {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "folder not found")
		return
	}
	folderID := r.PathValue("id")
	unlock := func() {}
	if a.runs != nil {
		a.runs.mutationMu.Lock()
		unlock = a.runs.mutationMu.Unlock
	}
	conversationIDs, err := a.convo.ListFolderConversationIDs(r.Context(), folderID, identity.UserID)
	if err != nil {
		unlock()
		writeFolderDeleteError(a, w, err)
		return
	}
	if a.runs != nil {
		for _, conversationID := range conversationIDs {
			active, activeErr := a.runs.store.Active(r.Context(), conversationID, identity.UserID)
			if activeErr == nil {
				unlock()
				writeRunInProgress(w, active, conversationID, "stop all running answers before deleting this folder")
				return
			}
			if !errors.Is(activeErr, chatrun.ErrNotFound) {
				unlock()
				a.runs.databaseUnavailable.Store(true)
				writeErr(w, http.StatusServiceUnavailable, "RUN_STORE_UNAVAILABLE", "could not verify folder run state")
				return
			}
		}
		a.runs.databaseUnavailable.Store(false)
	}
	if _, err := a.convo.DeleteFolder(r.Context(), folderID, identity.UserID); err != nil {
		unlock()
		writeFolderDeleteError(a, w, err)
		return
	}
	unlock()

	// Remote cleanup must not hold the process-wide run mutation lock. The
	// database deletion is authoritative; a failed release leaves the existing
	// session_storage row visible to Admin for manual retry.
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), folderSessionCleanupTimeout)
	err = a.releaseFolderSessions(cleanupCtx, identity.UserID, conversationIDs)
	cancel()
	if err != nil {
		a.log.Warn("release deleted folder sessions failed: " + err.Error())
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeFolderName(w http.ResponseWriter, r *http.Request) (string, bool) {
	var request folderNameRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "malformed JSON body")
		return "", false
	}
	name := strings.TrimSpace(request.Name)
	if name == "" || utf8.RuneCountInString(name) > maxFolderNameRunes {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "folder name must contain 1 to 80 characters")
		return "", false
	}
	return name, true
}

func decodeMoveFolderID(w http.ResponseWriter, r *http.Request) (string, bool) {
	var request moveConversationRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || len(request.FolderID) == 0 {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "folder_id is required")
		return "", false
	}
	if bytes.Equal(bytes.TrimSpace(request.FolderID), []byte("null")) {
		return "", true
	}
	var folderID string
	if err := json.Unmarshal(request.FolderID, &folderID); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "folder_id must be a string or null")
		return "", false
	}
	folderID = strings.TrimSpace(folderID)
	if folderID == "" {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "use null to remove a conversation from its folder")
		return "", false
	}
	return folderID, true
}

func (a *API) writeFolderStoreError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, convo.ErrFolderNameConflict) {
		writeErr(w, http.StatusConflict, "FOLDER_NAME_EXISTS", "a folder with this name already exists")
		return true
	}
	if errors.Is(err, convo.ErrInvalidFolderName) {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "folder name must contain 1 to 80 characters")
		return true
	}
	if errors.Is(err, convo.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "folder not found")
		return true
	}
	a.log.Warn("save folder failed: " + err.Error())
	writeErr(w, http.StatusInternalServerError, "INTERNAL", "could not save folder")
	return true
}

func writeFolderDeleteError(a *API, w http.ResponseWriter, err error) {
	if errors.Is(err, convo.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "folder not found")
		return
	}
	a.log.Warn("delete folder failed: " + err.Error())
	writeErr(w, http.StatusInternalServerError, "INTERNAL", "could not delete folder")
}

func writeRunInProgress(w http.ResponseWriter, run chatrun.Run, conversationID, message string) {
	writeJSON(w, http.StatusConflict, map[string]any{
		"error":           map[string]string{"code": "RUN_IN_PROGRESS", "message": message},
		"conversation_id": conversationID,
		"run_id":          run.ID,
	})
}

func (a *API) releaseFolderSessions(ctx context.Context, userID string, conversationIDs []string) error {
	if a.releaser == nil || len(conversationIDs) == 0 {
		return nil
	}
	var errs []error
	for _, conversationID := range conversationIDs {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		if err := a.releaser.ReleaseSession(ctx, userID, conversationID); err != nil {
			errs = append(errs, fmt.Errorf("release session %s: %w", conversationID, err))
		}
	}
	return errors.Join(errs...)
}
