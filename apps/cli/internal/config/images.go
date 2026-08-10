package config

import (
	"errors"
	"fmt"
	"strings"
)

type ImageSource string

const (
	ImageSourceCNMirror ImageSource = "cn-mirror"
	ImageSourceDirect   ImageSource = "direct"

	DefaultImageSource = ImageSourceCNMirror
	LegacyImageSource  = ImageSourceDirect

	DefaultRegistry       = "ghcr.io/sakurs2"
	CNMirrorRegistry      = "ghcr.nju.edu.cn/sakurs2"
	forgejoVersion        = "16.0.1"
	forgejoManifestDigest = "sha256:3eb3107bc9de4e9d6d9e539044e6c802dc0b7be351919a145540d4cb5422bf07"
	openVikingDigest      = "sha256:0d99361a0029ce5221fd11588d9f0f374c6e5f8f1eacbcf1d76de6a0f6cd82cb"
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

func ParseImageSource(value string) (ImageSource, error) {
	source := ImageSource(strings.TrimSpace(value))
	if !source.Valid() {
		return "", fmt.Errorf("image source must be %q or %q", ImageSourceCNMirror, ImageSourceDirect)
	}
	return source, nil
}

func (source ImageSource) Valid() bool {
	return source == ImageSourceCNMirror || source == ImageSourceDirect
}

func (source ImageSource) DisplayName() string {
	switch source {
	case ImageSourceCNMirror:
		return "Mainland China acceleration"
	case ImageSourceDirect:
		return "Direct download"
	default:
		return "Unknown"
	}
}

func (source ImageSource) Registry() string {
	if source == ImageSourceCNMirror {
		return CNMirrorRegistry
	}
	return DefaultRegistry
}

func ResolveImageReferences(source ImageSource, version, customRegistry string) (ImageReferences, error) {
	if !source.Valid() {
		return ImageReferences{}, errors.New("invalid image source")
	}
	if !validImagePart(version) {
		return ImageReferences{}, errors.New("version contains characters that are invalid in an image tag")
	}
	registry := customRegistry
	if registry == "" {
		registry = source.Registry()
	}
	if err := validateImageRegistry(registry); err != nil {
		return ImageReferences{}, err
	}
	refs := ImageReferences{
		Registry:         registry,
		OpenSandboxExecd: "sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/execd:v1.0.19",
		SandboxRuntime:   registry + "/cocola-sandbox-runtime:" + version,
	}
	if source == ImageSourceCNMirror {
		refs.Redis = "docker.nju.edu.cn/library/redis:7.4.10-alpine3.21"
		refs.Postgres = "docker.nju.edu.cn/library/postgres:16.14-alpine3.23"
		refs.Forgejo = "ghcr.nju.edu.cn/sakurs2/cocola-forgejo:" + forgejoVersion + "@" + forgejoManifestDigest
		refs.MinIO = "docker.nju.edu.cn/minio/minio:RELEASE.2025-09-07T16-13-09Z"
		refs.MinIOClient = "docker.nju.edu.cn/minio/mc:RELEASE.2025-08-13T08-35-41Z"
		refs.OpenViking = "ghcr.nju.edu.cn/volcengine/openviking:v0.4.12@" + openVikingDigest
		refs.OpenSandboxServer = "docker.nju.edu.cn/opensandbox/server:v0.1.14"
		refs.OpenSandboxEgress = "docker.nju.edu.cn/opensandbox/egress:v1.1.2"
		return refs, nil
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
