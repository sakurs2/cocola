package config

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

const (
	DefaultGHCREndpoint       = "ghcr.io"
	DefaultRegistry           = DefaultGHCREndpoint + "/sakurs2"
	openSandboxAliyunRegistry = "sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox"
)

type ImageResolutionOptions struct {
	Version        string
	CocolaRegistry string
	GHCREndpoint   string
}

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

func ResolveImageReferences(options ImageResolutionOptions) (ImageReferences, error) {
	if !validImagePart(options.Version) {
		return ImageReferences{}, errors.New("version contains characters that are invalid in an image tag")
	}
	endpoint, err := NormalizeGHCREndpoint(options.GHCREndpoint)
	if err != nil {
		return ImageReferences{}, err
	}
	registry := strings.TrimSuffix(strings.TrimSpace(options.CocolaRegistry), "/")
	if registry == "" {
		registry = defaultCocolaRegistry(endpoint)
	}
	if err := validateImageRegistry(registry); err != nil {
		return ImageReferences{}, err
	}
	refs := ImageReferences{
		Registry:          registry,
		Redis:             pinnedImage(rewriteGHCRImageHost(endpoint, redisTargetImage), redisVersion, redisManifestDigest),
		Postgres:          pinnedImage(rewriteGHCRImageHost(endpoint, postgresTargetImage), postgresVersion, postgresManifestDigest),
		Forgejo:           pinnedImage(rewriteGHCRImageHost(endpoint, forgejoTargetImage), forgejoVersion, forgejoManifestDigest),
		MinIO:             pinnedImage(rewriteGHCRImageHost(endpoint, minioTargetImage), minioVersion, minioManifestDigest),
		MinIOClient:       pinnedImage(rewriteGHCRImageHost(endpoint, minioClientTargetImage), minioClientVersion, minioClientManifestDigest),
		OpenViking:        pinnedImage(rewriteGHCRImageHost(endpoint, openVikingTargetImage), openVikingVersion, openVikingDigest),
		OpenSandboxServer: pinnedImage(rewriteGHCRImageHost(endpoint, openSandboxServerTargetImage), openSandboxServerVersion, openSandboxServerDigest),
		OpenSandboxExecd:  openSandboxAliyunRegistry + "/execd:v1.0.19",
		OpenSandboxEgress: pinnedImage(rewriteGHCRImageHost(endpoint, openSandboxEgressTargetImage), openSandboxEgressVersion, openSandboxEgressDigest),
		SandboxRuntime:    registry + "/cocola-sandbox-runtime:" + options.Version,
	}
	return refs, nil
}

func NormalizeGHCREndpoint(value string) (string, error) {
	endpoint := strings.ToLower(strings.TrimSpace(value))
	if endpoint == "" {
		endpoint = DefaultGHCREndpoint
	}
	if strings.ContainsAny(endpoint, " /\\@?#\t\r\n") || strings.Contains(endpoint, "://") {
		return "", errors.New("GHCR endpoint must be a registry hostname with an optional port, without a scheme or path")
	}
	host := endpoint
	port := ""
	if strings.Count(endpoint, ":") > 1 {
		return "", errors.New("GHCR endpoint does not support raw IPv6 addresses")
	}
	if strings.Contains(endpoint, ":") {
		var err error
		host, port, err = net.SplitHostPort(endpoint)
		if err != nil {
			return "", errors.New("GHCR endpoint port must be a number between 1 and 65535")
		}
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 1 || portNumber > 65535 {
			return "", errors.New("GHCR endpoint port must be a number between 1 and 65535")
		}
		port = strconv.Itoa(portNumber)
	}
	if host == "" || len(host) > 253 || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return "", errors.New("GHCR endpoint must contain a valid registry hostname")
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("GHCR endpoint must contain a valid registry hostname")
		}
		for _, char := range label {
			if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' {
				continue
			}
			return "", fmt.Errorf("GHCR endpoint contains invalid hostname character %q", char)
		}
	}
	if port != "" {
		return net.JoinHostPort(host, port), nil
	}
	return host, nil
}

func defaultCocolaRegistry(endpoint string) string {
	return endpoint + "/sakurs2"
}

func rewriteGHCRImageHost(endpoint, canonicalImage string) string {
	return endpoint + "/" + strings.TrimPrefix(canonicalImage, DefaultGHCREndpoint+"/")
}

func pinnedImage(repository, version, digest string) string {
	return repository + ":" + version + "@" + digest
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
