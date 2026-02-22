package server

import (
	"log"
	"net/http"
)

func handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	log.Println("Received GitHub webhook")
	w.WriteHeader(http.StatusOK)
}
