package api

import (
	"log"
	"net/http"
	"strings"
)

var (
	cfg *Config
)

func init() {
	var err error
	cfg, err = LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration for Vercel handler: %v", err)
	}
}

// Handler is the entry point for all Vercel serverless function requests.
func Handler(w http.ResponseWriter, r *http.Request) {
	// Route requests based on the URL path.
	switch {
	case strings.HasPrefix(r.URL.Path, "/api/send"):
		MsgProxyResender(w, r, cfg)
	case strings.HasPrefix(r.URL.Path, "/api/check"):
		CheckReplyHandler(w, r, cfg)
	default:
		// If no route matches, return a 404 Not Found error.
		http.NotFound(w, r)
	}
}
