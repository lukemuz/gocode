package agent

import "time"

// StreamBuffer provides reset-aware token delivery for streaming calls with retries.
// When callWithRetry retries a failed stream, the onToken callback may fire again
// for the new attempt, causing partial output from the failed attempt to appear
// before the successful attempt's output. StreamBuffer lets callers react to each
// retry by calling an onReset function, which can clear a display buffer, send an
// SSE reset event, or take any other corrective action.
//
// Wire StreamBuffer to a streaming call like this:
//
//	sb := agent.NewStreamBuffer(
//	    func(b agent.ContentBlock) { fmt.Print(b.Text) }, // forward tokens live
//	    func() { fmt.Print("\n[retrying…]\n") },          // reset partial output
//	)
//	cfg := agent.RetryConfig{OnRetry: sb.OnRetry}
//	client, _ := agent.New(agent.Config{..., Retry: cfg})
//	msg, err := client.AskStream(ctx, system, history, sb.OnToken)
//
// Either argument to NewStreamBuffer may be nil, in which case that half of the
// wiring is silently skipped.
type StreamBuffer struct {
	onToken func(ContentBlock)
	onReset func()
}

// NewStreamBuffer returns a StreamBuffer that forwards tokens to onToken and
// calls onReset before each retry attempt. Either argument may be nil.
func NewStreamBuffer(onToken func(ContentBlock), onReset func()) *StreamBuffer {
	return &StreamBuffer{onToken: onToken, onReset: onReset}
}

// OnToken is the onToken callback to pass to AskStream, LoopStream, or
// StepStream. It forwards each ContentBlock to the underlying onToken handler.
func (b *StreamBuffer) OnToken(cb ContentBlock) {
	if b.onToken != nil {
		b.onToken(cb)
	}
}

// OnRetry satisfies the RetryConfig.OnRetry signature. It calls the onReset
// handler so callers can clear any partial output before the next stream
// attempt begins. attempt is the 1-based retry number; wait is the computed
// backoff duration.
func (b *StreamBuffer) OnRetry(attempt int, wait time.Duration) {
	if b.onReset != nil {
		b.onReset()
	}
}
