package openai

import (
	"encoding/json"
	"fmt"

	"github.com/lukemuz/gocode"
)

// OpenAI Responses-API hosted tools (category 1: server-executed). The API
// runs the tool and emits its progress and result as opaque output items;
// the agent loop carries them through gocode.ContentBlock.Raw and never
// dispatches them locally.
//
// Each constructor returns a gocode.ProviderTool tagged for the Responses
// provider; passing one to the Chat Completions Provider or to a different
// vendor fails at request build time with a clear error.

// WebSearch returns the hosted web_search tool. The API performs the search
// and inlines results as web_search_call output items.
func WebSearch() gocode.ProviderTool {
	return gocode.ProviderTool{
		Provider: ResponsesProviderTag,
		Raw:      mustMarshalResponsesTool(map[string]any{"type": "web_search"}),
	}
}

// CodeInterpreterOpts configures the hosted code_interpreter tool. Container
// selects the execution sandbox: leave ContainerID empty to use the auto
// container (OpenAI provisions a fresh sandbox per response), or pass a
// specific container ID returned from a prior call.
type CodeInterpreterOpts struct {
	ContainerID string
}

// CodeInterpreter returns the hosted code_interpreter tool. With no opts the
// API allocates a fresh sandboxed Python container per response.
func CodeInterpreter(opts CodeInterpreterOpts) gocode.ProviderTool {
	body := map[string]any{"type": "code_interpreter"}
	if opts.ContainerID != "" {
		body["container"] = opts.ContainerID
	} else {
		body["container"] = map[string]any{"type": "auto"}
	}
	return gocode.ProviderTool{
		Provider: ResponsesProviderTag,
		Raw:      mustMarshalResponsesTool(body),
	}
}

// FileSearchOpts configures the hosted file_search tool.
type FileSearchOpts struct {
	// VectorStoreIDs lists the OpenAI vector stores to search. Required.
	VectorStoreIDs []string

	// MaxNumResults caps the number of chunks returned. 0 omits the field
	// and lets the API pick a default.
	MaxNumResults int
}

// FileSearch returns the hosted file_search tool over the supplied OpenAI
// vector stores. Uploading and indexing files into a vector store is out of
// scope for gocode; use the OpenAI SDK or REST API for that.
func FileSearch(opts FileSearchOpts) gocode.ProviderTool {
	body := map[string]any{
		"type":             "file_search",
		"vector_store_ids": opts.VectorStoreIDs,
	}
	if opts.MaxNumResults > 0 {
		body["max_num_results"] = opts.MaxNumResults
	}
	return gocode.ProviderTool{
		Provider: ResponsesProviderTag,
		Raw:      mustMarshalResponsesTool(body),
	}
}

// ImageGeneration returns the hosted image_generation tool. The API generates
// an image and returns it as an image_generation_call output item carrying
// base64-encoded image data.
func ImageGeneration() gocode.ProviderTool {
	return gocode.ProviderTool{
		Provider: ResponsesProviderTag,
		Raw:      mustMarshalResponsesTool(map[string]any{"type": "image_generation"}),
	}
}

// mustMarshalResponsesTool panics on json.Marshal error. Inputs are plain
// maps; marshal cannot fail in practice.
func mustMarshalResponsesTool(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Errorf("gocode: openai-responses: marshal provider tool: %w", err))
	}
	return b
}
