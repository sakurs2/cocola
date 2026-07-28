package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cocola-project/cocola/apps/gateway/internal/agent"
	"github.com/cocola-project/cocola/apps/gateway/internal/agentprofile"
	"github.com/cocola-project/cocola/apps/gateway/internal/auth"
	"github.com/cocola-project/cocola/apps/gateway/internal/convo"
	"github.com/cocola-project/cocola/apps/gateway/internal/wiki"
	"github.com/cocola-project/cocola/packages/go-common/logger"
)

type wikiStoreStub struct {
	wiki.Store
	createErr     error
	created       wiki.CreateFileInput
	saveErr       error
	savedVersion  wiki.Version
	resolvedNodes []wiki.Node
	resolved      []wiki.Version
	resolveErr    error
	resolveCalls  int
	currentNode   wiki.Node
	current       wiki.Version
	currentErr    error
	currentID     wiki.Identity
	currentNodeID string
}

func (s *wikiStoreStub) CreateFile(
	_ context.Context,
	_ wiki.Identity,
	input wiki.CreateFileInput,
) (wiki.Node, error) {
	s.created = input
	if s.createErr != nil {
		return wiki.Node{}, s.createErr
	}
	node := input.Node
	node.CurrentVersionID = input.Version.ID
	node.Revision = input.Version.Revision
	node.SizeBytes = input.Version.SizeBytes
	return node, nil
}

func (s *wikiStoreStub) SaveVersion(
	_ context.Context,
	_ wiki.Identity,
	_ string,
	_ int64,
	version wiki.Version,
	_ time.Time,
) (wiki.Node, error) {
	s.savedVersion = version
	if s.saveErr != nil {
		return wiki.Node{}, s.saveErr
	}
	return wiki.Node{ID: version.NodeID, Revision: version.Revision}, nil
}

func (s *wikiStoreStub) ResolveCurrent(
	_ context.Context,
	_ wiki.Identity,
	_ []string,
) ([]wiki.Node, []wiki.Version, error) {
	s.resolveCalls++
	return s.resolvedNodes, s.resolved, s.resolveErr
}

func (s *wikiStoreStub) GetCurrent(
	_ context.Context,
	identity wiki.Identity,
	nodeID string,
) (wiki.Node, wiki.Version, error) {
	s.currentID = identity
	s.currentNodeID = nodeID
	return s.currentNode, s.current, s.currentErr
}

type cleanupObjectStore struct {
	objects []string
	deleted []string
}

func (s *cleanupObjectStore) Put(
	_ context.Context,
	key string,
	_ []byte,
	_ string,
) error {
	s.objects = append(s.objects, key)
	return nil
}

func (*cleanupObjectStore) Get(context.Context, string) ([]byte, error) { return nil, nil }
func (*cleanupObjectStore) Health(context.Context) error                { return nil }

func (s *cleanupObjectStore) Delete(_ context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	return nil
}

func TestValidateWikiFileAcceptsSupportedYAMLMIMETypes(t *testing.T) {
	t.Parallel()
	for _, mimeType := range []string{"text/yaml", "application/x-yaml"} {
		name, normalizedMIME, err := validateWikiFile(
			"settings.yaml",
			mimeType,
			[]byte("enabled: true\n"),
		)
		if err != nil {
			t.Errorf("validateWikiFile(%q) error = %v", mimeType, err)
		}
		if name != "settings.yaml" || normalizedMIME != "application/yaml" {
			t.Errorf("validateWikiFile(%q) = %q, %q", mimeType, name, normalizedMIME)
		}
	}
}

func TestCreateWikiMarkdownRejectsNULText(t *testing.T) {
	t.Parallel()
	store := &cleanupObjectStore{}
	api := New(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithObjStore(store, DefaultInlineMaxBytes).
		WithWikiStore(&wikiStoreStub{}, 1024)
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/wiki/markdown",
		strings.NewReader(`{"name":"bad.md","content":"before\u0000after"}`),
	)
	recorder := httptest.NewRecorder()

	api.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body=%s; want 422", recorder.Code, recorder.Body.String())
	}
	if len(store.objects) != 0 {
		t.Fatalf("invalid Markdown wrote %d object(s)", len(store.objects))
	}
}

func TestCreateWikiMarkdownAllowsJSONEscapingWithinFileLimit(t *testing.T) {
	t.Parallel()
	const maxFileBytes = int64(2 << 20)
	store := &cleanupObjectStore{}
	api := New(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithObjStore(store, DefaultInlineMaxBytes).
		WithWikiStore(&wikiStoreStub{}, maxFileBytes)
	body, err := json.Marshal(createWikiNodeRequest{
		Name:    "escaped.md",
		Content: strings.Repeat("\n", int(maxFileBytes)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(body)) <= maxFileBytes+wikiMultipartOverheadBytes {
		t.Fatal("test body does not exceed the previous JSON transport limit")
	}
	recorder := httptest.NewRecorder()

	api.Handler().ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodPost, "/v1/wiki/markdown", bytes.NewReader(body)),
	)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s; want 201", recorder.Code, recorder.Body.String())
	}
	if len(store.objects) != 1 {
		t.Fatalf("Put calls = %d, want 1", len(store.objects))
	}
}

func TestCreateWikiFolderRejectsMalformedParentID(t *testing.T) {
	t.Parallel()
	api := New(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithObjStore(&cleanupObjectStore{}, DefaultInlineMaxBytes).
		WithWikiStore(&wikiStoreStub{}, 1024)
	recorder := httptest.NewRecorder()

	api.Handler().ServeHTTP(
		recorder,
		httptest.NewRequest(
			http.MethodPost,
			"/v1/wiki/folders",
			strings.NewReader(`{"parent_id":"not-a-uuid","name":"Folder"}`),
		),
	)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body=%s; want 400", recorder.Code, recorder.Body.String())
	}
}

func TestCreateWikiFileCleansUpObjectWhenMetadataWriteFails(t *testing.T) {
	t.Parallel()
	store := &cleanupObjectStore{}
	api := New(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithObjStore(store, DefaultInlineMaxBytes).
		WithWikiStore(&wikiStoreStub{createErr: wiki.ErrNameConflict}, 1024)

	_, err := api.createWikiFile(
		httptest.NewRequest(http.MethodPost, "/", nil),
		wiki.Identity{TenantID: "tenant", UserID: "user"},
		"",
		"page.md",
		"text/markdown; charset=utf-8",
		[]byte("# page"),
	)

	if !errors.Is(err, wiki.ErrNameConflict) {
		t.Fatalf("createWikiFile() error = %v", err)
	}
	if len(store.objects) != 1 {
		t.Fatalf("Put calls = %d, want 1", len(store.objects))
	}
	if len(store.deleted) != 1 || store.deleted[0] != store.objects[0] {
		t.Fatalf("Delete calls = %#v, want cleanup of %q", store.deleted, store.objects[0])
	}
}

func TestCreateWikiFileKeepsObjectWhenMetadataOutcomeIsUnknown(t *testing.T) {
	t.Parallel()
	store := &cleanupObjectStore{}
	metadataErr := errors.New("metadata commit outcome unknown")
	api := New(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithObjStore(store, DefaultInlineMaxBytes).
		WithWikiStore(&wikiStoreStub{createErr: metadataErr}, 1024)

	_, err := api.createWikiFile(
		httptest.NewRequest(http.MethodPost, "/", nil),
		wiki.Identity{TenantID: "tenant", UserID: "user"},
		"",
		"page.md",
		"text/markdown; charset=utf-8",
		[]byte("# page"),
	)

	if !errors.Is(err, metadataErr) {
		t.Fatalf("createWikiFile() error = %v", err)
	}
	if len(store.deleted) != 0 {
		t.Fatalf("Delete calls = %#v, want object retained for unknown metadata outcome", store.deleted)
	}
}

func TestSaveWikiMarkdownKeepsObjectWhenMetadataOutcomeIsUnknown(t *testing.T) {
	t.Parallel()
	store := &cleanupObjectStore{}
	metadataErr := errors.New("metadata commit outcome unknown")
	api := New(&fakeStreamer{}, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithObjStore(store, DefaultInlineMaxBytes).
		WithWikiStore(&wikiStoreStub{saveErr: metadataErr}, 1024)
	request := httptest.NewRequest(http.MethodPut, "/", strings.NewReader("# updated"))
	request.Header.Set("if-match", `"wiki-rev-1"`)
	recorder := httptest.NewRecorder()

	api.saveWikiMarkdown(
		recorder,
		request,
		wiki.Identity{TenantID: "tenant", UserID: "user"},
		wiki.Node{ID: uuid.NewString(), Extension: ".md"},
		wiki.Version{Revision: 1},
	)

	if len(store.deleted) != 0 {
		t.Fatalf("Delete calls = %#v, want object retained for unknown metadata outcome", store.deleted)
	}
}

func TestValidateOfficeArchive(t *testing.T) {
	t.Parallel()
	for extension := range wikiOfficeFormats {
		content := validOfficeArchive(t, extension)
		if err := validateOfficeArchive(extension, content); err != nil {
			t.Errorf("validateOfficeArchive(%q) error = %v", extension, err)
		}
	}
	if err := validateOfficeArchive(".docx", officeArchive(t, "[Content_Types].xml")); !errors.Is(err, errWikiInvalidFile) {
		t.Fatalf("missing document entrypoint error = %v", err)
	}
}

func TestValidateOfficeArchiveRejectsPlaceholderXML(t *testing.T) {
	t.Parallel()
	for extension, format := range wikiOfficeFormats {
		content := officeArchive(t, "[Content_Types].xml", format.entrypoint)
		if err := validateOfficeArchive(extension, content); !errors.Is(err, errWikiInvalidFile) {
			t.Errorf("validateOfficeArchive(%q) error = %v, want errWikiInvalidFile", extension, err)
		}
	}
}

func TestParseWikiRevision(t *testing.T) {
	t.Parallel()
	for _, value := range []string{`"wiki-rev-3"`, "wiki-rev-3", "3"} {
		revision, err := parseWikiRevision(value)
		if err != nil || revision != 3 {
			t.Errorf("parseWikiRevision(%q) = %d, %v", value, revision, err)
		}
	}
	for _, value := range []string{"", "wiki-rev-0", "wiki-rev-nope"} {
		if _, err := parseWikiRevision(value); !errors.Is(err, wiki.ErrRevisionConflict) {
			t.Errorf("parseWikiRevision(%q) error = %v", value, err)
		}
	}
}

func TestChatResolvesWikiReferenceIntoRunQuery(t *testing.T) {
	t.Parallel()
	nodeID := uuid.NewString()
	versionID := uuid.NewString()
	wikiStore := &wikiStoreStub{
		resolvedNodes: []wiki.Node{{
			ID: nodeID, Kind: "file", Name: "brief.docx",
			LogicalPath: "Product/Research/brief.docx",
		}},
		resolved: []wiki.Version{{
			ID: versionID, NodeID: nodeID, ObjectKey: "wiki/node/version",
			MimeType:  "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			SizeBytes: 42, SHA256: strings.Repeat("a", 64),
		}},
	}
	streamer := &fakeStreamer{script: []agent.Event{{Kind: "done"}}}
	api := newConfiguredTestAPI(streamer, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithObjStore(&cleanupObjectStore{}, DefaultInlineMaxBytes).
		WithWikiStore(wikiStore, 1024)
	body := `{"prompt":"summarize @brief","session_id":"wiki-chat","wiki_refs":[{"node_id":"` +
		nodeID + `"}]}`
	recorder := httptest.NewRecorder()

	api.Handler().ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(body)),
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if len(streamer.gotQuery.WikiReferences) != 1 {
		t.Fatalf("WikiReferences = %#v", streamer.gotQuery.WikiReferences)
	}
	got := streamer.gotQuery.WikiReferences[0]
	if got.NodeID != nodeID || got.VersionID != versionID ||
		got.LogicalPath != "Product/Research/brief.docx" ||
		got.ObjectKey != "wiki/node/version" {
		t.Fatalf("WikiReference = %#v", got)
	}
}

func TestChatResolvesAgentWikiKnowledgeWithoutAddingUserAttachmentPart(t *testing.T) {
	t.Parallel()
	nodeID := uuid.NewString()
	versionID := uuid.NewString()
	wikiStore := &wikiStoreStub{
		currentNode: wiki.Node{
			ID: nodeID, Kind: "file", Name: "handbook.md",
			LogicalPath: "Team/handbook.md",
		},
		current: wiki.Version{
			ID: versionID, NodeID: nodeID, ObjectKey: "wiki/node/version",
			MimeType: "text/markdown", SizeBytes: 42, SHA256: strings.Repeat("a", 64),
		},
	}
	agents := agentprofile.NewService(agentprofile.NewMemory())
	created, err := agents.Create(context.Background(), agentprofile.Identity{
		TenantID: auth.DevIdentity.TenantID,
		UserID:   auth.DevIdentity.UserID,
	}, agentprofile.CreateInput{
		Name: "Wiki specialist", RuntimeID: "claude-code",
		ModelRouteID: "route-1", ModelAlias: "sonnet",
		KnowledgeSources: []agentprofile.KnowledgeSource{{
			Type: agentprofile.KnowledgeTypeCocolaWiki, Label: "Handbook", NodeID: nodeID,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	streamer := &fakeStreamer{script: []agent.Event{{Kind: "done"}}}
	api := newConfiguredTestAPI(streamer, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithObjStore(&cleanupObjectStore{}, DefaultInlineMaxBytes).
		WithWikiStore(wikiStore, 1024).
		WithAgents(agents)
	body := `{"prompt":"use the handbook","session_id":"agent-wiki-chat","agent_id":"` +
		created.ID + `"}`
	recorder := httptest.NewRecorder()

	api.Handler().ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(body)),
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if len(streamer.gotQuery.WikiReferences) != 1 ||
		streamer.gotQuery.WikiReferences[0].VersionID != versionID {
		t.Fatalf("WikiReferences = %#v", streamer.gotQuery.WikiReferences)
	}
	parts := userMessageParts(chatRequest{
		Prompt: "use the handbook",
		AgentWikiReferences: []agent.WikiReference{{
			NodeID: nodeID, VersionID: versionID,
		}},
	})
	if len(parts) != 1 || parts[0].Type != convo.PartText {
		t.Fatalf("Agent Wiki leaked into user message parts: %#v", parts)
	}
}

func TestChatIgnoresDeletedAgentWikiKnowledge(t *testing.T) {
	t.Parallel()
	nodeID := uuid.NewString()
	wikiStore := &wikiStoreStub{currentErr: wiki.ErrNotFound}
	agents := agentprofile.NewService(agentprofile.NewMemory())
	created, err := agents.Create(context.Background(), agentprofile.Identity{
		TenantID: auth.DevIdentity.TenantID,
		UserID:   auth.DevIdentity.UserID,
	}, agentprofile.CreateInput{
		Name: "Wiki specialist", RuntimeID: "claude-code",
		ModelRouteID: "route-1", ModelAlias: "sonnet",
		KnowledgeSources: []agentprofile.KnowledgeSource{{
			Type: agentprofile.KnowledgeTypeCocolaWiki, Label: "Deleted", NodeID: nodeID,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	streamer := &fakeStreamer{script: []agent.Event{{Kind: "done"}}}
	api := newConfiguredTestAPI(streamer, auth.NewVerifier(auth.Config{}), logger.Must()).
		WithObjStore(&cleanupObjectStore{}, DefaultInlineMaxBytes).
		WithWikiStore(wikiStore, 1024).
		WithAgents(agents)
	body := `{"prompt":"hello","session_id":"deleted-agent-wiki","agent_id":"` +
		created.ID + `"}`
	recorder := httptest.NewRecorder()

	api.Handler().ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(body)),
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if len(streamer.gotQuery.WikiReferences) != 0 {
		t.Fatalf("deleted Wiki references = %#v", streamer.gotQuery.WikiReferences)
	}
}

func TestChatRejectsTooManyWikiReferencesBeforeResolving(t *testing.T) {
	t.Parallel()
	refs := make([]wikiRefDTO, maxWikiRefsPerTurn+1)
	for index := range refs {
		refs[index] = wikiRefDTO{NodeID: uuid.NewString()}
	}
	body, err := json.Marshal(chatRequest{
		Prompt: "summarize these files", SessionID: "wiki-chat", WikiRefs: refs,
	})
	if err != nil {
		t.Fatal(err)
	}
	wikiStore := &wikiStoreStub{}
	api := newConfiguredTestAPI(
		&fakeStreamer{},
		auth.NewVerifier(auth.Config{}),
		logger.Must(),
	).WithObjStore(&cleanupObjectStore{}, DefaultInlineMaxBytes).
		WithWikiStore(wikiStore, 1024)
	recorder := httptest.NewRecorder()

	api.Handler().ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodPost, "/v1/chat", bytes.NewReader(body)),
	)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if wikiStore.resolveCalls != 0 {
		t.Fatalf("ResolveCurrent calls = %d, want 0", wikiStore.resolveCalls)
	}
}

func TestChatAppliesWikiReferenceLimitAcrossUserAndAgentKnowledge(t *testing.T) {
	t.Parallel()
	agentSources := make([]agentprofile.KnowledgeSource, 10)
	for index := range agentSources {
		agentSources[index] = agentprofile.KnowledgeSource{
			Type:   agentprofile.KnowledgeTypeCocolaWiki,
			Label:  "Knowledge",
			NodeID: uuid.NewString(),
		}
	}
	agents := agentprofile.NewService(agentprofile.NewMemory())
	created, err := agents.Create(context.Background(), agentprofile.Identity{
		TenantID: auth.DevIdentity.TenantID,
		UserID:   auth.DevIdentity.UserID,
	}, agentprofile.CreateInput{
		Name: "Wiki specialist", RuntimeID: "claude-code",
		ModelRouteID: "route-1", ModelAlias: "sonnet",
		KnowledgeSources: agentSources,
	})
	if err != nil {
		t.Fatal(err)
	}
	refs := make([]wikiRefDTO, 11)
	for index := range refs {
		refs[index] = wikiRefDTO{NodeID: uuid.NewString()}
	}
	body, err := json.Marshal(chatRequest{
		Prompt: "summarize", SessionID: "combined-wiki-limit",
		AgentID: created.ID, WikiRefs: refs,
	})
	if err != nil {
		t.Fatal(err)
	}
	wikiStore := &wikiStoreStub{}
	api := newConfiguredTestAPI(
		&fakeStreamer{},
		auth.NewVerifier(auth.Config{}),
		logger.Must(),
	).WithObjStore(&cleanupObjectStore{}, DefaultInlineMaxBytes).
		WithWikiStore(wikiStore, 1024).
		WithAgents(agents)
	recorder := httptest.NewRecorder()

	api.Handler().ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodPost, "/v1/chat", bytes.NewReader(body)),
	)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if wikiStore.resolveCalls != 0 || wikiStore.currentNodeID != "" {
		t.Fatalf(
			"Wiki store was accessed before combined limit rejection: resolve=%d current=%q",
			wikiStore.resolveCalls,
			wikiStore.currentNodeID,
		)
	}
}

func TestChatRejectsWikiReferenceBytesOverPerTurnLimit(t *testing.T) {
	t.Parallel()
	nodeID := uuid.NewString()
	wikiStore := &wikiStoreStub{
		resolvedNodes: []wiki.Node{{ID: nodeID, Kind: "file", Name: "large.pdf"}},
		resolved: []wiki.Version{{
			ID: uuid.NewString(), NodeID: nodeID, SizeBytes: maxWikiBytesPerTurn + 1,
		}},
	}
	api := newConfiguredTestAPI(
		&fakeStreamer{},
		auth.NewVerifier(auth.Config{}),
		logger.Must(),
	).WithObjStore(&cleanupObjectStore{}, DefaultInlineMaxBytes).
		WithWikiStore(wikiStore, 1024)
	body := `{"prompt":"summarize it","session_id":"wiki-chat","wiki_refs":[{"node_id":"` +
		nodeID + `"}]}`
	recorder := httptest.NewRecorder()

	api.Handler().ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(body)),
	)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if wikiStore.resolveCalls != 1 {
		t.Fatalf("ResolveCurrent calls = %d, want 1", wikiStore.resolveCalls)
	}
}

func TestUserMessagePartsIncludesImmutableWikiVersion(t *testing.T) {
	t.Parallel()
	req := chatRequest{
		Prompt: "use @brief",
		WikiReferences: []agent.WikiReference{{
			NodeID: "node", VersionID: "version", Filename: "brief.md",
			Revision: 3, LogicalPath: "Team/brief.md", Mime: "text/markdown", Size: 8,
		}},
	}
	parts := userMessageParts(req)
	if len(parts) != 2 || parts[0].Text != req.Prompt {
		t.Fatalf("userMessageParts() = %#v", parts)
	}
	part := parts[1]
	if part.Type != "wiki-file" || part.WikiNodeID != "node" ||
		part.WikiVersionID != "version" ||
		part.Revision != 3 ||
		part.DownloadURL != "/api/wiki/versions/version/download" {
		t.Fatalf("Wiki part = %#v", part)
	}
}

func TestDefaultProductConfigPublishesWikiLimits(t *testing.T) {
	t.Parallel()
	config := DefaultProductConfig()
	if !config.Wiki.Enabled || config.Wiki.MaxFileBytes != DefaultWikiMaxFileBytes {
		t.Fatalf("DefaultProductConfig().Wiki = %#v", config.Wiki)
	}
	if err := (ProductConfig{
		AgentRuntime: config.AgentRuntime,
		Wiki:         WikiProductConfig{Enabled: true},
	}).Validate([]agent.Runtime{{ID: DefaultAgentRuntimeID}}); err == nil {
		t.Fatal("Validate() accepted an enabled Wiki with no file-size limit")
	}
}

func officeArchive(t *testing.T, entries ...string) []byte {
	t.Helper()
	contents := make(map[string]string, len(entries))
	for _, name := range entries {
		contents[name] = "<xml/>"
	}
	return officeArchiveWithContents(t, contents)
}

func validOfficeArchive(t *testing.T, extension string) []byte {
	t.Helper()
	type officeFixture struct {
		contentType string
		entrypoint  string
		document    string
	}
	fixtures := map[string]officeFixture{
		".docx": {
			contentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml",
			entrypoint:  "word/document.xml",
			document:    `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"/>`,
		},
		".xlsx": {
			contentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml",
			entrypoint:  "xl/workbook.xml",
			document:    `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"/>`,
		},
		".pptx": {
			contentType: "application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml",
			entrypoint:  "ppt/presentation.xml",
			document:    `<p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"/>`,
		},
	}
	fixture, ok := fixtures[extension]
	if !ok {
		t.Fatalf("no Office fixture for %q", extension)
	}
	contentTypes := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Override PartName="/` + fixture.entrypoint + `" ContentType="` + fixture.contentType + `"/>` +
		`</Types>`
	return officeArchiveWithContents(t, map[string]string{
		"[Content_Types].xml": contentTypes,
		fixture.entrypoint:    fixture.document,
	})
}

func officeArchiveWithContents(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
