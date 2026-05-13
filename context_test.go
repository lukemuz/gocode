package luft

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// countMessages is a deterministic TokenCounter that returns len(msgs).
// One token per message makes it easy to reason about budget math in tests.
func countMessages(msgs []Message) (int, error) { return len(msgs), nil }

// toolUseMsg builds an assistant message that requests a single tool call.
func toolUseMsg(id, name string) Message {
	return Message{
		Role: RoleAssistant,
		Content: []ContentBlock{
			{Type: TypeToolUse, ID: id, Name: name, Input: json.RawMessage(`{}`)},
		},
	}
}

// toolResultMsg builds the user message that carries a tool_result.
func toolResultMsg(id, content string) Message {
	return Message{
		Role: RoleUser,
		Content: []ContentBlock{
			{Type: TypeToolResult, ToolUseID: id, Content: content},
		},
	}
}

func TestContextManagerTrim(t *testing.T) {
	ctx := context.Background()

	// plainHistory builds n plain user→assistant pairs (no tool use).
	plainHistory := func(n int) []Message {
		msgs := make([]Message, 0, n*2)
		for i := 0; i < n; i++ {
			msgs = append(msgs, NewUserMessage(fmt.Sprintf("user %d", i)))
			msgs = append(msgs, Message{Role: RoleAssistant, Content: []ContentBlock{
				{Type: TypeText, Text: fmt.Sprintf("assistant %d", i)},
			}})
		}
		return msgs
	}

	t.Run("zero MaxTokens is no-op", func(t *testing.T) {
		cm := ContextManager{}
		h := plainHistory(5)
		got, err := cm.Trim(ctx, h)
		if err != nil || len(got) != len(h) {
			t.Fatalf("expected unchanged history, got %d msgs err=%v", len(got), err)
		}
	})

	t.Run("empty history is no-op", func(t *testing.T) {
		cm := ContextManager{MaxTokens: 10}
		got, err := cm.Trim(ctx, nil)
		if err != nil || len(got) != 0 {
			t.Fatalf("expected nil result for nil input, got %v err=%v", got, err)
		}
	})

	t.Run("within budget is no-op", func(t *testing.T) {
		h := plainHistory(3) // 6 messages
		cm := ContextManager{MaxTokens: 100, TokenCounter: countMessages}
		got, err := cm.Trim(ctx, h)
		if err != nil || len(got) != len(h) {
			t.Fatalf("expected unchanged history, got %d err=%v", len(got), err)
		}
	})

	t.Run("original slice is not modified", func(t *testing.T) {
		h := plainHistory(5)
		orig := make([]Message, len(h))
		copy(orig, h)
		cm := ContextManager{MaxTokens: 4, KeepFirst: 2, KeepRecent: 2, TokenCounter: countMessages}
		_, err := cm.Trim(ctx, h)
		if err != nil {
			t.Fatal(err)
		}
		if len(h) != len(orig) {
			t.Error("original slice length changed")
		}
		for i := range h {
			if h[i].Role != orig[i].Role {
				t.Errorf("original slice modified at index %d", i)
			}
		}
	})

	t.Run("trims middle messages", func(t *testing.T) {
		// 5 pairs = 10 messages. Budget 6, keep first 2, keep last 4.
		// Trim zone = messages [2..5] (4 messages dropped).
		h := plainHistory(5)
		cm := ContextManager{MaxTokens: 6, KeepFirst: 2, KeepRecent: 4, TokenCounter: countMessages}
		got, err := cm.Trim(ctx, h)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 6 {
			t.Fatalf("expected 6 messages, got %d", len(got))
		}
		// First 2 messages unchanged.
		for i := 0; i < 2; i++ {
			if got[i].Role != h[i].Role || textContent(got[i]) != textContent(h[i]) {
				t.Errorf("got[%d] != h[%d]", i, i)
			}
		}
		// Last 4 messages unchanged.
		for i := 0; i < 4; i++ {
			if got[2+i].Role != h[6+i].Role || textContent(got[2+i]) != textContent(h[6+i]) {
				t.Errorf("got[%d] != h[%d]", 2+i, 6+i)
			}
		}
	})

	t.Run("head boundary expands to preserve tool cycle", func(t *testing.T) {
		// History: user0, assistant(tool_use), user(tool_result), assistant1, user1, assistant2
		// KeepFirst=2 would cut after the tool_use message, stranding the tool_result.
		// nextCleanCut should push headEnd to index 3 (first plain user after the cycle).
		h := []Message{
			NewUserMessage("user0"),     // 0 — plain user
			toolUseMsg("tu1", "search"), // 1 — assistant with tool_use
			toolResultMsg("tu1", "res"), // 2 — tool_result (not a plain user)
			Message{Role: RoleAssistant, Content: []ContentBlock{{Type: TypeText, Text: "done"}}}, // 3
			NewUserMessage("user1"), // 4 — plain user (safe cut)
			Message{Role: RoleAssistant, Content: []ContentBlock{{Type: TypeText, Text: "ok"}}}, // 5
		}
		cm := ContextManager{MaxTokens: 4, KeepFirst: 2, KeepRecent: 2, TokenCounter: countMessages}
		got, err := cm.Trim(ctx, h)
		if err != nil {
			t.Fatal(err)
		}
		// headEnd expands from 2 to 4 (first plain user is at index 4).
		// tailStart = len(h)-2 = 4, then prevCleanCut(h, 4) = 4 (plain user).
		// trimZone = h[4:4] = empty → history returned unchanged.
		if len(got) != len(h) {
			t.Fatalf("expected unchanged history (trim zone empty), got %d msgs", len(got))
		}
	})

	t.Run("tail boundary expands to preserve tool cycle", func(t *testing.T) {
		// History: user0, asst0, user1, asst1, user2, asst(tool_use), user(tool_result), asst2
		// KeepRecent=3 would start at index 5 (asst with tool_use) — not a clean cut.
		// prevCleanCut should walk back to index 4 (user2, the plain user that precedes it).
		h := []Message{
			NewUserMessage("user0"), // 0
			Message{Role: RoleAssistant, Content: []ContentBlock{{Type: TypeText, Text: "a0"}}}, // 1
			NewUserMessage("user1"), // 2
			Message{Role: RoleAssistant, Content: []ContentBlock{{Type: TypeText, Text: "a1"}}}, // 3
			NewUserMessage("user2"),      // 4 — plain user (clean cut)
			toolUseMsg("tu1", "read"),    // 5
			toolResultMsg("tu1", "data"), // 6
			Message{Role: RoleAssistant, Content: []ContentBlock{{Type: TypeText, Text: "a2"}}}, // 7
		}
		cm := ContextManager{MaxTokens: 5, KeepFirst: 0, KeepRecent: 3, TokenCounter: countMessages}
		got, err := cm.Trim(ctx, h)
		if err != nil {
			t.Fatal(err)
		}
		// tailStart starts at 8-3=5 (asst with tool_use), prevCleanCut walks back to 4.
		// trimZone = h[0:4] (4 messages dropped).
		// Result = h[4:] = 4 messages.
		if len(got) != 4 {
			t.Fatalf("expected 4 messages (h[4:]), got %d", len(got))
		}
		if got[0].Role != RoleUser || textContent(got[0]) != "user2" {
			t.Errorf("expected got[0] = user2, got role=%s text=%q", got[0].Role, textContent(got[0]))
		}
	})

	t.Run("nothing to trim when pinned regions overlap", func(t *testing.T) {
		// KeepFirst=4 + KeepRecent=4 on a 6-message history → regions overlap.
		h := plainHistory(3) // 6 messages
		cm := ContextManager{MaxTokens: 2, KeepFirst: 4, KeepRecent: 4, TokenCounter: countMessages}
		got, err := cm.Trim(ctx, h)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(h) {
			t.Fatalf("expected unchanged history when regions overlap, got %d msgs", len(got))
		}
	})

	t.Run("summarizer receives trimmed messages", func(t *testing.T) {
		h := plainHistory(5) // 10 messages
		var summarized []Message
		cm := ContextManager{
			MaxTokens:    6,
			KeepFirst:    2,
			KeepRecent:   4,
			TokenCounter: countMessages,
			Summarizer: func(_ context.Context, trimmed []Message) (string, error) {
				summarized = trimmed
				return "summary of middle", nil
			},
		}
		got, err := cm.Trim(ctx, h)
		if err != nil {
			t.Fatal(err)
		}
		// trim zone = h[2:6] = 4 messages
		if len(summarized) != 4 {
			t.Errorf("summarizer received %d messages, want 4", len(summarized))
		}
		// result = head(2) + summary(1) + tail(4) = 7 messages
		if len(got) != 7 {
			t.Fatalf("expected 7 messages with summary, got %d", len(got))
		}
		if textContent(got[2]) != "summary of middle" {
			t.Errorf("summary message content = %q", textContent(got[2]))
		}
	})

	t.Run("summarizer error is propagated", func(t *testing.T) {
		h := plainHistory(5)
		boom := errors.New("summarizer failed")
		cm := ContextManager{
			MaxTokens:    4,
			KeepFirst:    2,
			KeepRecent:   2,
			TokenCounter: countMessages,
			Summarizer:   func(_ context.Context, _ []Message) (string, error) { return "", boom },
		}
		_, err := cm.Trim(ctx, h)
		if !errors.Is(err, boom) {
			t.Errorf("expected summarizer error, got %v", err)
		}
	})

	t.Run("token counter error is propagated", func(t *testing.T) {
		h := plainHistory(3)
		boom := errors.New("counter failed")
		cm := ContextManager{
			MaxTokens:    4,
			TokenCounter: func(_ []Message) (int, error) { return 0, boom },
		}
		_, err := cm.Trim(ctx, h)
		if !errors.Is(err, boom) {
			t.Errorf("expected counter error, got %v", err)
		}
	})

	t.Run("custom TokenCounter is used", func(t *testing.T) {
		h := plainHistory(3) // 6 messages
		called := false
		cm := ContextManager{
			MaxTokens: 5,
			TokenCounter: func(msgs []Message) (int, error) {
				called = true
				return len(msgs), nil
			},
			KeepFirst:  2,
			KeepRecent: 2,
		}
		_, err := cm.Trim(ctx, h)
		if err != nil {
			t.Fatal(err)
		}
		if !called {
			t.Error("custom TokenCounter was not called")
		}
	})
}

func TestIsPlainUserMessage(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
		want bool
	}{
		{
			name: "plain user text",
			msg:  NewUserMessage("hello"),
			want: true,
		},
		{
			name: "user with tool_results only",
			msg:  toolResultMsg("tu1", "result"),
			want: false,
		},
		{
			name: "user with mixed text and tool_result",
			msg: Message{Role: RoleUser, Content: []ContentBlock{
				{Type: TypeText, Text: "some text"},
				{Type: TypeToolResult, ToolUseID: "tu1", Content: "res"},
			}},
			want: true,
		},
		{
			name: "assistant message",
			msg:  Message{Role: RoleAssistant, Content: []ContentBlock{{Type: TypeText, Text: "hi"}}},
			want: false,
		},
		{
			name: "empty user message",
			msg:  Message{Role: RoleUser, Content: nil},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPlainUserMessage(tt.msg); got != tt.want {
				t.Errorf("isPlainUserMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEstimateTokens(t *testing.T) {
	t.Run("plain user message", func(t *testing.T) {
		// "hello world" = 11 chars / 4 = 2 prose tokens, +4 message envelope,
		// +2 block envelope = 8 tokens.
		got, err := estimateTokens([]Message{NewUserMessage("hello world")})
		if err != nil || got != 8 {
			t.Fatalf("got %d err=%v, want 8", got, err)
		}
	})

	t.Run("empty history is zero", func(t *testing.T) {
		got, err := estimateTokens(nil)
		if err != nil || got != 0 {
			t.Fatalf("got %d err=%v, want 0", got, err)
		}
	})

	t.Run("per-message overhead dominates for tiny turns", func(t *testing.T) {
		// Without per-message overhead, a thread of empty messages would be
		// counted as zero tokens. The new heuristic charges 4 per message.
		msgs := []Message{NewUserMessage(""), NewUserMessage(""), NewUserMessage("")}
		got, err := estimateTokens(msgs)
		if err != nil {
			t.Fatal(err)
		}
		if got < 3*4 {
			t.Errorf("expected at least %d tokens for 3 empty messages, got %d", 3*4, got)
		}
	})

	t.Run("tool_use input counts denser than prose", func(t *testing.T) {
		// 12-byte JSON input vs a 12-byte prose text: input should cost more
		// because JSON tokenizes denser (3 chars/token vs 4).
		toolUse := Message{Role: RoleAssistant, Content: []ContentBlock{
			{Type: TypeToolUse, ID: "x", Name: "ls", Input: json.RawMessage(`{"x":"yyyyy"}`)},
		}}
		prose := Message{Role: RoleAssistant, Content: []ContentBlock{
			{Type: TypeText, Text: "hello world!"}, // 12 bytes
		}}
		toolTokens, _ := estimateTokens([]Message{toolUse})
		proseTokens, _ := estimateTokens([]Message{prose})
		if toolTokens <= proseTokens {
			t.Errorf("expected tool_use (denser JSON) to outweigh equal-length prose; got tool=%d prose=%d", toolTokens, proseTokens)
		}
	})

	t.Run("tool_result content is counted", func(t *testing.T) {
		got, err := estimateTokens([]Message{toolResultMsg("tu1", strings.Repeat("a", 40))})
		if err != nil {
			t.Fatal(err)
		}
		// 40 chars / 4 = 10 prose tokens + 4 message + 2 block + tu1 (3/4=0) = 16.
		if got != 16 {
			t.Errorf("got %d, want 16", got)
		}
	})
}

// textContent is a test helper that extracts text from a message.
func textContent(msg Message) string {
	return TextContent(msg)
}
