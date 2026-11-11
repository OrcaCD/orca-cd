package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/distribution/reference"
	"github.com/docker/cli/cli/command"
	"github.com/moby/moby/api/pkg/authconfig"
)

// The OCI distribution API has no push time, so it is read from the vendor
// APIs of the registries that expose one. The created field of the image config
// can't be used: the publisher controls it and can backdate it.

// API base URLs, variables so tests can point them at a test server.
var (
	dockerHubAPI = "https://hub.docker.com"
	gitHubAPI    = "https://api.github.com"
	gitLabAPI    = "https://gitlab.com"
)

var registryHTTPClient = &http.Client{Timeout: 30 * time.Second}

const maxRegistryResponseSize = 4 << 20

// ghcrMaxPages limits how many pages of package versions are searched. A new
// digest is among the latest versions, which come first.
const ghcrMaxPages = 3

type registryImage struct {
	host   string
	path   string
	tag    string
	digest string
}

type registryCredentials struct {
	username string
	password string
}

type statusError struct {
	code int
}

func (e *statusError) Error() string {
	return fmt.Sprintf("unexpected status %d %s", e.code, http.StatusText(e.code))
}

func isNotFound(err error) bool {
	var statusErr *statusError
	return errors.As(err, &statusErr) && statusErr.code == http.StatusNotFound
}

// getPushTime returns when the digest of imageRef was pushed to its registry.
// It is a package-level var so tests can override it.
var getPushTime = func(ctx context.Context, dockerCli command.Cli, imageRef, digest string) (time.Time, error) {
	named, err := reference.ParseNormalizedNamed(imageRef)
	if err != nil {
		return time.Time{}, err
	}
	img := registryImage{
		host:   reference.Domain(named),
		path:   reference.Path(named),
		tag:    "latest",
		digest: digest,
	}
	if tagged, ok := named.(reference.Tagged); ok {
		img.tag = tagged.Tag()
	}
	return lookupPushTime(ctx, img, readCredentials(dockerCli, imageRef))
}

func readCredentials(dockerCli command.Cli, imageRef string) registryCredentials {
	encoded, err := command.RetrieveAuthTokenFromImage(dockerCli.ConfigFile(), imageRef)
	if err != nil {
		return registryCredentials{}
	}
	auth, err := authconfig.Decode(encoded)
	if err != nil {
		return registryCredentials{}
	}
	return registryCredentials{username: auth.Username, password: auth.Password}
}

func lookupPushTime(ctx context.Context, img registryImage, creds registryCredentials) (time.Time, error) {
	switch img.host {
	case "docker.io":
		return dockerHubPushTime(ctx, img, creds)
	case "ghcr.io":
		return ghcrPushTime(ctx, img, creds)
	case "registry.gitlab.com":
		return gitLabPushTime(ctx, img, creds)
	default:
		// Other registries are only supported when they run Harbor.
		pushed, err := harborPushTime(ctx, img, creds)
		if err != nil {
			return time.Time{}, fmt.Errorf("%s exposes no push time, only Docker Hub, GHCR, GitLab and Harbor do: %w", img.host, err)
		}
		return pushed, nil
	}
}

func dockerHubPushTime(ctx context.Context, img registryImage, creds registryCredentials) (time.Time, error) {
	header := http.Header{}
	if creds.password != "" {
		token, err := dockerHubToken(ctx, creds)
		if err != nil {
			return time.Time{}, fmt.Errorf("docker hub login: %w", err)
		}
		header.Set("Authorization", "Bearer "+token)
	}

	namespace, repository, _ := strings.Cut(img.path, "/")
	var tag struct {
		Digest        string     `json:"digest"`
		TagLastPushed *time.Time `json:"tag_last_pushed"`
		Images        []struct {
			Digest string `json:"digest"`
		} `json:"images"`
	}
	endpoint := fmt.Sprintf("%s/v2/namespaces/%s/repositories/%s/tags/%s",
		dockerHubAPI, url.PathEscape(namespace), url.PathEscape(repository), url.PathEscape(img.tag))
	if err := getJSON(ctx, endpoint, header, &tag); err != nil {
		return time.Time{}, fmt.Errorf("docker hub: %w", err)
	}

	// The push time belongs to the tag, so it only applies to the digest the
	// tag points to now.
	matches := tag.Digest == img.digest
	for _, image := range tag.Images {
		matches = matches || image.Digest == img.digest
	}
	if !matches {
		return time.Time{}, errors.New("docker hub reports another digest for the tag, it was probably pushed again")
	}
	if tag.TagLastPushed == nil {
		return time.Time{}, errors.New("docker hub reports no push time for the tag")
	}
	return *tag.TagLastPushed, nil
}

func dockerHubToken(ctx context.Context, creds registryCredentials) (string, error) {
	body, err := json.Marshal(map[string]string{"identifier": creds.username, "secret": creds.password})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dockerHubAPI+"/v2/auth/token", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := doJSON(req, &token); err != nil {
		return "", err
	}
	return token.AccessToken, nil
}

// ghcrPushTime reads the push time from the GitHub Packages API, which always
// needs a token, even for public packages.
func ghcrPushTime(ctx context.Context, img registryImage, creds registryCredentials) (time.Time, error) {
	if creds.password == "" {
		return time.Time{}, errors.New("ghcr: reading the push time needs credentials for ghcr.io with the read:packages scope in the agent's Docker config")
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+creds.password)
	header.Set("Accept", "application/vnd.github+json")
	header.Set("X-GitHub-Api-Version", "2022-11-28")

	owner, pkg, _ := strings.Cut(img.path, "/")
	// The owner is either an organization or a user.
	pushed, err := ghcrVersionPushTime(ctx, "orgs", owner, pkg, img.digest, header)
	if isNotFound(err) {
		pushed, err = ghcrVersionPushTime(ctx, "users", owner, pkg, img.digest, header)
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("ghcr: %w", err)
	}
	return pushed, nil
}

func ghcrVersionPushTime(ctx context.Context, ownerType, owner, pkg, digest string, header http.Header) (time.Time, error) {
	for page := 1; page <= ghcrMaxPages; page++ {
		var versions []struct {
			Name      string    `json:"name"`
			CreatedAt time.Time `json:"created_at"`
		}
		endpoint := fmt.Sprintf("%s/%s/%s/packages/container/%s/versions?per_page=100&page=%d",
			gitHubAPI, ownerType, url.PathEscape(owner), url.PathEscape(pkg), page)
		if err := getJSON(ctx, endpoint, header, &versions); err != nil {
			return time.Time{}, err
		}
		for _, version := range versions {
			if version.Name == digest {
				return version.CreatedAt, nil
			}
		}
		if len(versions) < 100 {
			break
		}
	}
	return time.Time{}, fmt.Errorf("no package version found for digest %s", digest)
}

// gitLabPushTime reads the push time from the GitLab API. Only gitlab.com is
// supported, the API of a self-hosted GitLab can't be derived from its registry.
func gitLabPushTime(ctx context.Context, img registryImage, creds registryCredentials) (time.Time, error) {
	header := http.Header{}
	if creds.password != "" {
		header.Set("PRIVATE-TOKEN", creds.password)
	}

	// The image path is the project path, optionally followed by an image name,
	// so the longest path that is a project is searched for.
	segments := strings.Split(img.path, "/")
	for i := len(segments); i >= 2; i-- {
		project := url.PathEscape(strings.Join(segments[:i], "/"))
		var repositories []struct {
			ID   int64  `json:"id"`
			Path string `json:"path"`
		}
		err := getJSON(ctx, fmt.Sprintf("%s/api/v4/projects/%s/registry/repositories?per_page=100", gitLabAPI, project), header, &repositories)
		if isNotFound(err) {
			continue
		}
		if err != nil {
			return time.Time{}, fmt.Errorf("gitlab: %w", err)
		}
		var repositoryID int64
		for _, repository := range repositories {
			if repository.Path == img.path {
				repositoryID = repository.ID
			}
		}
		if repositoryID == 0 {
			break
		}

		var tag struct {
			Digest    string     `json:"digest"`
			CreatedAt *time.Time `json:"created_at"`
		}
		endpoint := fmt.Sprintf("%s/api/v4/projects/%s/registry/repositories/%d/tags/%s",
			gitLabAPI, project, repositoryID, url.PathEscape(img.tag))
		if err := getJSON(ctx, endpoint, header, &tag); err != nil {
			return time.Time{}, fmt.Errorf("gitlab: %w", err)
		}
		if tag.Digest != img.digest {
			return time.Time{}, errors.New("gitlab reports another digest for the tag, it was probably pushed again")
		}
		if tag.CreatedAt == nil {
			return time.Time{}, errors.New("gitlab reports no push time for the tag")
		}
		return *tag.CreatedAt, nil
	}
	return time.Time{}, fmt.Errorf("gitlab: no registry repository found for %s", img.path)
}

func harborPushTime(ctx context.Context, img registryImage, creds registryCredentials) (time.Time, error) {
	project, repository, ok := strings.Cut(img.path, "/")
	if !ok {
		return time.Time{}, errors.New("image is not in a Harbor project")
	}
	header := http.Header{}
	if creds.password != "" {
		header.Set("Authorization", "Basic "+basicAuth(creds))
	}
	// Harbor expects slashes in the repository name to be encoded twice.
	endpoint := fmt.Sprintf("https://%s/api/v2.0/projects/%s/repositories/%s/artifacts/%s",
		img.host, url.PathEscape(project), url.PathEscape(url.PathEscape(repository)), url.PathEscape(img.digest))
	var artifact struct {
		PushTime *time.Time `json:"push_time"`
	}
	if err := getJSON(ctx, endpoint, header, &artifact); err != nil {
		return time.Time{}, err
	}
	if artifact.PushTime == nil {
		return time.Time{}, errors.New("no push time in the response")
	}
	return *artifact.PushTime, nil
}

func basicAuth(creds registryCredentials) string {
	req := http.Request{Header: http.Header{}}
	req.SetBasicAuth(creds.username, creds.password)
	return strings.TrimPrefix(req.Header.Get("Authorization"), "Basic ")
}

func getJSON(ctx context.Context, endpoint string, header http.Header, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	maps.Copy(req.Header, header)
	return doJSON(req, out)
}

func doJSON(req *http.Request, out any) error {
	resp, err := registryHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return &statusError{code: resp.StatusCode}
	}
	return json.NewDecoder(io.LimitReader(resp.Body, maxRegistryResponseSize)).Decode(out)
}
