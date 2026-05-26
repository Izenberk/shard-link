package storage

import (
	"context"
	"fmt"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

// Summarizer defines the contract for generating text summaries from prompts.
// This mirrors the Embedder pattern — allowing Cloud and Mock implementations.
type Summarizer interface {
	Summarize(ctx context.Context, prompt string) (string, error)
}

// --- MOCK SUMMARIZER (EMBEDDING_MODE=none) ---

type MockSummarizer struct{}

func NewMockSummarizer() *MockSummarizer {
	return &MockSummarizer{}
}

func (m *MockSummarizer) Summarize(ctx context.Context, prompt string) (string, error) {
	return "Mock community summary — LLM summarization disabled.", nil
}

// --- GEMINI SUMMARIZER (EMBEDDING_MODE=server) ---

type GeminiSummarizer struct {
	client *genai.Client
	model  string
}

func NewGeminiSummarizer(ctx context.Context, apiKey, model string) (*GeminiSummarizer, error) {
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini summarizer client: %w", err)
	}
	return &GeminiSummarizer{client: client, model: model}, nil
}

func (g *GeminiSummarizer) Summarize(ctx context.Context, prompt string) (string, error) {
	model := g.client.GenerativeModel(g.model)
	resp, err := model.GenerateContent(ctx, genai.Text(prompt))
	if err != nil {
		return "", fmt.Errorf("gemini summarization failed: %w", err)
	}

	// Extract text from the response candidates
	if resp == nil || len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return "", fmt.Errorf("gemini returned empty response")
	}

	var result string
	for _, part := range resp.Candidates[0].Content.Parts {
		if text, ok := part.(genai.Text); ok {
			result += string(text)
		}
	}

	if result == "" {
		return "", fmt.Errorf("gemini returned no text content")
	}

	return result, nil
}

func (g *GeminiSummarizer) Close() error {
	return g.client.Close()
}
