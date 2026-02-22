package db

import "database/sql"

// CreateTracker inserts a new tracker row and returns its ID.
// The slack_message_ts starts empty — we update it after posting to Slack.
func CreateTracker(database *sql.DB, channelID string) (int64, error) {
	result, err := database.Exec(
		"INSERT INTO trackers (slack_channel_id, slack_message_ts) VALUES (?, ?)",
		channelID, "",
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// CreatePullRequest inserts a pull request linked to a tracker and returns its ID.
func CreatePullRequest(database *sql.DB, trackerID int64, owner, repo string, prNumber int, prURL string) (int64, error) {
	result, err := database.Exec(
		`INSERT INTO pull_requests (tracker_id, github_owner, github_repo, github_pr_number, github_pr_url)
		 VALUES (?, ?, ?, ?, ?)`,
		trackerID, owner, repo, prNumber, prURL,
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

// UpdateTrackerMessageTS sets the Slack message timestamp on a tracker
// after the summary message has been posted.
func UpdateTrackerMessageTS(database *sql.DB, trackerID int64, messageTS string) error {
	_, err := database.Exec(
		"UPDATE trackers SET slack_message_ts = ? WHERE id = ?",
		messageTS, trackerID,
	)
	return err
}
