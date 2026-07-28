package resourcepack

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestLoadDefault(t *testing.T) {
	pack, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault: %v", err)
	}
	if pack.Profile != DefaultProfile {
		t.Fatalf("profile = %q", pack.Profile)
	}
	if pack.Manifest.Package != "cddm-dashboard-resources" || pack.Manifest.Version != "1.0" {
		t.Fatalf("unexpected manifest identity: %+v", pack.Manifest)
	}
	assertPackageRoles(t, pack)
}

func TestLoadV2(t *testing.T) {
	pack, err := Load(V2Profile)
	if err != nil {
		t.Fatalf("Load(V2Profile): %v", err)
	}
	if pack.Profile != V2Profile || pack.Manifest.BaseMethodology.Version != "2.1" || pack.Manifest.ResultProtocol.Version != "2" {
		t.Fatalf("unexpected v2 package identity: %+v", pack)
	}
	assertPackageRoles(t, pack)
	for _, key := range []string{"attachment_profiles", "action_vocabulary"} {
		if value, resourceErr := pack.Resource(key); resourceErr != nil || strings.TrimSpace(value) == "" {
			t.Fatalf("v2 resource %s unavailable: %v", key, resourceErr)
		}
	}
}

func assertPackageRoles(t *testing.T, pack Package) {
	t.Helper()
	if pack.Digest == "" {
		t.Fatal("package digest is empty")
	}
	for _, role := range []string{"lead", "implementor", "qa"} {
		value, err := pack.Role(role)
		if err != nil || value == "" {
			t.Fatalf("role %s unavailable: %v", role, err)
		}
	}
}

func TestPackageDigestDeterministic(t *testing.T) {
	for _, profile := range []string{DefaultProfile, V2Profile} {
		t.Run(profile, func(t *testing.T) {
			first, err := Load(profile)
			if err != nil {
				t.Fatal(err)
			}
			second, err := Load(profile)
			if err != nil {
				t.Fatal(err)
			}
			if first.Digest != second.Digest {
				t.Fatalf("digest changed: %s != %s", first.Digest, second.Digest)
			}
		})
	}
}

func TestUnsupportedProfileRejected(t *testing.T) {
	if _, err := Load("cddm-dashboard-resources/v9.9"); err == nil {
		t.Fatal("unsupported profile was accepted")
	}
}

func TestMissingResourceRejected(t *testing.T) {
	files := fstest.MapFS{
		"root/manifest.yaml":   {Data: []byte(testManifest)},
		"root/lead-trigger.md": {Data: []byte("lead")},
	}
	if _, err := loadFS(files, "root", DefaultProfile); err == nil {
		t.Fatal("incomplete package was accepted")
	}
}

func TestMalformedSchemaRejected(t *testing.T) {
	files := completeTestFS()
	files["root/worker-result.schema.json"] = &fstest.MapFile{Data: []byte("not-json")}
	if _, err := loadFS(files, "root", DefaultProfile); err == nil {
		t.Fatal("malformed schema was accepted")
	}
}

func TestUnknownIdentityFieldRejected(t *testing.T) {
	files := completeTestFS()
	files["root/manifest.yaml"] = &fstest.MapFile{Data: []byte(testManifest + "  extra: forbidden\n")}
	if _, err := loadFS(files, "root", DefaultProfile); err == nil {
		t.Fatal("unknown nested identity field was accepted")
	}
}

func TestV2AttachmentProfileDriftRejected(t *testing.T) {
	files := completeV2TestFS()
	files["root/attachment-profiles.json"] = &fstest.MapFile{Data: []byte(`{
  "$id":"cddm-dashboard-attachments/v2",
  "profiles":{
    "lead":{"bootstrap":["wrong.md"],"command":["gpt-gh-connector-guidelines.md"]},
    "implementor":{"bootstrap":["02-implementor-trigger.md","gpt-gh-connector-guidelines.md"],"command":["gpt-gh-connector-guidelines.md"]},
    "qa":{"bootstrap":["03-qa-trigger.md","gpt-gh-connector-guidelines.md"],"command":["gpt-gh-connector-guidelines.md"]}
  }
}`)}
	if _, err := loadFS(files, "root", V2Profile); err == nil {
		t.Fatal("drifted attachment profile was accepted")
	}
}

func TestV2RejectsUnexpectedManifestResource(t *testing.T) {
	files := completeV2TestFS()
	files["root/manifest.yaml"] = &fstest.MapFile{Data: []byte(v2TestManifest + "  unexpected: action-vocabulary.md\n")}
	if _, err := loadFS(files, "root", V2Profile); err == nil {
		t.Fatal("unexpected manifest resource was accepted")
	}
}

func completeTestFS() fstest.MapFS {
	return fstest.MapFS{
		"root/manifest.yaml":             {Data: []byte(testManifest)},
		"root/lead-trigger.md":           {Data: []byte("lead")},
		"root/implementor-trigger.md":    {Data: []byte("implementor")},
		"root/qa-trigger.md":             {Data: []byte("qa")},
		"root/worker-result-marker.md":   {Data: []byte("marker")},
		"root/worker-result.schema.json": {Data: []byte(`{"$id":"cddm-worker-result/v1","type":"object"}`)},
	}
}

func completeV2TestFS() fstest.MapFS {
	return fstest.MapFS{
		"root/manifest.yaml":             {Data: []byte(v2TestManifest)},
		"root/lead-trigger.md":           {Data: []byte("lead")},
		"root/implementor-trigger.md":    {Data: []byte("implementor")},
		"root/qa-trigger.md":             {Data: []byte("qa")},
		"root/worker-result-marker.md":   {Data: []byte("marker")},
		"root/worker-result.schema.json": {Data: []byte(`{"$id":"cddm-worker-result/v2","type":"object"}`)},
		"root/action-vocabulary.md":      {Data: []byte("actions")},
		"root/attachment-profiles.json":  {Data: []byte(validV2Attachments)},
	}
}

const testManifest = `package: cddm-dashboard-resources
version: "1.0"
base_methodology:
  package: cddm-minimal
  version: "2.0"
result_protocol:
  package: cddm-worker-result
  version: "1"
resources:
  lead: lead-trigger.md
  implementor: implementor-trigger.md
  qa: qa-trigger.md
  result_marker: worker-result-marker.md
  result_schema: worker-result.schema.json
`

const v2TestManifest = `package: cddm-dashboard-resources
version: "2.0"
base_methodology:
  package: cddm-minimal
  version: "2.1"
result_protocol:
  package: cddm-worker-result
  version: "2"
resources:
  lead: lead-trigger.md
  implementor: implementor-trigger.md
  qa: qa-trigger.md
  result_marker: worker-result-marker.md
  result_schema: worker-result.schema.json
  attachment_profiles: attachment-profiles.json
  action_vocabulary: action-vocabulary.md
`

const validV2Attachments = `{
  "$id":"cddm-dashboard-attachments/v2",
  "profiles":{
    "lead":{"bootstrap":["01-workflow.md","cddm-minimal-issue-sizing-standard.md"],"command":["gpt-gh-connector-guidelines.md"]},
    "implementor":{"bootstrap":["02-implementor-trigger.md","gpt-gh-connector-guidelines.md"],"command":["gpt-gh-connector-guidelines.md"]},
    "qa":{"bootstrap":["03-qa-trigger.md","gpt-gh-connector-guidelines.md"],"command":["gpt-gh-connector-guidelines.md"]}
  }
}`
