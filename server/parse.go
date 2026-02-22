package server

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type parsedPR struct {
	Owner  string
	Repo   string
	Number int
	URL    string
}

// parsePRURL extracts owner, repo, and PR number from a GitHub PR URL.
// Expected format: https://github.com/{owner}/{repo}/pull/{number}
func parsePRURL(raw string) (parsedPR, error) {
	raw = strings.TrimSpace(raw)

	u, err := url.Parse(raw)
	if err != nil {
		return parsedPR{}, fmt.Errorf("invalid URL %q: %w", raw, err)
	}

	if u.Host != "github.com" {
		return parsedPR{}, fmt.Errorf("not a GitHub URL: %q", raw)
	}

	// Path looks like: /owner/repo/pull/123
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 4 || parts[2] != "pull" {
		return parsedPR{}, fmt.Errorf("not a valid PR URL: %q (expected github.com/owner/repo/pull/123)", raw)
	}

	number, err := strconv.Atoi(parts[3])
	if err != nil {
		return parsedPR{}, fmt.Errorf("invalid PR number in %q: %w", raw, err)
	}

	return parsedPR{
		Owner:  parts[0],
		Repo:   parts[1],
		Number: number,
		URL:    raw,
	}, nil
}
