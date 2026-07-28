package resourcepack

import (
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
	if pack.Digest == "" {
		t.Fatal("package digest is empty")
	}
	for _, role := range []string{"lead", "implementor", "qa"} {
		value, roleErr := pack.Role(role)
		if roleErr != nil || value == "" {
			t.Fatalf("role %s unavailable: %v", role, roleErr)
		}
	}
}

func TestPackageDigestDeterministic(t *testing.T) {
	first, err := LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatalf("digest changed: %s != %s", first.Digest, second.Digest)
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
