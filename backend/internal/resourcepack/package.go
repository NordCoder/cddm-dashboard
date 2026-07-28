// Package resourcepack loads and validates versioned Dashboard worker-loop resources.
package resourcepack

import (
	"bufio"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

const DefaultProfile = "cddm-dashboard-resources/v1.0"

//go:embed assets/cddm-dashboard-resources/*/*
var embedded embed.FS

type Identity struct {
	Package string `json:"package"`
	Version string `json:"version"`
}

type Manifest struct {
	Package         string            `json:"package"`
	Version         string            `json:"version"`
	BaseMethodology Identity          `json:"base_methodology"`
	ResultProtocol  Identity          `json:"result_protocol"`
	Resources       map[string]string `json:"resources"`
}

type Package struct {
	Profile  string            `json:"profile"`
	Manifest Manifest          `json:"manifest"`
	Files    map[string]string `json:"-"`
	Digests  map[string]string `json:"digests"`
	Digest   string            `json:"digest"`
}

func Load(profile string) (Package, error) {
	profile = strings.Trim(strings.TrimSpace(profile), "/")
	if profile == "" {
		return Package{}, errors.New("resource profile is required")
	}
	return loadFS(embedded, path.Join("assets", profile), profile)
}

func LoadDefault() (Package, error) { return Load(DefaultProfile) }

func (p Package) Role(role string) (string, error) {
	key := strings.ToLower(strings.TrimSpace(role))
	if key != "lead" && key != "implementor" && key != "qa" {
		return "", fmt.Errorf("unsupported worker resource role %q", role)
	}
	return p.Resource(key)
}

func (p Package) Resource(key string) (string, error) {
	name, ok := p.Manifest.Resources[key]
	if !ok || strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("resource %q is not declared", key)
	}
	value, ok := p.Files[name]
	if !ok {
		return "", fmt.Errorf("resource %q file %q is unavailable", key, name)
	}
	return value, nil
}

func loadFS(files fs.FS, root, profile string) (Package, error) {
	manifestBytes, err := fs.ReadFile(files, path.Join(root, "manifest.yaml"))
	if err != nil {
		return Package{}, fmt.Errorf("read resource manifest for %s: %w", profile, err)
	}
	manifest, err := parseManifest(string(manifestBytes))
	if err != nil {
		return Package{}, fmt.Errorf("parse resource manifest for %s: %w", profile, err)
	}
	if err := validateManifest(profile, manifest); err != nil {
		return Package{}, err
	}

	result := Package{
		Profile:  profile,
		Manifest: manifest,
		Files:    make(map[string]string, len(manifest.Resources)),
		Digests:  make(map[string]string, len(manifest.Resources)+1),
	}
	result.Digests["manifest.yaml"] = digest(manifestBytes)
	for key, name := range manifest.Resources {
		contents, readErr := fs.ReadFile(files, path.Join(root, name))
		if readErr != nil {
			return Package{}, fmt.Errorf("read resource %s (%s): %w", key, name, readErr)
		}
		if strings.TrimSpace(string(contents)) == "" {
			return Package{}, fmt.Errorf("resource %s (%s) is empty", key, name)
		}
		result.Files[name] = string(contents)
		result.Digests[name] = digest(contents)
	}
	if err := validateSchema(result); err != nil {
		return Package{}, err
	}
	result.Digest = packageDigest(result.Digests)
	return result, nil
}

func validateManifest(profile string, manifest Manifest) error {
	expectedProfile := strings.Trim(manifest.Package+"/v"+manifest.Version, "/")
	if expectedProfile != profile {
		return fmt.Errorf("resource profile mismatch: requested=%s manifest=%s", profile, expectedProfile)
	}
	if manifest.BaseMethodology.Package != "cddm-minimal" || manifest.BaseMethodology.Version != "2.0" {
		return fmt.Errorf("unsupported base methodology %s/v%s", manifest.BaseMethodology.Package, manifest.BaseMethodology.Version)
	}
	if manifest.ResultProtocol.Package != "cddm-worker-result" || manifest.ResultProtocol.Version != "1" {
		return fmt.Errorf("unsupported result protocol %s/v%s", manifest.ResultProtocol.Package, manifest.ResultProtocol.Version)
	}
	for _, key := range []string{"lead", "implementor", "qa", "result_marker", "result_schema"} {
		name := strings.TrimSpace(manifest.Resources[key])
		if name == "" || path.Base(name) != name {
			return fmt.Errorf("resource manifest has invalid %s entry", key)
		}
	}
	return nil
}

func validateSchema(pack Package) error {
	contents, err := pack.Resource("result_schema")
	if err != nil {
		return err
	}
	var schema map[string]any
	if err := json.Unmarshal([]byte(contents), &schema); err != nil {
		return fmt.Errorf("decode worker result schema: %w", err)
	}
	if schema["$id"] != "cddm-worker-result/v1" || schema["type"] != "object" {
		return errors.New("worker result schema has unsupported identity or root type")
	}
	return nil
}

func parseManifest(contents string) (Manifest, error) {
	manifest := Manifest{Resources: make(map[string]string)}
	section := ""
	scanner := bufio.NewScanner(strings.NewReader(contents))
	for scanner.Scan() {
		raw := scanner.Text()
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " \t"))
		key, value, found := strings.Cut(trimmed, ":")
		if !found {
			return Manifest{}, fmt.Errorf("invalid manifest line %q", raw)
		}
		key, value = strings.TrimSpace(key), unquote(strings.TrimSpace(value))
		if indent == 0 {
			if value == "" {
				section = key
				continue
			}
			section = ""
			switch key {
			case "package":
				manifest.Package = value
			case "version":
				manifest.Version = value
			default:
				return Manifest{}, fmt.Errorf("unknown manifest key %q", key)
			}
			continue
		}
		if value == "" {
			return Manifest{}, fmt.Errorf("manifest field %s.%s is empty", section, key)
		}
		switch section {
		case "base_methodology":
			if err := assignIdentity(&manifest.BaseMethodology, key, value); err != nil {
				return Manifest{}, err
			}
		case "result_protocol":
			if err := assignIdentity(&manifest.ResultProtocol, key, value); err != nil {
				return Manifest{}, err
			}
		case "resources":
			manifest.Resources[key] = value
		default:
			return Manifest{}, fmt.Errorf("manifest field %q has no supported section", key)
		}
	}
	if err := scanner.Err(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func assignIdentity(identity *Identity, key, value string) error {
	switch key {
	case "package":
		identity.Package = value
	case "version":
		identity.Version = value
	default:
		return fmt.Errorf("unknown identity key %q", key)
	}
	return nil
}

func unquote(value string) string {
	if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
		return value[1 : len(value)-1]
	}
	return value
}

func digest(contents []byte) string {
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:])
}

func packageDigest(digests map[string]string) string {
	keys := make([]string, 0, len(digests))
	for key := range digests {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	for _, key := range keys {
		builder.WriteString(key)
		builder.WriteByte(0)
		builder.WriteString(digests[key])
		builder.WriteByte('\n')
	}
	return digest([]byte(builder.String()))
}
