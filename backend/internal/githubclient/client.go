package githubclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/supervisor"
)

const defaultBaseURL = "https://api.github.com/"

var (
	shortIssueReference = regexp.MustCompile(`#([1-9][0-9]*)`)
	fullIssueReference  = regexp.MustCompile(`/issues/([1-9][0-9]*)`)
)

type Config struct {
	Token          string
	BaseURL        string
	RequestTimeout time.Duration
	MaxPages       int
	MaxItems       int
	HTTPClient     *http.Client
}

type Client struct {
	token      string
	baseURL    *url.URL
	httpClient *http.Client
	maxPages   int
	maxItems   int
}

func New(config Config) (*Client, error) {
	baseURL := strings.TrimSpace(config.BaseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse GitHub API base URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("GitHub API base URL must be absolute")
	}
	if !strings.HasSuffix(parsed.Path, "/") {
		parsed.Path += "/"
	}

	requestTimeout := config.RequestTimeout
	if requestTimeout <= 0 {
		requestTimeout = 15 * time.Second
	}
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	} else {
		copy := *httpClient
		httpClient = &copy
	}
	if httpClient.Timeout == 0 || httpClient.Timeout > requestTimeout {
		httpClient.Timeout = requestTimeout
	}
	maxPages := config.MaxPages
	if maxPages <= 0 {
		maxPages = 10
	}
	maxItems := config.MaxItems
	if maxItems <= 0 {
		maxItems = 500
	}

	return &Client{
		token:      strings.TrimSpace(config.Token),
		baseURL:    parsed,
		httpClient: httpClient,
		maxPages:   maxPages,
		maxItems:   maxItems,
	}, nil
}

func (c *Client) Snapshot(ctx context.Context, owner, repository string) (supervisor.RepositorySnapshot, error) {
	owner = strings.TrimSpace(owner)
	repository = strings.TrimSpace(repository)
	if owner == "" || repository == "" {
		return supervisor.RepositorySnapshot{}, fmt.Errorf("owner and repository are required")
	}

	issues, err := c.listIssues(ctx, owner, repository)
	if err != nil {
		return supervisor.RepositorySnapshot{}, err
	}
	issueIndexes := make(map[int]int, len(issues))
	for index := range issues {
		issueIndexes[issues[index].Number] = index
		comments, err := c.listComments(ctx, owner, repository, issues[index].Number)
		if err != nil {
			return supervisor.RepositorySnapshot{}, err
		}
		issues[index].Comments = comments
	}

	pulls, err := c.listPullRequests(ctx, owner, repository)
	if err != nil {
		return supervisor.RepositorySnapshot{}, err
	}
	for _, pull := range pulls {
		references := referencedIssueNumbers(pull.Title + "\n" + pull.Body)
		linkedIndexes := make([]int, 0, len(references))
		for _, number := range references {
			if index, ok := issueIndexes[number]; ok {
				linkedIndexes = append(linkedIndexes, index)
			}
		}
		if len(linkedIndexes) == 0 {
			continue
		}

		detail, err := c.pullRequestDetail(ctx, owner, repository, pull.Number)
		if err != nil {
			return supervisor.RepositorySnapshot{}, err
		}
		ci, err := c.ciSummary(ctx, owner, repository, pull.Head.SHA)
		if err != nil {
			return supervisor.RepositorySnapshot{}, err
		}
		converted := supervisor.PullRequest{
			GitHubID:       pull.ID,
			Number:         pull.Number,
			Title:          pull.Title,
			State:          pull.State,
			Draft:          pull.Draft,
			MergeableState: detail.MergeableState,
			BaseRef:        pull.Base.Ref,
			HeadRef:        pull.Head.Ref,
			HeadSHA:        pull.Head.SHA,
			URL:            pull.HTMLURL,
			UpdatedAt:      pull.UpdatedAt,
			CI:             ci,
		}
		for _, index := range linkedIndexes {
			issues[index].PullRequests = append(issues[index].PullRequests, converted)
		}
	}

	for index := range issues {
		sort.Slice(issues[index].PullRequests, func(i, j int) bool {
			return issues[index].PullRequests[i].Number < issues[index].PullRequests[j].Number
		})
	}
	return supervisor.RepositorySnapshot{FetchedAt: time.Now().UTC(), Issues: issues}, nil
}
