package research

import (
	"context"
	"fmt"

	"github.com/lukemuz/gocode/agent"
)

const workerSystem = `You are a research specialist. You will be given one focused sub-question. Use the available search tools to find authoritative sources, read their snippets, and synthesize a concise factual answer.

Process:
1. Issue one or more searches with the available search tool(s). Refine queries if early results are weak.
2. Build a short answer (3-8 sentences) grounded ONLY in what the searches returned.
3. Call submit_findings with your summary and the URLs you actually used as evidence.

Constraints:
- Do not invent URLs or facts. Only cite sources that appeared in search results.
- If you cannot find good evidence, say so plainly in the summary.
- Call submit_findings exactly once when done. After that, end your turn.`

type findingsInput struct {
	Summary   string `json:"summary"`
	Citations []struct {
		URL     string `json:"url"`
		Title   string `json:"title"`
		Snippet string `json:"snippet"`
	} `json:"citations"`
}

var submitFindingsSchema = agent.Object(
	agent.String("summary", "Concise factual answer to the sub-question, 3-8 sentences", agent.Required()),
	agent.Array("citations", "URLs of sources used as evidence",
		agent.ObjectOf(
			agent.String("url", "Source URL", agent.Required()),
			agent.String("title", "Page title"),
			agent.String("snippet", "Quoted snippet that supports the summary"),
		),
		agent.Required()),
)

// Investigate runs a single worker against one subtask. searchTools is the
// caller-supplied toolset (typically Brave MCP); Extract adds the terminal
// submit_findings tool internally.
func Investigate(
	ctx context.Context,
	client *agent.Client,
	subtask Subtask,
	searchTools agent.Toolset,
	maxIter int,
) (Note, agent.Usage, error) {
	if maxIter <= 0 {
		maxIter = 12
	}
	prompt := fmt.Sprintf("Sub-question: %s\n\nResearch this and call submit_findings.", subtask.Question)

	raw, result, err := agent.Extract(ctx, client, workerSystem,
		[]agent.Message{agent.NewUserMessage(prompt)},
		agent.ExtractParams[findingsInput]{
			Description: "Submit your final findings for this sub-question. Call this exactly once when done.",
			Schema:      submitFindingsSchema,
			Name:        "submit_findings",
			Tools:       searchTools,
			MaxIter:     maxIter,
			Validate: func(in findingsInput) error {
				if in.Summary == "" {
					return fmt.Errorf("summary is required")
				}
				return nil
			},
		})

	note := Note{SubtaskID: subtask.ID, Question: subtask.Question}
	if err != nil {
		note.Err = err.Error()
		return note, result.Usage, err
	}
	note.Summary = raw.Summary
	note.Citations = make([]Citation, len(raw.Citations))
	for i, c := range raw.Citations {
		note.Citations[i] = Citation{URL: c.URL, Title: c.Title, Snippet: c.Snippet}
	}
	return note, result.Usage, nil
}
