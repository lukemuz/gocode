package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"
)

type mockProvider struct{}

var testResponse = ProviderResponse{
	Content:    []ContentBlock{{Type: TypeText, Text: "Hello from mock"}},
	StopReason: "end_turn",
	Usage:      Usage{InputTokens: 10, OutputTokens: 5},
}

// testProvider is a configurable mock implementing Provider. Fields control
// responses, errors, and spies for callbacks. Call/StreamCount and DeltaSpy
// allow verifying calls and deltas without side effects on real providers.
// Use newTestProvider() for defaults or set fields directly in tests.
type testProvider struct {
	Resp            ProviderResponse
	Err             error
	Deltas          []ContentBlock
	DeltaSpy        func(ContentBlock)
	CallCount       int
	StreamCount     int
	OnToolResultSpy func([]ToolResult) // spy for LoopStream (not part of Provider)
}

func newTestProvider() *testProvider {
	return &testProvider{
		Resp: testResponse,
	}
}

func (p *testProvider) Call(ctx context.Context, req ProviderRequest) (ProviderResponse, error) {
	p.CallCount++
	if p.Err != nil {
		return ProviderResponse{}, p.Err
	}
	return p.Resp, nil
}

func (p *testProvider) Stream(ctx context.Context, req ProviderRequest, onDelta func(ContentBlock)) (ProviderResponse, error) {
	p.StreamCount++
	if p.DeltaSpy != nil {
		for _, d := range p.Deltas {
			p.DeltaSpy(d)
			onDelta(d)
		}
	} else {
		for _, d := range p.Deltas {
			onDelta(d)
		}
	}
	if p.Err != nil {
		return ProviderResponse{}, p.Err
	}
	return p.Resp, nil
}

// test helpers (package level so methods are allowed)
type temporaryError struct {
	error
}

func (temporaryError) Temporary() bool { return true }

type nonTemporary struct {
	error
}

func (nonTemporary) Temporary() bool { return false }

func TestNew(t *testing.T) {
	t.Run("requires provider", func(t *testing.T) {
		_, err := New(Config{Model: "claude-sonnet-4-6"})
		if err == nil {
			t.Fatal("expected error for missing Provider")
		}
		if !strings.Contains(err.Error(), "Config.Provider is required") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("requires model", func(t *testing.T) {
		_, err := New(Config{Provider: newTestProvider()})
		if err == nil {
			t.Fatal("expected error for missing Model")
		}
		if !strings.Contains(err.Error(), "Config.Model is required") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("sets default MaxTokens", func(t *testing.T) {
		c, err := New(Config{
			Provider: newTestProvider(),
			Model:    "test-model",
		})
		if err != nil {
			t.Fatal(err)
		}
		if c.cfg.MaxTokens != defaultMaxTokens {
			t.Errorf("expected defaultMaxTokens %d, got %d", defaultMaxTokens, c.cfg.MaxTokens)
		}
	})
}

func TestTextContent(t *testing.T) {
	msg := Message{
		Role: RoleUser,
		Content: []ContentBlock{
			{Type: TypeText, Text: "Hello "},
			{Type: TypeToolUse, ID: "tu_1", Name: "calc"},
			{Type: TypeText, Text: "world!"},
			{Type: TypeToolResult, Content: "ignored"},
		},
	}
	if got := TextContent(msg); got != "Hello world!" {
		t.Errorf("TextContent() = %q, want %q", got, "Hello world!")
	}

	msg2 := NewUserMessage("plain text message")
	if got := TextContent(msg2); got != "plain text message" {
		t.Errorf("TextContent(NewUserMessage) = %q, want %q", got, "plain text message")
	}
}

func TestNewUserMessage(t *testing.T) {
	msg := NewUserMessage("hello from test")
	if msg.Role != RoleUser {
		t.Errorf("expected RoleUser, got %q", msg.Role)
	}
	if len(msg.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(msg.Content))
	}
	block := msg.Content[0]
	if block.Type != TypeText || block.Text != "hello from test" {
		t.Errorf("unexpected content block: %+v", block)
	}
}

func TestNewToolResultMessage(t *testing.T) {
	results := []ToolResult{
		{ToolUseID: "tu_1", Content: "success result"},
		{ToolUseID: "tu_2", Content: "something went wrong", IsError: true},
	}
	msg := NewToolResultMessage(results)

	if msg.Role != RoleUser {
		t.Errorf("expected RoleUser, got %q", msg.Role)
	}
	if len(msg.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(msg.Content))
	}

	b1 := msg.Content[0]
	if b1.Type != TypeToolResult || b1.ToolUseID != "tu_1" || b1.Content != "success result" || b1.IsError {
		t.Errorf("bad first block: %+v", b1)
	}

	b2 := msg.Content[1]
	if b2.Type != TypeToolResult || b2.ToolUseID != "tu_2" || b2.Content != "something went wrong" || !b2.IsError {
		t.Errorf("bad second block: %+v", b2)
	}
}

func TestNewTool(t *testing.T) {
	schema := Object(
		String("a", "first param", Required()),
		Integer("b", ""),
	)

	tool, err := NewTool("test_tool", "A tool for testing", schema)
	if err != nil {
		t.Fatal(err)
	}

	if tool.Name != "test_tool" || tool.Description != "A tool for testing" {
		t.Errorf("metadata mismatch: %+v", tool)
	}
	if len(tool.InputSchema) == 0 {
		t.Error("InputSchema was not populated by json.Marshal")
	}

	// Verify it round-trips reasonably
	var parsed InputSchema
	if err := json.Unmarshal(tool.InputSchema, &parsed); err != nil {
		t.Errorf("schema did not contain valid JSON: %v", err)
	}
	if parsed.Type != "object" || len(parsed.Properties) != 2 {
		t.Errorf("parsed schema invalid: %+v", parsed)
	}
}

func TestTypedToolFunc(t *testing.T) {
	type input struct {
		Op string `json:"op"`
		Val int    `json:"val"`
	}
	fn := TypedToolFunc(func(_ context.Context, in input) (string, error) {
		if in.Op != "echo" {
			return "", fmt.Errorf("unknown op %s", in.Op)
		}
		return fmt.Sprintf("echo:%d", in.Val), nil
	})

	ctx := context.Background()
	t.Run("success", func(t *testing.T) {
		out, err := fn(ctx, json.RawMessage(`{"op":"echo","val":42}`))
		if err != nil {
			t.Fatal(err)
		}
		if out != "echo:42" {
			t.Errorf("got %q", out)
		}
	})
	t.Run("unmarshal fails", func(t *testing.T) {
		_, err := fn(ctx, json.RawMessage(`{"op":"echo","val":"bad"}`))
		if err == nil || !strings.Contains(err.Error(), "unmarshal tool input") {
			t.Errorf("expected unmarshal error, got %v", err)
		}
	})
	t.Run("handler fails", func(t *testing.T) {
		_, err := fn(ctx, json.RawMessage(`{"op":"bad","val":1}`))
		if err == nil || !strings.Contains(err.Error(), "unknown op") {
			t.Errorf("expected handler error, got %v", err)
		}
	})

	t.Run("NewTypedTool", func(t *testing.T) {
		schema := Object(
			String("op", "", Required()),
			Integer("val", "", Required()),
		)
		tool, fn2, err := NewTypedTool[input](
			"echo",
			"echo a value",
			schema,
			func(_ context.Context, in input) (string, error) {
				if in.Op != "echo" {
					return "", fmt.Errorf("unknown op %s", in.Op)
				}
				return fmt.Sprintf("echo:%d", in.Val), nil
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		if tool.Name != "echo" || tool.Description != "echo a value" {
			t.Errorf("bad tool metadata: %+v", tool)
		}
		if len(tool.InputSchema) == 0 {
			t.Error("InputSchema was not populated")
		}
		// verify fn works like the wrapper
		ctx := context.Background()
		out, err := fn2(ctx, json.RawMessage(`{"op":"echo","val":123}`))
		if err != nil {
			t.Fatal(err)
		}
		if out != "echo:123" {
			t.Errorf("got %q, want echo:123", out)
		}
	})
}

func TestJSONResult(t *testing.T) {
	type out struct{ Sum int `json:"sum"` }
	got, err := JSONResult(out{Sum: 7})
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"sum":7}` {
		t.Errorf("got %q", got)
	}
}

func TestErrorTypes(t *testing.T) {
	t.Run("APIError", func(t *testing.T) {
		err := &APIError{
			StatusCode: 429,
			Type:       "rate_limit_error",
			Message:    "too many requests",
			RetryAfter: 10 * time.Second,
		}
		want := `agent: API 429 (rate_limit_error): too many requests (retry after 10s)`
		if got := err.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("ToolError wrapping", func(t *testing.T) {
		cause := errors.New("division by zero")
		err := &ToolError{
			ToolName:  "divide",
			ToolUseID: "tu_42",
			Cause:     cause,
		}
		if !strings.Contains(err.Error(), `tool "divide" (tu_42): division by zero`) {
			t.Errorf("unexpected error string: %v", err)
		}
		if !errors.Is(err, cause) {
			t.Error("ToolError should unwrap to its Cause")
		}
	})

	t.Run("LoopError wrapping", func(t *testing.T) {
		cause := ErrMaxIter
		err := &LoopError{Iter: 10, Cause: cause}
		if !strings.Contains(err.Error(), "loop aborted at iteration 10") {
			t.Errorf("unexpected error string: %v", err)
		}
		if !errors.Is(err, ErrMaxIter) {
			t.Error("LoopError should unwrap to ErrMaxIter")
		}
	})

	t.Run("RetryExhaustedError", func(t *testing.T) {
		cause := errors.New("connection reset")
		err := &RetryExhaustedError{Attempts: 4, Cause: cause}
		if !strings.Contains(err.Error(), "retry exhausted after 4 attempt(s)") {
			t.Errorf("unexpected error string: %v", err)
		}
		if !errors.Is(err, ErrRetryExhausted) {
			t.Error("should match ErrRetryExhausted via Unwrap")
		}
		if !errors.Is(err, cause) {
			t.Error("should unwrap to original cause")
		}
	})
}

func TestRunTools(t *testing.T) {
	ctx := context.Background()
	dispatch := map[string]ToolFunc{
		"echo": func(_ context.Context, input json.RawMessage) (string, error) {
			return string(input), nil
		},
		"fail": func(_ context.Context, _ json.RawMessage) (string, error) {
			return "", fmt.Errorf("tool failure")
		},
	}

	t.Run("successful tools", func(t *testing.T) {
		content := []ContentBlock{
			{
				Type:  TypeToolUse,
				ID:    "tu_1",
				Name:  "echo",
				Input: json.RawMessage(`"hello"`),
			},
		}
		results, err := runTools(ctx, content, dispatch)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 || results[0].ToolUseID != "tu_1" || results[0].Content != `"hello"` || results[0].IsError {
			t.Errorf("unexpected result: %+v", results)
		}
	})

	t.Run("tool errors become is_error results", func(t *testing.T) {
		content := []ContentBlock{
			{
				Type:  TypeToolUse,
				ID:    "tu_2",
				Name:  "fail",
				Input: json.RawMessage(`{}`),
			},
		}
		results, err := runTools(ctx, content, dispatch)
		if err != nil {
			t.Fatal("runTools should not return error for tool execution failures")
		}
		if len(results) != 1 || !results[0].IsError || !strings.Contains(results[0].Content, "tool failure") {
			t.Errorf("expected IsError result: %+v", results[0])
		}
	})

	t.Run("missing tool returns ToolError", func(t *testing.T) {
		content := []ContentBlock{
			{
				Type: TypeToolUse,
				ID:   "tu_3",
				Name: "missing_tool",
			},
		}
		_, err := runTools(ctx, content, dispatch)
		if err == nil {
			t.Fatal("expected error for missing tool")
		}
		var te *ToolError
		if !errors.As(err, &te) {
			t.Fatalf("expected *ToolError, got %T", err)
		}
		if te.ToolName != "missing_tool" || !errors.Is(err, ErrMissingTool) {
			t.Errorf("unexpected ToolError: %+v", te)
		}
	})
}

func TestParallel(t *testing.T) {
	ctx := context.Background()

	steps := []StepFunc[string]{
		func(ctx context.Context) (string, error) {
			return "success1", nil
		},
		func(ctx context.Context) (string, error) {
			return "", errors.New("step failed")
		},
		func(ctx context.Context) (string, error) {
			return "success2", nil
		},
	}

	results := Parallel(ctx, steps...)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	if results[0].Value != "success1" || results[0].Err != nil {
		t.Errorf("unexpected result[0]: %+v", results[0])
	}
	if results[1].Value != "" || results[1].Err == nil || results[1].Err.Error() != "step failed" {
		t.Errorf("unexpected result[1]: %+v", results[1])
	}
	if results[2].Value != "success2" || results[2].Err != nil {
		t.Errorf("unexpected result[2]: %+v", results[2])
	}
}

func TestRetryConfig_applyDefaults(t *testing.T) {
	tests := []struct {
		name string
		input RetryConfig
		want  RetryConfig
	}{
		{
			name:  "all defaults",
			input: RetryConfig{},
			want: RetryConfig{
				MaxRetries:  defaultMaxRetries,
				InitialWait: defaultInitialWait,
				MaxWait:     defaultMaxWait,
				Disabled:    false,
			},
		},
		{
			name: "custom values preserved",
			input: RetryConfig{
				MaxRetries:  10,
				InitialWait: 500 * time.Millisecond,
				MaxWait:     60 * time.Second,
				Disabled:    true,
			},
			want: RetryConfig{
				MaxRetries:  10,
				InitialWait: 500 * time.Millisecond,
				MaxWait:     60 * time.Second,
				Disabled:    true,
			},
		},
		{
			name: "partial override",
			input: RetryConfig{
				MaxRetries: 2,
				Disabled:   true,
			},
			want: RetryConfig{
				MaxRetries:  2,
				InitialWait: defaultInitialWait,
				MaxWait:     defaultMaxWait,
				Disabled:    true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.input.applyDefaults()
			if got != tt.want {
				t.Errorf("applyDefaults() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestIsTemporary(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"temporary error", temporaryError{errors.New("temp")}, true},
		{"wrapped temporary", fmt.Errorf("wrapped: %w", temporaryError{errors.New("temp")}), true},
		{"ordinary error", errors.New("ordinary"), false},
		{"non-temporary interface", nonTemporary{errors.New("no")}, false},
		{"context canceled", context.Canceled, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTemporary(tt.err); got != tt.want {
				t.Errorf("isTemporary(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestClientAsk(t *testing.T) {
	c, err := New(Config{
		Provider: newTestProvider(),
		Model:    "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	history := []Message{NewUserMessage("hello")}
	msg, err := c.Ask(context.Background(), "be helpful", history)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Role != RoleAssistant || TextContent(msg) != "Hello from mock" {
		t.Errorf("unexpected message: role=%s content=%s", msg.Role, TextContent(msg))
	}
}

func TestClientAskStream(t *testing.T) {
	p := newTestProvider()
	p.Deltas = []ContentBlock{{Type: TypeText, Text: "Hello from mock"}}
	c, err := New(Config{Provider: p, Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	var received []ContentBlock
	onToken := func(b ContentBlock) {
		received = append(received, b)
	}
	history := []Message{NewUserMessage("hello")}
	msg, err := c.AskStream(context.Background(), "", history, onToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(received) == 0 || received[0].Text != "Hello from mock" {
		t.Errorf("onToken not called with expected delta: %+v", received)
	}
	if TextContent(msg) != "Hello from mock" {
		t.Errorf("final message content mismatch: %s", TextContent(msg))
	}
	if p.StreamCount != 1 {
		t.Errorf("expected 1 Stream call, got %d", p.StreamCount)
	}
}

func TestClientLoop(t *testing.T) {
	p := newTestProvider()
	c, err := New(Config{Provider: p, Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	history := []Message{NewUserMessage("start conversation")}
	result, err := c.Loop(context.Background(), "system", history, nil, nil, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Messages) != 2 || result.Messages[1].Role != RoleAssistant {
		t.Errorf("expected 1 new assistant message, got %d messages", len(result.Messages))
	}
	if result.Usage.InputTokens != 10 || result.Usage.OutputTokens != 5 {
		t.Errorf("unexpected usage: %+v", result.Usage)
	}
	if p.CallCount != 1 {
		t.Errorf("expected 1 Call, got %d", p.CallCount)
	}
}

func TestClientLoopStream(t *testing.T) {
	p := newTestProvider()
	p.Deltas = []ContentBlock{{Type: TypeText, Text: "Hello from mock"}}
	c, err := New(Config{Provider: p, Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	var tokenCalls int
	onToken := func(ContentBlock) { tokenCalls++ }
	var toolCalls int
	onToolResult := func([]ToolResult) { toolCalls++ }
	history := []Message{NewUserMessage("start")}
	result, err := c.LoopStream(context.Background(), "system", history, nil, nil, 0, onToken, onToolResult)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tokenCalls == 0 {
		t.Error("onToken callback was not invoked")
	}
	if toolCalls != 0 {
		t.Error("onToolResult should not be called on end_turn path")
	}
	if len(result.Messages) != 2 {
		t.Errorf("expected history + assistant msg, got %d", len(result.Messages))
	}
	if p.StreamCount != 1 {
		t.Errorf("expected 1 Stream call, got %d", p.StreamCount)
	}
}

func TestCallWithRetry(t *testing.T) {
	rand.Seed(1) // for deterministic jitter in backoff tests
	ctx := context.Background()

	tests := []struct {
		name          string
		retryCfg      RetryConfig
		fn            func() (ProviderResponse, error)
		wantErr       error
		wantAttempts  int
		wantResp      bool
	}{
		{
			name: "success on first try",
			retryCfg: RetryConfig{},
			fn: func() (ProviderResponse, error) {
				return testResponse, nil
			},
			wantErr:      nil,
			wantAttempts: 1,
			wantResp:     true,
		},
		{
			name: "non-retryable 400 returns immediately",
			retryCfg: RetryConfig{},
			fn: func() (ProviderResponse, error) {
				return ProviderResponse{}, &APIError{StatusCode: 400, Message: "bad request"}
			},
			wantErr:      nil, // wrapped but we check type below
			wantAttempts: 1,
		},
		{
			name: "retryable 429 with exhaustion",
			retryCfg: RetryConfig{MaxRetries: 1},
			fn: func() (ProviderResponse, error) {
				return ProviderResponse{}, &APIError{StatusCode: 429, Message: "rate limited"}
			},
			wantErr:      nil, // will be RetryExhaustedError
			wantAttempts: 2,
		},
		{
			name: "context canceled aborts immediately",
			retryCfg: RetryConfig{},
			fn: func() (ProviderResponse, error) {
				return ProviderResponse{}, context.Canceled
			},
			wantErr: context.Canceled,
		},
		{
			name: "ErrMissingTool never retries",
			retryCfg: RetryConfig{},
			fn: func() (ProviderResponse, error) {
				return ProviderResponse{}, &ToolError{ToolName: "foo", Cause: ErrMissingTool}
			},
			wantErr: ErrMissingTool,
		},
		{
			name: "disabled retry",
			retryCfg: RetryConfig{Disabled: true},
			fn: func() (ProviderResponse, error) {
				return ProviderResponse{}, &APIError{StatusCode: 503}
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var attempts int
			wrappedFn := func() (ProviderResponse, error) {
				attempts++
				return tt.fn()
			}
			resp, err := callWithRetry(ctx, tt.retryCfg, wrappedFn)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("got err %v, want %v", err, tt.wantErr)
				}
				var exhausted *RetryExhaustedError
				if errors.As(err, &exhausted) {
					if exhausted.Attempts != tt.wantAttempts {
						t.Errorf("expected %d attempts, got %d", tt.wantAttempts, exhausted.Attempts)
					}
				}
			} else if err != nil {
				t.Errorf("unexpected err: %v", err)
			}
			if tt.wantResp && len(resp.Content) == 0 {
				t.Error("expected valid response")
			}
		})
	}
}

func TestLoop(t *testing.T) {
	tests := []struct {
		name           string
		stopReason     string
		tools          []Tool
		dispatch       map[string]ToolFunc
		maxIter        int
		wantErr        error
		wantMsgCount   int
		wantStopReason string
	}{
		{
			name:        "end_turn success",
			stopReason:  "end_turn",
			wantErr:     nil,
			wantMsgCount: 2,
		},
		{
			name:        "tool_use with successful dispatch",
			stopReason:  "tool_use",
			tools:       []Tool{{Name: "echo"}},
			dispatch: map[string]ToolFunc{
				"echo": func(context.Context, json.RawMessage) (string, error) { return "result", nil },
			},
			wantErr:      nil,
			wantMsgCount: 3, // history + assistant + tool_result
		},
		{
			name:       "missing tool aborts with ToolError",
			stopReason: "tool_use",
			tools:      []Tool{{Name: "echo"}},
			dispatch:   map[string]ToolFunc{},
			wantErr:    ErrMissingTool,
			wantMsgCount: 2,
		},
		{
			name:         "maxIter exhaustion",
			stopReason:   "end_turn",
			maxIter:      1,
			wantErr:      ErrMaxIter,
			wantMsgCount: 2,
		},
		{
			name:        "max_tokens error",
			stopReason:  "max_tokens",
			wantErr:     nil, // wrapped in LoopError
			wantMsgCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestProvider()
			p.Resp.StopReason = tt.stopReason
			if tt.stopReason == "tool_use" {
				p.Resp.Content = []ContentBlock{{Type: TypeToolUse, ID: "tu1", Name: "echo"}}
			}
			c, _ := New(Config{Provider: p, Model: "test-model"})
			history := []Message{NewUserMessage("hi")}
			result, err := c.Loop(context.Background(), "sys", history, tt.tools, tt.dispatch, tt.maxIter)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					var le *LoopError
					if errors.As(err, &le) {
						if !errors.Is(le.Cause, tt.wantErr) {
							t.Errorf("got err %v, want containing %v", err, tt.wantErr)
						}
					} else {
						t.Errorf("got err %v, want %v", err, tt.wantErr)
					}
				}
			}
			if len(result.Messages) != tt.wantMsgCount {
				t.Errorf("got %d messages, want %d", len(result.Messages), tt.wantMsgCount)
			}
			if result.Usage.InputTokens != 10 {
				t.Errorf("unexpected usage %+v", result.Usage)
			}
		})
	}
}

func TestLoopStream(t *testing.T) {
	// Similar table-driven structure as TestLoop but with callback spies
	p := newTestProvider()
	p.Resp.StopReason = "end_turn"
	p.Deltas = []ContentBlock{{Type: TypeText, Text: "delta1"}, {Type: TypeText, Text: "delta2"}}
	c, _ := New(Config{Provider: p, Model: "test-model"})
	var tokenCount, toolCount int
	onToken := func(ContentBlock) { tokenCount++ }
	onTool := func([]ToolResult) { toolCount++ }
	history := []Message{NewUserMessage("hi")}
	result, err := c.LoopStream(context.Background(), "sys", history, nil, nil, 0, onToken, onTool)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if tokenCount != 2 {
		t.Errorf("expected 2 onToken calls, got %d", tokenCount)
	}
	if toolCount != 0 {
		t.Error("no tool calls expected")
	}
	if len(result.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(result.Messages))
	}
}
