package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
)

func main() {
	// 1. Configure the Remote Target
	targetURL := os.Getenv("TARGET_URL")
	if targetURL == "" {
		targetURL = "https://hub.izenberk.com" 
	}
	target, err := url.Parse(targetURL)
	if err != nil {
		log.Fatal("Invalid target URL:", err)
	}

	// 2. Load the mTLS Keys (The "Keyholder" part)
	cert, err := tls.LoadX509KeyPair("certs/client.crt", "certs/client.key")
	if err != nil {
		log.Fatalf("Failed to load client cert/key: %v. (Check if certs/client.crt and certs/client.key exist)", err)
	}

	caCert, err := os.ReadFile("certs/ca.crt")
	if err != nil {
		log.Fatalf("Failed to read CA cert: %v. (Check if certs/ca.crt exists)", err)
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	// 3. Create the Secured Reverse Proxy
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			RootCAs:      caCertPool,
		},
	}

	// 4. Handle Incoming Local Requests
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[Keyholder] Proxying: %s %s -> %s", r.Method, r.URL.Path, target.String())
		r.Host = target.Host
		proxy.ServeHTTP(w, r)
	})

	port := 8081
	fmt.Printf("Shard-Link Keyholder Proxy active on http://localhost:%d\n", port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}
