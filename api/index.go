package api

import (
	"fmt"
	"log"
	"net/http"
	pkg2 "profile-chat-service/pkg"
	"strings"
)

var (
	Cfg       *pkg2.Config
	ConfigErr error
)

func init() {
	// Safely capture the error without calling os.Exit(1)
	Cfg, ConfigErr = pkg2.LoadConfig()
}

// Handler is the entry point for all Vercel serverless function requests.
func Handler(w http.ResponseWriter, r *http.Request) {
	// 1. Centralize CORS handling for all responses.
	if err := pkg2.SetupCORS(w, Cfg); err != nil {
		// If CORS setup fails, it's a server config issue.
		// WriteErrorResponse is not used here because it would be a circular dependency on CORS.
		log.Printf("FATAL: CORS configuration failed: %v", err)
		http.Error(w, `{"error":"CORS configuration error"}`, http.StatusInternalServerError)
		return
	}

	// 2. Handle pre-flight OPTIONS requests globally.
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent) // Use 204 No Content for OPTIONS
		return
	}

	// 3. Handle any potential configuration loading errors.
	if ConfigErr != nil {
		log.Printf("Runtime config error: %v", ConfigErr)
		pkg2.WriteErrorResponse(w, http.StatusInternalServerError, "Internal server configuration mismatch")
		return
	}

	// 4. Route the request to the appropriate handler.
	path := r.URL.Path
	switch {
	case strings.HasPrefix(path, "/api/send"):
		MsgProxyResender(w, r, Cfg)
	case strings.HasPrefix(path, "/api/check"):
		CheckReplyHandler(w, r, Cfg)
	default:
		pkg2.WriteErrorResponse(w, http.StatusNotFound, fmt.Sprintf("Route not found: %s", path))
	}
}
