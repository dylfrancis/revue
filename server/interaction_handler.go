package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/dylfrancis/revue/db"
	"github.com/slack-go/slack"
)

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
		handleAddRemovePR(payload)
	case "edit_tracker":
		handleEditTrackerButton(payload)
	}

	w.WriteHeader(http.StatusOK)
}

// handleAddRemovePR handles the add/remove PR URL buttons in both
// the create and edit modals.
func handleAddRemovePR(payload slack.InteractionCallback) {
	action := payload.ActionCallback.BlockActions[0]
	values := payload.View.State.Values

	// Count current URL fields and collect their values
	numURLFields := 0
	var currentURLs []string
	for i := 0; ; i++ {
		blockID := fmt.Sprintf("pr_url_block_%d", i)
		actionID := fmt.Sprintf("pr_url_%d", i)
		block, ok := values[blockID]
		if !ok {
			break
		}
		currentURLs = append(currentURLs, block[actionID].Value)
		numURLFields++
	}

	if action.ActionID == "add_pr_url" {
		numURLFields++
	} else if numURLFields > 1 {
		numURLFields--
		currentURLs = currentURLs[:len(currentURLs)-1]
	}

	// Rebuild the modal with the new field count.
	// For edit modals, use buildEditModalBlocks to preserve values.
	isEdit := payload.View.CallbackID == "edit_tracker"
	var blocks slack.Blocks
	if isEdit {
		currentTitle := values["title_block"]["title"].Value
		var reviewerIDs []string
		if sel, ok := values["reviewers_block"]["reviewers"]; ok {
			reviewerIDs = sel.SelectedUsers
		}
		blocks = buildEditModalBlocks(currentTitle, currentURLs, numURLFields, reviewerIDs)
	} else {
		blocks = buildTrackModalBlocks(numURLFields)
	}

	modal := slack.ModalViewRequest{
		Type:            slack.VTModal,
		CallbackID:      payload.View.CallbackID,
		Title:           payload.View.Title,
		Submit:          slack.NewTextBlockObject("plain_text", "Submit", false, false),
		Close:           slack.NewTextBlockObject("plain_text", "Cancel", false, false),
		PrivateMetadata: payload.View.PrivateMetadata,
		Blocks:          blocks,
	}

	_, err := slackClient.UpdateView(modal, "", "", payload.View.ID)
	if err != nil {
		log.Printf("Failed to update view: %v", err)
	}
}

// handleEditTrackerButton opens the edit modal when the Edit button
// on a tracker message is clicked.
func handleEditTrackerButton(payload slack.InteractionCallback) {
	action := payload.ActionCallback.BlockActions[0]
	trackerID, err := strconv.ParseInt(action.Value, 10, 64)
	if err != nil {
		log.Printf("Invalid tracker ID in edit button: %v", err)
		return
	}

	tracker, err := db.GetTrackerByID(database, trackerID)
	if err != nil {
		log.Printf("Failed to get tracker %d: %v", trackerID, err)
		return
	}

	prs, err := db.GetPullRequestsByTracker(database, trackerID)
	if err != nil {
		log.Printf("Failed to get PRs for tracker %d: %v", trackerID, err)
		return
	}

	// Collect existing PR URLs
	var prURLs []string
	for _, pr := range prs {
		prURLs = append(prURLs, pr.GithubPRURL)
	}

	// Collect unique reviewer IDs across all PRs
	reviewerSet := make(map[string]bool)
	for _, pr := range prs {
		reviewers, err := db.GetReviewersByPR(database, pr.ID)
		if err != nil {
			log.Printf("Failed to get reviewers for PR %d: %v", pr.ID, err)
			continue
		}
		for _, uid := range reviewers {
			reviewerSet[uid] = true
		}
	}
	var reviewerIDs []string
	for uid := range reviewerSet {
		reviewerIDs = append(reviewerIDs, uid)
	}

	numFields := len(prURLs)
	if numFields == 0 {
		numFields = 1
	}

	modal := slack.ModalViewRequest{
		Type:            slack.VTModal,
		CallbackID:      "edit_tracker",
		Title:           slack.NewTextBlockObject("plain_text", "Edit Tracker", false, false),
		Submit:          slack.NewTextBlockObject("plain_text", "Save", false, false),
		Close:           slack.NewTextBlockObject("plain_text", "Cancel", false, false),
		PrivateMetadata: fmt.Sprintf("%d", trackerID),
		Blocks:          buildEditModalBlocks(tracker.Title, prURLs, numFields, reviewerIDs),
	}

	_, err = slackClient.OpenView(payload.TriggerID, modal)
	if err != nil {
		log.Printf("Failed to open edit modal: %v", err)
	}
}

// handleViewSubmission processes modal form submissions.
func handleViewSubmission(w http.ResponseWriter, payload slack.InteractionCallback) {
	switch payload.View.CallbackID {
	case "track_pr":
		handleTrackPRSubmission(w, payload)
	case "edit_tracker":
		handleEditTrackerSubmission(w, payload)
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

	title := values["title_block"]["title"].Value
	reviewerIDs := values["reviewers_block"]["reviewers"].SelectedUsers

	trackerID, err := db.CreateTracker(database, channelID, title)
	if err != nil {
		log.Printf("Failed to create tracker: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Fetch required approvals per repo (cache to avoid duplicate API calls
	// when multiple PRs are from the same repo)
	approvalCache := make(map[string]int)
	for _, pr := range prs {
		key := pr.Owner + "/" + pr.Repo
		if _, exists := approvalCache[key]; !exists {
			required, err := fetchRequiredApprovals(pr.Owner, pr.Repo)
			if err != nil {
				log.Printf("Failed to fetch approvals for %s: %v (defaulting to 1)", key, err)
				required = 1
			}
			approvalCache[key] = required
		}
	}

	// Insert each PR, fetch its current review state, and link reviewers
	for _, pr := range prs {
		approvalsRequired := approvalCache[pr.Owner+"/"+pr.Repo]

		// Fetch current state from GitHub (title + review state)
		reviewState, err := fetchPRReviewState(pr.Owner, pr.Repo, pr.Number)
		if err != nil {
			log.Printf("Failed to fetch review state for %s/%s#%d: %v", pr.Owner, pr.Repo, pr.Number, err)
		}

		prID, err := db.CreatePullRequest(database, trackerID, pr.Owner, pr.Repo, pr.Number, pr.URL, reviewState.Title, approvalsRequired)
		if err != nil {
			log.Printf("Failed to create pull request: %v", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}

		if reviewState.Approvals > 0 {
			if err := db.UpdatePullRequestApprovals(database, prID, reviewState.Approvals); err != nil {
				log.Printf("Failed to set initial approvals: %v", err)
			}
		}

		// Determine initial status (priority: merged > closed > changes_requested > approved)
		var initialStatus string
		switch {
		case reviewState.Merged:
			initialStatus = "merged"
		case reviewState.Closed:
			initialStatus = "closed"
		case reviewState.ChangesRequested:
			initialStatus = "changes_requested"
		case reviewState.Approvals >= approvalsRequired:
			initialStatus = "approved"
		}

		if initialStatus != "" {
			if err := db.UpdatePullRequestStatus(database, prID, initialStatus); err != nil {
				log.Printf("Failed to set initial status: %v", err)
			}
		}

		for _, reviewerID := range reviewerIDs {
			if err := db.CreateReviewer(database, prID, reviewerID); err != nil {
				log.Printf("Failed to create reviewer: %v", err)
				http.Error(w, "Internal error", http.StatusInternalServerError)
				return
			}
		}
	}

	messageTS, err := postTrackerMessage(channelID, title)
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

	// Immediately refresh the message with actual DB state
	// (accounts for pre-existing reviews fetched above)
	if err := updateTrackerMessage(trackerID); err != nil {
		log.Printf("Failed to refresh tracker message: %v", err)
	}

	w.WriteHeader(http.StatusOK)
}

// postTrackerMessage posts a placeholder message to the Slack channel
// and returns the message timestamp. The message is immediately refreshed
// by updateTrackerMessage with the full Block Kit content.
func postTrackerMessage(channelID string, title string) (string, error) {
	_, ts, err := slackClient.PostMessage(
		channelID,
		slack.MsgOptionText(fmt.Sprintf("*%s* — loading...", title), false),
	)
	if err != nil {
		return "", fmt.Errorf("failed to post message: %w", err)
	}

	return ts, nil
}

// handleEditTrackerSubmission processes the edit modal submission.
// It diffs the submitted values against the DB and applies changes.
func handleEditTrackerSubmission(w http.ResponseWriter, payload slack.InteractionCallback) {
	trackerID, err := strconv.ParseInt(payload.View.PrivateMetadata, 10, 64)
	if err != nil {
		log.Printf("Invalid tracker ID in edit submission: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	values := payload.View.State.Values

	// Extract submitted PR URLs
	var submittedPRs []parsedPR
	for i := 0; ; i++ {
		blockID := fmt.Sprintf("pr_url_block_%d", i)
		actionID := fmt.Sprintf("pr_url_%d", i)

		block, ok := values[blockID]
		if !ok {
			break
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
		submittedPRs = append(submittedPRs, pr)
	}

	if len(submittedPRs) == 0 {
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

	newTitle := values["title_block"]["title"].Value
	newReviewerIDs := values["reviewers_block"]["reviewers"].SelectedUsers

	// Update title if changed
	tracker, err := db.GetTrackerByID(database, trackerID)
	if err != nil {
		log.Printf("Failed to get tracker: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if newTitle != tracker.Title {
		if err := db.UpdateTrackerTitle(database, trackerID, newTitle); err != nil {
			log.Printf("Failed to update tracker title: %v", err)
		}
	}

	// Get existing PRs from DB
	existingPRs, err := db.GetPullRequestsByTracker(database, trackerID)
	if err != nil {
		log.Printf("Failed to get existing PRs: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Build a map of existing PRs by URL for quick lookup
	existingByURL := make(map[string]db.PullRequest)
	for _, pr := range existingPRs {
		existingByURL[pr.GithubPRURL] = pr
	}

	// Build a set of submitted URLs
	submittedURLs := make(map[string]bool)
	for _, pr := range submittedPRs {
		submittedURLs[pr.URL] = true
	}

	// Delete PRs that were removed
	for _, pr := range existingPRs {
		if !submittedURLs[pr.GithubPRURL] {
			if err := db.DeletePullRequest(database, pr.ID); err != nil {
				log.Printf("Failed to delete PR %d: %v", pr.ID, err)
			}
		}
	}

	// Add new PRs that don't exist yet
	approvalCache := make(map[string]int)
	for _, pr := range submittedPRs {
		if _, exists := existingByURL[pr.URL]; exists {
			continue // already tracked
		}

		// Fetch required approvals (with cache)
		key := pr.Owner + "/" + pr.Repo
		if _, exists := approvalCache[key]; !exists {
			required, err := fetchRequiredApprovals(pr.Owner, pr.Repo)
			if err != nil {
				log.Printf("Failed to fetch approvals for %s: %v (defaulting to 1)", key, err)
				required = 1
			}
			approvalCache[key] = required
		}
		approvalsRequired := approvalCache[key]

		// Fetch current state from GitHub
		reviewState, err := fetchPRReviewState(pr.Owner, pr.Repo, pr.Number)
		if err != nil {
			log.Printf("Failed to fetch review state for %s/%s#%d: %v", pr.Owner, pr.Repo, pr.Number, err)
		}

		prID, err := db.CreatePullRequest(database, trackerID, pr.Owner, pr.Repo, pr.Number, pr.URL, reviewState.Title, approvalsRequired)
		if err != nil {
			log.Printf("Failed to create pull request: %v", err)
			continue
		}

		if reviewState.Approvals > 0 {
			if err := db.UpdatePullRequestApprovals(database, prID, reviewState.Approvals); err != nil {
				log.Printf("Failed to set initial approvals: %v", err)
			}
		}

		var initialStatus string
		switch {
		case reviewState.Merged:
			initialStatus = "merged"
		case reviewState.Closed:
			initialStatus = "closed"
		case reviewState.ChangesRequested:
			initialStatus = "changes_requested"
		case reviewState.Approvals >= approvalsRequired:
			initialStatus = "approved"
		}

		if initialStatus != "" {
			if err := db.UpdatePullRequestStatus(database, prID, initialStatus); err != nil {
				log.Printf("Failed to set initial status: %v", err)
			}
		}
	}

	// Update reviewers on all PRs (replace with new set)
	currentPRs, err := db.GetPullRequestsByTracker(database, trackerID)
	if err != nil {
		log.Printf("Failed to get PRs for reviewer update: %v", err)
	} else {
		for _, pr := range currentPRs {
			if err := db.DeleteReviewersByPR(database, pr.ID); err != nil {
				log.Printf("Failed to delete reviewers for PR %d: %v", pr.ID, err)
				continue
			}
			for _, reviewerID := range newReviewerIDs {
				if err := db.CreateReviewer(database, pr.ID, reviewerID); err != nil {
					log.Printf("Failed to create reviewer: %v", err)
				}
			}
		}
	}

	// Refresh the tracker message
	if err := updateTrackerMessage(trackerID); err != nil {
		log.Printf("Failed to refresh tracker message: %v", err)
	}

	w.WriteHeader(http.StatusOK)
}
