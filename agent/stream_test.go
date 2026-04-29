package agent

import (
	"context"
	"testing"
	"time"
)

func TestStreamBufferOnToken(t *testing.T) {
	var received []ContentBlock
	sb := NewStreamBuffer(func(b ContentBlock) { received = append(received, b) }, nil)

	sb.OnToken(ContentBlock{Type: TypeText, Text: "hello"})
	sb.OnToken(ContentBlock{Type: TypeText, Text: " world"})

	if len(received) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(received))
	}
	if received[0].Text != "hello" || received[1].Text != " world" {
		t.Errorf("unexpected tokens: %v", received)
	}
}

func TestStreamBufferOnRetryCallsReset(t *testing.T) {
	var resets int
	sb := NewStreamBuffer(nil, func() { resets++ })

	sb.OnRetry(1, 100*time.Millisecond)
	sb.OnRetry(2, 200*time.Millisecond)

	if resets != 2 {
		t.Errorf("expected 2 resets, got %d", resets)
	}
}

func TestStreamBufferNilSafe(t *testing.T) {
	sb := NewStreamBuffer(nil, nil)
	sb.OnToken(ContentBlock{Type: TypeText, Text: "x"})
	sb.OnRetry(1, time.Second)
}

// failThenSucceedProvider fails the first failN Stream calls (firing deltas
// before returning the error), then succeeds on subsequent calls.
type failThenSucceedProvider struct {
	failN  int
	calls  int
	deltas []ContentBlock
	resp   ProviderResponse
	err    error
}

func (p *failThenSucceedProvider) Call(_ context.Context, _ ProviderRequest) (ProviderResponse, error) {
	return p.resp, nil
}

func (p *failThenSucceedProvider) Stream(_ context.Context, _ ProviderRequest, onDelta func(ContentBlock)) (ProviderResponse, error) {
	p.calls++
	for _, d := range p.deltas {
		onDelta(d)
	}
	if p.calls <= p.failN {
		return ProviderResponse{}, p.err
	}
	return p.resp, nil
}

func TestStreamBufferWiredToRetryConfig(t *testing.T) {
	var tokens []string
	var resets int

	sb := NewStreamBuffer(
		func(b ContentBlock) {
			if b.Type == TypeText {
				tokens = append(tokens, b.Text)
			}
		},
		func() { resets++ },
	)

	stub := &failThenSucceedProvider{
		failN:  2,
		deltas: []ContentBlock{{Type: TypeText, Text: "token"}},
		resp:   testResponse,
		err:    &APIError{StatusCode: 429, Message: "rate limited"},
	}

	cfg := RetryConfig{
		MaxRetries:  3,
		InitialWait: time.Millisecond,
		OnRetry:     sb.OnRetry,
	}
	c, _ := New(Config{Provider: stub, Model: "test", Retry: cfg})

	_, err := c.AskStream(context.Background(), "", []Message{NewUserMessage("hi")}, sb.OnToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// OnRetry fires once before each retry, so 2 failed attempts → 2 resets.
	if resets != 2 {
		t.Errorf("expected 2 resets (one per retry), got %d", resets)
	}
	// Each of the 3 stream calls fires 1 delta → 3 total token deliveries.
	if len(tokens) != 3 {
		t.Errorf("expected 3 token deliveries (2 failed + 1 success), got %d: %v", len(tokens), tokens)
	}
}
