package githubclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReadMergeFactsUsesExactIssueAndPullRequest(t *testing.T) {
	mergedAt := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	head := "241401d9f5c1fb2004eeb19ec612323f74b57199"
	mergeCommit := "341401d9f5c1fb2004eeb19ec612323f74b57199"
	requested := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/acme/service/issues/101":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": 1001, "number": 101, "title": "Candidate", "body": "", "state": "closed",
				"html_url": "https://example/issues/101", "user": map[string]any{"login": "owner"},
				"created_at": mergedAt.Add(-time.Hour), "updated_at": mergedAt,
				"labels": []map[string]any{{"name": "status:done", "color": "ffffff"}},
			})
		case "/repos/acme/service/pulls/150":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 150, "state": "closed", "merged": true,
				"merge_commit_sha": mergeCommit, "merged_at": mergedAt,
				"base": map[string]any{"ref": "main"}, "head": map[string]any{"sha": head},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL + "/", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	facts, err := client.ReadMergeFacts(context.Background(), "acme", "service", 101, 150)
	if err != nil {
		t.Fatal(err)
	}
	if facts.Repository != "acme/service" || facts.IssueNumber != 101 || facts.IssueState != "closed" || len(facts.IssueLabels) != 1 || facts.IssueLabels[0] != "status:done" {
		t.Fatalf("Issue facts = %+v", facts)
	}
	if facts.PRNumber != 150 || facts.PRState != "closed" || !facts.Merged || facts.ApprovedHead != head || facts.BaseRef != "main" || facts.MergeCommit != mergeCommit || facts.MergedAt == nil || !facts.MergedAt.Equal(mergedAt) {
		t.Fatalf("PR facts = %+v", facts)
	}
	if len(requested) != 2 || requested[0] != "/repos/acme/service/issues/101" || requested[1] != "/repos/acme/service/pulls/150" {
		t.Fatalf("requests = %+v", requested)
	}
}

func TestReadMergeFactsRejectsIncompleteIdentity(t *testing.T) {
	client, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ReadMergeFacts(context.Background(), "", "service", 101, 150); err == nil {
		t.Fatal("empty owner accepted")
	}
	if _, err := client.ReadMergeFacts(context.Background(), "acme", "service", 0, 150); err == nil {
		t.Fatal("zero Issue accepted")
	}
}
