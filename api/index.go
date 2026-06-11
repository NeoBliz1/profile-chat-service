package api

import (
	"fmt"
	"log"
	"net/http"
	"strings"
)

var (
	cfg       *Config
	configErr error
)

func init() {
	// Safely capture the error without calling os.Exit(1)
	cfg, configErr = LoadConfig()
}

// Handler is the entry point for all Vercel serverless function requests.
func Handler(w http.ResponseWriter, r *http.Request) {
	// Catch initialization errors gracefully
	if configErr != nil {
		log.Printf("Runtime config error: %v", configErr)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		if _, err := fmt.Fprintf(w, `{"error": "Internal server configuration mismatch"}`); err != nil {
			log.Printf("Failed to write error response: %v", err)
		}
		return
	}

	// Clean up path variations (stripping trailing slashes or domain prefixes if any)
	path := r.URL.Path

	// Route requests based on the URL path.
	switch {
	case strings.HasPrefix(path, "/api/send"):
		MsgProxyResender(w, r, cfg)
	case strings.HasPrefix(path, "/api/check"):
		CheckReplyHandler(w, r, cfg)
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		if _, err := fmt.Fprintf(w, `{"error": "Route not found", "requested_path": "%s"}`, path); err != nil {
			log.Printf("Failed to write error response: %v", err)
		}
	}
}
