package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/izenberk/shard-link/internal/janitor"
	"github.com/izenberk/shard-link/internal/mcp"
	"github.com/izenberk/shard-link/internal/storage"
)

func main() {
	// 1. Ignite the Vessel
	v, err := storage.NewVessel("data/shard-link.db")
	if err != nil {
		log.Fatalf("Vessel failed to ignite: %v", err)
	}
	defer v.Close()

	// 2. Summon the Janitor (max 1000 shards, checks every minute)
	jan := janitor.NewJanitor(v, 1*time.Minute, 1000)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go jan.Run(ctx)		// Run the Janitor in the background

	// 3. Launch the Bridge
	srv := mcp.NewMCPServer(v)

	// Graceful Shutdown handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)

	go func() {
		if err := srv.StartSSE(8080); err != nil {
			log.Fatalf("Bridge collapsed: %v", err)
		}
	}()

	log.Println("SHARD-LINK Hub is ONLINE.")
	<-sigChan	// Wait for Ctrl+C
	log.Println("SHARD-LINK Hub shutting down...")
}
