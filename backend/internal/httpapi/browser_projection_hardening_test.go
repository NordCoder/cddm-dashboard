package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/NordCoder/cddm-dashboard/backend/internal/browserbinding"
)

func TestBrowserBindingResponseDoesNotSerializeMutablePresencePointer(t *testing.T) {
	lastSeen := time.Date(2026, 7, 28, 1, 0, 0, 0, time.UTC)
	value := browserbinding.Binding{BindingID: "binding", BindingVersion: 1, ProjectID: 1, LaneKey: "lane", WorkerID: "worker", Enabled: true, Readiness: "ready", LastSeen: &lastSeen, UpdatedAt: lastSeen}
	response := httptest.NewRecorder()
	(&browserHandler{}).writeBrowserResult(response, 200, value, nil)
	var decoded map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["last_seen"]; ok {
		t.Fatalf("response serialized mutable presence pointer: %s", response.Body.String())
	}
}
