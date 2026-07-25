package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	"github.com/cocola-project/cocola/apps/gateway/internal/wiki"
)

const (
	DefaultWikiMaxFileBytes    = int64(20 << 20)
	wikiMultipartOverheadBytes = int64(1 << 20)
	wikiMaxArchiveEntries      = 10_000
	wikiMaxExpandedBytes       = uint64(200 << 20)
	wikiMaxCompressionRatio    = uint64(100)
	wikiMaxOfficeXMLBytes      = int64(4 << 20)
	wikiObjectCleanupTimeout   = 5 * time.Second
)

var wikiMIMETypes = map[string]string{
	".md":   "text/markdown; charset=utf-8",
	".txt":  "text/plain; charset=utf-8",
	".csv":  "text/csv; charset=utf-8",
	".json": "application/json",
	".yaml": "application/yaml",
	".yml":  "application/yaml",
	".pdf":  "application/pdf",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
}

type wikiOfficeFormat struct {
	entrypoint  string
	contentType string
	rootName    xml.Name
}

var wikiOfficeFormats = map[string]wikiOfficeFormat{
	".docx": {
		entrypoint:  "word/document.xml",
		contentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml",
		rootName: xml.Name{
			Space: "http://schemas.openxmlformats.org/wordprocessingml/2006/main",
			Local: "document",
		},
	},
	".xlsx": {
		entrypoint:  "xl/workbook.xml",
		contentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml",
		rootName: xml.Name{
			Space: "http://schemas.openxmlformats.org/spreadsheetml/2006/main",
			Local: "workbook",
		},
	},
	".pptx": {
		entrypoint:  "ppt/presentation.xml",
		contentType: "application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml",
		rootName: xml.Name{
			Space: "http://schemas.openxmlformats.org/presentationml/2006/main",
			Local: "presentation",
		},
	},
}

type createWikiNodeRequest struct {
	ParentID string `json:"parent_id"`
	Name     string `json:"name"`
	Content  string `json:"content,omitempty"`
}

type updateWikiNodeRequest struct {
	Name string `json:"name"`
}

type moveWikiNodeRequest struct {
	ParentID string `json:"parent_id"`
}

func (a *API) WithWikiStore(store wiki.Store, maxFileBytes int64) *API {
	a.wiki = store
	if maxFileBytes <= 0 {
		maxFileBytes = DefaultWikiMaxFileBytes
	}
	a.wikiMaxFileBytes = maxFileBytes
	return a
}

func (a *API) wikiIdentity(r *http.Request) (wiki.Identity, bool) {
	identity, ok := auth.IdentityOf(r)
	if !ok {
		return wiki.Identity{}, false
	}
	return wiki.Identity{TenantID: identity.TenantID, UserID: identity.UserID}, true
}

func (a *API) requireWiki(w http.ResponseWriter, r *http.Request) (wiki.Identity, bool) {
	identity, ok := a.wikiIdentity(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing identity")
		return wiki.Identity{}, false
	}
	if a.wiki == nil || a.store == nil {
		writeErr(w, http.StatusServiceUnavailable, "WIKI_UNAVAILABLE", "Wiki is not configured")
		return wiki.Identity{}, false
	}
	return identity, true
}

func (a *API) listWikiTree(w http.ResponseWriter, r *http.Request) {
	identity, ok := a.requireWiki(w, r)
	if !ok {
		return
	}
	nodes, err := a.wiki.List(r.Context(), identity)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "INTERNAL", "could not load Wiki")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes})
}

func (a *API) searchWiki(w http.ResponseWriter, r *http.Request) {
	identity, ok := a.requireWiki(w, r)
	if !ok {
		return
	}
	nodes, err := a.wiki.List(r.Context(), identity)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "INTERNAL", "could not search Wiki")
		return
	}
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed > 0 && parsed <= 50 {
			limit = parsed
		}
	}
	results := make([]wiki.Node, 0, limit)
	for _, node := range nodes {
		if node.Kind != "file" {
			continue
		}
		if query != "" &&
			!strings.Contains(strings.ToLower(node.Name), query) &&
			!strings.Contains(strings.ToLower(node.LogicalPath), query) {
			continue
		}
		results = append(results, node)
		if len(results) == limit {
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": results})
}

func decodeWikiJSON(w http.ResponseWriter, r *http.Request, target any, maxBytes int64) bool {
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "malformed JSON body")
		return false
	}
	return true
}

func (a *API) createWikiFolder(w http.ResponseWriter, r *http.Request) {
	identity, ok := a.requireWiki(w, r)
	if !ok {
		return
	}
	var request createWikiNodeRequest
	if !decodeWikiJSON(w, r, &request, 2<<20) {
		return
	}
	request.ParentID = strings.TrimSpace(request.ParentID)
	if !validOptionalWikiID(w, request.ParentID) {
		return
	}
	name, err := wiki.NormalizeName(request.Name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "WIKI_INVALID_NAME", "folder name is invalid")
		return
	}
	now := time.Now().UTC()
	node, err := a.wiki.CreateFolder(r.Context(), identity, wiki.Node{
		ID: uuid.NewString(), ParentID: request.ParentID,
		Kind: "folder", Name: name, CreatedAt: now, UpdatedAt: now,
	})
	if writeWikiStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, node)
}

func (a *API) createWikiMarkdown(w http.ResponseWriter, r *http.Request) {
	identity, ok := a.requireWiki(w, r)
	if !ok {
		return
	}
	bodyLimit := wikiJSONBodyLimit(a.wikiMaxFileBytes)
	r.Body = http.MaxBytesReader(w, r.Body, bodyLimit)
	var request createWikiNodeRequest
	if !decodeWikiJSON(w, r, &request, bodyLimit) {
		return
	}
	request.ParentID = strings.TrimSpace(request.ParentID)
	if !validOptionalWikiID(w, request.ParentID) {
		return
	}
	name, err := normalizeWikiFilename(request.Name, ".md")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "WIKI_INVALID_NAME", "Markdown filename is invalid")
		return
	}
	content := []byte(request.Content)
	if int64(len(content)) > a.wikiMaxFileBytes {
		writeErr(w, http.StatusRequestEntityTooLarge, "WIKI_FILE_TOO_LARGE", "file exceeds the configured size limit")
		return
	}
	if !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
		writeErr(w, http.StatusUnprocessableEntity, "WIKI_FILE_INVALID", "Markdown must be valid UTF-8 text")
		return
	}
	node, err := a.createWikiFile(
		r, identity, request.ParentID, name,
		"text/markdown; charset=utf-8", content,
	)
	if writeWikiStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, node)
}

func (a *API) uploadWikiFile(w http.ResponseWriter, r *http.Request) {
	identity, ok := a.requireWiki(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, a.wikiMaxFileBytes+wikiMultipartOverheadBytes)
	if err := r.ParseMultipartForm(a.wikiMaxFileBytes + wikiMultipartOverheadBytes); err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, "WIKI_FILE_TOO_LARGE", "file exceeds the configured size limit")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "multipart field file is required")
		return
	}
	defer func() { _ = file.Close() }()
	content, err := io.ReadAll(io.LimitReader(file, a.wikiMaxFileBytes+1))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "WIKI_FILE_INVALID", "could not read uploaded file")
		return
	}
	if int64(len(content)) > a.wikiMaxFileBytes {
		writeErr(w, http.StatusRequestEntityTooLarge, "WIKI_FILE_TOO_LARGE", "file exceeds the configured size limit")
		return
	}
	name, mimeType, err := validateWikiFile(header.Filename, header.Header.Get("content-type"), content)
	if err != nil {
		writeWikiValidationError(w, err)
		return
	}
	parentID := strings.TrimSpace(r.FormValue("parent_id"))
	if !validOptionalWikiID(w, parentID) {
		return
	}
	node, err := a.createWikiFile(
		r, identity, parentID, name, mimeType, content,
	)
	if writeWikiStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, node)
}

func (a *API) createWikiFile(
	r *http.Request,
	identity wiki.Identity,
	parentID, name, mimeType string,
	content []byte,
) (wiki.Node, error) {
	nodeID := uuid.NewString()
	versionID := uuid.NewString()
	objectKey := "wiki/" + nodeID + "/" + versionID
	if err := a.store.Put(r.Context(), objectKey, content, mimeType); err != nil {
		return wiki.Node{}, err
	}
	sum := sha256.Sum256(content)
	now := time.Now().UTC()
	extension := strings.ToLower(filepath.Ext(name))
	node, err := a.wiki.CreateFile(r.Context(), identity, wiki.CreateFileInput{
		Node: wiki.Node{
			ID: nodeID, ParentID: parentID, Kind: "file", Name: name,
			Extension: extension, MimeType: mimeType, CreatedAt: now, UpdatedAt: now,
		},
		Version: wiki.Version{
			ID: versionID, NodeID: nodeID, Revision: 1, ObjectKey: objectKey,
			SizeBytes: int64(len(content)), SHA256: hex.EncodeToString(sum[:]),
			MimeType: mimeType, CreatedAt: now,
		},
	})
	if shouldCleanupWikiObject(err) {
		a.cleanupWikiObject(r.Context(), objectKey)
	}
	return node, err
}

func (a *API) updateWikiNode(w http.ResponseWriter, r *http.Request) {
	identity, ok := a.requireWiki(w, r)
	if !ok {
		return
	}
	nodeID := strings.TrimSpace(r.PathValue("id"))
	var request updateWikiNodeRequest
	if !validWikiID(w, nodeID) || !decodeWikiJSON(w, r, &request, 2<<20) {
		return
	}
	name, err := wiki.NormalizeName(request.Name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "WIKI_INVALID_NAME", "name is invalid")
		return
	}
	current, _, err := a.wiki.GetCurrent(r.Context(), identity, nodeID)
	if errors.Is(err, wiki.ErrNotFound) {
		// Folders have no current version, so locate them in the owner tree.
		nodes, listErr := a.wiki.List(r.Context(), identity)
		if listErr != nil {
			writeWikiStoreError(w, listErr)
			return
		}
		for _, candidate := range nodes {
			if candidate.ID == nodeID {
				current = candidate
				err = nil
				break
			}
		}
	}
	if err != nil {
		writeWikiStoreError(w, err)
		return
	}
	if current.Kind == "file" && strings.ToLower(filepath.Ext(name)) != current.Extension {
		writeErr(w, http.StatusBadRequest, "WIKI_EXTENSION_IMMUTABLE", "file extension cannot be changed")
		return
	}
	node, err := a.wiki.Rename(r.Context(), identity, nodeID, name, time.Now().UTC())
	if writeWikiStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, node)
}

func (a *API) moveWikiNode(w http.ResponseWriter, r *http.Request) {
	identity, ok := a.requireWiki(w, r)
	if !ok {
		return
	}
	nodeID := strings.TrimSpace(r.PathValue("id"))
	var request moveWikiNodeRequest
	if !validWikiID(w, nodeID) || !decodeWikiJSON(w, r, &request, 2<<20) {
		return
	}
	request.ParentID = strings.TrimSpace(request.ParentID)
	if !validOptionalWikiID(w, request.ParentID) {
		return
	}
	node, err := a.wiki.Move(
		r.Context(), identity, nodeID, request.ParentID, time.Now().UTC(),
	)
	if writeWikiStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, node)
}

func (a *API) deleteWikiNode(w http.ResponseWriter, r *http.Request) {
	identity, ok := a.requireWiki(w, r)
	if !ok {
		return
	}
	nodeID := strings.TrimSpace(r.PathValue("id"))
	if !validWikiID(w, nodeID) {
		return
	}
	if err := a.wiki.Delete(
		r.Context(), identity, nodeID, time.Now().UTC(),
	); writeWikiStoreError(w, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) wikiFileContent(w http.ResponseWriter, r *http.Request) {
	identity, ok := a.requireWiki(w, r)
	if !ok {
		return
	}
	nodeID := strings.TrimSpace(r.PathValue("id"))
	if !validWikiID(w, nodeID) {
		return
	}
	node, version, err := a.wiki.GetCurrent(r.Context(), identity, nodeID)
	if writeWikiStoreError(w, err) {
		return
	}
	if r.Method == http.MethodPut {
		a.saveWikiMarkdown(w, r, identity, node, version)
		return
	}
	a.serveWikiVersion(w, r, node, version, true)
}

func (a *API) saveWikiMarkdown(
	w http.ResponseWriter,
	r *http.Request,
	identity wiki.Identity,
	node wiki.Node,
	current wiki.Version,
) {
	if node.Extension != ".md" {
		writeErr(w, http.StatusUnsupportedMediaType, "WIKI_NOT_EDITABLE", "only Markdown files can be edited")
		return
	}
	expected, err := parseWikiRevision(r.Header.Get("if-match"))
	if err != nil {
		writeErr(w, http.StatusPreconditionRequired, "WIKI_REVISION_REQUIRED", "If-Match revision is required")
		return
	}
	content, err := io.ReadAll(io.LimitReader(r.Body, a.wikiMaxFileBytes+1))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "WIKI_FILE_INVALID", "could not read Markdown content")
		return
	}
	if int64(len(content)) > a.wikiMaxFileBytes {
		writeErr(w, http.StatusRequestEntityTooLarge, "WIKI_FILE_TOO_LARGE", "file exceeds the configured size limit")
		return
	}
	if !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
		writeErr(w, http.StatusUnprocessableEntity, "WIKI_FILE_INVALID", "Markdown must be valid UTF-8 text")
		return
	}
	versionID := uuid.NewString()
	objectKey := "wiki/" + node.ID + "/" + versionID
	mimeType := wikiMIMETypes[".md"]
	if err := a.store.Put(r.Context(), objectKey, content, mimeType); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "WIKI_STORAGE_UNAVAILABLE", "could not save file")
		return
	}
	sum := sha256.Sum256(content)
	now := time.Now().UTC()
	updated, err := a.wiki.SaveVersion(r.Context(), identity, node.ID, expected, wiki.Version{
		ID: versionID, NodeID: node.ID, Revision: current.Revision + 1,
		ObjectKey: objectKey, SizeBytes: int64(len(content)),
		SHA256: hex.EncodeToString(sum[:]), MimeType: mimeType, CreatedAt: now,
	}, now)
	if shouldCleanupWikiObject(err) {
		a.cleanupWikiObject(r.Context(), objectKey)
	}
	if writeWikiStoreError(w, err) {
		return
	}
	w.Header().Set("etag", formatWikiRevision(updated.Revision))
	writeJSON(w, http.StatusOK, updated)
}

func (a *API) downloadWikiFile(w http.ResponseWriter, r *http.Request) {
	identity, ok := a.requireWiki(w, r)
	if !ok {
		return
	}
	nodeID := strings.TrimSpace(r.PathValue("id"))
	if !validWikiID(w, nodeID) {
		return
	}
	node, version, err := a.wiki.GetCurrent(r.Context(), identity, nodeID)
	if writeWikiStoreError(w, err) {
		return
	}
	a.serveWikiVersion(w, r, node, version, false)
}

func (a *API) downloadWikiVersion(w http.ResponseWriter, r *http.Request) {
	identity, ok := a.requireWiki(w, r)
	if !ok {
		return
	}
	versionID := strings.TrimSpace(r.PathValue("id"))
	if !validWikiID(w, versionID) {
		return
	}
	node, version, err := a.wiki.GetVersion(
		r.Context(), identity, versionID,
	)
	if writeWikiStoreError(w, err) {
		return
	}
	a.serveWikiVersion(w, r, node, version, false)
}

func validWikiID(w http.ResponseWriter, value string) bool {
	if _, err := uuid.Parse(value); err != nil || len(value) != 36 {
		writeErr(w, http.StatusBadRequest, "INVALID_ARGUMENT", "Wiki id must be a UUID")
		return false
	}
	return true
}

func validOptionalWikiID(w http.ResponseWriter, value string) bool {
	return value == "" || validWikiID(w, value)
}

func wikiJSONBodyLimit(fileLimit int64) int64 {
	const maxJSONExpansion = int64(6) // A byte may be encoded as one \u00XX escape.
	maxInt64 := int64(^uint64(0) >> 1)
	if fileLimit > (maxInt64-wikiMultipartOverheadBytes)/maxJSONExpansion {
		return maxInt64
	}
	return fileLimit*maxJSONExpansion + wikiMultipartOverheadBytes
}

func (a *API) serveWikiVersion(
	w http.ResponseWriter,
	r *http.Request,
	node wiki.Node,
	version wiki.Version,
	inline bool,
) {
	data, err := a.store.Get(r.Context(), version.ObjectKey)
	if err != nil {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "file bytes not found")
		return
	}
	disposition := "attachment"
	if inline {
		disposition = "inline"
	}
	value := mime.FormatMediaType(disposition, map[string]string{"filename": node.Name})
	w.Header().Set("content-type", version.MimeType)
	w.Header().Set("content-disposition", value)
	w.Header().Set("content-length", strconv.Itoa(len(data)))
	w.Header().Set("etag", formatWikiRevision(version.Revision))
	w.Header().Set("cache-control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func formatWikiRevision(revision int64) string {
	return `"wiki-rev-` + strconv.FormatInt(revision, 10) + `"`
}

func parseWikiRevision(value string) (int64, error) {
	value = strings.TrimSpace(strings.Trim(value, `"`))
	value = strings.TrimPrefix(value, "wiki-rev-")
	revision, err := strconv.ParseInt(value, 10, 64)
	if err != nil || revision <= 0 {
		return 0, wiki.ErrRevisionConflict
	}
	return revision, nil
}

func normalizeWikiFilename(value, requiredExtension string) (string, error) {
	name, err := wiki.NormalizeName(value)
	if err != nil {
		return "", err
	}
	extension := strings.ToLower(filepath.Ext(name))
	if requiredExtension != "" {
		if extension == "" {
			name += requiredExtension
			extension = requiredExtension
		}
		if extension != requiredExtension {
			return "", wiki.ErrInvalidName
		}
	}
	return name, nil
}

func validateWikiFile(filename, declaredMIME string, content []byte) (string, string, error) {
	name, err := normalizeWikiFilename(filename, "")
	if err != nil {
		return "", "", wiki.ErrInvalidName
	}
	extension := strings.ToLower(filepath.Ext(name))
	expectedMIME, allowed := wikiMIMETypes[extension]
	if !allowed {
		return "", "", errWikiUnsupportedType
	}
	declaredBase, _, _ := mime.ParseMediaType(declaredMIME)
	expectedBase, _, _ := mime.ParseMediaType(expectedMIME)
	yamlAlias := (extension == ".yaml" || extension == ".yml") &&
		(declaredBase == "text/yaml" || declaredBase == "application/x-yaml")
	if declaredBase != "" && declaredBase != "application/octet-stream" && !yamlAlias &&
		declaredBase != expectedBase &&
		!(strings.HasPrefix(expectedBase, "text/") && strings.HasPrefix(declaredBase, "text/")) {
		return "", "", errWikiInvalidFile
	}
	switch extension {
	case ".md", ".txt", ".csv", ".yaml", ".yml":
		if !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
			return "", "", errWikiInvalidFile
		}
	case ".json":
		if !utf8.Valid(content) || !json.Valid(content) {
			return "", "", errWikiInvalidFile
		}
	case ".pdf":
		prefix := content
		if len(prefix) > 1024 {
			prefix = prefix[:1024]
		}
		if !bytes.Contains(prefix, []byte("%PDF-")) {
			return "", "", errWikiInvalidFile
		}
	case ".docx", ".xlsx", ".pptx":
		if err := validateOfficeArchive(extension, content); err != nil {
			return "", "", err
		}
	}
	return name, expectedMIME, nil
}

type wikiObjectDeleter interface {
	Delete(context.Context, string) error
}

func (a *API) cleanupWikiObject(ctx context.Context, objectKey string) {
	deleter, ok := a.store.(wikiObjectDeleter)
	if !ok {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), wikiObjectCleanupTimeout)
	defer cancel()
	if err := deleter.Delete(cleanupCtx, objectKey); err != nil {
		a.log.Warn("Wiki orphan object cleanup failed: " + err.Error())
	}
}

func shouldCleanupWikiObject(err error) bool {
	return errors.Is(err, wiki.ErrNameConflict) ||
		errors.Is(err, wiki.ErrInvalidParent) ||
		errors.Is(err, wiki.ErrRevisionConflict) ||
		errors.Is(err, wiki.ErrNotMarkdown)
}

var (
	errWikiUnsupportedType = errors.New("wiki: unsupported file type")
	errWikiInvalidFile     = errors.New("wiki: invalid file")
)

func validateOfficeArchive(extension string, content []byte) error {
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil || len(reader.File) == 0 || len(reader.File) > wikiMaxArchiveEntries {
		return errWikiInvalidFile
	}
	format, ok := wikiOfficeFormats[extension]
	if !ok {
		return errWikiInvalidFile
	}
	files := make(map[string]*zip.File, len(reader.File))
	var expanded uint64
	for _, file := range reader.File {
		clean := path.Clean(file.Name)
		normalizedName := strings.TrimSuffix(file.Name, "/")
		if clean == "." ||
			clean == ".." ||
			clean != normalizedName ||
			strings.HasPrefix(clean, "../") ||
			strings.HasPrefix(clean, "/") ||
			strings.Contains(file.Name, "\\") {
			return errWikiInvalidFile
		}
		if _, exists := files[clean]; exists {
			return errWikiInvalidFile
		}
		files[clean] = file
		if file.UncompressedSize64 > wikiMaxExpandedBytes-expanded {
			return errWikiInvalidFile
		}
		expanded += file.UncompressedSize64
		if file.CompressedSize64 > 0 &&
			file.UncompressedSize64/file.CompressedSize64 > wikiMaxCompressionRatio {
			return errWikiInvalidFile
		}
	}
	contentTypesFile := files["[Content_Types].xml"]
	entrypointFile := files[format.entrypoint]
	if contentTypesFile == nil || entrypointFile == nil {
		return errWikiInvalidFile
	}
	contentTypesXML, err := readOfficeXML(contentTypesFile)
	if err != nil || !validOfficeContentTypes(contentTypesXML, format) {
		return errWikiInvalidFile
	}
	entrypointXML, err := readOfficeXML(entrypointFile)
	if err != nil || !validOfficeRoot(entrypointXML, format.rootName) {
		return errWikiInvalidFile
	}
	return nil
}

func readOfficeXML(file *zip.File) ([]byte, error) {
	if file.UncompressedSize64 > uint64(wikiMaxOfficeXMLBytes) {
		return nil, errWikiInvalidFile
	}
	reader, err := file.Open()
	if err != nil {
		return nil, errWikiInvalidFile
	}
	content, readErr := io.ReadAll(io.LimitReader(reader, wikiMaxOfficeXMLBytes+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || int64(len(content)) > wikiMaxOfficeXMLBytes {
		return nil, errWikiInvalidFile
	}
	return content, nil
}

func validOfficeContentTypes(content []byte, format wikiOfficeFormat) bool {
	var types struct {
		XMLName   xml.Name `xml:"Types"`
		Overrides []struct {
			PartName    string `xml:"PartName,attr"`
			ContentType string `xml:"ContentType,attr"`
		} `xml:"Override"`
	}
	if err := xml.Unmarshal(content, &types); err != nil ||
		types.XMLName.Space != "http://schemas.openxmlformats.org/package/2006/content-types" {
		return false
	}
	for _, override := range types.Overrides {
		if override.PartName == "/"+format.entrypoint &&
			override.ContentType == format.contentType {
			return true
		}
	}
	return false
}

func validOfficeRoot(content []byte, expected xml.Name) bool {
	var document struct {
		XMLName xml.Name
	}
	return xml.Unmarshal(content, &document) == nil && document.XMLName == expected
}

func writeWikiValidationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, wiki.ErrInvalidName):
		writeErr(w, http.StatusBadRequest, "WIKI_INVALID_NAME", "filename is invalid")
	case errors.Is(err, errWikiUnsupportedType):
		writeErr(w, http.StatusUnsupportedMediaType, "WIKI_FILE_TYPE_UNSUPPORTED", "file type is not supported")
	default:
		writeErr(w, http.StatusUnprocessableEntity, "WIKI_FILE_INVALID", "file content does not match its format")
	}
}

func writeWikiStoreError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, wiki.ErrNotFound):
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "Wiki item not found")
	case errors.Is(err, wiki.ErrNameConflict):
		writeErr(w, http.StatusConflict, "WIKI_NAME_CONFLICT", "an item with this name already exists")
	case errors.Is(err, wiki.ErrInvalidName):
		writeErr(w, http.StatusBadRequest, "WIKI_INVALID_NAME", "name is invalid")
	case errors.Is(err, wiki.ErrInvalidParent):
		writeErr(w, http.StatusNotFound, "WIKI_PARENT_NOT_FOUND", "destination folder not found")
	case errors.Is(err, wiki.ErrMoveCycle):
		writeErr(w, http.StatusConflict, "WIKI_MOVE_CYCLE", "folder cannot be moved into itself")
	case errors.Is(err, wiki.ErrRevisionConflict):
		writeErr(w, http.StatusPreconditionFailed, "WIKI_EDIT_CONFLICT", "file changed in another tab")
	case errors.Is(err, wiki.ErrNotMarkdown):
		writeErr(w, http.StatusUnsupportedMediaType, "WIKI_NOT_EDITABLE", "only Markdown files can be edited")
	default:
		writeErr(w, http.StatusInternalServerError, "INTERNAL", "Wiki operation failed")
	}
	return true
}
