package agent

// OpenAI Responses-API hosted tools (category 1: server-executed). The
// API runs the tool and emits its progress and result as opaque output
// items; the agent loop carries them through ContentBlock.Raw and never
// dispatches them locally.
//
// Each constructor returns a ProviderTool tagged for the Responses provider;
// passing one to the Chat Completions provider (OpenAIProvider) or to
// AnthropicProvider fails at request build time with a clear error.

// OpenAIWebSearch returns the hosted web_search tool. The API performs the
// search and inlines results as web_search_call output items.
func OpenAIWebSearch() ProviderTool {
	return ProviderTool{
		Provider: providerTagOpenAIResponses,
		Raw:      mustMarshal(map[string]any{"type": "web_search"}),
	}
}

// OpenAICodeInterpreterOpts configures the hosted code_interpreter tool.
// Container selects the execution sandbox: "auto" lets OpenAI pick a default
// container, or pass a specific container ID returned from a prior call.
type OpenAICodeInterpreterOpts struct {
	// ContainerID, if set, reuses a specific container. Leave empty to use
	// the auto container ({"type":"auto"}) which OpenAI provisions for you.
	ContainerID string
}

// OpenAICodeInterpreter returns the hosted code_interpreter tool. With no
// opts the API allocates a fresh sandboxed Python container per response.
func OpenAICodeInterpreter(opts OpenAICodeInterpreterOpts) ProviderTool {
	body := map[string]any{"type": "code_interpreter"}
	if opts.ContainerID != "" {
		body["container"] = opts.ContainerID
	} else {
		body["container"] = map[string]any{"type": "auto"}
	}
	return ProviderTool{
		Provider: providerTagOpenAIResponses,
		Raw:      mustMarshal(body),
	}
}

// OpenAIFileSearchOpts configures the hosted file_search tool.
type OpenAIFileSearchOpts struct {
	// VectorStoreIDs lists the OpenAI vector stores to search. Required.
	VectorStoreIDs []string

	// MaxNumResults caps the number of chunks returned. 0 omits the field
	// and lets the API pick a default.
	MaxNumResults int
}

// OpenAIFileSearch returns the hosted file_search tool over the supplied
// OpenAI vector stores. Uploading and indexing files into a vector store
// is out of scope for gocode; use the OpenAI SDK or REST API for that.
func OpenAIFileSearch(opts OpenAIFileSearchOpts) ProviderTool {
	body := map[string]any{
		"type":             "file_search",
		"vector_store_ids": opts.VectorStoreIDs,
	}
	if opts.MaxNumResults > 0 {
		body["max_num_results"] = opts.MaxNumResults
	}
	return ProviderTool{
		Provider: providerTagOpenAIResponses,
		Raw:      mustMarshal(body),
	}
}

// OpenAIImageGeneration returns the hosted image_generation tool. The API
// generates an image and returns it as an image_generation_call output
// item carrying base64-encoded image data.
func OpenAIImageGeneration() ProviderTool {
	return ProviderTool{
		Provider: providerTagOpenAIResponses,
		Raw:      mustMarshal(map[string]any{"type": "image_generation"}),
	}
}
