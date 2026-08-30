package mcpregistry

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikts/cms/internal/providers"
)

func testRegistryServer(t *testing.T, handler http.Handler) string {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("network unavailable: %v", err)
	}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return "http://" + listener.Addr().String()
}

func TestSearchResolveAndCache(t *testing.T) {
	hits := 0
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("network unavailable: %v", err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path == "/v0.1/servers" {
			_, _ = w.Write([]byte(`{"servers":[{"name":"demo","description":"Demo","version":"1.2.3"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"server":{"name":"demo","version":"1.2.3","packages":[{"registryType":"npm","name":"@demo/mcp","version":"1.2.3"}],"remotes":[{"url":"https://demo.test/mcp"}]}}`))
	})}
	go server.Serve(listener)
	defer server.Close()
	c := New("http://"+listener.Addr().String(), filepath.Join(t.TempDir(), "cache"), time.Hour)
	results, err := c.Search(context.Background(), "demo", 30)
	if err != nil || len(results) != 1 {
		t.Fatalf("search=%#v err=%v", results, err)
	}
	variants, err := c.Resolve(context.Background(), providers.MCPProviderRef{Name: "demo"})
	if err != nil || len(variants) != 2 {
		t.Fatalf("variants=%#v err=%v", variants, err)
	}
	if _, err := c.Search(context.Background(), "demo", 30); err != nil {
		t.Fatal(err)
	}
	if hits != 2 {
		t.Fatalf("expected cached second search, hits=%d", hits)
	}
}

func TestResolveRejectsUnsupportedTransports(t *testing.T) {
	base := testRegistryServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"server":{"name":"legacy","version":"1","remotes":[{"type":"sse","url":"https://legacy.test/sse"}],"packages":[{"registryType":"npm","name":"legacy","transport":{"type":"sse"}}]}}`))
	}))
	c := New(base, t.TempDir(), time.Minute)
	_, err := c.Resolve(context.Background(), providers.MCPProviderRef{Name: "legacy"})
	if err == nil || !strings.Contains(err.Error(), "no supported") {
		t.Fatalf("Resolve() error=%v, want unsupported transport error", err)
	}
}

func TestProviderMalformedResponse(t *testing.T) {
	base := testRegistryServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"servers":`))
	}))
	c := New(base, t.TempDir(), time.Minute)
	if _, err := c.Search(context.Background(), "demo", 30); err == nil {
		t.Fatal("Search() accepted malformed JSON")
	}
}

func TestSearchPreservesRegistryMetadataForTheTUI(t *testing.T) {
	base := testRegistryServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"servers":[{"server":{"name":"io.example/demo","title":"Demo MCP","description":"A useful demo server","version":"2.0.0","websiteUrl":"https://demo.test","repository":{"url":"https://github.com/example/demo","source":"github"},"packages":[{"registryType":"npm","identifier":"@example/demo","version":"2.0.0","runtimeHint":"npx"}],"remotes":[{"type":"streamable-http","url":"https://demo.test/mcp"}],"icons":[{"src":"https://demo.test/icon.png","mimeType":"image/png","sizes":["any"]}]},"_meta":{"io.modelcontextprotocol.registry/official":{"status":"active","isLatest":true,"publishedAt":"2026-01-01T00:00:00Z"}}}]}`))
	}))
	results, err := New(base, t.TempDir(), time.Minute).Search(context.Background(), "demo", 30)
	if err != nil || len(results) != 1 {
		t.Fatalf("results=%#v err=%v", results, err)
	}
	got := results[0]
	if got.Title != "Demo MCP" || got.RepositoryURL != "https://github.com/example/demo" || got.WebsiteURL != "https://demo.test" || got.Status != "active" || !got.IsLatest || len(got.Packages) != 1 || got.Packages[0].Identifier != "@example/demo" || len(got.Remotes) != 1 || len(got.Icons) != 1 {
		t.Fatalf("metadata was not preserved: %#v", got)
	}
}

func TestProviderTimeoutAndNetworkError(t *testing.T) {
	base := testRegistryServer(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	c := New(base, t.TempDir(), time.Minute)
	c.HTTPClient.Timeout = 20 * time.Millisecond
	if _, err := c.Search(context.Background(), "demo", 30); err == nil || !strings.Contains(err.Error(), "request failed") {
		t.Fatalf("timeout error=%v", err)
	}

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("network unavailable: %v", err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	offline := New("http://"+address, t.TempDir(), time.Minute)
	offline.HTTPClient.Timeout = time.Second
	if _, err := offline.Search(context.Background(), "demo", 30); err == nil || !strings.Contains(err.Error(), "request failed") {
		t.Fatalf("network error=%v", err)
	}
}
