package research

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lukemuz/gocode/agent"
)

const plannerSystem = `You are a research planner. Decompose the user's question into a small set of focused, mutually-distinct sub-questions that, when answered together, will fully answer the original question.

Rules:
- Each sub-question must be answerable independently from web search.
- Avoid redundancy: each sub-question should investigate a different angle.
- Prefer breadth over depth — workers will dig in.
- Produce between 2 and the maximum number of sub-questions allowed.

When you are ready, call the submit_plan tool with your decomposition. Do not produce any text answer; only call the tool.`

// planInput is the typed argument the model passes to submit_plan.
type planInput struct {
	Reasoning string `json:"reasoning"`
	Subtasks  []struct {
		Question  string `json:"question"`
		Rationale string `json:"rationale"`
	} `json:"subtasks"`
}

// submitPlanSchema is the JSON Schema for the submit_plan tool. We hand-write
// it because gocode's InputSchema helpers don't model arrays-of-objects, and
// Tool.InputSchema is json.RawMessage so any valid JSON Schema works.
const submitPlanSchema = `{
  "type": "object",
  "properties": {
    "reasoning": {
      "type": "string",
      "description": "Brief explanation of why these sub-questions cover the original."
    },
    "subtasks": {
      "type": "array",
      "minItems": 1,
      "items": {
        "type": "object",
        "properties": {
          "question": {"type": "string", "description": "Self-contained sub-question to research."},
          "rationale": {"type": "string", "description": "Why this sub-question matters."}
        },
        "required": ["question"]
      }
    }
  },
  "required": ["subtasks"]
}`

// Decompose splits question into subtasks using a single Loop with a
// submit_plan tool. The returned Plan has stable IDs (s1, s2, ...).
func Decompose(ctx context.Context, client *agent.Client, question string, maxSubtasks int) (Plan, agent.Usage, error) {
	if maxSubtasks <= 0 {
		maxSubtasks = 6
	}

	var captured planInput
	submitted := false

	submitFn := agent.TypedToolFunc(func(ctx context.Context, in planInput) (string, error) {
		if len(in.Subtasks) == 0 {
			return "", fmt.Errorf("submit_plan requires at least one subtask")
		}
		if len(in.Subtasks) > maxSubtasks {
			return "", fmt.Errorf("too many subtasks: got %d, max is %d", len(in.Subtasks), maxSubtasks)
		}
		captured = in
		submitted = true
		return "plan accepted", nil
	})

	submitTool := agent.Tool{
		Name:        "submit_plan",
		Description: "Submit the final research plan. Call this exactly once when you have decided on the sub-questions.",
		InputSchema: json.RawMessage(submitPlanSchema),
	}

	prompt := fmt.Sprintf("Original question: %s\n\nMaximum sub-questions allowed: %d\n\nDecompose, then call submit_plan.",
		question, maxSubtasks)

	tools := agent.Toolset{Bindings: []agent.ToolBinding{{
		Tool: submitTool, Func: submitFn, Meta: agent.ToolMetadata{Source: "research/planner"},
	}}}

	result, err := client.Loop(
		ctx,
		plannerSystem,
		[]agent.Message{agent.NewUserMessage(prompt)},
		tools,
		4,
	)
	if err != nil {
		return Plan{}, result.Usage, fmt.Errorf("planner: loop: %w", err)
	}
	if !submitted {
		return Plan{}, result.Usage, fmt.Errorf("planner: model finished without calling submit_plan")
	}

	subs := make([]Subtask, len(captured.Subtasks))
	for i, s := range captured.Subtasks {
		subs[i] = Subtask{
			ID:        fmt.Sprintf("s%d", i+1),
			Question:  s.Question,
			Rationale: s.Rationale,
		}
	}
	return Plan{
		Question:  question,
		Subtasks:  subs,
		Reasoning: captured.Reasoning,
	}, result.Usage, nil
}
