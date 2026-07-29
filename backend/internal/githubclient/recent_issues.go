package githubclient

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
)

// listRecentIssues preserves the complete bounded open-Issue view and adds a
// separately bounded recently closed view. A successful PR merge may close its
// linked Issue before the Lead publishes the command-bound `merged` marker.
func (c *Client) listRecentIssues(ctx context.Context, owner, repository string) ([]supervisor.Issue, error) {
	open, err := c.listIssues(ctx, owner, repository)
	if err != nil {
		return nil, err
	}
	issues := append([]supervisor.Issue(nil), open...)
	seen := make(map[int64]bool, len(open))
	for _, issue := range open {
		seen[issue.GitHubID] = true
	}

	basePath := fmt.Sprintf("repos/%s/%s/issues?state=closed&sort=updated&direction=desc&per_page=100", url.PathEscape(owner), url.PathEscape(repository))
	closedCount := 0
	for page := 1; page <= c.maxPages && closedCount < c.maxItems; page++ {
		var items []apiIssue
		if err := c.getJSON(ctx, basePath+"&page="+strconv.Itoa(page), &items); err != nil {
			return nil, fmt.Errorf("list recently closed issues: %w", err)
		}
		for _, item := range items {
			if len(item.PullRequest) != 0 && string(item.PullRequest) != "null" {
				continue
			}
			closedCount++
			if seen[item.ID] {
				continue
			}
			seen[item.ID] = true
			issues = append(issues, normalizedAPIIssue(item))
			if closedCount >= c.maxItems {
				break
			}
		}
		if len(items) < 100 {
			break
		}
	}
	return issues, nil
}

func normalizedAPIIssue(item apiIssue) supervisor.Issue {
	labels := make([]supervisor.Label, 0, len(item.Labels))
	for _, label := range item.Labels {
		description := ""
		if label.Description != nil {
			description = *label.Description
		}
		labels = append(labels, supervisor.Label{Name: label.Name, Color: label.Color, Description: description})
	}
	return supervisor.Issue{
		GitHubID: item.ID, Number: item.Number, Title: item.Title, Body: item.Body, State: item.State,
		URL: item.HTMLURL, Author: item.User.Login, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
		Labels: labels, Comments: []supervisor.Comment{}, PullRequests: []supervisor.PullRequest{},
	}
}
