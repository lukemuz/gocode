package agent

import (
	"context"
	"fmt"
)

// Config holds everything needed to create a Client.
type Config struct {
	Provider  Provider    // required — the LLM backend to use
	Model     string      // required — provider-specific model identifier
	MaxTokens int         // max tokens per response; defaults to 1024
	Retry     RetryConfig // controls automatic retry behaviour for transient API errors
}

// Well-known Anthropic model identifiers.
const (
	ModelOpus   = "claude-opus-4-7"
	ModelSonnet = "claude-sonnet-4-6"
	ModelHaiku  = "claude-haiku-4-5-20251001"
)

// Client is a stateless API facade. It holds configuration but no conversation
// state — history is owned by the caller. The same Client is safe for
// concurrent use across goroutines.
type Client struct {
	cfg Config
}

// New creates a Client from cfg, filling in defaults for zero-value fields.
// Returns an error if Provider or Model is empty.
func New(cfg Config) (*Client, error) {
	if cfg.Provider == nil {
		return nil, fmt.Errorf("agent: Config.Provider is required")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("agent: Config.Model is required")
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = defaultMaxTokens
	}
	return &Client{cfg: cfg}, nil
}

// Ask makes a single LLM call and returns the model's reply as a Message.
//
// system sets the system prompt; pass "" to omit it.
// history is the conversation so far and is not modified by Ask.
// Append the returned Message to your history slice to continue the conversation.
func (c *Client) Ask(ctx context.Context, system string, history []Message) (Message, error) {
	req := ProviderRequest{
		Model:     c.cfg.Model,
		MaxTokens: c.cfg.MaxTokens,
		System:    system,
		Messages:  history,
	}
	resp, err := callWithRetry(ctx, c.cfg.Retry, func() (ProviderResponse, error) {
		return c.cfg.Provider.Call(ctx, req)
	})
	if err != nil {
		return Message{}, err
	}
	return Message{Role: RoleAssistant, Content: resp.Content}, nil
}

// AskStream is the streaming variant of Ask. It invokes the onToken
// callback for every ContentBlock delta delivered by the provider
// (typically incremental TypeText blocks). The final assembled
// Message is returned once the stream completes. history is not
// modified by this call.
//
// Retry interaction: callWithRetry wraps the stream call, so onToken may fire
// for partial content on a failed attempt before a successful retry begins.
// Use StreamBuffer with RetryConfig.OnRetry to react to retries and clear
// partial output before the next attempt starts.
//
// onToken may be nil, in which case token deltas are discarded.
func (c *Client) AskStream(ctx context.Context, system string, history []Message, onToken func(ContentBlock)) (Message, error) {
	if onToken == nil {
		onToken = func(ContentBlock) {}
	}
	req := ProviderRequest{
		Model:     c.cfg.Model,
		MaxTokens: c.cfg.MaxTokens,
		System:    system,
		Messages:  history,
	}
	resp, err := callWithRetry(ctx, c.cfg.Retry, func() (ProviderResponse, error) {
		return c.cfg.Provider.Stream(ctx, req, onToken)
	})
	if err != nil {
		return Message{}, err
	}
	return Message{Role: RoleAssistant, Content: resp.Content}, nil
}

// LoopResult is returned by Loop and carries the complete updated history
// together with aggregate token usage across all API calls in the run.
type LoopResult struct {
	Messages []Message // full conversation: original history + all new turns
	Usage    Usage     // total tokens consumed across all iterations
}

// Loop runs the agent in a tool-use loop until the model signals end_turn or
// an error occurs. It returns the full conversation including all new turns.
//
// tools is the list of tools advertised to the model on every call.
// dispatch maps each tool name to its Go implementation. A tool name that
// appears in a model response but is absent from dispatch causes an immediate
// LoopError wrapping ErrMissingTool.
// maxIter caps the total number of API calls; 0 means no limit.
func (c *Client) Loop(
	ctx context.Context,
	system string,
	history []Message,
	tools []Tool,
	dispatch map[string]ToolFunc,
	maxIter int,
) (LoopResult, error) {
	msgs := make([]Message, len(history))
	copy(msgs, history)
	var total Usage

	for iter := 0; maxIter == 0 || iter < maxIter; iter++ {
		req := ProviderRequest{
			Model:     c.cfg.Model,
			MaxTokens: c.cfg.MaxTokens,
			System:    system,
			Messages:  msgs,
			Tools:     tools,
		}
		resp, err := callWithRetry(ctx, c.cfg.Retry, func() (ProviderResponse, error) {
			return c.cfg.Provider.Call(ctx, req)
		})
		if err != nil {
			return LoopResult{Messages: msgs, Usage: total}, &LoopError{Iter: iter, Cause: err}
		}
		total.InputTokens += resp.Usage.InputTokens
		total.OutputTokens += resp.Usage.OutputTokens
		msgs = append(msgs, Message{Role: RoleAssistant, Content: resp.Content})

		switch resp.StopReason {
		case "end_turn":
			return LoopResult{Messages: msgs, Usage: total}, nil

		case "tool_use":
			results, err := runTools(ctx, resp.Content, dispatch)
			if err != nil {
				return LoopResult{Messages: msgs, Usage: total}, &LoopError{Iter: iter, Cause: err}
			}
			msgs = append(msgs, NewToolResultMessage(results))

		case "max_tokens":
			return LoopResult{Messages: msgs, Usage: total}, &LoopError{
				Iter:  iter,
				Cause: fmt.Errorf("model hit max_tokens limit; increase Config.MaxTokens"),
			}

		default:
			return LoopResult{Messages: msgs, Usage: total}, &LoopError{
				Iter:  iter,
				Cause: fmt.Errorf("unexpected stop_reason %q", resp.StopReason),
			}
		}
	}
	return LoopResult{Messages: msgs, Usage: total}, &LoopError{Iter: maxIter, Cause: ErrMaxIter}
}

// LoopStream is the streaming variant of Loop. It mirrors Loop's structure,
// control flow, error handling, stop-reason switching, maxIter limiting, and
// history/usage accumulation but invokes Provider.Stream (wrapped by
// callWithRetry) on every iteration so that onToken receives each
// ContentBlock delta as it arrives. After runTools completes, onToolResult
// is called with the results (allowing live UI updates or logging) before
// the tool results are appended and the loop continues.
//
// Retry interaction: onToken may fire multiple times for a given turn if retries
// occur. Use StreamBuffer with RetryConfig.OnRetry to react to retries and
// clear partial output before the next attempt starts.
//
// Both callbacks may be nil, in which case their respective events are discarded.
func (c *Client) LoopStream(
	ctx context.Context,
	system string,
	history []Message,
	tools []Tool,
	dispatch map[string]ToolFunc,
	maxIter int,
	onToken func(ContentBlock),
	onToolResult func([]ToolResult),
) (LoopResult, error) {
	if onToken == nil {
		onToken = func(ContentBlock) {}
	}
	if onToolResult == nil {
		onToolResult = func([]ToolResult) {}
	}
	msgs := make([]Message, len(history))
	copy(msgs, history)
	var total Usage

	for iter := 0; maxIter == 0 || iter < maxIter; iter++ {
		req := ProviderRequest{
			Model:     c.cfg.Model,
			MaxTokens: c.cfg.MaxTokens,
			System:    system,
			Messages:  msgs,
			Tools:     tools,
		}
		resp, err := callWithRetry(ctx, c.cfg.Retry, func() (ProviderResponse, error) {
			return c.cfg.Provider.Stream(ctx, req, onToken)
		})
		if err != nil {
			return LoopResult{Messages: msgs, Usage: total}, &LoopError{Iter: iter, Cause: err}
		}
		total.InputTokens += resp.Usage.InputTokens
		total.OutputTokens += resp.Usage.OutputTokens
		msgs = append(msgs, Message{Role: RoleAssistant, Content: resp.Content})

		switch resp.StopReason {
		case "end_turn":
			return LoopResult{Messages: msgs, Usage: total}, nil

		case "tool_use":
			results, err := runTools(ctx, resp.Content, dispatch)
			if err != nil {
				return LoopResult{Messages: msgs, Usage: total}, &LoopError{Iter: iter, Cause: err}
			}
			onToolResult(results)
			msgs = append(msgs, NewToolResultMessage(results))

		case "max_tokens":
			return LoopResult{Messages: msgs, Usage: total}, &LoopError{
				Iter:  iter,
				Cause: fmt.Errorf("model hit max_tokens limit; increase Config.MaxTokens"),
			}

		default:
			return LoopResult{Messages: msgs, Usage: total}, &LoopError{
				Iter:  iter,
				Cause: fmt.Errorf("unexpected stop_reason %q", resp.StopReason),
			}
		}
	}
	return LoopResult{Messages: msgs, Usage: total}, &LoopError{Iter: maxIter, Cause: ErrMaxIter}
}

// runTools executes all tool_use blocks in content concurrently and returns
// their results in the original order. Individual tool errors become
// is_error=true results so the model can see and recover from them. A missing
// tool is a programming error (wrong dispatch map) and aborts before any
// goroutines are spawned.
func runTools(ctx context.Context, content []ContentBlock, dispatch map[string]ToolFunc) ([]ToolResult, error) {
	uses := extractToolUses(content)

	// Validate all names up front so a missing tool aborts immediately rather
	// than after other calls have already started.
	for _, use := range uses {
		if _, ok := dispatch[use.Name]; !ok {
			return nil, &ToolError{ToolName: use.Name, ToolUseID: use.ID, Cause: ErrMissingTool}
		}
	}

	type indexed struct {
		i      int
		result ToolResult
	}
	ch := make(chan indexed, len(uses))
	for i, use := range uses {
		i, use := i, use
		go func() {
			output, err := dispatch[use.Name](ctx, use.Input)
			r := ToolResult{ToolUseID: use.ID}
			if err != nil {
				r.Content = err.Error()
				r.IsError = true
			} else {
				r.Content = output
			}
			ch <- indexed{i: i, result: r}
		}()
	}
	results := make([]ToolResult, len(uses))
	for range uses {
		it := <-ch
		results[it.i] = it.result
	}
	return results, nil
}
