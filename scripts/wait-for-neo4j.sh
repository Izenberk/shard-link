#!/bin/bash
# Blocks until the shard-link_graph (Neo4j) container reports "healthy" via
# its own Docker healthcheck, or exits 1 after TIMEOUT seconds.
#
# Without this, visual_ego dials bolt://localhost:7687 immediately on boot
# and loses the race against Neo4j's JVM cold start (ConnectivityError: EOF),
# spinning in systemd's restart-on-failure loop until Neo4j finally answers.
set -euo pipefail

CONTAINER="shard-link_graph"
TIMEOUT=150
INTERVAL=2
elapsed=0

while true; do
  status=$(docker inspect -f '{{.State.Health.Status}}' "$CONTAINER" 2>/dev/null || echo "unknown")
  if [ "$status" = "healthy" ]; then
    exit 0
  fi
  if [ "$elapsed" -ge "$TIMEOUT" ]; then
    echo "wait-for-neo4j: timed out after ${TIMEOUT}s waiting for $CONTAINER (last status: $status)" >&2
    exit 1
  fi
  sleep "$INTERVAL"
  elapsed=$((elapsed + INTERVAL))
done
