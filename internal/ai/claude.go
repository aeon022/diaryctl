package ai

import (
	"context"
	"fmt"
	"strings"

	coreai "github.com/aeon022/missionctl-core/ai"
)

const systemPrompt = `You are helping a developer write a personal diary entry.

The user provides a markdown template. Sections marked with <!-- AI: INSTRUCTION --> tell you what to write. Replace each such comment with actual prose — keep everything else (stats, commits, calendar, tasks, time log) exactly as-is.

Write in first person. Be specific about the work described. Mention what felt hard, what felt good, what you learned. 2-3 short paragraphs per section. Personal, not corporate.

Right after the "# YYYY-MM-DD" heading, insert two lines the template doesn't already have:
  ## <a short, specific, descriptive title for what the day was actually about — not "Daily Update" or the date>
  **Tags:** <one life category, then optionally 1-2 more specific tags>

For the category, reuse one already seen in past entries if this day's work clearly matches it; otherwise pick the closest fit from: FH, Studium, Projekt, Day-Job. Only invent a new category if none of those fit at all.

Return only the completed markdown — no preamble, no explanation.`

func userPrompt(body string) string {
	return "Here is my diary template for today. Fill in all <!-- AI: --> sections:\n\n" + body
}

// Fill calls the configured AI provider (Anthropic/OpenAI/Gemini/local
// Ollama — see missionctl-core/ai) and returns the completed entry (blocking).
func Fill(body string) (string, error) {
	info, err := coreai.Detect("DIARYCTL")
	if err != nil {
		return "", err
	}
	result, err := coreai.Call(context.Background(), info, systemPrompt, userPrompt(body), nil)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(result) == "" {
		return "", fmt.Errorf("empty response from AI provider")
	}
	return result, nil
}

// StreamResult is sent back through the channel during streaming.
type StreamResult struct {
	Chunk string // non-empty = partial text
	Done  bool   // true = stream finished, Chunk holds full text
	Err   error  // non-nil = error, stream stopped
}

// Stream calls the configured AI provider and sends incremental results to ch.
// Caller must drain ch until Done or Err is received.
func Stream(body string, ch chan<- StreamResult) {
	info, err := coreai.Detect("DIARYCTL")
	if err != nil {
		ch <- StreamResult{Err: err}
		return
	}

	fullText, err := coreai.Call(context.Background(), info, systemPrompt, userPrompt(body), func(chunk string) {
		ch <- StreamResult{Chunk: chunk}
	})
	if err != nil {
		ch <- StreamResult{Err: err}
		return
	}
	ch <- StreamResult{Done: true, Chunk: fullText}
}
