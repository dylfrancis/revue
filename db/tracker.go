package db

import (
	"database/sql"
	"fmt"
)

// Tracker represents a row from the trackers table.
type Tracker struct {
	ID             int64
	SlackChannelID string
	SlackMessageTS string
	Status         string
	Title          string
}

// CreateTracker inserts a new tracker row and returns its ID.
// The slack_message_ts starts empty - we update it after posting to Slack.
func CreateTracker(database *sql.DB, channelID string, title string) (int64, error) {
	result, err := database.Exec(
		"INSERT INTO trackers (slack_channel_id, slack_message_ts, title) VALUES (?, ?, ?)",
		channelID, "", title,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
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

// UpdateTrackerTitle sets the title on a tracker.
func UpdateTrackerTitle(database *sql.DB, trackerID int64, title string) error {
	_, err := database.Exec(
		"UPDATE trackers SET title = ? WHERE id = ?",
		title, trackerID,
	)
	return err
}

// GetTrackerByID fetches a single tracker row.
func GetTrackerByID(database *sql.DB, trackerID int64) (*Tracker, error) {
	t := &Tracker{}
	err := database.QueryRow(
		"SELECT id, slack_channel_id, slack_message_ts, status, title FROM trackers WHERE id = ?",
		trackerID,
	).Scan(&t.ID, &t.SlackChannelID, &t.SlackMessageTS, &t.Status, &t.Title)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// CompleteTrackerIfDone checks if all PRs in a tracker are merged or closed.
// If so, it marks the tracker status as "completed".
// Returns true if the tracker was completed.
func CompleteTrackerIfDone(database *sql.DB, trackerID int64) (bool, error) {
	var openCount int
	err := database.QueryRow(
		"SELECT COUNT(*) FROM pull_requests WHERE tracker_id = ? AND status NOT IN ('merged', 'closed')",
		trackerID,
	).Scan(&openCount)
	if err != nil {
		return false, fmt.Errorf("failed to count open PRs: %w", err)
	}

	if openCount > 0 {
		return false, nil
	}

	_, err = database.Exec(
		"UPDATE trackers SET status = 'completed' WHERE id = ?",
		trackerID,
	)
	if err != nil {
		return false, fmt.Errorf("failed to update tracker status: %w", err)
	}

	return true, nil
}
