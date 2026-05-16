package storage

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

// --- 1. MOCK EMBEDDER (Option: "none") ---

type MockEmbedder struct {
	dims int
}

func NewMockEmbedder(dims int) *MockEmbedder {
	return &MockEmbedder{dims: dims}
}

func (m *MockEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	vec := make([]float32, m.dims)
	for i := 0; i < len(text) && i < m.dims; i++ {
		vec[i] = float32(text[i]) / 255.0
	}
	return vec, nil
}

func (m *MockEmbedder) Dimension() int {
	return m.dims
}

// --- 2. GEMINI EMBEDDER (Option: "server") ---

type GeminiEmbedder struct {
	client *genai.Client
	model  string
}

func NewGeminiEmbedder(ctx context.Context, apiKey, model string) (*GeminiEmbedder, error) {
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}
	return &GeminiEmbedder{client: client, model: model}, nil
}

func (g *GeminiEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	em := g.client.EmbeddingModel(g.model)
	res, err := em.EmbedContent(ctx, genai.Text(text))
	if err != nil {
		return nil, fmt.Errorf("gemini embedding failed: %w", err)
	}
	return res.Embedding.Values, nil
}

func (g *GeminiEmbedder) Dimension() int {
	// Standard for text-embedding-004
	return 768 
}

func (g *GeminiEmbedder) Close() error {
	return g.client.Close()
}

// --- 3. TINY LOCAL EMBEDDER (Option: "local") ---

type TinyLocalEmbedder struct {
	// Placeholder for future Go-native lightweight model
}

func NewTinyLocalEmbedder() *TinyLocalEmbedder {
	return &TinyLocalEmbedder{}
}

func (l *TinyLocalEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	// For now, this acts like a more 'advanced' mock until we pick a library
	return []float32{0.1, 0.2, 0.3}, nil
}

func (l *TinyLocalEmbedder) Dimension() int {
	return 384
}

// --- HELPERS ---

func EncodeVector(floats []float32) []byte {
	b := make([]byte, len(floats)*4)
	for i, f := range floats {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}
