package gocode

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"
)

// EventType identifies what happened in an Event. The set is closed: every
// event emitted by Loop, LoopStream, runTools, and callWithRetry has one of
// these types.
type EventType string

const (
	// EventTurnStart is emitted once at the beginning of Loop / LoopStream,
	// before any model call. History carries the trimmed history about to be
	// sent.
	EventTurnStart EventType = "turn_start"

	// EventModelRequest is emitted before each model call. Iter is the 0-based
	// iteration within the turn.
	EventModelRequest EventType = "model_request"

	// EventModelResponse is emitted after each successful model call (after
	// retries, if any). Message holds the assistant content; Usage holds the
	// per-call token usage; StopReason carries the model's stop reason.
	EventModelResponse EventType = "model_response"

	// EventRetryAttempt is emitted before each retry sleep, mirroring
	// RetryConfig.OnRetry but routed through the Recorder. Attempt is the
	// 1-based retry number; Wait is the computed backoff.
	EventRetryAttempt EventType = "retry_attempt"

	// EventToolCallStart is emitted just before a tool dispatch begins.
	// Concurrent tool calls share the same Iter; Seq orders them.
	EventToolCallStart EventType = "tool_call_start"

	// EventToolCallEnd is emitted after a tool dispatch returns. ToolOutput
	// carries the result string; if the tool returned an error, ToolError
	// holds the error message and IsError is true.
	EventToolCallEnd EventType = "tool_call_end"

	// EventTurnEnd is emitted on successful turn completion (model returned
	// end_turn).
	EventTurnEnd EventType = "turn_end"

	// EventTurnError is emitted when Loop / LoopStream returns a non-nil
	// error. Err carries the error message.
	EventTurnError EventType = "turn_error"
)

// Event is one record in a turn's activity log. Fields are populated based on
// Type — see EventType constants for which fields are set when. Event is
// designed to be JSON-friendly so a Recorder can serialize it directly.
//
// Seq is assigned by the Recorder when Record is called and is monotonic per
// Recorder. TurnID is stable for all events in a single Loop / LoopStream
// invocation so events from concurrent turns can be separated. Iter is the
// 0-based iteration within the turn (the i-th model call). For tool events
// Iter matches the iteration whose tool_use block produced the call, so all
// tool events from one parallel batch share Iter and are ordered by Seq.
type Event struct {
	Seq     int64     `json:"seq"`
	TurnID  string    `json:"turn_id"`
	Iter    int       `json:"iter"`
	Type    EventType `json:"type"`
	Time    time.Time `json:"time"`
	History []Message `json:"history,omitempty"`     // TurnStart
	Message *Message  `json:"message,omitempty"`     // ModelResponse
	Usage   *Usage    `json:"usage,omitempty"`       // ModelResponse, TurnEnd
	StopReason string `json:"stop_reason,omitempty"` // ModelResponse

	ToolUseID  string          `json:"tool_use_id,omitempty"`  // ToolCallStart, ToolCallEnd
	ToolName   string          `json:"tool_name,omitempty"`    // ToolCallStart, ToolCallEnd
	ToolInput  json.RawMessage `json:"tool_input,omitempty"`   // ToolCallStart
	ToolOutput string          `json:"tool_output,omitempty"`  // ToolCallEnd
	ToolError  string          `json:"tool_error,omitempty"`   // ToolCallEnd (when IsError)
	IsError    bool            `json:"is_error,omitempty"`     // ToolCallEnd

	Attempt int           `json:"attempt,omitempty"` // RetryAttempt
	Wait    time.Duration `json:"wait,omitempty"`    // RetryAttempt
	Err     string        `json:"err,omitempty"`     // TurnError
}

// Recorder receives Events as they happen during Loop / LoopStream. Implementations
// must be safe for concurrent use because runTools dispatches tool calls in
// parallel and each goroutine emits its own start/end events.
//
// The Seq field on the incoming event is ignored; the Recorder assigns its own
// monotonic Seq before storing or forwarding. This keeps Seq meaningful even
// when multiple Loops share one Recorder.
type Recorder interface {
	Record(ctx context.Context, ev Event)
}

// MemoryRecorder collects events in memory. Useful for tests and for the
// open-or-create chat pattern where events round-trip through Session.Events.
type MemoryRecorder struct {
	mu     sync.Mutex
	seq    int64
	events []Event
}

// NewMemoryRecorder returns an empty MemoryRecorder.
func NewMemoryRecorder() *MemoryRecorder {
	return &MemoryRecorder{}
}

// Record appends ev to the in-memory log, assigning a monotonic Seq.
func (r *MemoryRecorder) Record(_ context.Context, ev Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	ev.Seq = r.seq
	r.events = append(r.events, ev)
}

// Events returns a copy of the recorded events in order.
func (r *MemoryRecorder) Events() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Event, len(r.events))
	copy(out, r.events)
	return out
}

// Reset clears the recorder. Seq numbering restarts at 1.
func (r *MemoryRecorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq = 0
	r.events = nil
}

// JSONLRecorder writes one JSON object per line to the underlying writer.
// Errors from Write are silently dropped — recording is best-effort and
// must not interfere with the agent's main control flow. Wrap the writer
// yourself if you want error visibility.
type JSONLRecorder struct {
	mu  sync.Mutex
	seq int64
	w   io.Writer
}

// NewJSONLRecorder returns a JSONLRecorder writing to w.
func NewJSONLRecorder(w io.Writer) *JSONLRecorder {
	return &JSONLRecorder{w: w}
}

// Record marshals ev to JSON and writes it as one line.
func (r *JSONLRecorder) Record(_ context.Context, ev Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	ev.Seq = r.seq
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	_, _ = r.w.Write(append(data, '\n'))
}

// MultiRecorder fans Record calls out to several recorders in order. Each
// child assigns its own Seq independently.
type MultiRecorder []Recorder

// Record forwards ev to every child recorder.
func (m MultiRecorder) Record(ctx context.Context, ev Event) {
	for _, r := range m {
		if r != nil {
			r.Record(ctx, ev)
		}
	}
}

// RecorderToSession returns a Recorder that appends to sess.Events. The
// returned Recorder is safe for concurrent use; events accumulate on the
// session in record order so a subsequent Save persists the full log.
func RecorderToSession(sess *Session) Recorder {
	return &sessionRecorder{sess: sess}
}

type sessionRecorder struct {
	mu   sync.Mutex
	sess *Session
	seq  int64
}

func (r *sessionRecorder) Record(_ context.Context, ev Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	ev.Seq = r.seq
	r.sess.Events = append(r.sess.Events, ev)
}

// emit is a nil-safe helper used by Loop, runTools, and callWithRetry. It sets
// Time to now if zero and forwards to the Recorder.
func emit(ctx context.Context, rec Recorder, ev Event) {
	if rec == nil {
		return
	}
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	rec.Record(ctx, ev)
}

// newTurnID returns a short random hex string suitable for grouping events
// from a single Loop / LoopStream invocation.
func newTurnID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure is exceptional; fall back to a time-based id.
		return fmt.Sprintf("t-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
