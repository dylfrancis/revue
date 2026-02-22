package server

import (
	"fmt"
	"log"
	"net/http"
)

var botToken string

func Start(port string, slackBotToken string) error {
	botToken = slackBotToken

	http.HandleFunc("/slack/commands", handleSlashCommand)
	http.HandleFunc("/slack/interactions", handleInteraction)
	http.HandleFunc("/github/webhooks", handleGitHubWebhook)

	log.Printf("Server started on port %s", port)
	return http.ListenAndServe(fmt.Sprintf(":%s", port), nil)
}

// TODO implement
func handleInteraction(w http.ResponseWriter, r *http.Request) {
	log.Println("Received interaction")
	w.WriteHeader(http.StatusOK)
}
