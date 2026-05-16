package main

import (
	"context"
	"log"
	"os"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

func main() {
	uri := os.Getenv("NEO4J_URL")
	user := os.Getenv("NEO4J_USER")
	pass := os.Getenv("NEO4J_PASS")
	
	driver, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(user, pass, ""))
	if err != nil {
		log.Fatal(err)
	}
	defer driver.Close(context.Background())

	ctx := context.Background()
	
	threshold := os.Getenv("MESH_LINK_THRESHOLD")
	if threshold == "" {
		threshold = "0.70"
	}

	// Cypher query to automatically link shards based on embedding similarity
	query := `
	MATCH (s1:Shard)
	MATCH (s2:Shard)
	WHERE elementId(s1) < elementId(s2)
	  AND size(s1.embedding) = 3072 
	  AND size(s2.embedding) = 3072
	WITH s1, s2, gds.similarity.cosine(s1.embedding, s2.embedding) AS sim
	WHERE sim > parseFloat($threshold)
	MERGE (s1)-[r:CONNECTED_TO]->(s2)
	SET r.weight = sim
	RETURN s1.id as from, s2.id as to, sim
	`
	
	result, err := neo4j.ExecuteQuery(ctx, driver, query, map[string]any{"threshold": threshold}, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase("neo4j"))
	if err != nil {
		log.Fatalf("Failed to link shards: %v", err)
	}

	for _, record := range result.Records {
		from, _ := record.Get("from")
		to, _ := record.Get("to")
		sim, _ := record.Get("sim")
		log.Printf("[LINK] Bond Created: %s <-> %s (Sim: %.4f)", from, to, sim)
	}
	log.Printf("Knowledge Mesh Synchronization Complete. %d bonds active.", len(result.Records))
}
