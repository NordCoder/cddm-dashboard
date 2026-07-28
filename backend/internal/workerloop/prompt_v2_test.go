package workerloop

import (
	"strings"
	"testing"

	"github.com/NordCoder/cddm-dashboard/backend/internal/resourcepack"
)

func TestBuildWorkflowCommandUsesSelectedV2ProtocolIdentity(t *testing.T) {
	_, project, store, _ := testService(t, ":memory:")
	resources, err := resourcepack.Load(resourcepack.V2Profile)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := NewCommandEngine(store, resources).BuildWorkflowCommand(project.ID, 140, testGeneration(project.ID, "lead", ""))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"Resources: cddm-dashboard-resources/v2.0",
		"Base methodology: cddm-minimal/v2.1",
		"Result protocol: cddm-worker-result/v2",
		"marker conforming to `cddm-worker-result/v2`",
		"# CDDM Dashboard Lead Worker v2",
	} {
		if !strings.Contains(prepared.Prompt, expected) {
			t.Fatalf("v2 prompt does not contain %q\n%s", expected, prepared.Prompt)
		}
	}
}

func TestDefaultWorkflowCommandRemainsV1(t *testing.T) {
	_, project, store, _ := testService(t, ":memory:")
	resources, err := resourcepack.LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := NewCommandEngine(store, resources).BuildWorkflowCommand(project.ID, 140, testGeneration(project.ID, "lead", ""))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prepared.Prompt, "Result protocol: cddm-worker-result/v1") || strings.Contains(prepared.Prompt, "cddm-worker-result/v2") {
		t.Fatalf("default prompt identity changed\n%s", prepared.Prompt)
	}
}
