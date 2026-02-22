# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What is Revue?

Revue is a self-hosted bot that tracks pull request reviews across GitHub and Slack. It receives GitHub webhooks for PR activity and uses Slack Block Kit for interactive modals and messaging.

## Commands

```bash
go run main.go        # Run the server (requires .env file with SLACK_BOT_TOKEN)
go build -o revue .   # Build binary
go mod tidy           # Sync dependencies
```

No test suite exists yet. The project has no linter configured.

## Architecture

- **`main.go`** — Entry point. Loads `.env`, connects to DB, starts HTTP server on port 8080.
- **`server/`** — HTTP handlers. `server.go` registers routes and starts the server. `slack_handler.go` handles Slack slash commands and opens Block Kit modals via the Slack API. `github_handler.go` handles GitHub webhook events. The Slack bot token is stored as a package-level var set during `Start()`.
- **`db/`** — SQLite connection with WAL mode and auto-migrations using `golang-migrate`. Migrations live in `db/migrations/` as numbered `.up.sql`/`.down.sql` pairs.

## Key Details

- Uses `modernc.org/sqlite` (pure Go, no CGo) — no C compiler needed.
- Migrations run automatically on startup from `file://db/migrations` (path is relative to working directory, so run from repo root).
- The `.env` file must define `SLACK_BOT_TOKEN`. The app will fatal on startup without it.
- SQLite DB file is `revue.db` in the repo root (gitignored state files: `.db`, `.db-shm`, `.db-wal`).

## Database Schema

Four tables: `trackers` (groups of PRs posted to a Slack channel), `pull_requests` (individual PRs linked to a tracker), `reviewers` (Slack users assigned to review a PR), `channel_reminders` (per-channel reminder intervals).
