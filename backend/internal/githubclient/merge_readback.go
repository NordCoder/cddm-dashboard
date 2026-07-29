package githubclient

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type MergeFacts struct {
	Repository     string
	IssueNumber   int
	IssueState    string
	IssueLabels   []string
	PRNumber      int
	PRState       string
	Merged        bool
	ApprovedHead  string
	BaseRef       string
	MergeCommit   string
	MergedAt      *time.Time
}

type mergePullRequestDetail struct {
	Number         int        `json:"number"`
	State          string     `json:"state"`
	Merged         bool       `json:"merged"`
	MergeCommitSHA string     `json:"merge_commit_sha"`
	MergedAt       *time.Time `json:"merged_at"`
	Base           struct {
		Ref string `json:"ref"`
	} `json:"base"`
	Head struct {
		SHA string `json:"sha"`
	} `json:"head"`
}

func (c *Client) ReadMergeFacts(ctx context.Context, owner, repository string, issueNumber, prNumber int) (MergeFacts, error) {
	owner = strings.TrimSpace(owner)
	repository = strings.TrimSpace(repository)
	if owner == "" || repository == "" || issueNumber <= 0 || prNumber <= 0 {
		return MergeFacts{}, fmt.Errorf("owner, repository, Issue and PR are required")
	}
	var issue apiIssue
	issuePath := fmt.Sprintf("repos/%s/%s/issues/%d", url.PathEscape(owner), url.PathEscape(repository), issueNumber)
	if err := c.getJSON(ctx, issuePath, &issue); err != nil {
		return MergeFacts{}, fmt.Errorf("read Issue #%d for merge verification: %w", issueNumber, err)
	}
	var pull mergePullRequestDetail
	pullPath := fmt.Sprintf("repos/%s/%s/pulls/%d", url.PathEscape(owner), url.PathEscape(repository), prNumber)
	if err := c.getJSON(ctx, pullPath, &pull); err != nil {
		return MergeFacts{}, fmt.Errorf("read PR #%d for merge verification: %w", prNumber, err)
	}
	labels := make([]string, 0, len(issue.Labels))
	for _, label := range issue.Labels {
		labels = append(labels, label.Name)
	}
	return MergeFacts{
		Repository: owner + "/" + repository, IssueNumber: issue.Number,
		IssueState: issue.State, IssueLabels: labels, PRNumber: pull.Number,
		PRState: pull.State, Merged: pull.Merged, ApprovedHead: pull.Head.SHA,
		BaseRef: pull.Base.Ref, MergeCommit: pull.MergeCommitSHA, MergedAt: pull.MergedAt,
	}, nil
}
