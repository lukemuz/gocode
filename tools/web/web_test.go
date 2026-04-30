package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetchHTMLConvertsToText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><head><style>body{color:red}</style><script>alert(1)</script></head><body><h1>Title</h1><p>Hello &amp; goodbye.</p><p>Second &lt;para&gt;.</p></body></html>`))
	}))
	defer srv.Close()

	f := New(Config{})
	out, err := f.handle(context.Background(), fetchInput{URL: srv.URL})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if strings.Contains(out, "alert(1)") {
		t.Errorf("script body leaked into output: %s", out)
	}
	if strings.Contains(out, "color:red") {
		t.Errorf("style body leaked into output: %s", out)
	}
	if !strings.Contains(out, "Title") || !strings.Contains(out, "Hello & goodbye.") {
		t.Errorf("expected prose preserved, got: %s", out)
	}
	if !strings.Contains(out, "Second <para>.") {
		t.Errorf("expected entity-decoded text, got: %s", out)
	}
	if !strings.Contains(out, "Status: 200") {
		t.Errorf("expected status header, got: %s", out)
	}
}

func TestFetchRawSkipsConversion(t *testing.T) {
	body := `{"hello":"world"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	f := New(Config{})
	out, err := f.handle(context.Background(), fetchInput{URL: srv.URL})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if !strings.Contains(out, body) {
		t.Errorf("expected raw JSON in output, got: %s", out)
	}
}

func TestFetchPagination(t *testing.T) {
	// 30 KiB plain-text body so we definitely overflow max_length.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(strings.Repeat("abcdefghij", 3000)))
	}))
	defer srv.Close()

	f := New(Config{})
	out, err := f.handle(context.Background(), fetchInput{URL: srv.URL, MaxLength: 100})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if !strings.Contains(out, "start_index=100") {
		t.Errorf("expected pagination hint pointing at next chunk, got: %s", out)
	}

	out2, err := f.handle(context.Background(), fetchInput{URL: srv.URL, MaxLength: 100, StartIndex: 100})
	if err != nil {
		t.Fatalf("handle pg2: %v", err)
	}
	if !strings.Contains(out2, "start_index=200") {
		t.Errorf("expected pagination hint advancing, got: %s", out2)
	}
}

func TestFetchRejectsNonHTTP(t *testing.T) {
	f := New(Config{})
	_, err := f.handle(context.Background(), fetchInput{URL: "file:///etc/passwd"})
	if err == nil {
		t.Fatal("expected rejection of file:// scheme")
	}
	if !strings.Contains(err.Error(), "http") {
		t.Errorf("expected scheme-related error, got: %v", err)
	}
}

func TestFetchRejectsBadURL(t *testing.T) {
	f := New(Config{})
	if _, err := f.handle(context.Background(), fetchInput{URL: ""}); err == nil {
		t.Fatal("expected error for empty url")
	}
	if _, err := f.handle(context.Background(), fetchInput{URL: "://nope"}); err == nil {
		t.Fatal("expected error for malformed url")
	}
}

func TestFetchSurfacesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("nope"))
	}))
	defer srv.Close()

	f := New(Config{})
	_, err := f.handle(context.Background(), fetchInput{URL: srv.URL})
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 in error, got: %v", err)
	}
}

func TestFetchTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	f := New(Config{Timeout: 30 * time.Millisecond})
	_, err := f.handle(context.Background(), fetchInput{URL: srv.URL})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestFetchRedirectLimit(t *testing.T) {
	// Server that redirects to itself indefinitely.
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/loop", http.StatusFound)
	}))
	defer srv.Close()

	f := New(Config{MaxRedirects: 2})
	_, err := f.handle(context.Background(), fetchInput{URL: srv.URL})
	if err == nil {
		t.Fatal("expected redirect-limit error")
	}
	if !strings.Contains(err.Error(), "redirect") {
		t.Errorf("expected redirect-related error, got: %v", err)
	}
}

func TestToolsetRegistersWebFetch(t *testing.T) {
	f := New(Config{})
	ts := f.Toolset()
	if len(ts.Bindings) != 1 {
		t.Fatalf("expected 1 binding, got %d", len(ts.Bindings))
	}
	if ts.Bindings[0].Tool.Name != "web_fetch" {
		t.Errorf("expected tool name web_fetch, got %s", ts.Bindings[0].Tool.Name)
	}
	// Ensure schema marshals cleanly.
	if _, err := json.Marshal(ts.Bindings[0].Tool); err != nil {
		t.Errorf("tool marshal failed: %v", err)
	}
}
