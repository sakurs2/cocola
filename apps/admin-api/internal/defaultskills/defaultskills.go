package defaultskills

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed assets/lark-cli-manifest.json
var larkCLIManifestJSON []byte

//go:embed assets/lark-cli-skills-v1.0.77.zip
var larkCLIArchive []byte

type Set struct {
	Name          string
	Version       string
	UpstreamURL   string
	UpstreamRef   string
	Archive       []byte
	ArchiveSHA256 string
	SkillIDs      []string
}

type manifest struct {
	Name          string   `json:"name"`
	Version       string   `json:"version"`
	UpstreamURL   string   `json:"upstream_url"`
	UpstreamRef   string   `json:"upstream_ref"`
	Archive       string   `json:"archive"`
	ArchiveSHA256 string   `json:"archive_sha256"`
	SkillIDs      []string `json:"skill_ids"`
}

func LarkCLI() (Set, error) {
	var value manifest
	if err := json.Unmarshal(larkCLIManifestJSON, &value); err != nil {
		return Set{}, fmt.Errorf("decode embedded lark-cli Skill manifest: %w", err)
	}
	expectedArchive := "lark-cli-skills-v" + value.Version + ".zip"
	if value.Archive != expectedArchive {
		return Set{}, fmt.Errorf(
			"embedded lark-cli Skill archive mismatch: got %q, want %q",
			value.Archive,
			expectedArchive,
		)
	}
	return Set{
		Name:          value.Name,
		Version:       value.Version,
		UpstreamURL:   value.UpstreamURL,
		UpstreamRef:   value.UpstreamRef,
		Archive:       append([]byte(nil), larkCLIArchive...),
		ArchiveSHA256: value.ArchiveSHA256,
		SkillIDs:      append([]string(nil), value.SkillIDs...),
	}, nil
}
