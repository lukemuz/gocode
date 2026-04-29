package agent

import (
	"context"
	"fmt"
)

// ContextManager trims conversation history to keep it within a token budget.
// A zero-value ContextManager is a no-op: MaxTokens == 0 disables trimming.
//
// Typical usage:
//
//	cm := agent.ContextManager{
//	    MaxTokens:  8000,
//	    KeepFirst:  1,   // always keep the initial user message
//	    KeepRecent: 10,  // always keep the last 10 messages
//	}
//	trimmed, err := cm.Trim(ctx, history)
//	result, err  := client.Loop(ctx, system, trimmed, tools, dispatch, 10)
type ContextManager struct {
	// MaxTokens is the token budget. Trim returns history unchanged if it
	// fits within this budget. Zero disables trimming entirely.
	MaxTokens int

	// KeepFirst is the number of messages to always retain from the start of
	// history regardless of token budget — useful for preserving an initial
	// instruction or persistent context. The actual number kept may be larger
	// when preserving tool-use/tool-result integrity requires it.
	KeepFirst int

	// KeepRecent is the number of messages to always retain from the end of
	// history. The actual number kept may be larger when integrity requires
	// including the full tool cycle that contains the boundary message.
	KeepRecent int

	// TokenCounter estimates the token count of a message slice. If nil, a
	// character-based heuristic (4 chars ≈ 1 token) is used. Provide a
	// model-specific counter for accurate budget enforcement.
	TokenCounter func([]Message) (int, error)

	// Summarizer condenses trimmed messages into a single string. When set,
	// the trimmed portion is replaced by a user message containing that string.
	// When nil, trimmed messages are dropped without replacement.
	//
	// The summarizer typically calls the LLM. That call is caller-owned and
	// visible: no hidden model calls occur inside Trim.
	Summarizer func(ctx context.Context, trimmed []Message) (string, error)
}

// Trim returns a copy of history that fits within MaxTokens. The original
// slice is never modified. If MaxTokens is zero or history is already within
// budget, history is returned as-is.
//
// Trimming always preserves tool-use/tool-result integrity: an assistant
// message that contains tool_use blocks and the user message that carries the
// corresponding tool_results are always kept or dropped as a unit — the model
// must never see one without the other.
//
// If KeepFirst > 0, the first KeepFirst messages are pinned. If KeepRecent >
// 0, the last KeepRecent messages are pinned. Both boundaries are expanded as
// needed to land on a clean cut point. Everything between the two pinned
// regions is the trim zone.
//
// If Summarizer is set, the trim zone is passed to it and the returned string
// becomes a single user message inserted in place of the trimmed content.
// Otherwise the trim zone is dropped entirely.
//
// Trim does not iterate: it removes the trim zone in one pass. If the result
// is still over budget (because KeepFirst + KeepRecent alone exceeds
// MaxTokens), the history is returned as trimmed without error — the caller
// will encounter a max_tokens API error and can decide how to respond.
func (m ContextManager) Trim(ctx context.Context, history []Message) ([]Message, error) {
	if m.MaxTokens == 0 || len(history) == 0 {
		return history, nil
	}

	counter := m.TokenCounter
	if counter == nil {
		counter = estimateTokens
	}

	count, err := counter(history)
	if err != nil {
		return nil, fmt.Errorf("agent: context: count tokens: %w", err)
	}
	if count <= m.MaxTokens {
		return history, nil
	}

	n := len(history)

	// Determine the protected head: first KeepFirst messages, then expanded
	// forward to the nearest clean cut so we don't strand a tool cycle.
	headEnd := min(m.KeepFirst, n)
	headEnd = nextCleanCut(history, headEnd)

	// Determine the protected tail: last KeepRecent messages, then expanded
	// backward to the nearest clean cut.
	tailStart := n
	if m.KeepRecent > 0 {
		tailStart = max(n-m.KeepRecent, headEnd)
		tailStart = prevCleanCut(history, tailStart)
	}
	if tailStart < headEnd {
		tailStart = headEnd
	}

	trimZone := history[headEnd:tailStart]
	if len(trimZone) == 0 {
		// Nothing can be trimmed while respecting the pinned regions.
		return history, nil
	}

	if m.Summarizer == nil {
		result := make([]Message, 0, headEnd+(n-tailStart))
		result = append(result, history[:headEnd]...)
		result = append(result, history[tailStart:]...)
		return result, nil
	}

	summary, err := m.Summarizer(ctx, trimZone)
	if err != nil {
		return nil, fmt.Errorf("agent: context: summarize: %w", err)
	}
	result := make([]Message, 0, headEnd+1+(n-tailStart))
	result = append(result, history[:headEnd]...)
	result = append(result, NewUserMessage(summary))
	result = append(result, history[tailStart:]...)
	return result, nil
}

// nextCleanCut returns the smallest index >= start at which history[i] is a
// plain user message (not a tool_result turn). Returns len(history) if no
// such position exists, meaning the entire tail must be kept.
func nextCleanCut(history []Message, start int) int {
	for i := start; i < len(history); i++ {
		if isPlainUserMessage(history[i]) {
			return i
		}
	}
	return len(history)
}

// prevCleanCut returns the largest index <= start at which history[i] is a
// plain user message. If none exists at or before start, returns start to
// avoid expanding the tail into the trim zone.
func prevCleanCut(history []Message, start int) int {
	for i := start; i >= 0; i-- {
		if isPlainUserMessage(history[i]) {
			return i
		}
	}
	return start
}

// isPlainUserMessage reports whether msg is a user turn that starts a new
// logical exchange — i.e., a user message whose content is not exclusively
// tool_result blocks. Such messages are safe cut points: the history can be
// split immediately before them without breaking tool-use/tool-result pairing.
func isPlainUserMessage(msg Message) bool {
	if msg.Role != RoleUser || len(msg.Content) == 0 {
		return false
	}
	for _, b := range msg.Content {
		if b.Type != TypeToolResult {
			return true
		}
	}
	return false
}

// estimateTokens provides a rough token count when no TokenCounter is
// configured. Uses 4 characters per token as a widely-cited heuristic for
// English prose; real counts will vary by language and model vocabulary.
func estimateTokens(msgs []Message) (int, error) {
	var chars int
	for _, msg := range msgs {
		for _, b := range msg.Content {
			chars += len(b.Text) + len(b.Content) + len(b.Input)
		}
	}
	return max(0, chars/4), nil
}
