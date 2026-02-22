package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/slack-go/slack"
)

var (
	slackClient *slack.Client
	database    *sql.DB
)

func Start(port string, slackBotToken string, db *sql.DB) error {
	slackClient = slack.New(slackBotToken)
	database = db

	http.HandleFunc("/slack/commands", handleSlashCommand)
	http.HandleFunc("/slack/interactions", handleInteraction)
	http.HandleFunc("/github/webhooks", handleGitHubWebhook)

	log.Printf("Server started on port %s", port)
	return http.ListenAndServe(fmt.Sprintf(":%s", port), nil)
}

func handleInteraction(w http.ResponseWriter, r *http.Request) {
	var payload slack.InteractionCallback
	if err := json.Unmarshal([]byte(r.FormValue("payload")), &payload); err != nil {
		log.Printf("Failed to parse interaction payload: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	switch payload.Type {
	case slack.InteractionTypeBlockActions:
		handleBlockAction(w, payload)
	case slack.InteractionTypeViewSubmission:
		handleViewSubmission(w, payload)
	default:
		log.Printf("Unhandled interaction type: %s", payload.Type)
		w.WriteHeader(http.StatusOK)
	}
}

// handleBlockAction processes button clicks inside modals.
// For the track modal, it handles "Add another PR" and "Remove last".
func handleBlockAction(w http.ResponseWriter, payload slack.InteractionCallback) {
	if len(payload.ActionCallback.BlockActions) == 0 {
		w.WriteHeader(http.StatusOK)
		return
	}

	action := payload.ActionCallback.BlockActions[0]

	switch action.ActionID {
	case "add_pr_url", "remove_pr_url":
		// Count current URL fields by looking at block IDs in the existing view.
		// Our URL blocks are named "pr_url_block_0", "pr_url_block_1", etc.
		numURLFields := 0
		for _, block := range payload.View.Blocks.BlockSet {
			if strings.HasPrefix(block.ID(), "pr_url_block_") {
				numURLFields++
			}
		}

		if action.ActionID == "add_pr_url" {
			numURLFields++
		} else if numURLFields > 1 {
			numURLFields--
		}

		// Rebuild the modal with the new field count
		modal := slack.ModalViewRequest{
			Type:            slack.VTModal,
			CallbackID:      "track_pr",
			Title:           slack.NewTextBlockObject("plain_text", "Track PRs", false, false),
			Submit:          slack.NewTextBlockObject("plain_text", "Submit", false, false),
			Close:           slack.NewTextBlockObject("plain_text", "Cancel", false, false),
			PrivateMetadata: payload.View.PrivateMetadata,
			Blocks:          buildTrackModalBlocks(numURLFields),
		}

		// UpdateView replaces the current modal content in-place.
		// We pass the view ID so Slack knows which modal to update.
		_, err := slackClient.UpdateView(modal, "", "", payload.View.ID)
		if err != nil {
			log.Printf("Failed to update view: %v", err)
		}
	}

	w.WriteHeader(http.StatusOK)
}

// handleViewSubmission processes modal form submissions.
func handleViewSubmission(w http.ResponseWriter, payload slack.InteractionCallback) {
	switch payload.View.CallbackID {
	case "track_pr":
		handleTrackPRSubmission(w, payload)
	default:
		log.Printf("Unhandled view submission callback: %s", payload.View.CallbackID)
		w.WriteHeader(http.StatusOK)
	}
}

// handleTrackPRSubmission processes the "Track PRs" modal submission.
// TODO: parse PR URLs, save to DB, post summary message
func handleTrackPRSubmission(w http.ResponseWriter, payload slack.InteractionCallback) {
	log.Printf("Track PR submission from channel: %s", payload.View.PrivateMetadata)
	w.WriteHeader(http.StatusOK)
}
