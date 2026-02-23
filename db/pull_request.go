package db

import (
	"database/sql"
	"log"
)

// PullRequest represents a row from the pull_requests table.
type PullRequest struct {
	ID                int64
	TrackerID         int64
	GithubOwner       string
	GithubRepo        string
	GithubPRNumber    int
	GithubPRURL       string
	Title             string
	Status            string
	ApprovalsRequired int
	ApprovalsCurrent  int
}

// CreatePullRequest inserts a pull request linked to a tracker and returns its ID.
func CreatePullRequest(database *sql.DB, trackerID int64, owner, repo string, prNumber int, prURL string, title string, approvalsRequired int) (int64, error) {
	result, err := database.Exec(
		`INSERT INTO pull_requests (tracker_id, github_owner, github_repo, github_pr_number, github_pr_url, title, approvals_required)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		trackerID, owner, repo, prNumber, prURL, title, approvalsRequired,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// CreateReviewer links a Slack user as a reviewer to a pull request.
func CreateReviewer(database *sql.DB, pullRequestID int64, slackUserID string) error {
	_, err := database.Exec(
		"INSERT INTO reviewers (pull_request_id, slack_user_id) VALUES (?, ?)",
		pullRequestID, slackUserID,
	)
	return err
}

// FindPullRequest looks up a tracked PR by its GitHub identifiers.
// Returns sql.ErrNoRows if the PR is not being tracked.
func FindPullRequest(database *sql.DB, owner, repo string, prNumber int) (*PullRequest, error) {
	pr := &PullRequest{}
	err := database.QueryRow(
		`SELECT id, tracker_id, github_owner, github_repo, github_pr_number, github_pr_url,
		        title, status, approvals_required, approvals_current
		 FROM pull_requests
		 WHERE github_owner = ? AND github_repo = ? AND github_pr_number = ?`,
		owner, repo, prNumber,
	).Scan(&pr.ID, &pr.TrackerID, &pr.GithubOwner, &pr.GithubRepo, &pr.GithubPRNumber,
		&pr.GithubPRURL, &pr.Title, &pr.Status, &pr.ApprovalsRequired, &pr.ApprovalsCurrent)
	if err != nil {
		return nil, err
	}
	return pr, nil
}

// UpdatePullRequestApprovals sets the current approval count for a PR.
func UpdatePullRequestApprovals(database *sql.DB, prID int64, approvalsCurrent int) error {
	_, err := database.Exec(
		"UPDATE pull_requests SET approvals_current = ? WHERE id = ?",
		approvalsCurrent, prID,
	)
	return err
}

// UpdatePullRequestTitle sets the title of a PR (synced from GitHub).
func UpdatePullRequestTitle(database *sql.DB, prID int64, title string) error {
	_, err := database.Exec(
		"UPDATE pull_requests SET title = ? WHERE id = ?",
		title, prID,
	)
	return err
}

// UpdatePullRequestStatus sets the status of a PR (e.g. "open", "approved", "merged", "closed").
func UpdatePullRequestStatus(database *sql.DB, prID int64, status string) error {
	_, err := database.Exec(
		"UPDATE pull_requests SET status = ? WHERE id = ?",
		status, prID,
	)
	return err
}

// GetPullRequestsByTracker fetches all PRs belonging to a tracker.
func GetPullRequestsByTracker(database *sql.DB, trackerID int64) ([]PullRequest, error) {
	rows, err := database.Query(
		`SELECT id, tracker_id, github_owner, github_repo, github_pr_number, github_pr_url,
		        title, status, approvals_required, approvals_current
		 FROM pull_requests WHERE tracker_id = ?`,
		trackerID,
	)
	if err != nil {
		return nil, err
	}
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Printf("Failed to close rows: %v", err)
		}
	}(rows)

	var prs []PullRequest
	for rows.Next() {
		var pr PullRequest
		if err := rows.Scan(&pr.ID, &pr.TrackerID, &pr.GithubOwner, &pr.GithubRepo,
			&pr.GithubPRNumber, &pr.GithubPRURL, &pr.Title, &pr.Status, &pr.ApprovalsRequired,
			&pr.ApprovalsCurrent); err != nil {
			return nil, err
		}
		prs = append(prs, pr)
	}
	return prs, rows.Err()
}

// GetReviewersByPR fetches all reviewer Slack user IDs for a pull request.
func GetReviewersByPR(database *sql.DB, prID int64) ([]string, error) {
	rows, err := database.Query(
		"SELECT slack_user_id FROM reviewers WHERE pull_request_id = ?",
		prID,
	)
	if err != nil {
		return nil, err
	}
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Printf("Failed to close rows: %v", err)
		}
	}(rows)

	var userIDs []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		userIDs = append(userIDs, uid)
	}
	return userIDs, rows.Err()
}
