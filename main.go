package main

import (
	"fmt"
	"net/http"
)

func main() {
	fmt.Println("SHARD-LINK Hub Ignited.")
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Shard-Link Heartbeat: Active")
	})
	http.ListenAndServe(":8080", nil)
}
