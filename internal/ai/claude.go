package ai

import (
	"context"
	"fmt"
	"os"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const systemPrompt = `You are helping a developer write a personal diary entry.

The user provides a markdown template. Sections marked with <!-- AI: INSTRUCTION --> tell you what to write. Replace each such comment with actual prose — keep everything else (stats, commits, calendar, tasks, time log) exactly as-is.

Write in first person. Be specific about the work described. Mention what felt hard, what felt good, what you learned. 2-3 short paragraphs per section. Personal, not corporate.

Return only the completed markdown — no preamble, no explanation.`

// ErrNoAPIKey is returned when ANTHROPIC_API_KEY is not set.
var ErrNoAPIKey = fmt.Errorf("ANTHROPIC_API_KEY not set — set it or use Claude Desktop via MCP")

func client() (*anthropic.Client, error) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return nil, ErrNoAPIKey
	}
	c := anthropic.NewClient(option.WithAPIKey(key))
	return &c, nil
}

func userPrompt(body string) string {
	return "Here is my diary template for today. Fill in all <!-- AI: --> sections:\n\n" + body
}

// Fill calls Claude and returns the completed entry (blocking).
func Fill(body string) (string, error) {
	c, err := client()
	if err != nil {
		return "", err
	}

	msg, err := c.Messages.New(context.Background(), anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeHaiku4_5,
		MaxTokens: 1024,
		System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt(body)))},
	})
	if err != nil {
		return "", fmt.Errorf("Claude API: %w", err)
	}
	if len(msg.Content) == 0 {
		return "", fmt.Errorf("empty response from Claude")
	}
	result := msg.Content[0].Text
	if strings.TrimSpace(result) == "" {
		return "", fmt.Errorf("Claude returned blank response")
	}
	return result, nil
}

// StreamResult is sent back through the channel during streaming.
type StreamResult struct {
	Chunk string // non-empty = partial text
	Done  bool   // true = stream finished, Chunk holds full text
	Err   error  // non-nil = error, stream stopped
}

// Stream calls Claude and sends incremental results to ch.
// Caller must drain ch until Done or Err is received.
func Stream(body string, ch chan<- StreamResult) {
	c, err := client()
	if err != nil {
		ch <- StreamResult{Err: err}
		return
	}

	stream := c.Messages.NewStreaming(context.Background(), anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeHaiku4_5,
		MaxTokens: 1024,
		System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt(body)))},
	})

	var full strings.Builder
	for stream.Next() {
		event := stream.Current()
		if event.Type == "content_block_delta" && event.Delta.Type == "text_delta" {
			text := event.Delta.Text
			if text != "" {
				full.WriteString(text)
				ch <- StreamResult{Chunk: text}
			}
		}
	}
	if err := stream.Err(); err != nil {
		ch <- StreamResult{Err: fmt.Errorf("Claude stream: %w", err)}
		return
	}
	ch <- StreamResult{Done: true, Chunk: full.String()}
}
