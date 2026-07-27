package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
)

const bundledSkillSourceType = "bundled"

// DefaultSkillSet is one versioned collection shipped with the cocola release.
// Archive contains the same multi-Skill ZIP accepted by the normal Admin import
// flow; SkillIDs makes accidental partial or extra upstream packaging fail
// before the catalog is mutated.
type DefaultSkillSet struct {
	Name          string
	Version       string
	UpstreamURL   string
	UpstreamRef   string
	Archive       []byte
	ArchiveSHA256 string
	SkillIDs      []string
}

type DefaultSkillReconcileResult struct {
	Created   int
	Updated   int
	Unchanged int
	Skipped   int
}

// ReconcileDefaultSkills installs or upgrades a release-owned Admin Skill set.
// Existing non-bundled entries are treated as an explicit administrator
// takeover and are never overwritten. Disabling a bundled Skill also survives
// restarts and upgrades.
func (a *Admin) ReconcileDefaultSkills(
	ctx context.Context,
	set DefaultSkillSet,
) (DefaultSkillReconcileResult, error) {
	var result DefaultSkillReconcileResult
	if a.skillBundles == nil ||
		strings.TrimSpace(set.Name) == "" ||
		strings.TrimSpace(set.Version) == "" ||
		strings.TrimSpace(set.UpstreamURL) == "" ||
		strings.TrimSpace(set.UpstreamRef) == "" ||
		len(set.Archive) == 0 ||
		len(set.SkillIDs) == 0 {
		return result, fmt.Errorf("%w: incomplete default Skill set", ErrInvalidArg)
	}
	sum := sha256.Sum256(set.Archive)
	actualArchiveSHA := hex.EncodeToString(sum[:])
	if !strings.EqualFold(strings.TrimSpace(set.ArchiveSHA256), actualArchiveSHA) {
		return result, fmt.Errorf("%w: default Skill archive checksum mismatch", ErrInvalidArg)
	}

	candidates, err := parseSkillArchive(set.Archive)
	if err != nil {
		return result, err
	}
	if err := validateDefaultSkillCandidates(candidates, set.SkillIDs); err != nil {
		return result, err
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	for i := range candidates {
		outcome, err := a.reconcileDefaultSkill(ctx, set, candidates[i])
		if err != nil {
			return result, fmt.Errorf("reconcile default Skill %q: %w", candidates[i].ID, err)
		}
		switch outcome {
		case "created":
			result.Created++
		case "updated":
			result.Updated++
		case "unchanged":
			result.Unchanged++
		case "skipped":
			result.Skipped++
		}
	}
	return result, nil
}

func validateDefaultSkillCandidates(
	candidates []SkillImportCandidate,
	expectedIDs []string,
) error {
	actual := make([]string, 0, len(candidates))
	for i := range candidates {
		if !candidates[i].Valid {
			return fmt.Errorf(
				"%w: bundled Skill %q is invalid: %s",
				ErrInvalidArg,
				candidates[i].ID,
				strings.Join(candidates[i].Errors, "; "),
			)
		}
		actual = append(actual, candidates[i].ID)
	}
	expected := append([]string(nil), expectedIDs...)
	sort.Strings(actual)
	sort.Strings(expected)
	if len(actual) != len(expected) {
		return fmt.Errorf(
			"%w: bundled Skill IDs differ: got %d, want %d",
			ErrInvalidArg,
			len(actual),
			len(expected),
		)
	}
	for i := range actual {
		if actual[i] != expected[i] {
			return fmt.Errorf(
				"%w: bundled Skill IDs differ at %d: got %q, want %q",
				ErrInvalidArg,
				i,
				actual[i],
				expected[i],
			)
		}
	}
	return nil
}

func (a *Admin) reconcileDefaultSkill(
	ctx context.Context,
	set DefaultSkillSet,
	candidate SkillImportCandidate,
) (string, error) {
	existing, err := a.store.GetSkill(ctx, candidate.ID)
	switch {
	case err == nil:
		return a.updateDefaultSkill(ctx, set, candidate, existing)
	case !errors.Is(err, store.ErrNotFound):
		return "", err
	}

	desired := bundledSkill(set, candidate, true, a.now().UTC())
	if err := a.putDefaultSkillBundle(ctx, &desired, candidate); err != nil {
		return "", err
	}
	if err := a.store.CreateSkill(ctx, desired); err == nil {
		return "created", nil
	} else if !errors.Is(err, store.ErrConflict) {
		return "", err
	}

	// Another admin-api replica won the create race. Re-read and converge
	// without overwriting a simultaneous administrator-owned entry.
	existing, err = a.store.GetSkill(ctx, candidate.ID)
	if err != nil {
		return "", err
	}
	return a.updateDefaultSkill(ctx, set, candidate, existing)
}

func (a *Admin) updateDefaultSkill(
	ctx context.Context,
	set DefaultSkillSet,
	candidate SkillImportCandidate,
	existing store.Skill,
) (string, error) {
	if existing.SourceType != bundledSkillSourceType {
		return "skipped", nil
	}
	desired := bundledSkill(set, candidate, existing.Enabled, a.now().UTC())
	desired.CreatedAt = existing.CreatedAt
	desired.CreatedBy = existing.CreatedBy
	if defaultSkillMatches(existing, desired) {
		return "unchanged", nil
	}
	if err := a.putDefaultSkillBundle(ctx, &desired, candidate); err != nil {
		return "", err
	}
	if err := a.store.UpdateBundledSkill(ctx, desired); errors.Is(err, store.ErrConflict) {
		// The administrator took ownership while the bundle was being uploaded.
		// The conditional store update guarantees the release reconciler cannot
		// replace that newer row with its earlier bundled snapshot.
		return "skipped", nil
	} else if err != nil {
		return "", err
	}
	return "updated", nil
}

func bundledSkill(
	set DefaultSkillSet,
	candidate SkillImportCandidate,
	enabled bool,
	now time.Time,
) store.Skill {
	return store.Skill{
		ID:              candidate.ID,
		RuntimeID:       candidate.ID,
		Name:            candidate.Name,
		Description:     candidate.Description,
		Version:         set.Version,
		Entrypoint:      "$CLAUDE_CONFIG_DIR/skills/" + candidate.ID,
		Enabled:         enabled,
		Scope:           "admin",
		SourceType:      bundledSkillSourceType,
		SourceURL:       set.UpstreamURL,
		SourceRef:       set.UpstreamRef,
		SourcePath:      candidate.Path,
		ContentSHA256:   candidate.ContentSHA256,
		ManifestJSON:    skillManifestJSON(candidate),
		FrontmatterJSON: skillFrontmatterJSON(candidate),
		ResultContract:  candidate.ResultContract,
		SkillMD:         candidate.SkillMD,
		FileCount:       candidate.FileCount,
		SizeBytes:       candidate.SizeBytes,
		CreatedAt:       now,
		UpdatedAt:       now,
		CreatedBy:       "system:default-skill-reconciler",
		UpdatedBy:       "system:default-skill-reconciler",
	}
}

func (a *Admin) putDefaultSkillBundle(
	ctx context.Context,
	skill *store.Skill,
	candidate SkillImportCandidate,
) error {
	objectKey := fmt.Sprintf(
		"skills/admin/%s/%s.zip",
		skill.ID,
		candidate.ContentSHA256,
	)
	if err := a.skillBundles.PutBytes(ctx, objectKey, candidate.Bundle, "application/zip"); err != nil {
		return err
	}
	skill.BundleObjectKey = objectKey
	return nil
}

func defaultSkillMatches(existing, desired store.Skill) bool {
	return existing.RuntimeID == desired.RuntimeID &&
		existing.Name == desired.Name &&
		existing.Description == desired.Description &&
		existing.Version == desired.Version &&
		existing.Entrypoint == desired.Entrypoint &&
		existing.Scope == desired.Scope &&
		existing.OwnerUserID == desired.OwnerUserID &&
		existing.SourceType == desired.SourceType &&
		existing.SourceURL == desired.SourceURL &&
		existing.SourceRef == desired.SourceRef &&
		existing.SourcePath == desired.SourcePath &&
		existing.ContentSHA256 == desired.ContentSHA256 &&
		existing.BundleObjectKey == fmt.Sprintf(
			"skills/admin/%s/%s.zip",
			desired.ID,
			desired.ContentSHA256,
		) &&
		existing.SkillMD == desired.SkillMD &&
		existing.FileCount == desired.FileCount &&
		existing.SizeBytes == desired.SizeBytes &&
		string(existing.ManifestJSON) == string(desired.ManifestJSON) &&
		string(existing.FrontmatterJSON) == string(desired.FrontmatterJSON)
}
