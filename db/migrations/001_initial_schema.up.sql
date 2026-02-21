CREATE TABLE trackers
(
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    slack_channel_id TEXT     NOT NULL,
    slack_message_ts TEXT     NOT NULL,
    status           TEXT     NOT NULL DEFAULT 'active',
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE pull_requests
(
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    tracker_id         INTEGER  NOT NULL REFERENCES trackers (id),
    github_owner       TEXT     NOT NULL,
    github_repo        TEXT     NOT NULL,
    github_pr_number   INTEGER  NOT NULL,
    github_pr_url      TEXT     NOT NULL,
    status             TEXT     NOT NULL DEFAULT 'open',
    approvals_required INTEGER  NOT NULL DEFAULT 1,
    approvals_current  INTEGER  NOT NULL DEFAULT 0,
    created_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE reviewers
(
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    pull_request_id INTEGER NOT NULL REFERENCES pull_requests (id),
    slack_user_id   TEXT    NOT NULL
);

CREATE TABLE channel_reminders
(
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    slack_channel_id TEXT    NOT NULL UNIQUE,
    interval_minutes INTEGER NOT NULL,
    enabled          INTEGER NOT NULL DEFAULT 1
);