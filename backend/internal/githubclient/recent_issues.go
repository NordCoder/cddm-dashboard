package githubclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
)

// listRecentIssues includes closed Issues because a successful PR merge may
// close its linked Issue before the Lead publishes the command-bound `merged`
// marker. Results remain bounded by the existing maxPages/maxItems limits and
// sorted by GitHub update time.
func (c *Client) listRecentIssues(ctx context.Context, owner, repository string) ([]supervisor.Issue, error) {
	basePath := fmt.Sprintf("repos/%s/%s/issues?state=all&sort=updated&direction=desc&per_page=100", url.PathEscape(owner), url.PathEscape(repository))
	issues := make([]supervisor.Issue, 0)
	for page := 1; page <= c.maxPages && len(issues) < c.maxItems; page++ {
		var items []apiIssue
		if err := c.getJSON(ctx, basePath+"&page="+strconv.Itoa(page), &items); err != nil {
			return nil, fmt.Errorf("list recently updated issues: %w", err)
		}
		for _, item := range items {
			if len(item.PullRequest) != 0 && string(item.PullRequest) != "null" {
				continue
			}
			labels := make([]supervisor.Label, 0, len(item.Labels))
			for _, label := range item.Labels {
				description := ""
				if label.Description != nil {
					description = *label.Description
				}
				labels = append(labels, supervisor.Label{Name: label.Name, Color: label.Color, Description: description})
			}
			issues = append(issues, supervisor.Issue{
				GitHubID: item.ID, Number: item.Number, Title: item.Title, Body: item.Body, State: item.State,
				URL: item.HTMLURL, Author: item.User.Login, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
				Labels: labels, Comments: []supervisor.Comment{}, PullRequests: []supervisor.PullRequest{},
			})
			if len(issues) >= c.maxItems {
				break
			}
		}
		if len(items) < 100 {
			break
		}
	}
	return issues, nil
}
