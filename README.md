# Revue

A self-hosted, open-source bot written in Go that helps teams track pull request reviews without context switching between GitHub and Slack.

## Tech Stack

- **Go** — single binary distribution
- **SQLite** (WAL mode) — local state via `modernc.org/sqlite` (pure Go, no CGo)
- **Slack Block Kit** — modals + interactive messaging
- **GitHub Webhooks + REST API** — real-time PR activity
- **Docker + Docker Compose** — self-hosting

## Getting Started

### Prerequisites

- Go 1.21+

### Setup

```bash
git clone https://github.com/dylfrancis/revue.git
cd revue
go mod tidy
go run main.go
```

The app will create a `revue.db` SQLite database and run migrations automatically on startup.

Update for test

## License

MIT
