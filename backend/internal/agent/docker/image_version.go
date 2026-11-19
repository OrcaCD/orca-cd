package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"runtime"
	"strings"

	"github.com/distribution/reference"
	"github.com/docker/cli/cli/command"
	"github.com/moby/moby/client"
)

// versionLabel is the OCI annotation images use for their human readable version.
const versionLabel = "org.opencontainers.image.version"

const manifestMediaTypes = "application/vnd.oci.image.index.v1+json, " +
	"application/vnd.docker.distribution.manifest.list.v2+json, " +
	"application/vnd.oci.image.manifest.v1+json, " +
	"application/vnd.docker.distribution.manifest.v2+json"

// maxVersionCacheSize bounds the cached versions, which are looked up by digest.
const maxVersionCacheSize = 512

// getImageVersion reads the version label of digest from the registry of
// imageRef. It is a package-level var so tests can override it.
var getImageVersion = func(ctx context.Context, dockerCli command.Cli, imageRef, digest string) (string, error) {
	named, err := reference.ParseNormalizedNamed(imageRef)
	if err != nil {
		return "", err
	}
	host := reference.Domain(named)
	if host == "docker.io" {
		host = "registry-1.docker.io"
	}
	registry := &registryClient{
		base:  "https://" + host + "/v2/" + reference.Path(named),
		repo:  reference.Path(named),
		creds: readCredentials(dockerCli, imageRef),
	}
	return registry.version(ctx, digest)
}

// getLocalVersion reads the version label of a local image.
// It is a package-level var so tests can override it.
var getLocalVersion = func(ctx context.Context, cli client.APIClient, imageRef string) string {
	result, err := cli.ImageInspect(ctx, imageRef)
	if err != nil || result.Config == nil {
		return ""
	}
	return result.Config.Labels[versionLabel]
}

// imageVersion returns the version label of digest, or empty if it has none
// or the registry can't be read. Versions are cached, a digest never changes.
func (c *Client) imageVersion(ctx context.Context, imageRef, digest string) string {
	c.releaseAgeMu.Lock()
	version, ok := c.versions[digest]
	c.releaseAgeMu.Unlock()
	if ok {
		return version
	}

	version, err := getImageVersion(ctx, c.cli, imageRef, digest)
	if err != nil {
		c.log.Debug().Err(err).Str("image", imageRef).Msg("could not read the version of the new image")
		return ""
	}

	c.releaseAgeMu.Lock()
	defer c.releaseAgeMu.Unlock()
	if c.versions == nil || len(c.versions) >= maxVersionCacheSize {
		c.versions = make(map[string]string)
	}
	c.versions[digest] = version
	return version
}

// registryClient reads manifests and blobs with the OCI distribution API.
type registryClient struct {
	base  string
	repo  string
	creds registryCredentials
	auth  string
}

type ociManifest struct {
	Manifests []struct {
		Digest   string `json:"digest"`
		Platform *struct {
			OS           string `json:"os"`
			Architecture string `json:"architecture"`
		} `json:"platform"`
	} `json:"manifests"`
	Config struct {
		Digest string `json:"digest"`
	} `json:"config"`
}

func (r *registryClient) version(ctx context.Context, digest string) (string, error) {
	var manifest ociManifest
	if err := r.get(ctx, "/manifests/"+digest, manifestMediaTypes, &manifest); err != nil {
		return "", err
	}
	// A multi-platform image is an index of manifests, the version label is
	// read from the one the agent's platform runs.
	if len(manifest.Manifests) > 0 {
		platformDigest := platformManifest(manifest)
		manifest = ociManifest{}
		if err := r.get(ctx, "/manifests/"+platformDigest, manifestMediaTypes, &manifest); err != nil {
			return "", err
		}
	}
	if manifest.Config.Digest == "" {
		return "", errors.New("manifest has no config")
	}

	var config struct {
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"config"`
	}
	if err := r.get(ctx, "/blobs/"+manifest.Config.Digest, "", &config); err != nil {
		return "", err
	}
	return config.Config.Labels[versionLabel], nil
}

// platformManifest picks the manifest of the agent's platform, or the first
// one that is not an attestation.
func platformManifest(index ociManifest) string {
	fallback := ""
	for _, m := range index.Manifests {
		if m.Platform == nil || m.Platform.OS == "unknown" {
			continue
		}
		if m.Platform.OS == "linux" && m.Platform.Architecture == runtime.GOARCH {
			return m.Digest
		}
		if fallback == "" {
			fallback = m.Digest
		}
	}
	if fallback == "" {
		return index.Manifests[0].Digest
	}
	return fallback
}

func (r *registryClient) get(ctx context.Context, path, accept string, out any) error {
	resp, err := r.do(ctx, path, accept)
	if err != nil {
		return err
	}
	// The registry answers with a challenge that says how to authenticate.
	if resp.StatusCode == http.StatusUnauthorized && r.auth == "" {
		challenge := resp.Header.Get("WWW-Authenticate")
		_ = resp.Body.Close()
		if r.auth, err = r.authenticate(ctx, challenge); err != nil {
			return fmt.Errorf("authenticate: %w", err)
		}
		if resp, err = r.do(ctx, path, accept); err != nil {
			return err
		}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return &statusError{code: resp.StatusCode}
	}
	return json.NewDecoder(io.LimitReader(resp.Body, maxRegistryResponseSize)).Decode(out)
}

func (r *registryClient) do(ctx context.Context, path, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.base+path, nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if r.auth != "" {
		req.Header.Set("Authorization", r.auth)
	}
	return registryHTTPClient.Do(req)
}

var challengeParam = regexp.MustCompile(`(\w+)="([^"]*)"`)

// authenticate returns the Authorization header for a WWW-Authenticate
// challenge: basic auth, or a bearer token from the registry's token service.
func (r *registryClient) authenticate(ctx context.Context, challenge string) (string, error) {
	scheme, _, _ := strings.Cut(challenge, " ")
	if strings.EqualFold(scheme, "Basic") {
		if r.creds.password == "" {
			return "", errors.New("registry needs credentials")
		}
		return "Basic " + basicAuth(r.creds), nil
	}
	if !strings.EqualFold(scheme, "Bearer") {
		return "", fmt.Errorf("unsupported challenge %q", challenge)
	}

	params := map[string]string{}
	for _, match := range challengeParam.FindAllStringSubmatch(challenge, -1) {
		params[strings.ToLower(match[1])] = match[2]
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, params["realm"], nil)
	if err != nil {
		return "", err
	}
	query := req.URL.Query()
	if params["service"] != "" {
		query.Set("service", params["service"])
	}
	query.Set("scope", "repository:"+r.repo+":pull")
	req.URL.RawQuery = query.Encode()
	if r.creds.password != "" {
		req.SetBasicAuth(r.creds.username, r.creds.password)
	}

	var token struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := doJSON(req, &token); err != nil {
		return "", err
	}
	if token.Token != "" {
		return "Bearer " + token.Token, nil
	}
	if token.AccessToken != "" {
		return "Bearer " + token.AccessToken, nil
	}
	return "", errors.New("token service returned no token")
}
