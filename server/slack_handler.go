package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

func handleSlashCommand(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	command := r.FormValue("command")
	text := r.FormValue("text")
	triggerID := r.FormValue("trigger_id")

	log.Printf("Received command: %s %s", command, text)

	if text == "track" {
		if err := openTrackModal(triggerID); err != nil {
			log.Printf("Error opening modal: %v", err)
			http.Error(w, "Failed to open modal", http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
}

func openTrackModal(triggerID string) error {
	modal := map[string]interface{}{
		"trigger_id": triggerID,
		"view": map[string]interface{}{
			"type":        "modal",
			"callback_id": "track_pr",
			"title":       map[string]string{"type": "plain_text", "text": "Track PRs"},
			"submit":      map[string]string{"type": "plain_text", "text": "Submit"},
			"close":       map[string]string{"type": "plain_text", "text": "Cancel"},
			"blocks": []map[string]interface{}{
				{
					"type":     "input",
					"block_id": "pr_urls_block",
					"label":    map[string]string{"type": "plain_text", "text": "PR URLs"},
					"element": map[string]interface{}{
						"type":        "plain_text_input",
						"action_id":   "pr_urls",
						"multiline":   true,
						"placeholder": map[string]string{"type": "plain_text", "text": "Paste one PR URL per line"},
					},
				},
				{
					"type":     "input",
					"block_id": "reviewers_block",
					"label":    map[string]string{"type": "plain_text", "text": "Reviewers"},
					"element": map[string]interface{}{
						"type":        "multi_users_select",
						"action_id":   "reviewers",
						"placeholder": map[string]string{"type": "plain_text", "text": "Select reviewers"},
					},
				},
			},
		},
	}

	body, err := json.Marshal(modal)
	if err != nil {
		return fmt.Errorf("failed to marshal modal: %w", err)
	}

	req, err := http.NewRequest("POST", "https://slack.com/api/views.open", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+botToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Printf("Error closing response body: %v", err)
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack API returned status %d", resp.StatusCode)
	}

	return nil
}
