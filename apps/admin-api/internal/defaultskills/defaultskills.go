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

//go:embed assets/community-core-manifest.json
var communityCoreManifestJSON []byte

//go:embed assets/community-core-skills-v1.0.0.zip
var communityCoreArchive []byte

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
	return loadSet(larkCLIManifestJSON, larkCLIArchive)
}

func CommunityCore() (Set, error) {
	return loadSet(communityCoreManifestJSON, communityCoreArchive)
}

func All() ([]Set, error) {
	loaders := []func() (Set, error){LarkCLI, CommunityCore}
	sets := make([]Set, 0, len(loaders))
	for _, load := range loaders {
		set, err := load()
		if err != nil {
			return nil, err
		}
		sets = append(sets, set)
	}
	return sets, nil
}

func loadSet(manifestJSON, archive []byte) (Set, error) {
	var value manifest
	if err := json.Unmarshal(manifestJSON, &value); err != nil {
		return Set{}, fmt.Errorf("decode embedded default Skill manifest: %w", err)
	}
	expectedArchive := value.Name + "-skills-v" + value.Version + ".zip"
	if value.Archive != expectedArchive {
		return Set{}, fmt.Errorf(
			"embedded default Skill archive mismatch: got %q, want %q",
			value.Archive,
			expectedArchive,
		)
	}
	return Set{
		Name:          value.Name,
		Version:       value.Version,
		UpstreamURL:   value.UpstreamURL,
		UpstreamRef:   value.UpstreamRef,
		Archive:       append([]byte(nil), archive...),
		ArchiveSHA256: value.ArchiveSHA256,
		SkillIDs:      append([]string(nil), value.SkillIDs...),
	}, nil
}
