package server

import (
	"database/sql"
	"errors"
	"log"
	"net/http"

	"github.com/dylfrancis/revue/db"
	"github.com/google/go-github/v83/github"
)

func handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	// ValidatePayload reads the body, verifies the HMAC-SHA256 signature
	// from the X-Hub-Signature-256 header, and returns the raw payload.
	// If the signature doesn't match, it returns an error.
	payload, err := github.ValidatePayload(r, []byte(githubWebhookSecret))
	if err != nil {
		log.Printf("Invalid GitHub webhook signature: %v", err)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// ParseWebHook reads the X-GitHub-Event header to determine the event
	// type, then unmarshals the payload into the appropriate typed struct.
	eventType := github.WebHookType(r)
	event, err := github.ParseWebHook(eventType, payload)
	if err != nil {
		log.Printf("Failed to parse GitHub webhook: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Type switch — Go's way of handling polymorphism. ParseWebHook returns
	// interface{}, and we switch on the concrete type to handle each event.
	switch e := event.(type) {
	case *github.PullRequestReviewEvent:
		handlePRReview(e)
	case *github.PullRequestEvent:
		handlePRStateChange(e)
	default:
		log.Printf("Ignoring GitHub event type: %s", eventType)
	}

	w.WriteHeader(http.StatusOK)
}

// handlePRReview processes pull_request_review events.
// When a review is submitted with an "approved" state, we increment the
// approval count and update the Slack tracker message.
func handlePRReview(event *github.PullRequestReviewEvent) {
	// Only care about newly submitted reviews that are approvals
	if event.GetAction() != "submitted" {
		return
	}
	if event.GetReview().GetState() != "approved" {
		return
	}

	owner := event.GetRepo().GetOwner().GetLogin()
	repo := event.GetRepo().GetName()
	prNumber := event.GetPullRequest().GetNumber()

	pr, err := db.FindPullRequest(database, owner, repo, prNumber)
	if errors.Is(err, sql.ErrNoRows) {
		return // PR not tracked by us, ignore
	}
	if err != nil {
		log.Printf("Failed to find PR %s/%s#%d: %v", owner, repo, prNumber, err)
		return
	}

	// Increment approval count
	newApprovals := pr.ApprovalsCurrent + 1
	if err := db.UpdatePullRequestApprovals(database, pr.ID, newApprovals); err != nil {
		log.Printf("Failed to update approvals for PR %d: %v", pr.ID, err)
		return
	}

	// If approvals meet the threshold, mark as approved
	if newApprovals >= pr.ApprovalsRequired && pr.Status == "open" {
		if err := db.UpdatePullRequestStatus(database, pr.ID, "approved"); err != nil {
			log.Printf("Failed to update PR status: %v", err)
			return
		}
	}

	if err := updateTrackerMessage(pr.TrackerID); err != nil {
		log.Printf("Failed to update tracker message: %v", err)
	}
}

// handlePRStateChange processes pull_request events (opened, closed, merged, etc.).
// We only care about the "closed" action — GitHub uses "closed" for both
// merges and closes, and we check the Merged field to distinguish them.
func handlePRStateChange(event *github.PullRequestEvent) {
	if event.GetAction() != "closed" {
		return
	}

	owner := event.GetRepo().GetOwner().GetLogin()
	repo := event.GetRepo().GetName()
	prNumber := event.GetPullRequest().GetNumber()

	pr, err := db.FindPullRequest(database, owner, repo, prNumber)
	if errors.Is(err, sql.ErrNoRows) {
		return // not tracked
	}
	if err != nil {
		log.Printf("Failed to find PR %s/%s#%d: %v", owner, repo, prNumber, err)
		return
	}

	status := "closed"
	if event.GetPullRequest().GetMerged() {
		status = "merged"
	}

	if err := db.UpdatePullRequestStatus(database, pr.ID, status); err != nil {
		log.Printf("Failed to update PR status: %v", err)
		return
	}

	// Check if all PRs in the tracker are done
	completed, err := db.CompleteTrackerIfDone(database, pr.TrackerID)
	if err != nil {
		log.Printf("Failed to check tracker completion: %v", err)
	}
	if completed {
		log.Printf("Tracker %d completed — all PRs merged/closed", pr.TrackerID)
	}

	if err := updateTrackerMessage(pr.TrackerID); err != nil {
		log.Printf("Failed to update tracker message: %v", err)
	}
}
