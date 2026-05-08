package openrouter

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lukemuz/gocode"
)

func TestWebSearchEmptyOptsOmitsParameters(t *testing.T) {
	pt := WebSearch(WebSearchOpts{})
	if pt.Provider != ProviderTag {
		t.Errorf("Provider = %q, want %q", pt.Provider, ProviderTag)
	}
	var got map[string]any
	if err := json.Unmarshal(pt.Raw, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["type"] != "openrouter:web_search" {
		t.Errorf("type = %v, want openrouter:web_search", got["type"])
	}
	if _, ok := got["parameters"]; ok {
		t.Errorf("parameters should be omitted for empty opts, got %v", got)
	}
}

func TestWebSearchPopulatesParameters(t *testing.T) {
	pt := WebSearch(WebSearchOpts{Engine: "exa", MaxResults: 3, SearchPrompt: "be brief"})
	var got map[string]any
	if err := json.Unmarshal(pt.Raw, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	params, ok := got["parameters"].(map[string]any)
	if !ok {
		t.Fatalf("parameters missing or wrong type: %v", got)
	}
	if params["engine"] != "exa" {
		t.Errorf("engine = %v, want exa", params["engine"])
	}
	if params["max_results"].(float64) != 3 {
		t.Errorf("max_results = %v, want 3", params["max_results"])
	}
	if params["search_prompt"] != "be brief" {
		t.Errorf("search_prompt = %v, want 'be brief'", params["search_prompt"])
	}
}

func TestProviderSplicesWebSearchIntoRequest(t *testing.T) {
	srv, cap := newCaptureServer(t)
	p := newProviderForTest(t, srv.URL)

	_, err := p.Call(context.Background(), gocode.ProviderRequest{
		Model:    "anthropic/claude-test",
		Messages: []gocode.Message{gocode.NewUserMessage("what's new in go 1.24?")},
		ProviderTools: []gocode.ProviderTool{
			WebSearch(WebSearchOpts{MaxResults: 5}),
		},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	var sent map[string]any
	if err := json.Unmarshal(cap.body, &sent); err != nil {
		t.Fatalf("decode captured body: %v", err)
	}
	tools, ok := sent["tools"].([]any)
	if !ok {
		t.Fatalf("tools field missing or wrong type: %v", sent["tools"])
	}
	if len(tools) != 1 {
		t.Fatalf("got %d tools, want 1", len(tools))
	}
	tool := tools[0].(map[string]any)
	if tool["type"] != "openrouter:web_search" {
		t.Errorf("tool type = %v, want openrouter:web_search", tool["type"])
	}
	params, ok := tool["parameters"].(map[string]any)
	if !ok {
		t.Fatalf("parameters missing on wire entry: %v", tool)
	}
	if params["max_results"].(float64) != 5 {
		t.Errorf("max_results on wire = %v, want 5", params["max_results"])
	}
}

func TestProviderRejectsForeignProviderTag(t *testing.T) {
	srv, _ := newCaptureServer(t)
	p := newProviderForTest(t, srv.URL)

	_, err := p.Call(context.Background(), gocode.ProviderRequest{
		Model:    "anthropic/claude-test",
		Messages: []gocode.Message{gocode.NewUserMessage("hi")},
		ProviderTools: []gocode.ProviderTool{{
			Provider: "anthropic",
			Raw:      json.RawMessage(`{"type":"web_search_20260209","name":"web_search"}`),
		}},
	})
	if err == nil {
		t.Fatal("expected error for foreign provider tag, got nil")
	}
	if !strings.Contains(err.Error(), "openrouter") || !strings.Contains(err.Error(), "anthropic") {
		t.Errorf("error message should name both providers, got %q", err.Error())
	}
}
