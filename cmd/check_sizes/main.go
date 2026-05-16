package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

func main() {
	ctx := context.Background()
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		log.Fatal("GEMINI_API_KEY not set")
	}

	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	em := client.EmbeddingModel("gemini-embedding-001")
	res, err := em.EmbedContent(ctx, genai.Text("Hello world"))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Model: gemini-embedding-001 | Dims: %d\n", len(res.Embedding.Values))

	em2 := client.EmbeddingModel("text-embedding-004")
	res2, err := em2.EmbedContent(ctx, genai.Text("Hello world"))
	if err == nil {
		fmt.Printf("Model: text-embedding-004 | Dims: %d\n", len(res2.Embedding.Values))
	} else {
		fmt.Printf("Model: text-embedding-004 failed: %v\n", err)
	}
}
