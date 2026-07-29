package resourcepack

import (
	"encoding/json"
	"fmt"
	"strings"
)

const AttachmentProfileV2 = "cddm-dashboard-attachments/v2"

type AttachmentSelection struct {
	Profile string   `json:"profile"`
	Files   []string `json:"files"`
}

// BootstrapAttachments returns the exact ordered Library filenames frozen by
// the v2 resource package. Callers cannot supply or extend this list.
func (p Package) BootstrapAttachments(role string) (AttachmentSelection, error) {
	if p.Profile != V2Profile {
		return AttachmentSelection{}, fmt.Errorf("bootstrap attachments require %s", V2Profile)
	}
	role = strings.ToLower(strings.TrimSpace(role))
	if role != "lead" && role != "implementor" && role != "qa" {
		return AttachmentSelection{}, fmt.Errorf("unsupported attachment role %q", role)
	}
	contents, err := p.Resource("attachment_profiles")
	if err != nil {
		return AttachmentSelection{}, err
	}
	var profiles attachmentProfiles
	decoder := json.NewDecoder(strings.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&profiles); err != nil {
		return AttachmentSelection{}, fmt.Errorf("decode attachment profiles: %w", err)
	}
	profile, ok := profiles.Profiles[role]
	if !ok || profiles.ID != AttachmentProfileV2 || len(profile.Bootstrap) == 0 {
		return AttachmentSelection{}, fmt.Errorf("attachment profile %s is unavailable", role)
	}
	return AttachmentSelection{
		Profile: AttachmentProfileV2 + ":" + role + ":bootstrap",
		Files:   append([]string(nil), profile.Bootstrap...),
	}, nil
}
