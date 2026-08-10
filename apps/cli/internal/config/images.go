package config

import (
	"errors"
	"strings"
)

const (
	DefaultRegistry           = "ghcr.io/sakurs2"
	openSandboxAliyunRegistry = "sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox"
	forgejoVersion            = "16.0.1"
	forgejoManifestDigest     = "sha256:3eb3107bc9de4e9d6d9e539044e6c802dc0b7be351919a145540d4cb5422bf07"
	openVikingDigest          = "sha256:0d99361a0029ce5221fd11588d9f0f374c6e5f8f1eacbcf1d76de6a0f6cd82cb"
)

type ImageReferences struct {
	Registry          string
	Redis             string
	Postgres          string
	Forgejo           string
	MinIO             string
	MinIOClient       string
	OpenViking        string
	OpenSandboxServer string
	OpenSandboxExecd  string
	OpenSandboxEgress string
	SandboxRuntime    string
}

func ResolveImageReferences(version, customRegistry string) (ImageReferences, error) {
	if !validImagePart(version) {
		return ImageReferences{}, errors.New("version contains characters that are invalid in an image tag")
	}
	registry := customRegistry
	if registry == "" {
		registry = DefaultRegistry
	}
	if err := validateImageRegistry(registry); err != nil {
		return ImageReferences{}, err
	}
	refs := ImageReferences{
		Registry:         registry,
		OpenSandboxExecd: openSandboxAliyunRegistry + "/execd:v1.0.19",
		SandboxRuntime:   registry + "/cocola-sandbox-runtime:" + version,
	}
	refs.Redis = "docker.io/library/redis:7.4.10-alpine3.21"
	refs.Postgres = "docker.io/library/postgres:16.14-alpine3.23"
	refs.Forgejo = "ghcr.io/sakurs2/cocola-forgejo:" + forgejoVersion + "@" + forgejoManifestDigest
	refs.MinIO = "docker.io/minio/minio:RELEASE.2025-09-07T16-13-09Z"
	refs.MinIOClient = "docker.io/minio/mc:RELEASE.2025-08-13T08-35-41Z"
	refs.OpenViking = "ghcr.io/volcengine/openviking:v0.4.12@" + openVikingDigest
	refs.OpenSandboxServer = "docker.io/opensandbox/server:v0.1.14"
	refs.OpenSandboxEgress = "docker.io/opensandbox/egress:v1.1.2"
	return refs, nil
}

func validateImageRegistry(registry string) error {
	if strings.TrimSpace(registry) == "" || strings.ContainsAny(registry, " \t\r\n") ||
		strings.Contains(registry, "://") || strings.HasPrefix(registry, "/") ||
		strings.HasSuffix(registry, "/") || strings.Contains(registry, "//") {
		return errors.New("registry must be a non-empty host/path without whitespace")
	}
	for _, char := range registry {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '.' || char == '_' ||
			char == '-' || char == '/' || char == ':' {
			continue
		}
		return errors.New("registry contains characters that are invalid in a container image reference")
	}
	return nil
}

func (refs ImageReferences) ManagedRuntimeImages() []string {
	return []string{refs.SandboxRuntime, refs.OpenSandboxExecd, refs.OpenSandboxEgress}
}
