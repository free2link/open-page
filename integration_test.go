package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const testToken = "openpage-isolated-integration-token"

type authTransport struct {
	token string
}

func (t authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	req := r.Clone(r.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(req)
}

func TestPublishingAcrossHTTPAndMCP(t *testing.T) {
	dir := t.TempDir()
	store, err := openStore(dir, testToken)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if store != nil {
			store.close()
		}
	})
	a := &Service{store: store}
	server := httptest.NewUnstartedServer(a.handler())
	a.origin = "http://" + server.Listener.Addr().String()
	server.Start()
	t.Cleanup(server.Close)
	client := &http.Client{Transport: authTransport{testToken}, Timeout: 4 * time.Second}

	call := func(method, address, body string, headers map[string]string, want int) ([]byte, http.Header) {
		t.Helper()
		req, err := http.NewRequest(method, address, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		for name, value := range headers {
			req.Header.Set(name, value)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != want {
			t.Fatalf("%s %s = %d, want %d: %s", method, address, res.StatusCode, want, data)
		}
		return data, res.Header
	}
	root := server.URL + "/api/pages"
	files := root + "/kiosk/files"
	entry := files + "/index.html"
	call("POST", root, `{"id":"kiosk","title":"Kiosk"}`, nil, 201)
	call("POST", root+"/kiosk/publish", `{"expected":"stale"}`, nil, 409)
	call("PUT", entry, `<!doctype html><title>First</title><h1>First</h1>`, map[string]string{"If-None-Match": "*"}, 200)
	call("PUT", files+"/assets/style.css", "h1 { color: red }", map[string]string{"If-None-Match": "*"}, 200)
	revision := func() string {
		t.Helper()
		data, _ := call("GET", files, "", nil, 200)
		var listing struct{ Revision string }
		if err := json.Unmarshal(data, &listing); err != nil || listing.Revision == "" {
			t.Fatalf("revision: %s, %v", data, err)
		}
		return listing.Revision
	}
	publish := func() Version {
		t.Helper()
		data, _ := call("POST", root+"/kiosk/publish", fmt.Sprintf(`{"expected":%q}`, revision()), nil, 201)
		var version Version
		if err := json.Unmarshal(data, &version); err != nil || version.ID == "" {
			t.Fatalf("version: %s, %v", data, err)
		}
		return version
	}
	first := publish()
	call("PUT", server.URL+"/api/settings", `{"defaultPage":"kiosk"}`, nil, 200)
	page, publishedHeaders := call("GET", server.URL+"/", "", nil, 200)
	if !strings.Contains(publishedHeaders.Get("Content-Security-Policy"), "sandbox allow-scripts") ||
		publishedHeaders.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("published document isolation headers missing: %v", publishedHeaders)
	}
	if !strings.Contains(string(page), "<h1>First</h1>") || !strings.Contains(string(page), first.ID) {
		t.Fatalf("missing published HTML or version metadata: %s", page)
	}
	call("GET", server.URL+"/assets/style.css", "", nil, 200)
	call("GET", server.URL+"/admin/", "", nil, 200)
	call("GET", root, "", map[string]string{"Origin": "null"}, 403)
	call("GET", root, "", map[string]string{"Origin": server.URL}, 200)
	unauthorized, err := http.Get(root)
	if err != nil {
		t.Fatal(err)
	}
	unauthorized.Body.Close()
	if unauthorized.StatusCode != 401 {
		t.Fatalf("unauthorized: %d", unauthorized.StatusCode)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "OpenPage test", Version: "1"}, nil)
	session, err := mcpClient.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: server.URL + "/mcp", HTTPClient: client, DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) < 13 {
		t.Fatalf("MCP tools: %v %v", tools, err)
	}
	_, header := call("GET", entry, "", nil, 200)
	staleRevision := revision()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "write_file", Arguments: map[string]any{
		"id": "kiosk", "path": "index.html", "expected": strings.Trim(header.Get("ETag"), `"`),
		"content": "<!doctype html><h1>Second</h1>",
	}})
	if err != nil || result.IsError {
		t.Fatalf("MCP write: %v %v", result, err)
	}
	call("POST", root+"/kiosk/publish", fmt.Sprintf(`{"expected":%q}`, staleRevision), nil, 409)
	call("PUT", entry, "stale", map[string]string{"If-Match": header.Get("ETag")}, 409)
	page, _ = call("GET", server.URL+"/", "", nil, 200)
	if !strings.Contains(string(page), "<h1>First</h1>") {
		t.Fatalf("draft changed publication: %s", page)
	}
	ticket, _ := call("POST", root+"/kiosk/preview", "", nil, 200)
	var signed struct{ URL string }
	if err := json.Unmarshal(ticket, &signed); err != nil {
		t.Fatal(err)
	}
	previewPage, pHeaders := call("GET", signed.URL, "", nil, 200)
	if !strings.Contains(string(previewPage), "Second") || pHeaders.Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("draft preview: %s %v", previewPage, pHeaders)
	}
	call("GET", signed.URL+"assets/style.css", "", nil, 200)
	parsed, err := url.Parse(signed.URL)
	if err != nil {
		t.Fatal(err)
	}
	pathParts := strings.Split(parsed.Path, "/")
	pathParts[3] = "0.invalid"
	parsed.Path = strings.Join(pathParts, "/")
	call("GET", parsed.String(), "", nil, 404)

	eventRequest, err := http.NewRequestWithContext(ctx, "GET", server.URL+"/events?page=kiosk", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(eventRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	eventStream := bufio.NewScanner(response.Body)
	nextVersion := func(want string) {
		t.Helper()
		for eventStream.Scan() {
			if line, ok := strings.CutPrefix(eventStream.Text(), "data: "); ok {
				var state liveVersion
				if err := json.Unmarshal([]byte(line), &state); err != nil || state.Version != want || state.Page != "kiosk" {
					t.Fatalf("unexpected event %s: %v", line, err)
				}
				return
			}
		}
		t.Fatalf("event %s missing: %v", want, eventStream.Err())
	}
	nextVersion(first.ID)
	second := publish()
	nextVersion(second.ID)
	call("GET", root+"/kiosk/versions/"+first.ID+"/files/index.html", "", nil, 200)
	restored, _ := call("POST", root+"/kiosk/restore", fmt.Sprintf(`{"version":%q}`, first.ID), nil, 201)
	var restoredVersion Version
	if err := json.Unmarshal(restored, &restoredVersion); err != nil || restoredVersion.RestoredFrom != first.ID || restoredVersion.ID == first.ID {
		t.Fatalf("restore result %s: %v", restored, err)
	}
	nextVersion(restoredVersion.ID)
	response.Body.Close()
	call("POST", root+"/kiosk/reset", fmt.Sprintf(`{"version":%q,"expected":%q}`, first.ID, revision()), nil, 200)
	reset, _ := call("GET", entry, "", nil, 200)
	if !strings.Contains(string(reset), "First") {
		t.Fatalf("reset draft: %s", reset)
	}
	call("PUT", root+"/kiosk", `{"private":true}`, nil, 400)
	call("PUT", server.URL+"/api/settings", `{"defaultPage":""}`, nil, 200)
	call("PUT", root+"/kiosk", `{"private":true}`, nil, 200)
	call("GET", server.URL+"/pages/kiosk/", "", nil, 404)
	call("GET", signed.URL, "", nil, 200)
	call("PUT", root+"/kiosk", `{"private":false}`, nil, 200)
	call("PUT", server.URL+"/api/settings", `{"defaultPage":"kiosk"}`, nil, 200)
	rotation, _ := call("PUT", server.URL+"/api/settings", `{"rotateToken":true}`, nil, 200)
	var credential struct{ Token string }
	if err := json.Unmarshal(rotation, &credential); err != nil || credential.Token == "" {
		t.Fatalf("rotation failed: %v", err)
	}
	call("GET", root, "", nil, 401)
	call("GET", signed.URL, "", nil, 404)

	store.close()
	store = nil
	reopened, err := openStore(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.close()
	p, err := reopened.load("kiosk")
	if err != nil || p.Current != restoredVersion.ID || reopened.settings.DefaultPage != "kiosk" || reopened.settings.TokenHash != digest([]byte(credential.Token)) {
		t.Fatalf("incorrect persisted state: %v %v", p, err)
	}
}

func TestStorageConfinementAndRecovery(t *testing.T) {
	dir := t.TempDir()
	s, err := openStore(dir, testToken)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if s != nil {
			s.close()
		}
	}()
	p, err := s.create("board", "Board")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.editFile(p, "index.html", "new", []byte("<h1>Hello</h1>"), false); err != nil {
		t.Fatal(err)
	}
	a := Service{store: s}
	initialETag := digest([]byte("<h1>Hello</h1>"))
	results := make(chan error, 2)
	var clients sync.WaitGroup
	for _, body := range []string{"<h1>First edit</h1>", "<h1>Second edit</h1>"} {
		clients.Go(func() {
			_, err := a.execute("write_file", Command{ID: p.ID, Path: "index.html", Expected: initialETag, Content: &body})
			results <- err
		})
	}
	clients.Wait()
	firstResult, secondResult := <-results, <-results
	if !((firstResult == nil && secondResult == errConflict) || (secondResult == nil && firstResult == errConflict)) {
		t.Fatalf("concurrent writes must have one winner: %v, %v", firstResult, secondResult)
	}
	if _, err := s.editFile(p, "../settings.json", "new", []byte("unexpected"), false); err == nil {
		t.Fatal("parent path accepted")
	}
	external := filepath.Join(t.TempDir(), "external.txt")
	if err := os.WriteFile(external, []byte("external"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "pages", "board", "draft", "link.txt")
	if err := os.Symlink(external, link); err != nil {
		t.Fatal(err)
	}
	if _, err := s.readFile("pages/board/draft", "link.txt"); err == nil {
		t.Fatal("linked file accepted")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	firstRevision, err := s.draftRevision(p)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.changeVersion(p, "publish", "", "initial", firstRevision)
	if err != nil {
		t.Fatal(err)
	}
	first := result.(Version)
	p, err = s.load(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	p.Versions[0].Protected = true
	if err := s.save(p); err != nil {
		t.Fatal(err)
	}
	for range retention + 1 {
		if _, err := s.changeVersion(p, "publish", "", "", firstRevision); err != nil {
			t.Fatal(err)
		}
		p, err = s.load(p.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(p.Versions) != retention+1 || p.Versions[len(p.Versions)-1].ID != first.ID {
		t.Fatalf("protected version or retention rule lost: %v", p.Versions)
	}
	for _, orphan := range []string{"pages/.new-staging", "pages/board/versions/abandoned", "pages/board/draft-abandoned"} {
		if err := s.root.Mkdir(orphan, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.root.WriteFile("pages/board/draft/.tmp-abandoned", []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	s.close()
	s, err = openStore(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.draftRevision(p); err != nil {
		t.Fatal(err)
	}
	for _, orphan := range []string{"pages/.new-staging", "pages/board/versions/abandoned", "pages/board/draft-abandoned"} {
		if _, err := s.root.Lstat(orphan); !os.IsNotExist(err) {
			t.Fatalf("orphan remains: %s (%v)", orphan, err)
		}
	}
}

func TestDefaultPasswordAndPreviewKey(t *testing.T) {
	dir := t.TempDir()
	store, err := openStore(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	a := &Service{store: store}
	handler := a.handler()
	check := func(password string, expected int) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
		req.Header.Set("Authorization", "Bearer "+password)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != expected {
			t.Fatalf("credentials returned %d, expected %d", res.Code, expected)
		}
	}
	defer store.close()
	check("admin", 200)
	check("other", 401)
	if len(store.settings.PreviewKey) < 32 || store.settings.PreviewKey == store.settings.TokenHash {
		t.Fatal("preview key must be independently generated")
	}
}

func TestExistingCredentialSurvivesPreviewKeyMigration(t *testing.T) {
	dir := t.TempDir()
	store, err := openStore(dir, testToken)
	if err != nil {
		t.Fatal(err)
	}
	legacy := store.settings
	legacy.PreviewKey = ""
	err = store.writeJSON("config/settings.json", legacy)
	store.close()
	if err != nil {
		t.Fatal(err)
	}
	migrated, err := openStore(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.close()
	if migrated.settings.TokenHash != digest([]byte(testToken)) || migrated.settings.PreviewKey == "" {
		t.Fatal("migration must retain the configured credential and create a preview key")
	}
}
