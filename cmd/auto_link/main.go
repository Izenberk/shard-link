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
	
	// Cypher query to automatically link shards based on embedding similarity
	query := `
	MATCH (s1:Shard)
	MATCH (s2:Shard)
	WHERE elementId(s1) < elementId(s2)
	  AND size(s1.embedding) = 3072 
	  AND size(s2.embedding) = 3072
	WITH s1, s2, gds.similarity.cosine(s1.embedding, s2.embedding) AS sim
	WHERE sim > 0.70
	MERGE (s1)-[r:CONNECTED_TO]->(s2)
	SET r.weight = sim
	RETURN count(r) as bondsCreated
	`
	
	result, err := neo4j.ExecuteQuery(ctx, driver, query, nil, neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase("neo4j"))
	if err != nil {
		log.Fatalf("Failed to link shards: %v", err)
	}

	count, _ := result.Records[0].Get("bondsCreated")
	log.Printf("Successfully created %d semantic bonds in the Knowledge Mesh.", count)
}
