package storage

import (
	"encoding/binary"
	"math"
	"testing"
)

// helper to create a little-endian float32 vector blob
func encodeVector(v []float32) []byte {
	b := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

func TestVessel_Resonance(t *testing.T) {
	// 1. Initialize Vessel in memory
	v, err := NewVessel(":memory:")
	if err != nil {
		t.Fatalf("Failed to create vessel: %v", err)
	}
	defer v.Close()

	// 2. Prepare test data
	// Vector A: [1.0, 0.0]
	// Vector B: [0.0, 1.0]
	vecA := encodeVector([]float32{1.0, 0.0})
	vecB := encodeVector([]float32{0.0, 1.0})

	s1 := Shard{ID: "shard-a", Category: "memory", Content: "Horizontal", Vector: vecA}
	s2 := Shard{ID: "shard-b", Category: "memory", Content: "Vertical", Vector: vecB}

	if err := v.SaveShard(s1); err != nil {
		t.Fatal(err)
	}
	if err := v.SaveShard(s2); err != nil {
		t.Fatal(err)
	}

	// 3.Search using Vector A
	// It should return shard-a as the top result (distance 0.0)
	results, err := v.FindResonant(vecA, 1)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("Expected results, got none")
	}

	if results[0].ID != "shard-a" {
		t.Errorf("Expected shard-a to be most resonant, got %s", results[0].ID)
	}

	t.Logf("Resonance verified: Found %s as top match", results[0].ID)

}