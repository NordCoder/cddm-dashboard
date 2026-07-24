package githubclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestListIssuesPreservesAuthoritativeBody(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/service/issues" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"id": 101, "number": 11, "title": "Stage 4", "body": "authoritative objective",
			"state": "open", "html_url": "https://example.invalid/issues/11",
			"user": map[string]any{"login": "owner"}, "created_at": now, "updated_at": now,
			"labels": []any{},
		}})
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL + "/", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	issues, err := client.listIssues(context.Background(), "acme", "service")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].Body != "authoritative objective" {
		t.Fatalf("issues = %#v", issues)
	}
}
