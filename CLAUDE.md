# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What is Revue?

Revue is a self-hosted bot that tracks pull request reviews across GitHub and Slack. It receives GitHub webhooks for PR activity and uses Slack Block Kit for interactive modals and messaging. Designed for single-binary deployment on a VPS with SQLite — no external database or services needed.

## Commands

```bash
go run main.go        # Run the server (requires .env file)
go build -o revue .   # Build binary
go mod tidy           # Sync dependencies
```

No test suite exists yet. The project has no linter configured.

## Architecture

- **`main.go`** — Entry point. Loads `.env`, connects to DB, starts HTTP server on port 8080.
- **`server/`** — HTTP handlers split by concern:
  - `server.go` — Route registration, `Start()`, Slack request verification middleware, interaction dispatch, tracker message posting/updating
  - `slack_handler.go` — Slash command handling, Block Kit modal building (dynamic add/remove PR fields), `updateTrackerMessage()`
  - `github_handler.go` — GitHub webhook verification (HMAC-SHA256), event parsing via go-github, handles `pull_request_review` and `pull_request` events
  - `parse.go` — PR URL parser (extracts owner/repo/number from GitHub URLs)
- **`db/`** — SQLite connection with WAL mode and auto-migrations using `golang-migrate`.
  - `db.go` — Connection setup and migration runner
  - `tracker.go` — Tracker CRUD and completion logic
  - `pull_request.go` — PR and reviewer CRUD, queries by GitHub identifiers

## Key Libraries

- `slack-go/slack` — Slack API client (modals, messages, signature verification)
- `google/go-github` — GitHub webhook parsing and signature verification
- `modernc.org/sqlite` — Pure Go SQLite driver (no CGo, no C compiler needed)
- `golang-migrate/migrate/v4` — Database migrations

## Environment Variables (.env)

All required — app fatals on startup if missing:
- `SLACK_BOT_TOKEN` — Slack bot OAuth token (xoxb-...)
- `SLACK_SIGNING_SECRET` — Slack app signing secret for request verification
- `GITHUB_TOKEN` — GitHub personal access token for API access
- `GITHUB_WEBHOOK_SECRET` — Secret for verifying GitHub webhook signatures

## Key Details

- Package-level vars in `server/` for shared state (`slackClient`, `signingSecret`, `githubWebhookSecret`, `database`), set once during `Start()`.
- Migrations run automatically on startup from `file://db/migrations` (path is relative to working directory, so run from repo root).
- SQLite DB file is `revue.db` in the repo root (gitignored: `.db`, `.db-shm`, `.db-wal`).
- Deployment target: single VPS (e.g., DigitalOcean Droplet) with Docker Compose. Keep architecture stateless-friendly except for SQLite.

## Database Schema

Four tables: `trackers` (groups of PRs posted to a Slack channel), `pull_requests` (individual PRs linked to a tracker), `reviewers` (Slack users assigned to review a PR), `channel_reminders` (per-channel reminder intervals).
