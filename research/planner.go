package research

import (
	"context"
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

var submitPlanSchema = agent.Object(
	agent.String("reasoning", "Brief explanation of why these sub-questions cover the original."),
	agent.Array("subtasks", "List of focused sub-questions to research",
		agent.ObjectOf(
			agent.String("question", "Self-contained sub-question to research", agent.Required()),
			agent.String("rationale", "Why this sub-question matters"),
		),
		agent.Required()),
)

// Decompose splits question into subtasks. The returned Plan has stable IDs
// (s1, s2, ...). The validator enforces the maxSubtasks cap as a retriable
// tool error so the model can self-correct.
func Decompose(ctx context.Context, client *agent.Client, question string, maxSubtasks int) (Plan, agent.Usage, error) {
	if maxSubtasks <= 0 {
		maxSubtasks = 6
	}
	prompt := fmt.Sprintf("Original question: %s\n\nMaximum sub-questions allowed: %d\n\nDecompose, then call submit_plan.",
		question, maxSubtasks)

	raw, result, err := agent.Extract(ctx, client, plannerSystem,
		[]agent.Message{agent.NewUserMessage(prompt)},
		agent.ExtractParams[planInput]{
			Description: "Submit the final research plan. Call this exactly once when you have decided on the sub-questions.",
			Schema:      submitPlanSchema,
			Name:        "submit_plan",
			MaxIter:     4,
			Validate: func(in planInput) error {
				if len(in.Subtasks) == 0 {
					return fmt.Errorf("at least one subtask required")
				}
				if len(in.Subtasks) > maxSubtasks {
					return fmt.Errorf("too many subtasks: got %d, max is %d", len(in.Subtasks), maxSubtasks)
				}
				return nil
			},
		})
	if err != nil {
		return Plan{}, result.Usage, fmt.Errorf("planner: %w", err)
	}

	subs := make([]Subtask, len(raw.Subtasks))
	for i, s := range raw.Subtasks {
		subs[i] = Subtask{
			ID:        fmt.Sprintf("s%d", i+1),
			Question:  s.Question,
			Rationale: s.Rationale,
		}
	}
	return Plan{
		Question:  question,
		Subtasks:  subs,
		Reasoning: raw.Reasoning,
	}, result.Usage, nil
}
