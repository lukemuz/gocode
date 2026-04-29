package research

import (
	"context"
	"fmt"
	"strings"

	"github.com/lukemuz/gocode/agent"
)

const synthesizerSystem = `You are a research synthesizer. You will be given an original question and a set of notes produced by specialist workers, each addressing a sub-question. Produce a single coherent, well-structured answer to the original question.

Rules:
- Ground every claim in the supplied notes. Do NOT introduce new facts.
- Use inline citations of the form [n] referencing the numbered sources at the end.
- Reuse the same [n] when citing the same URL twice.
- End with a "Sources" section listing each URL once, numbered to match the inline citations.
- If notes disagree or are missing evidence on a point, say so explicitly.
- Aim for 4-8 short paragraphs unless the question demands more.`

// Synthesize combines worker notes into a final report body.
func Synthesize(
	ctx context.Context,
	client *agent.Client,
	question string,
	notes []Note,
) (string, agent.Usage, error) {
	prompt := buildSynthesisPrompt(question, notes)
	// Use Loop with an empty toolset so we get aggregate Usage back. Ask would
	// be one line shorter but discards usage.
	result, err := client.Loop(ctx, synthesizerSystem,
		[]agent.Message{agent.NewUserMessage(prompt)},
		agent.Toolset{}, 1)
	if err != nil {
		return "", result.Usage, fmt.Errorf("synthesizer: %w", err)
	}
	return result.FinalText(), result.Usage, nil
}

func buildSynthesisPrompt(question string, notes []Note) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Original question: %s\n\n", question)
	fmt.Fprintf(&b, "You have %d notes from research workers.\n\n", len(notes))
	for i, n := range notes {
		fmt.Fprintf(&b, "--- Note %d ---\n", i+1)
		fmt.Fprintf(&b, "Sub-question: %s\n", n.Question)
		if n.Err != "" {
			fmt.Fprintf(&b, "(worker error: %s)\n", n.Err)
		}
		if n.Summary != "" {
			fmt.Fprintf(&b, "Summary: %s\n", n.Summary)
		}
		if len(n.Citations) > 0 {
			b.WriteString("Citations:\n")
			for _, c := range n.Citations {
				if c.Title != "" {
					fmt.Fprintf(&b, "  - %s — %s\n", c.Title, c.URL)
				} else {
					fmt.Fprintf(&b, "  - %s\n", c.URL)
				}
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("Now write the final answer with inline [n] citations and a Sources section.")
	return b.String()
}
