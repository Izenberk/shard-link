package storage

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net/http"

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
	client    *genai.Client
	model     string
	targetDim int
}

func NewGeminiEmbedder(ctx context.Context, apiKey, model string, targetDim int) (*GeminiEmbedder, error) {
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}
	return &GeminiEmbedder{client: client, model: model, targetDim: targetDim}, nil
}

func (g *GeminiEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	em := g.client.EmbeddingModel(g.model)
	res, err := em.EmbedContent(ctx, genai.Text(text))
	if err != nil {
		return nil, fmt.Errorf("gemini embedding failed: %w", err)
	}

	vec := res.Embedding.Values

	// Matryoshka truncation: take first N dimensions, then L2-normalize.
	// Required when the Go SDK doesn't expose OutputDimensionality.
	if g.targetDim > 0 && g.targetDim < len(vec) {
		vec = l2Normalize(vec[:g.targetDim])
	}

	return vec, nil
}

func (g *GeminiEmbedder) Dimension() int {
	return g.targetDim
}

// l2Normalize divides each element by the vector's L2 norm.
// Required after Matryoshka truncation to maintain unit-length vectors for cosine similarity.
func l2Normalize(vec []float32) []float32 {
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	if sum == 0 {
		return vec
	}
	norm := float32(math.Sqrt(sum))
	out := make([]float32, len(vec))
	for i, v := range vec {
		out[i] = v / norm
	}
	return out
}

func (g *GeminiEmbedder) Close() error {
	return g.client.Close()
}

// --- 3. OLLAMA EMBEDDER (Option: "local") ---

type OllamaEmbedder struct {
	url   string
	model string
	dim   int
}

func NewOllamaEmbedder(url, model string, dim int) *OllamaEmbedder {
	if url == "" {
		url = "http://localhost:11434"
	}
	if model == "" {
		model = "nomic-embed-text"
	}
	if dim <= 0 {
		dim = 768 // nomic-embed-text default
	}
	return &OllamaEmbedder{url: url, model: model, dim: dim}
}

type ollamaResponse struct {
	Embedding []float32 `json:"embedding"`
}

func (o *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	payload := map[string]string{
		"model":  o.model,
		"prompt": text,
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequestWithContext(ctx, "POST", o.url+"/api/embeddings", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama returned status: %d", resp.StatusCode)
	}

	var res ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("failed to decode ollama response: %w", err)
	}

	return res.Embedding, nil
}

func (o *OllamaEmbedder) Dimension() int {
	return o.dim
}

// --- HELPERS ---

func EncodeVector(floats []float32) []byte {
	b := make([]byte, len(floats)*4)
	for i, f := range floats {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}
