package main

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
)

func encodeVector(v []float32) string {
	b := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return base64.StdEncoding.EncodeToString(b)
}

func main() {
	// Let's create 3 distinct vectors (1536 dimensions)
	// We only set the first few dimensions to keep it simple but distinct
	
	// Vector A: [1.0, 0.0, 0.0, ...]
	vA := make([]float32, 1536)
	vA[0] = 1.0
	fmt.Printf("Vector A (Janitor): %s\n\n", encodeVector(vA)[:100])

	// Vector B: [0.0, 1.0, 0.0, ...]
	vB := make([]float32, 1536)
	vB[1] = 1.0
	fmt.Printf("Vector B (Vessel): %s\n\n", encodeVector(vB)[:100])

	// Vector C: [0.0, 0.0, 1.0, ...]
	vC := make([]float32, 1536)
	vC[2] = 1.0
	fmt.Printf("Vector C (Bridge): %s\n\n", encodeVector(vC)[:100])
}
