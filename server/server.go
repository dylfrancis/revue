package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/dylfrancis/revue/db"
	"github.com/slack-go/slack"
)

var (
	slackClient   *slack.Client
	signingSecret string
	database      *sql.DB
)

func Start(port string, slackBotToken string, slackSigningSecret string, db *sql.DB) error {
	slackClient = slack.New(slackBotToken)
	signingSecret = slackSigningSecret
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
// It parses PR URLs, saves everything to the database, and posts a
// summary message to the Slack channel.
func handleTrackPRSubmission(w http.ResponseWriter, payload slack.InteractionCallback) {
	channelID := payload.View.PrivateMetadata
	values := payload.View.State.Values

	// Extract PR URLs from the dynamic input fields.
	// Each field has block_id "pr_url_block_0", "pr_url_block_1", etc.
	// and action_id "pr_url_0", "pr_url_1", etc.
	var prs []parsedPR
	for i := 0; ; i++ {
		blockID := fmt.Sprintf("pr_url_block_%d", i)
		actionID := fmt.Sprintf("pr_url_%d", i)

		block, ok := values[blockID]
		if !ok {
			break // no more URL fields
		}

		raw := block[actionID].Value
		pr, err := parsePRURL(raw)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(w).Encode(map[string]interface{}{
				"response_action": "errors",
				"errors": map[string]string{
					blockID: err.Error(),
				},
			})
			if err != nil {
				log.Printf("Failed to encode JSON response: %v", err)
				return
			}
			return
		}
		prs = append(prs, pr)
	}

	if len(prs) == 0 {
		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(map[string]interface{}{
			"response_action": "errors",
			"errors": map[string]string{
				"pr_url_block_0": "At least one PR URL is required",
			},
		})
		if err != nil {
			log.Printf("Failed to encode JSON response: %v", err)
			return
		}
		return
	}

	reviewerIDs := values["reviewers_block"]["reviewers"].SelectedUsers

	trackerID, err := db.CreateTracker(database, channelID)
	if err != nil {
		log.Printf("Failed to create tracker: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Insert each PR and link all reviewers to it
	for _, pr := range prs {
		prID, err := db.CreatePullRequest(database, trackerID, pr.Owner, pr.Repo, pr.Number, pr.URL)
		if err != nil {
			log.Printf("Failed to create pull request: %v", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		for _, reviewerID := range reviewerIDs {
			if err := db.CreateReviewer(database, prID, reviewerID); err != nil {
				log.Printf("Failed to create reviewer: %v", err)
				http.Error(w, "Internal error", http.StatusInternalServerError)
				return
			}
		}
	}

	messageTS, err := postTrackerMessage(channelID, prs, reviewerIDs)
	if err != nil {
		log.Printf("Failed to post tracker message: %v", err)
		// DB rows created but message failed — still close the modal
		w.WriteHeader(http.StatusOK)
		return
	}

	// Save the message timestamp so we can update this message later
	if err := db.UpdateTrackerMessageTS(database, trackerID, messageTS); err != nil {
		log.Printf("Failed to update tracker message TS: %v", err)
	}

	w.WriteHeader(http.StatusOK)
}

// postTrackerMessage sends a summary of tracked PRs to the Slack channel
// and returns the message timestamp (used to update the message later).
func postTrackerMessage(channelID string, prs []parsedPR, reviewerIDs []string) (string, error) {
	var lines []string
	lines = append(lines, "*PR Tracker*\n")
	for _, pr := range prs {
		lines = append(lines, fmt.Sprintf("• <%s|%s/%s#%d> — :white_circle: awaiting review",
			pr.URL, pr.Owner, pr.Repo, pr.Number))
	}

	var mentions []string
	for _, uid := range reviewerIDs {
		mentions = append(mentions, fmt.Sprintf("<@%s>", uid))
	}
	lines = append(lines, "\nReviewers: "+strings.Join(mentions, " "))

	text := strings.Join(lines, "\n")

	_, ts, err := slackClient.PostMessage(
		channelID,
		slack.MsgOptionText(text, false),
	)
	if err != nil {
		return "", fmt.Errorf("failed to post message: %w", err)
	}

	return ts, nil
}
