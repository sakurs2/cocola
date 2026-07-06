package service

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	maxSkillArchiveBytes      = 64 << 20
	maxSkillUncompressedBytes = 128 << 20
	gitCloneTimeout           = 60 * time.Second
)

var skillIDCleanRE = regexp.MustCompile(`[^a-z0-9]+`)

type SkillFileManifest struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type SkillImportCandidate struct {
	ID              string              `json:"id"`
	Name            string              `json:"name"`
	Description     string              `json:"description"`
	Version         string              `json:"version,omitempty"`
	Path            string              `json:"path"`
	Valid           bool                `json:"valid"`
	Errors          []string            `json:"errors,omitempty"`
	Warnings        []string            `json:"warnings,omitempty"`
	FileCount       int                 `json:"file_count"`
	SizeBytes       int64               `json:"size_bytes"`
	ContentSHA256   string              `json:"content_sha256,omitempty"`
	Bundle          []byte              `json:"-"`
	Manifest        []SkillFileManifest `json:"manifest,omitempty"`
	Frontmatter     map[string]string   `json:"frontmatter,omitempty"`
	SkillMD         string              `json:"skill_md,omitempty"`
	BundleObjectKey string              `json:"bundle_object_key,omitempty"`
}

type archivedFile struct {
	rel  string
	data []byte
}

func sanitizeSkillID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = skillIDCleanRE.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if value == "" {
		return "skill"
	}
	return value
}

func parseSkillFrontmatter(data string) (map[string]string, string, error) {
	text := strings.ReplaceAll(data, "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return nil, "", fmt.Errorf("SKILL.md must start with YAML frontmatter")
	}
	rest := text[len("---\n"):]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return nil, "", fmt.Errorf("SKILL.md frontmatter is not closed")
	}
	raw := rest[:idx]
	body := rest[idx+len("\n---"):]
	body = strings.TrimPrefix(body, "\n")
	fm := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key := strings.TrimSpace(k)
		val := strings.TrimSpace(v)
		val = strings.Trim(val, `"'`)
		if key != "" {
			fm[key] = val
		}
	}
	return fm, body, nil
}

func safeArchivePath(name string) (string, bool) {
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.TrimPrefix(name, "./")
	cleaned := path.Clean(name)
	if cleaned == "." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") || strings.Contains(cleaned, "/../") {
		return "", false
	}
	if strings.HasPrefix(cleaned, "__MACOSX/") || path.Base(cleaned) == ".DS_Store" {
		return "", false
	}
	return cleaned, true
}

func parseSkillArchive(data []byte) ([]SkillImportCandidate, error) {
	if len(data) == 0 || len(data) > maxSkillArchiveBytes {
		return nil, ErrInvalidArg
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, ErrInvalidArg
	}

	files := map[string][]byte{}
	var total int64
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		cleaned, ok := safeArchivePath(f.Name)
		if !ok {
			continue
		}
		if f.UncompressedSize64 > maxSkillUncompressedBytes {
			return nil, ErrInvalidArg
		}
		total += int64(f.UncompressedSize64)
		if total > maxSkillUncompressedBytes {
			return nil, ErrInvalidArg
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(rc, maxSkillUncompressedBytes+1))
		_ = rc.Close()
		if err != nil {
			return nil, err
		}
		files[cleaned] = b
	}

	roots := make([]string, 0)
	for name := range files {
		if path.Base(name) == "SKILL.md" {
			roots = append(roots, path.Dir(name))
		}
	}
	sort.Strings(roots)
	out := make([]SkillImportCandidate, 0, len(roots))
	for _, root := range roots {
		c := buildSkillCandidate(root, files)
		out = append(out, c)
	}
	return out, nil
}

func skillArchiveFromGit(ctx context.Context, repoURL, ref, skillPath string) ([]byte, error) {
	repoURL = strings.TrimSpace(repoURL)
	ref = strings.TrimSpace(ref)
	skillPath = strings.Trim(strings.TrimSpace(skillPath), "/")
	if repoURL == "" {
		return nil, ErrInvalidArg
	}
	if skillPath == "" {
		skillPath = "skills"
	}
	ctx, cancel := context.WithTimeout(ctx, gitCloneTimeout)
	defer cancel()
	tmp, err := os.MkdirTemp("", "cocola-skill-git-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	cloneDir := filepath.Join(tmp, "repo")
	args := []string{"clone", "--depth", "1"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, repoURL, cloneDir)
	cmd := exec.CommandContext(ctx, "git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("%w: git clone failed: %s", ErrInvalidArg, detail)
	}
	root := filepath.Join(cloneDir, filepath.FromSlash(skillPath))
	if _, err := os.Stat(root); err != nil {
		if skillPath == "skills" {
			root = cloneDir
		} else {
			return nil, ErrInvalidArg
		}
	}
	return zipDirectory(root)
}

func zipDirectory(root string) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() && (name == ".git" || name == "node_modules" || name == "__pycache__") {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		if name == ".DS_Store" {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		w, err := zw.Create(rel)
		if err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	})
	if closeErr := zw.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func buildSkillCandidate(root string, all map[string][]byte) SkillImportCandidate {
	root = strings.Trim(root, ".")
	root = strings.Trim(root, "/")
	skillPath := "SKILL.md"
	if root != "" {
		skillPath = root + "/SKILL.md"
	}
	c := SkillImportCandidate{
		Path:  root,
		Valid: true,
	}
	if root == "" {
		c.ID = "skill"
	} else {
		c.ID = sanitizeSkillID(path.Base(root))
	}

	skillMD := string(all[skillPath])
	c.SkillMD = skillMD
	fm, body, err := parseSkillFrontmatter(skillMD)
	if err != nil {
		c.Valid = false
		c.Errors = append(c.Errors, err.Error())
	} else {
		c.Frontmatter = fm
		if name := strings.TrimSpace(fm["name"]); name != "" {
			c.Name = name
			c.ID = sanitizeSkillID(name)
		}
		c.Description = strings.TrimSpace(fm["description"])
		c.Version = strings.TrimSpace(fm["version"])
		if c.Description == "" {
			c.Valid = false
			c.Errors = append(c.Errors, "frontmatter.description is required")
		}
		if strings.TrimSpace(body) == "" {
			c.Valid = false
			c.Errors = append(c.Errors, "SKILL.md body is required")
		}
	}
	if c.Name == "" {
		c.Name = c.ID
	}

	files := make([]archivedFile, 0)
	prefix := ""
	if root != "" {
		prefix = root + "/"
	}
	for name, data := range all {
		if prefix != "" {
			if !strings.HasPrefix(name, prefix) {
				continue
			}
			name = strings.TrimPrefix(name, prefix)
		}
		if name == "" || strings.HasPrefix(name, "../") {
			continue
		}
		files = append(files, archivedFile{rel: name, data: data})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })
	manifest := make([]SkillFileManifest, 0, len(files))
	var size int64
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	for _, f := range files {
		size += int64(len(f.data))
		sum := sha256.Sum256(f.data)
		manifest = append(manifest, SkillFileManifest{
			Path:   f.rel,
			Size:   int64(len(f.data)),
			SHA256: hex.EncodeToString(sum[:]),
		})
		h := &zip.FileHeader{Name: f.rel, Method: zip.Deflate}
		w, err := zw.CreateHeader(h)
		if err == nil {
			_, err = w.Write(f.data)
		}
		if err != nil {
			c.Valid = false
			c.Errors = append(c.Errors, "failed to normalize archive")
			break
		}
	}
	if err := zw.Close(); err != nil {
		c.Valid = false
		c.Errors = append(c.Errors, "failed to finalize normalized archive")
	}
	sum := sha256.Sum256(buf.Bytes())
	c.Bundle = buf.Bytes()
	c.FileCount = len(files)
	c.SizeBytes = size
	c.Manifest = manifest
	c.ContentSHA256 = hex.EncodeToString(sum[:])
	if c.FileCount == 0 {
		c.Valid = false
		c.Errors = append(c.Errors, "skill contains no files")
	}
	return c
}

func skillManifestJSON(c SkillImportCandidate) json.RawMessage {
	b, _ := json.Marshal(c.Manifest)
	return b
}

func skillFrontmatterJSON(c SkillImportCandidate) json.RawMessage {
	b, _ := json.Marshal(c.Frontmatter)
	if b == nil {
		return []byte("{}")
	}
	return b
}
