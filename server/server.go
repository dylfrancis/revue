package server

import (
	"fmt"
	"log"
	"net/http"
)

func Start(port string) error {
	http.HandleFunc("/slack/commands", handleSlashCommand)
	http.HandleFunc("/slack/interactions", handleInteraction)
	http.HandleFunc("/github/webhooks", handleGitHubWebhook)

	log.Printf("Server started on port %s", port)
	return http.ListenAndServe(fmt.Sprintf(":%s", port), nil)
}

func handleSlashCommand(w http.ResponseWriter, r *http.Request) {
	log.Println("Received slash command")
	w.WriteHeader(http.StatusOK)
}

func handleInteraction(w http.ResponseWriter, r *http.Request) {
	log.Println("Received interaction")
	w.WriteHeader(http.StatusOK)
}

func handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	log.Println("Received GitHub webhook")
	w.WriteHeader(http.StatusOK)
}
