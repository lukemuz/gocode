// Package todo provides an in-memory todo list as a pair of tools.
//
// `todo_write` replaces the entire list. `todo_read` returns the current
// list. The list lives in the List value for the lifetime of the process,
// so a single coding-agent session can use it as scratch planning space
// without hitting disk.
//
// The "replace whole list" shape is deliberate: it forces the model to
// re-state its plan each time, which keeps the list small and consistent
// with what the model believes the plan to be. This pattern is what
// Claude Code's built-in TodoWrite tool uses.
package todo

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/lukemuz/luft"
)

// Status is the lifecycle state of a single todo item.
type Status string

const (
	StatusPending    Status = "pending"
	StatusInProgress Status = "in_progress"
	StatusCompleted  Status = "completed"
)

// Item is one row in the list.
type Item struct {
	Content string `json:"content"`
	Status  Status `json:"status"`
}

// List is the shared state behind the two tools. Construct with New.
// Safe for concurrent use.
type List struct {
	mu    sync.Mutex
	items []Item
}

// New returns an empty List.
func New() *List { return &List{} }

// Items returns a snapshot copy of the current list.
func (l *List) Items() []Item {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Item, len(l.items))
	copy(out, l.items)
	return out
}

// Toolset returns the two-tool toolset (todo_write, todo_read).
func (l *List) Toolset() luft.Toolset {
	return luft.Tools(l.writeBinding(), l.readBinding())
}

type writeInput struct {
	Items []Item `json:"items"`
}

func (l *List) writeBinding() luft.ToolBinding {
	t, fn := luft.NewTypedTool(
		"todo_write",
		"Replace the agent's planning todo list with a new set of items. Each item has 'content' and 'status' (pending|in_progress|completed). Call this whenever your plan changes — at the start of a multi-step task, when finishing a step, or when the plan needs revision. Keep at most one item in_progress.",
		luft.InputSchema{
			Type: "object",
			Properties: map[string]luft.SchemaProperty{
				"items": {
					Type:        "array",
					Description: "Full replacement list of todo items.",
					Items: &luft.SchemaProperty{
						Type: "object",
						Properties: map[string]luft.SchemaProperty{
							"content": {Type: "string"},
							"status":  {Type: "string", Enum: []any{"pending", "in_progress", "completed"}},
						},
						Required: []string{"content", "status"},
					},
				},
			},
			Required: []string{"items"},
		},
		func(ctx context.Context, in writeInput) (string, error) {
			for i, it := range in.Items {
				if it.Content == "" {
					return "", fmt.Errorf("item %d: content is empty", i)
				}
				switch it.Status {
				case StatusPending, StatusInProgress, StatusCompleted:
				default:
					return "", fmt.Errorf("item %d: invalid status %q", i, it.Status)
				}
			}
			l.mu.Lock()
			l.items = append(l.items[:0:0], in.Items...)
			l.mu.Unlock()
			return l.render(), nil
		},
	)
	return luft.ToolBinding{Tool: t, Func: fn}
}

func (l *List) readBinding() luft.ToolBinding {
	t, fn := luft.NewTypedTool(
		"todo_read",
		"Return the current planning todo list as a numbered checklist.",
		luft.InputSchema{Type: "object", Properties: map[string]luft.SchemaProperty{}},
		func(ctx context.Context, _ struct{}) (string, error) {
			return l.render(), nil
		},
	)
	return luft.ToolBinding{Tool: t, Func: fn}
}

func (l *List) render() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.items) == 0 {
		return "(todo list is empty)"
	}
	var b strings.Builder
	for i, it := range l.items {
		mark := "[ ]"
		switch it.Status {
		case StatusInProgress:
			mark = "[~]"
		case StatusCompleted:
			mark = "[x]"
		}
		fmt.Fprintf(&b, "%d. %s %s\n", i+1, mark, it.Content)
	}
	return strings.TrimRight(b.String(), "\n")
}
