package server

import (
	"fmt"
	"log"
	"net/http"

	"github.com/slack-go/slack"
)

func handleSlashCommand(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	command := r.FormValue("command")
	text := r.FormValue("text")
	triggerID := r.FormValue("trigger_id")
	channelID := r.FormValue("channel_id")

	log.Printf("Received command: %s %s", command, text)

	if text == "track" {
		if err := openTrackModal(triggerID, channelID); err != nil {
			log.Printf("Error opening modal: %v", err)
			http.Error(w, "Failed to open modal", http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
}

// buildTrackModalBlocks builds the Block Kit blocks for the track modal.
// numURLFields controls how many PR URL input fields to show.
// This is called both when opening the modal (with 1 field) and when
// updating it after the user clicks "Add another PR".
func buildTrackModalBlocks(numURLFields int) slack.Blocks {
	var blocks []slack.Block

	// One input block per URL field
	for i := 0; i < numURLFields; i++ {
		urlInput := slack.NewPlainTextInputBlockElement(
			slack.NewTextBlockObject("plain_text", "https://github.com/owner/repo/pull/123", false, false),
			fmt.Sprintf("pr_url_%d", i),
		)

		blockID := fmt.Sprintf("pr_url_block_%d", i)
		label := slack.NewTextBlockObject("plain_text", fmt.Sprintf("PR URL #%d", i+1), false, false)
		inputBlock := slack.NewInputBlock(blockID, label, nil, urlInput)
		blocks = append(blocks, inputBlock)
	}

	// Action block with Add / Remove buttons
	addBtn := slack.NewButtonBlockElement("add_pr_url", "", slack.NewTextBlockObject("plain_text", "+ Add another PR", false, false))
	var actionElements []slack.BlockElement
	actionElements = append(actionElements, addBtn)

	if numURLFields > 1 {
		removeBtn := slack.NewButtonBlockElement("remove_pr_url", "", slack.NewTextBlockObject("plain_text", "- Remove last", false, false)).
			WithStyle(slack.StyleDanger)
		actionElements = append(actionElements, removeBtn)
	}

	blocks = append(blocks, slack.NewActionBlock("pr_url_actions", actionElements...))

	// Reviewers multi-user select
	reviewerSelect := slack.NewOptionsMultiSelectBlockElement(
		slack.MultiOptTypeUser,
		slack.NewTextBlockObject("plain_text", "Select reviewers", false, false),
		"reviewers",
	)
	reviewerBlock := slack.NewInputBlock(
		"reviewers_block",
		slack.NewTextBlockObject("plain_text", "Reviewers", false, false),
		nil,
		reviewerSelect,
	)
	blocks = append(blocks, reviewerBlock)

	return slack.Blocks{BlockSet: blocks}
}

// openTrackModal opens the "Track PRs" modal with 1 URL field to start.
func openTrackModal(triggerID string, channelID string) error {
	modal := slack.ModalViewRequest{
		Type:            slack.VTModal,
		CallbackID:      "track_pr",
		Title:           slack.NewTextBlockObject("plain_text", "Track PRs", false, false),
		Submit:          slack.NewTextBlockObject("plain_text", "Submit", false, false),
		Close:           slack.NewTextBlockObject("plain_text", "Cancel", false, false),
		PrivateMetadata: channelID,
		Blocks:          buildTrackModalBlocks(1),
	}

	_, err := slackClient.OpenView(triggerID, modal)
	if err != nil {
		return fmt.Errorf("failed to open modal: %w", err)
	}

	return nil
}
