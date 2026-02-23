package server

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/dylfrancis/revue/db"
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

	// Title input for the feature/item being worked on
	titleInput := slack.NewPlainTextInputBlockElement(
		slack.NewTextBlockObject("plain_text", "e.g. User authentication, Bug fix for login", false, false),
		"title",
	)
	titleBlock := slack.NewInputBlock(
		"title_block",
		slack.NewTextBlockObject("plain_text", "Feature / Item", false, false),
		nil,
		titleInput,
	)
	blocks = append(blocks, titleBlock)

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

// statusEmoji maps a PR status to its display emoji.
func statusEmoji(status string) string {
	switch status {
	case "approved":
		return ":white_check_mark:"
	case "changes_requested":
		return ":x:"
	case "merged":
		return ":purple_circle:"
	case "closed":
		return ":red_circle:"
	default: // "open"
		return ":white_circle:"
	}
}

// statusLabel maps a PR status to a human-readable label.
func statusLabel(status string) string {
	switch status {
	case "approved":
		return "approved"
	case "changes_requested":
		return "changes requested"
	case "merged":
		return "merged"
	case "closed":
		return "closed"
	default:
		return "awaiting review"
	}
}

// updateTrackerMessage fetches the current state of a tracker from the DB
// and updates the Slack message with the latest PR statuses.
func updateTrackerMessage(trackerID int64) error {
	tracker, err := db.GetTrackerByID(database, trackerID)
	if err != nil {
		return fmt.Errorf("failed to get tracker: %w", err)
	}

	prs, err := db.GetPullRequestsByTracker(database, trackerID)
	if err != nil {
		return fmt.Errorf("failed to get PRs: %w", err)
	}

	// Collect all unique reviewers across all PRs
	reviewerSet := make(map[string]bool)
	for _, pr := range prs {
		reviewers, err := db.GetReviewersByPR(database, pr.ID)
		if err != nil {
			return fmt.Errorf("failed to get reviewers: %w", err)
		}
		for _, uid := range reviewers {
			reviewerSet[uid] = true
		}
	}

	// Build the message
	title := fmt.Sprintf("*%s*", tracker.Title)
	if tracker.Title == "" {
		title = "*PR Tracker*"
	}
	if tracker.Status == "completed" {
		title += " — :tada: All done!"
	}

	var lines []string
	lines = append(lines, title+"\n")
	for _, pr := range prs {
		suffix := fmt.Sprintf(" (%d/%d approvals)", pr.ApprovalsCurrent, pr.ApprovalsRequired)
		if pr.Status == "merged" || pr.Status == "closed" {
			suffix = ""
		}
		prLabel := fmt.Sprintf("%s/%s#%d", pr.GithubOwner, pr.GithubRepo, pr.GithubPRNumber)
		if pr.Title != "" {
			prLabel = pr.Title
		}
		lines = append(lines, fmt.Sprintf("• <%s|%s> — %s %s%s",
			pr.GithubPRURL, prLabel,
			statusEmoji(pr.Status), statusLabel(pr.Status), suffix))
	}

	var mentions []string
	for uid := range reviewerSet {
		mentions = append(mentions, fmt.Sprintf("<@%s>", uid))
	}
	lines = append(lines, "\nReviewers: "+strings.Join(mentions, " "))

	text := strings.Join(lines, "\n")

	_, _, _, err = slackClient.UpdateMessage(
		tracker.SlackChannelID,
		tracker.SlackMessageTS,
		slack.MsgOptionText(text, false),
	)
	if err != nil {
		return fmt.Errorf("failed to update message: %w", err)
	}

	return nil
}
