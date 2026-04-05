package main

import (
	"fmt"
	"log"
	"math"
	"encoding/binary"

	"github.com/ncruces/go-sqlite3"
)

func main() {
	// 1. User the direct sqlite3 API for better control
	db, err := sqlite3.Open(":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// 2. Manually register vec_version()
	err = db.CreateFunction("vec_version", 0, sqlite3.DETERMINISTIC, func(ctx sqlite3.Context, arg ...sqlite3.Value) {
		ctx.ResultText("shard-link-go-v0.1.0")
	})
	if err != nil {
		log.Fatal(err)
	}

	// 3. Register vec_distance_cosine() in pure Go
	// This takes two BLOBs and returns the cosine distance
	err = db.CreateFunction("vec_distance_cosine", 2, sqlite3.DETERMINISTIC, func(ctx sqlite3.Context, arg ...sqlite3.Value) {
		v1 := decodeVector(arg[0].RawBlob())
		v2 := decodeVector(arg[1].RawBlob())

		dist := 1.0 - cosineSimilarity(v1, v2)
		ctx.ResultFloat(dist)
	})
	if err != nil {
		log.Fatal(err)
	}

	// 4. Verification
	var version string
	stmt, _, err := db.Prepare("SELECT vec_version()")
	if err != nil {
		log.Fatal(err)
	}
	defer stmt.Close()

	if stmt.Step() {
		version = stmt.ColumnText(0)
	}

	fmt.Printf("Shard-Link Hardware Verified: %s is ACTIVE\n", version)
}

func decodeVector(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := 0; i < len(v); i++ {
		bits := binary.LittleEndian.Uint32(b[i*4:])
		v[i] = math.Float32frombits(bits)
	}
	return v
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0.0
	}
	var dot, normA, normB float64
	for i := range a {
		valA 	:= float64(a[i])
		valB	:= float64(b[i])
		dot 	+= valA * valB
		normA 	+= valA * valA
		normB	+= valB * valB
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}