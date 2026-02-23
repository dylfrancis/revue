package server

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/google/go-github/v83/github"
	"github.com/slack-go/slack"
)

var (
	slackClient         *slack.Client
	githubClient        *github.Client
	signingSecret       string
	githubWebhookSecret string
	database            *sql.DB
)

func Start(port string, slackBotToken string, slackSigningSecret string, ghWebhookSecret string, ghToken string, db *sql.DB) error {
	slackClient = slack.New(slackBotToken)
	githubClient = github.NewClient(nil).WithAuthToken(ghToken)
	signingSecret = slackSigningSecret
	githubWebhookSecret = ghWebhookSecret
	database = db

	http.HandleFunc("/slack/commands", verifySlackRequest(handleSlashCommand))
	http.HandleFunc("/slack/interactions", verifySlackRequest(handleInteraction))
	http.HandleFunc("/github/webhooks", handleGitHubWebhook)

	log.Printf("Server started on port %s", port)
	return http.ListenAndServe(fmt.Sprintf(":%s", port), nil)
}

// verifySlackRequest is middleware that verifies the request signature
// from Slack using HMAC-SHA256. It wraps a handler function and rejects
// requests with invalid or missing signatures.
func verifySlackRequest(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		verifier, err := slack.NewSecretsVerifier(r.Header, signingSecret)
		if err != nil {
			log.Printf("Failed to create secrets verifier: %v", err)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("Failed to read request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		r.Body.Close()

		if _, err := verifier.Write(body); err != nil {
			log.Printf("Failed to write body to verifier: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if err := verifier.Ensure(); err != nil {
			log.Printf("Invalid Slack signature: %v", err)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		// Put the body back so downstream handlers can read it.
		// r.Body was consumed by ReadAll above, so we create a new reader.
		r.Body = io.NopCloser(bytes.NewBuffer(body))

		next(w, r)
	}
}
