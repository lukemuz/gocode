package main

// Repro harness for the "explore subagent errors out" report.
//
// Builds the same explore subagent stack as main.go (workspace + clock +
// restricted bash + web_fetch + batch fan-out, MaxIter 50) but points the
// underlying client at a stub HTTP server that mimics OpenRouter's chat
// completions endpoint. Each scenario forces a different shape of provider
// response so we can see which one triggers the error path the user is
// seeing.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lukemuz/gocode"
	"github.com/lukemuz/gocode/providers/openrouter"
	"github.com/lukemuz/gocode/tools/bash"
	"github.com/lukemuz/gocode/tools/batch"
	"github.com/lukemuz/gocode/tools/clock"
	"github.com/lukemuz/gocode/tools/subagent"
	"github.com/lukemuz/gocode/tools/web"
	"github.com/lukemuz/gocode/tools/workspace"
)

// scriptedServer replays the supplied JSON response bodies in order, one per
// HTTP POST, returning HTTP 200 with the body. After the script is exhausted
// every further request returns 500.
type scriptedServer struct {
	t        *testing.T
	bodies   []string
	requests []json.RawMessage
	calls    int
}

func newScriptedServer(t *testing.T, bodies ...string) *httptest.Server {
	t.Helper()
	s := &scriptedServer{t: t, bodies: bodies}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		s.requests = append(s.requests, raw)
		if s.calls >= len(s.bodies) {
			http.Error(w, "script exhausted", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(s.bodies[s.calls]))
		s.calls++
	}))
	t.Cleanup(srv.Close)
	return srv
}

// buildExploreBinding mirrors the setup in main.go for the explore subagent.
// dir is the workspace root for the subagent's filesystem-touching tools.
// baseURL is the base URL of the stub HTTP server (no /api/v1/... suffix —
// openrouter.Provider appends that itself).
func buildExploreBinding(t *testing.T, dir, baseURL string) gocode.ToolBinding {
	t.Helper()

	provider, err := openrouter.NewProvider(openrouter.Config{
		APIKey:  "test-key",
		BaseURL: baseURL,
	})
	if err != nil {
		t.Fatalf("openrouter.NewProvider: %v", err)
	}

	mainClient, err := gocode.New(gocode.Config{
		Provider:    provider,
		Model:       "x-ai/grok-4.3",
		MaxTokens:   8192,
		SystemCache: &gocode.CacheControl{Type: "ephemeral"},
	})
	if err != nil {
		t.Fatalf("gocode.New: %v", err)
	}

	ws, err := workspace.NewReadOnly(workspace.Config{Root: dir})
	if err != nil {
		t.Fatalf("workspace.NewReadOnly: %v", err)
	}
	clk := clock.New()
	roMiddleware := []gocode.Middleware{
		gocode.WithTimeout(60 * time.Second),
		gocode.WithResultLimit(64 * 1024),
	}
	roTools := gocode.MustJoin(ws.Toolset(), clk.Toolset()).Wrap(roMiddleware...)

	subBashTool, err := bash.New(bash.Config{Root: dir, Mode: bash.ModeRestricted})
	if err != nil {
		t.Fatalf("bash.New: %v", err)
	}
	subBashToolset := subBashTool.Toolset().Wrap(roMiddleware...)

	webTools := web.New(web.Config{}).Toolset().Wrap(
		gocode.WithTimeout(30*time.Second),
		gocode.WithResultLimit(64*1024),
	)

	roBatchBinding := batch.New(batch.Config{
		Bindings: append(append(append([]gocode.ToolBinding{}, roTools.Bindings...),
			subBashToolset.Bindings...), webTools.Bindings...),
		MaxParallel: 8,
	})

	exploreTools := gocode.MustJoin(roTools, subBashToolset, webTools, gocode.Tools(roBatchBinding)).
		CacheLast(gocode.Ephemeral())

	exploreClient := mainClient.WithModel("openai/gpt-oss-120b")
	exploreBinding, err := subagent.New(subagent.Config{
		Name:        "explore",
		Description: "explore subagent",
		Client:      exploreClient,
		System:      exploreSystemPrompt,
		Tools:       exploreTools,
		MaxIter:     50,
	})
	if err != nil {
		t.Fatalf("subagent.New: %v", err)
	}
	return exploreBinding
}

// invokeExplore drives the subagent tool function directly, the way Loop's
// dispatch would. Returns the tool result string and the error the subagent
// surfaced (which is what gets converted to is_error=true for the parent
// agent).
func invokeExplore(t *testing.T, b gocode.ToolBinding, task string) (string, error) {
	t.Helper()
	in, _ := json.Marshal(map[string]string{"task": task})
	return b.Func(context.Background(), in)
}

// --- scenarios --------------------------------------------------------------

// Scenario A: provider returns finish_reason=stop with empty content (the
// shape a reasoning model would return if its answer ended up in `reasoning`
// and the openai chat-completions decoder dropped it).
func TestExploreSubagent_EmptyContentEndTurn(t *testing.T) {
	srv := newScriptedServer(t,
		`{"choices":[{"message":{"role":"assistant","content":""},"finish_reason":"stop"}],
		  "usage":{"prompt_tokens":10,"completion_tokens":0}}`,
	)
	tmp := t.TempDir()
	bind := buildExploreBinding(t, tmp, srv.URL)

	out, err := invokeExplore(t, bind, "summarise the readme")
	t.Logf("output=%q err=%v", out, err)
	if err == nil {
		t.Fatalf("expected error from empty-content end_turn, got nil (output=%q)", out)
	}
	if !strings.Contains(err.Error(), "returned no text") {
		t.Errorf("expected 'returned no text' error, got: %v", err)
	}
}

// Scenario B: provider returns finish_reason=stop AND no choices at all. The
// canonical loop is supposed to fail with "unexpected stop_reason \"\"".
func TestExploreSubagent_NoChoices(t *testing.T) {
	srv := newScriptedServer(t,
		`{"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":0}}`,
	)
	tmp := t.TempDir()
	bind := buildExploreBinding(t, tmp, srv.URL)

	out, err := invokeExplore(t, bind, "do a thing")
	t.Logf("output=%q err=%v", out, err)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

// Scenario C: provider does the right thing — emits a tool call, then a final
// text response. This is the success path. It sanity-checks that the wiring
// itself isn't broken and that the failure modes above are model-specific.
func TestExploreSubagent_HappyPath(t *testing.T) {
	srv := newScriptedServer(t,
		`{"choices":[{"message":{"role":"assistant","tool_calls":[
			{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{\"path\":\".\"}"}}
		]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":50,"completion_tokens":10}}`,
		`{"choices":[{"message":{"role":"assistant","content":"the directory contains the file 'readme.txt'"},"finish_reason":"stop"}],
		  "usage":{"prompt_tokens":80,"completion_tokens":15}}`,
	)
	tmp := t.TempDir()
	if err := os.WriteFile(tmp+"/readme.txt", []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	bind := buildExploreBinding(t, tmp, srv.URL)

	out, err := invokeExplore(t, bind, "what files are here?")
	t.Logf("output=%q err=%v", out, err)
	if err != nil {
		t.Fatalf("happy path returned error: %v", err)
	}
	if !strings.Contains(out, "readme.txt") {
		t.Errorf("expected output to mention readme.txt, got: %q", out)
	}
}

// Scenario D-pre: provider returns the harmony shape gpt-oss-120b is observed
// to use on OpenRouter for its final turn — content:null with the answer in a
// "reasoning" field. The chat-completions decoder only reads "content", so the
// canonical text comes back empty and the subagent surfaces "returned no
// text". This matches the symptom reported by the user verbatim.
func TestExploreSubagent_ReasoningOnlyFinalTurn(t *testing.T) {
	srv := newScriptedServer(t,
		// First turn: tool call (works fine — reasoning is ignored, tool_calls drive the loop).
		`{"choices":[{"message":{"role":"assistant","content":null,"reasoning":"I should list the dir.",
			"tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{\"path\":\".\"}"}}]
		},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":50,"completion_tokens":10}}`,
		// Final turn: the model puts its answer in "reasoning" with content=null.
		`{"choices":[{"message":{"role":"assistant","content":null,
			"reasoning":"the directory contains readme.txt"
		},"finish_reason":"stop"}],"usage":{"prompt_tokens":80,"completion_tokens":15}}`,
	)
	tmp := t.TempDir()
	if err := os.WriteFile(tmp+"/readme.txt", []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	bind := buildExploreBinding(t, tmp, srv.URL)

	out, err := invokeExplore(t, bind, "what files are here?")
	t.Logf("output=%q err=%v", out, err)
	if err == nil {
		t.Fatalf("expected 'returned no text', got success: %q", out)
	}
	if !strings.Contains(err.Error(), "returned no text") {
		t.Errorf("expected 'returned no text', got: %v", err)
	}
}

// Scenario D: provider returns 4xx — the error the user would see if the
// model id is wrong, the API key is invalid, or the request body is rejected.
// Helpful for distinguishing config errors from logic errors.
func TestExploreSubagent_ProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"model not found","type":"invalid_request_error"}}`))
	}))
	defer srv.Close()
	tmp := t.TempDir()
	bind := buildExploreBinding(t, tmp, srv.URL)

	out, err := invokeExplore(t, bind, "anything")
	t.Logf("output=%q err=%v", out, err)
	if err == nil {
		t.Fatalf("expected provider error, got nil")
	}
}
