package githubclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
)

type apiUser struct {
	Login string `json:"login"`
}

type apiLabel struct {
	Name        string  `json:"name"`
	Color       string  `json:"color"`
	Description *string `json:"description"`
}

type apiIssue struct {
	ID          int64           `json:"id"`
	Number      int             `json:"number"`
	Title       string          `json:"title"`
	Body        string          `json:"body"`
	State       string          `json:"state"`
	HTMLURL     string          `json:"html_url"`
	User        apiUser         `json:"user"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	Labels      []apiLabel      `json:"labels"`
	PullRequest json.RawMessage `json:"pull_request"`
}

type apiComment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	HTMLURL   string    `json:"html_url"`
	User      apiUser   `json:"user"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type apiPullRequest struct {
	ID        int64     `json:"id"`
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	Draft     bool      `json:"draft"`
	HTMLURL   string    `json:"html_url"`
	UpdatedAt time.Time `json:"updated_at"`
	Base      struct {
		Ref string `json:"ref"`
	} `json:"base"`
	Head struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
}

type apiPullRequestDetail struct {
	MergeableState string `json:"mergeable_state"`
}

type apiCheckRun struct {
	Status      string     `json:"status"`
	Conclusion  string     `json:"conclusion"`
	HTMLURL     string     `json:"html_url"`
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
}

type apiCheckRuns struct {
	CheckRuns []apiCheckRun `json:"check_runs"`
}

type apiCombinedStatus struct {
	State    string `json:"state"`
	Statuses []struct {
		State     string    `json:"state"`
		TargetURL string    `json:"target_url"`
		UpdatedAt time.Time `json:"updated_at"`
	} `json:"statuses"`
}

func (c *Client) listIssues(ctx context.Context, owner, repository string) ([]supervisor.Issue, error) {
	basePath := fmt.Sprintf("repos/%s/%s/issues?state=open&per_page=100", url.PathEscape(owner), url.PathEscape(repository))
	issues := make([]supervisor.Issue, 0)
	for page := 1; page <= c.maxPages && len(issues) < c.maxItems; page++ {
		var items []apiIssue
		if err := c.getJSON(ctx, basePath+"&page="+strconv.Itoa(page), &items); err != nil {
			return nil, fmt.Errorf("list open issues: %w", err)
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

func (c *Client) listComments(ctx context.Context, owner, repository string, issueNumber int) ([]supervisor.Comment, error) {
	path := fmt.Sprintf("repos/%s/%s/issues/%d/comments?per_page=100", url.PathEscape(owner), url.PathEscape(repository), issueNumber)
	items, err := fetchPages[apiComment](ctx, c, path)
	if err != nil {
		return nil, fmt.Errorf("list comments for issue %d: %w", issueNumber, err)
	}
	comments := make([]supervisor.Comment, 0, len(items))
	for _, item := range items {
		comments = append(comments, supervisor.Comment{
			GitHubID: item.ID, Body: item.Body, Author: item.User.Login, URL: item.HTMLURL,
			CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
		})
		if len(comments) >= c.maxItems {
			break
		}
	}
	return comments, nil
}

func (c *Client) listPullRequests(ctx context.Context, owner, repository string) ([]apiPullRequest, error) {
	path := fmt.Sprintf("repos/%s/%s/pulls?state=all&sort=updated&direction=desc&per_page=100", url.PathEscape(owner), url.PathEscape(repository))
	items, err := fetchPages[apiPullRequest](ctx, c, path)
	if err != nil {
		return nil, fmt.Errorf("list pull requests: %w", err)
	}
	if len(items) > c.maxItems {
		items = items[:c.maxItems]
	}
	return items, nil
}

func (c *Client) pullRequestDetail(ctx context.Context, owner, repository string, number int) (apiPullRequestDetail, error) {
	path := fmt.Sprintf("repos/%s/%s/pulls/%d", url.PathEscape(owner), url.PathEscape(repository), number)
	var detail apiPullRequestDetail
	if err := c.getJSON(ctx, path, &detail); err != nil {
		return apiPullRequestDetail{}, fmt.Errorf("read pull request %d: %w", number, err)
	}
	return detail, nil
}

func (c *Client) ciSummary(ctx context.Context, owner, repository, headSHA string) (supervisor.CISummary, error) {
	checks, err := c.listCheckRuns(ctx, owner, repository, headSHA)
	if err != nil {
		return supervisor.CISummary{}, err
	}
	if len(checks) > 0 {
		return summarizeCheckRuns(headSHA, checks), nil
	}

	path := fmt.Sprintf("repos/%s/%s/commits/%s/status", url.PathEscape(owner), url.PathEscape(repository), url.PathEscape(headSHA))
	var combined apiCombinedStatus
	if err := c.getJSON(ctx, path, &combined); err != nil {
		return supervisor.CISummary{}, fmt.Errorf("read combined status for %s: %w", headSHA, err)
	}
	summary := supervisor.CISummary{
		HeadSHA: headSHA, Status: combined.State, Conclusion: combined.State,
		Source: "commit_status", UpdatedAt: time.Unix(0, 0).UTC(),
	}
	for _, status := range combined.Statuses {
		if summary.DetailsURL == "" {
			summary.DetailsURL = status.TargetURL
		}
		if status.UpdatedAt.After(summary.UpdatedAt) {
			summary.UpdatedAt = status.UpdatedAt
		}
	}
	if summary.UpdatedAt.Equal(time.Unix(0, 0).UTC()) {
		summary.UpdatedAt = time.Now().UTC()
	}
	return summary, nil
}

func (c *Client) listCheckRuns(ctx context.Context, owner, repository, headSHA string) ([]apiCheckRun, error) {
	checks := make([]apiCheckRun, 0)
	for page := 1; page <= c.maxPages && len(checks) < c.maxItems; page++ {
		path := fmt.Sprintf("repos/%s/%s/commits/%s/check-runs?per_page=100&page=%d", url.PathEscape(owner), url.PathEscape(repository), url.PathEscape(headSHA), page)
		var response apiCheckRuns
		if err := c.getJSON(ctx, path, &response); err != nil {
			return nil, fmt.Errorf("list check runs for %s: %w", headSHA, err)
		}
		checks = append(checks, response.CheckRuns...)
		if len(response.CheckRuns) < 100 {
			break
		}
	}
	if len(checks) > c.maxItems {
		checks = checks[:c.maxItems]
	}
	return checks, nil
}

func summarizeCheckRuns(headSHA string, checks []apiCheckRun) supervisor.CISummary {
	summary := supervisor.CISummary{
		HeadSHA: headSHA, Status: "completed", Conclusion: "success",
		Source: "check_runs", UpdatedAt: time.Unix(0, 0).UTC(),
	}
	worst := 0
	for _, check := range checks {
		if check.Status != "completed" {
			summary.Status = "in_progress"
			summary.Conclusion = ""
		}
		if summary.DetailsURL == "" {
			summary.DetailsURL = check.HTMLURL
		}
		if check.CompletedAt != nil && check.CompletedAt.After(summary.UpdatedAt) {
			summary.UpdatedAt = *check.CompletedAt
		} else if check.StartedAt != nil && check.StartedAt.After(summary.UpdatedAt) {
			summary.UpdatedAt = *check.StartedAt
		}
		if summary.Status == "completed" {
			priority := conclusionPriority(check.Conclusion)
			if priority > worst {
				worst = priority
				summary.Conclusion = check.Conclusion
			}
		}
	}
	if summary.UpdatedAt.Equal(time.Unix(0, 0).UTC()) {
		summary.UpdatedAt = time.Now().UTC()
	}
	return summary
}

func conclusionPriority(conclusion string) int {
	switch conclusion {
	case "failure", "timed_out", "action_required", "startup_failure":
		return 5
	case "cancelled", "stale":
		return 4
	case "neutral":
		return 3
	case "skipped":
		return 2
	case "success":
		return 1
	default:
		return 0
	}
}

func referencedIssueNumbers(text string) []int {
	set := make(map[int]struct{})
	for _, expression := range []*regexp.Regexp{shortIssueReference, fullIssueReference} {
		for _, match := range expression.FindAllStringSubmatch(text, -1) {
			number, err := strconv.Atoi(match[1])
			if err == nil {
				set[number] = struct{}{}
			}
		}
	}
	numbers := make([]int, 0, len(set))
	for number := range set {
		numbers = append(numbers, number)
	}
	sort.Ints(numbers)
	return numbers
}

func fetchPages[T any](ctx context.Context, client *Client, path string) ([]T, error) {
	items := make([]T, 0)
	separator := "&"
	if !strings.Contains(path, "?") {
		separator = "?"
	}
	for page := 1; page <= client.maxPages && len(items) < client.maxItems; page++ {
		var response []T
		if err := client.getJSON(ctx, path+separator+"page="+strconv.Itoa(page), &response); err != nil {
			return nil, err
		}
		items = append(items, response...)
		if len(response) < 100 {
			break
		}
	}
	if len(items) > client.maxItems {
		items = items[:client.maxItems]
	}
	return items, nil
}

func (c *Client) getJSON(ctx context.Context, path string, destination any) error {
	relative, err := url.Parse(path)
	if err != nil {
		return fmt.Errorf("parse GitHub API path: %w", err)
	}
	endpoint := c.baseURL.ResolveReference(relative)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return fmt.Errorf("create GitHub request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "cddm-dashboard-supervisor")
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("GitHub request %s: %w", endpoint.EscapedPath(), err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := http.StatusText(response.StatusCode)
		var body struct {
			Message string `json:"message"`
		}
		limited := io.LimitReader(response.Body, 4096)
		if json.NewDecoder(limited).Decode(&body) == nil && body.Message != "" {
			message = body.Message
		}
		return fmt.Errorf("GitHub request %s returned %d: %s", endpoint.EscapedPath(), response.StatusCode, message)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(destination); err != nil {
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("GitHub request %s returned an empty response", endpoint.EscapedPath())
		}
		return fmt.Errorf("decode GitHub response %s: %w", endpoint.EscapedPath(), err)
	}
	return nil
}
