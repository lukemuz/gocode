package research

import (
	"context"
	"encoding/json"
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

const submitFindingsSchema = `{
  "type": "object",
  "properties": {
    "summary": {
      "type": "string",
      "description": "Concise factual answer to the sub-question, 3-8 sentences."
    },
    "citations": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "url": {"type": "string"},
          "title": {"type": "string"},
          "snippet": {"type": "string"}
        },
        "required": ["url"]
      }
    }
  },
  "required": ["summary", "citations"]
}`

// Investigate runs a single worker against one subtask. searchTools is the
// caller-supplied toolset (typically Brave MCP) plus an internally-added
// submit_findings tool.
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

	var captured findingsInput
	submitted := false

	submitFn := agent.TypedToolFunc(func(ctx context.Context, in findingsInput) (string, error) {
		if in.Summary == "" {
			return "", fmt.Errorf("summary is required")
		}
		captured = in
		submitted = true
		return "findings accepted", nil
	})

	submitTool := agent.Tool{
		Name:        "submit_findings",
		Description: "Submit your final findings for this sub-question. Call this exactly once when done.",
		InputSchema: json.RawMessage(submitFindingsSchema),
	}

	submitSet := agent.Toolset{Bindings: []agent.ToolBinding{{
		Tool: submitTool, Func: submitFn, Meta: agent.ToolMetadata{Source: "research/worker"},
	}}}

	tools, err := agent.Join(searchTools, submitSet)
	if err != nil {
		return Note{SubtaskID: subtask.ID, Question: subtask.Question}, agent.Usage{},
			fmt.Errorf("worker: combine toolsets: %w", err)
	}

	prompt := fmt.Sprintf("Sub-question: %s\n\nResearch this and call submit_findings.", subtask.Question)

	result, err := client.Loop(
		ctx,
		workerSystem,
		[]agent.Message{agent.NewUserMessage(prompt)},
		tools,
		maxIter,
	)
	note := Note{SubtaskID: subtask.ID, Question: subtask.Question}
	if err != nil {
		note.Err = err.Error()
		// Best-effort: if the worker submitted before erroring on a later turn,
		// preserve what it captured.
		if submitted {
			note.Summary = captured.Summary
			note.Citations = convertCitations(captured)
		}
		return note, result.Usage, err
	}
	if !submitted {
		note.Err = "worker finished without calling submit_findings"
		return note, result.Usage, fmt.Errorf("%s", note.Err)
	}
	note.Summary = captured.Summary
	note.Citations = convertCitations(captured)
	return note, result.Usage, nil
}

func convertCitations(in findingsInput) []Citation {
	out := make([]Citation, len(in.Citations))
	for i, c := range in.Citations {
		out[i] = Citation{URL: c.URL, Title: c.Title, Snippet: c.Snippet}
	}
	return out
}
