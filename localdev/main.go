package main

import (
	"log"
	"net/http"
	"profile-chat-service/api"
	"profile-chat-service/pkg"
)

func main() {
	// 1. Verify config loads locally from environment variables/system shell
	_, err := pkg.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to initialize system parameters: %v", err)
	}

	// 2. Pass ALL traffic directly to Vercel Entry Point Handler
	// This runs SetupCORS and checks OPTIONS automatically for all routes!
	http.HandleFunc("/api/", api.Handler)

	log.Println("Server starting on secure local port 8080...")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("Critical server engine crash: %v", err)
	}
}
