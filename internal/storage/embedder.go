package storage

import (
	"context"
	"encoding/binary"
	"math"
)

// MockEmbedder is a hardware-friendly placeholder for local testing.
// It generates a deterministic vector from text without using an LLM.
type MockEmbedder struct {
	dims int
}

func NewMockEmbedder(dims int) *MockEmbedder {
	return &MockEmbedder{dims: dims}
}

func (m *MockEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	vec := make([]float32, m.dims)
	// Simple deterministic generation for testing
	for i := 0; i < len(text) && i < m.dims; i++ {
		vec[i] = float32(text[i]) / 255.0
	}
	return vec, nil
}

func (m *MockEmbedder) Dimension() int {
	return m.dims
}

// encodeVector converts []float32 to []byte for storage in Shards.
func EncodeVector(floats []float32) []byte {
	b := make([]byte, len(floats)*4)
	for i, f := range floats {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}
