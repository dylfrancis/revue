package server

import (
	"context"
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

	// Type switch - Go's way of handling polymorphism. ParseWebHook returns
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

// findTrackedPR looks up a PR in the database by its GitHub identifiers.
// Returns nil if the PR is not tracked by us.
func findTrackedPR(owner, repo string, prNumber int) *db.PullRequest {
	pr, err := db.FindPullRequest(database, owner, repo, prNumber)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		log.Printf("Failed to find PR %s/%s#%d: %v", owner, repo, prNumber, err)
		return nil
	}
	return pr
}

// handlePRReview processes pull_request_review events.
// Handles both "approved" and "changes_requested" review states.
func handlePRReview(event *github.PullRequestReviewEvent) {
	if event.GetAction() != "submitted" {
		return
	}

	reviewState := event.GetReview().GetState()
	if reviewState != "approved" && reviewState != "changes_requested" {
		return
	}

	pr := findTrackedPR(
		event.GetRepo().GetOwner().GetLogin(),
		event.GetRepo().GetName(),
		event.GetPullRequest().GetNumber(),
	)
	if pr == nil {
		return
	}

	if reviewState == "approved" {
		newApprovals := pr.ApprovalsCurrent + 1
		if err := db.UpdatePullRequestApprovals(database, pr.ID, newApprovals); err != nil {
			log.Printf("Failed to update approvals for PR %d: %v", pr.ID, err)
			return
		}
		if newApprovals >= pr.ApprovalsRequired && pr.Status != "approved" {
			if err := db.UpdatePullRequestStatus(database, pr.ID, "approved"); err != nil {
				log.Printf("Failed to update PR status: %v", err)
				return
			}
		}
	} else {
		if err := db.UpdatePullRequestApprovals(database, pr.ID, 0); err != nil {
			log.Printf("Failed to reset approvals for PR %d: %v", pr.ID, err)
			return
		}
		if err := db.UpdatePullRequestStatus(database, pr.ID, "changes_requested"); err != nil {
			log.Printf("Failed to update PR status: %v", err)
			return
		}
	}

	if err := updateTrackerMessage(pr.TrackerID); err != nil {
		log.Printf("Failed to update tracker message: %v", err)
	}
}

// handlePRStateChange processes pull_request events (opened, closed, merged, etc.).
// We only care about the "closed" action - GitHub uses "closed" for both
// merges and closes, and we check the Merged field to distinguish them.
func handlePRStateChange(event *github.PullRequestEvent) {
	action := event.GetAction()

	pr := findTrackedPR(
		event.GetRepo().GetOwner().GetLogin(),
		event.GetRepo().GetName(),
		event.GetPullRequest().GetNumber(),
	)
	if pr == nil {
		return
	}

	// Sync title on any PR event
	if newTitle := event.GetPullRequest().GetTitle(); newTitle != "" && newTitle != pr.Title {
		if err := db.UpdatePullRequestTitle(database, pr.ID, newTitle); err != nil {
			log.Printf("Failed to update PR title: %v", err)
		}
	}

	if action != "closed" {
		// For non-close events (e.g. edited), just refresh the message
		if err := updateTrackerMessage(pr.TrackerID); err != nil {
			log.Printf("Failed to update tracker message: %v", err)
		}
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

	completed, err := db.CompleteTrackerIfDone(database, pr.TrackerID)
	if err != nil {
		log.Printf("Failed to check tracker completion: %v", err)
	}
	if completed {
		log.Printf("Tracker %d completed - all PRs merged/closed", pr.TrackerID)
	}

	if err := updateTrackerMessage(pr.TrackerID); err != nil {
		log.Printf("Failed to update tracker message: %v", err)
	}
}

// fetchRequiredApprovals queries the GitHub API for the branch protection
// rules on a repo's default branch and returns the required number of
// approving reviews. Returns 1 if no branch protection is configured.
func fetchRequiredApprovals(owner, repo string) (int, error) {
	ctx := context.Background()

	// First, get the repo to find its default branch name
	repoInfo, _, err := githubClient.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return 1, err
	}

	defaultBranch := repoInfo.GetDefaultBranch()
	if defaultBranch == "" {
		return 1, nil
	}

	// Fetch branch protection rules for the default branch.
	// Returns 404 if no branch protection is configured - we default to 1.
	protection, _, err := githubClient.Repositories.GetBranchProtection(ctx, owner, repo, defaultBranch)
	if err != nil {
		var ghErr *github.ErrorResponse
		if errors.As(err, &ghErr) && ghErr.Response.StatusCode == http.StatusNotFound {
			return 1, nil
		}
		return 1, err
	}

	if protection.RequiredPullRequestReviews != nil && protection.RequiredPullRequestReviews.RequiredApprovingReviewCount > 0 {
		return protection.RequiredPullRequestReviews.RequiredApprovingReviewCount, nil
	}

	return 1, nil
}

// prReviewState represents the current review state of a PR on GitHub.
type prReviewState struct {
	Title            string
	Approvals        int
	ChangesRequested bool
	Merged           bool
	Closed           bool
}

// fetchPRReviewState fetches all reviews on a PR and computes the current
// state. GitHub can have multiple reviews per user - we take the latest
// review per user to determine the current state.
func fetchPRReviewState(owner, repo string, prNumber int) (prReviewState, error) {
	ctx := context.Background()
	var state prReviewState

	// Fetch the PR itself to check if it's already merged or closed
	pr, _, err := githubClient.PullRequests.Get(ctx, owner, repo, prNumber)
	if err != nil {
		return state, err
	}
	state.Title = pr.GetTitle()
	if pr.GetMerged() {
		state.Merged = true
	} else if pr.GetState() == "closed" {
		state.Closed = true
	}

	// Always fetch reviews so we can show the approval count regardless of state
	opts := &github.ListOptions{PerPage: 100}
	reviews, _, err := githubClient.PullRequests.ListReviews(ctx, owner, repo, prNumber, opts)
	if err != nil {
		return state, err
	}

	// Track the latest review state per user.
	// A user can review multiple times - only the most recent matters.
	latestByUser := make(map[string]string)
	for _, review := range reviews {
		user := review.GetUser().GetLogin()
		reviewState := review.GetState()
		// Only track actionable states (skip "COMMENTED", "PENDING", "DISMISSED")
		if reviewState == "APPROVED" || reviewState == "CHANGES_REQUESTED" {
			latestByUser[user] = reviewState
		}
	}

	for _, reviewState := range latestByUser {
		switch reviewState {
		case "APPROVED":
			state.Approvals++
		case "CHANGES_REQUESTED":
			state.ChangesRequested = true
		}
	}

	return state, nil
}
