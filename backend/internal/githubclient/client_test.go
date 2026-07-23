package githubclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSnapshotPaginatesAndBuildsNormalizedIssueState(t *testing.T) {
	const token = "secret-token"
	var mu sync.Mutex
	requestedPages := make([]string, 0)
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "cddm-dashboard-supervisor" {
			t.Errorf("User-Agent = %q", got)
		}
		mu.Lock()
		requestedPages = append(requestedPages, r.URL.Path+"?"+r.URL.RawQuery)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/repos/acme/service/issues" && r.URL.Query().Get("page") == "1":
			items := make([]map[string]any, 0, 100)
			items = append(items, issuePayload(101, 1, now))
			for index := 0; index < 99; index++ {
				item := issuePayload(int64(1000+index), 1000+index, now)
				item["pull_request"] = map[string]any{"url": "https://api.example/pr"}
				items = append(items, item)
			}
			_ = json.NewEncoder(w).Encode(items)
		case r.URL.Path == "/repos/acme/service/issues" && r.URL.Query().Get("page") == "2":
			_ = json.NewEncoder(w).Encode([]map[string]any{issuePayload(202, 2, now)})
		case r.URL.Path == "/repos/acme/service/issues/1/comments":
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"id": 501, "body": "Lead Dispatch", "html_url": "https://example/comments/501",
				"user": map[string]any{"login": "lead"}, "created_at": now, "updated_at": now,
			}})
		case r.URL.Path == "/repos/acme/service/issues/2/comments":
			_ = json.NewEncoder(w).Encode([]any{})
		case r.URL.Path == "/repos/acme/service/pulls":
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"id": 9001, "number": 7, "title": "Implement stage", "body": "Closes #1",
				"state": "open", "draft": true, "html_url": "https://example/pulls/7", "updated_at": now,
				"base": map[string]any{"ref": "main"}, "head": map[string]any{"ref": "stage", "sha": "abc123"},
			}})
		case r.URL.Path == "/repos/acme/service/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"mergeable_state": "clean"})
		case r.URL.Path == "/repos/acme/service/commits/abc123/check-runs":
			_ = json.NewEncoder(w).Encode(map[string]any{"check_runs": []map[string]any{{
				"status": "completed", "conclusion": "success", "html_url": "https://example/checks/1",
				"started_at": now.Add(-time.Minute), "completed_at": now,
			}}})
		default:
			http.Error(w, fmt.Sprintf("unexpected path %s", r.URL.String()), http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := New(Config{
		Token: token, BaseURL: server.URL + "/", RequestTimeout: time.Second,
		MaxPages: 3, MaxItems: 200, HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	snapshot, err := client.Snapshot(context.Background(), "acme", "service")
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Issues) != 2 {
		t.Fatalf("issue count = %d, want 2", len(snapshot.Issues))
	}
	if len(snapshot.Issues[0].Comments) != 1 || len(snapshot.Issues[0].PullRequests) != 1 {
		t.Fatalf("issue 1 = %#v", snapshot.Issues[0])
	}
	pullRequest := snapshot.Issues[0].PullRequests[0]
	if pullRequest.HeadSHA != "abc123" || pullRequest.MergeableState != "clean" || pullRequest.CI.Conclusion != "success" {
		t.Fatalf("pull request = %#v", pullRequest)
	}
	if len(snapshot.Issues[1].PullRequests) != 0 {
		t.Fatalf("unrelated pull request leaked to issue 2: %#v", snapshot.Issues[1].PullRequests)
	}
	mu.Lock()
	joined := strings.Join(requestedPages, "\n")
	mu.Unlock()
	if !strings.Contains(joined, "/issues?state=open&per_page=100&page=2") {
		t.Fatalf("second issue page was not requested:\n%s", joined)
	}
}

func TestErrorDoesNotExposeToken(t *testing.T) {
	const token = "never-log-this"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Bad credentials"})
	}))
	defer server.Close()

	client, err := New(Config{Token: token, BaseURL: server.URL + "/", HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = client.Snapshot(context.Background(), "acme", "service")
	if err == nil {
		t.Fatal("Snapshot() error = nil, want error")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("error leaked token: %v", err)
	}
}

func issuePayload(id int64, number int, now time.Time) map[string]any {
	return map[string]any{
		"id": id, "number": number, "title": fmt.Sprintf("Issue %d", number), "state": "open",
		"html_url": fmt.Sprintf("https://example/issues/%d", number), "user": map[string]any{"login": "owner"},
		"created_at": now.Add(-time.Hour), "updated_at": now,
		"labels": []map[string]any{{"name": "stage", "color": "ffffff", "description": "Stage work"}},
	}
}

func TestPaginationIsBoundedByConfiguredPageLimit(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		items := make([]map[string]any, 100)
		for index := range items {
			items[index] = issuePayload(int64(index+1), index+1, time.Now().UTC())
		}
		_ = json.NewEncoder(w).Encode(items)
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL + "/", MaxPages: 2, MaxItems: 500, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	items, err := fetchPages[apiIssue](context.Background(), client, "repos/acme/service/issues?state=open&per_page=100")
	if err != nil {
		t.Fatalf("fetchPages() error = %v", err)
	}
	if requests != 2 || len(items) != 200 {
		t.Fatalf("requests = %d, items = %d; want 2 and 200", requests, len(items))
	}
}
