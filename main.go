package main

import (
	"log"
	"net/http"
	"profile-chat-service/api"
)

// App holds the application's dependencies.
type App struct {
	Config *api.Config
}

func main() {
	cfg, err := api.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	app := &App{Config: cfg}

	http.HandleFunc("/api/send", app.MsgProxyResender)
	http.HandleFunc("/api/check", app.CheckReplyHandler)

	log.Println("Server starting on port 8080...")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

// MsgProxyResender is the handler for the /api/send endpoint.
func (app *App) MsgProxyResender(w http.ResponseWriter, r *http.Request) {
	api.MsgProxyResender(w, r, app.Config)
}

// CheckReplyHandler is the handler for the /api/check endpoint.
func (app *App) CheckReplyHandler(w http.ResponseWriter, r *http.Request) {
	api.CheckReplyHandler(w, r, app.Config)
}
