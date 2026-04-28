package agent

import (
	"context"
	"fmt"
	"net/http"
)

// Config holds everything needed to connect to the Anthropic API.
type Config struct {
	APIKey     string       // required
	Model      string       // required; use ModelSonnet, ModelOpus, or ModelHaiku
	MaxTokens  int          // max tokens per response; defaults to 1024
	BaseURL    string       // API endpoint; defaults to https://api.anthropic.com
	HTTPClient *http.Client // defaults to a 60-second timeout client
}

// Well-known model identifiers.
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
// Returns an error if APIKey or Model is empty.
func New(cfg Config) (*Client, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("agent: Config.APIKey is required")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("agent: Config.Model is required")
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = defaultMaxTokens
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &Client{cfg: cfg}, nil
}

// Ask makes a single LLM call and returns the model's reply as a Message.
//
// system sets the system prompt; pass "" to omit it.
// history is the conversation so far and is not modified by Ask.
// Append the returned Message to your history slice to continue the conversation.
func (c *Client) Ask(ctx context.Context, system string, history []Message) (Message, error) {
	resp, err := c.do(ctx, anthropicRequest{
		Model:     c.cfg.Model,
		MaxTokens: c.cfg.MaxTokens,
		System:    system,
		Messages:  history,
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
		resp, err := c.do(ctx, anthropicRequest{
			Model:     c.cfg.Model,
			MaxTokens: c.cfg.MaxTokens,
			System:    system,
			Messages:  msgs,
			Tools:     tools,
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

// runTools executes all tool_use blocks in content and returns their results.
// Individual tool errors become is_error=true results so the model can see
// and recover from them. Only a missing-tool lookup causes a hard abort.
func runTools(ctx context.Context, content []ContentBlock, dispatch map[string]ToolFunc) ([]ToolResult, error) {
	uses := extractToolUses(content)
	results := make([]ToolResult, len(uses))
	for i, use := range uses {
		fn, ok := dispatch[use.Name]
		if !ok {
			return nil, &ToolError{ToolName: use.Name, ToolUseID: use.ID, Cause: ErrMissingTool}
		}
		output, err := fn(ctx, use.Input)
		results[i] = ToolResult{ToolUseID: use.ID}
		if err != nil {
			results[i].Content = err.Error()
			results[i].IsError = true
		} else {
			results[i].Content = output
		}
	}
	return results, nil
}
