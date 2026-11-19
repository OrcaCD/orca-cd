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

// gitLabPushTime reads the push time from the GitLab GraphQL API. The REST API
// only has the time the tag was first created, which stays the same when a new
// image is pushed to it. Only gitlab.com is supported, the API of a self-hosted
// GitLab can't be derived from its registry.
func gitLabPushTime(ctx context.Context, img registryImage, creds registryCredentials) (time.Time, error) {
	header := http.Header{}
	if creds.password != "" {
		header.Set("Authorization", "Bearer "+creds.password)
	}

	// The image path is the project path, optionally followed by an image name,
	// so the longest path that is a project is searched for.
	segments := strings.Split(img.path, "/")
	for i := len(segments); i >= 2; i-- {
		repositoryID, found, err := gitLabRepositoryID(ctx, strings.Join(segments[:i], "/"), strings.Join(segments[i:], "/"), img.path, header)
		if err != nil {
			return time.Time{}, fmt.Errorf("gitlab: %w", err)
		}
		if !found {
			continue
		}
		if repositoryID == "" {
			break
		}

		var data struct {
			ContainerRepository *struct {
				TagDetails *struct {
					Digest      string     `json:"digest"`
					PublishedAt *time.Time `json:"publishedAt"`
				} `json:"tagDetails"`
			} `json:"containerRepository"`
		}
		err = gitLabGraphQL(ctx, `query($id: ContainerRepositoryID!, $tag: String!) {
			containerRepository(id: $id) { tagDetails(name: $tag) { digest publishedAt } }
		}`, map[string]any{"id": repositoryID, "tag": img.tag}, header, &data)
		if err != nil {
			return time.Time{}, fmt.Errorf("gitlab: %w", err)
		}
		if data.ContainerRepository == nil || data.ContainerRepository.TagDetails == nil {
			return time.Time{}, fmt.Errorf("gitlab: no tag %s in %s", img.tag, img.path)
		}
		tag := data.ContainerRepository.TagDetails
		if tag.Digest != img.digest {
			return time.Time{}, errors.New("gitlab reports another digest for the tag, it was probably pushed again")
		}
		if tag.PublishedAt == nil {
			return time.Time{}, errors.New("gitlab reports no push time for the tag")
		}
		return *tag.PublishedAt, nil
	}
	return time.Time{}, fmt.Errorf("gitlab: no registry repository found for %s", img.path)
}

// gitLabRepositoryID returns the ID of the container repository with the given
// path in a project. found is false if there is no such project.
func gitLabRepositoryID(ctx context.Context, project, name, path string, header http.Header) (id string, found bool, err error) {
	var data struct {
		Project *struct {
			ContainerRepositories struct {
				Nodes []struct {
					ID   string `json:"id"`
					Path string `json:"path"`
				} `json:"nodes"`
			} `json:"containerRepositories"`
		} `json:"project"`
	}
	variables := map[string]any{"project": project}
	if name != "" {
		variables["name"] = name
	}
	err = gitLabGraphQL(ctx, `query($project: ID!, $name: String) {
		project(fullPath: $project) { containerRepositories(name: $name, first: 100) { nodes { id path } } }
	}`, variables, header, &data)
	if err != nil || data.Project == nil {
		return "", false, err
	}
	for _, repository := range data.Project.ContainerRepositories.Nodes {
		if repository.Path == path {
			return repository.ID, true, nil
		}
	}
	return "", true, nil
}

func gitLabGraphQL(ctx context.Context, query string, variables map[string]any, header http.Header, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gitLabAPI+"/api/graphql", bytes.NewReader(body))
	if err != nil {
		return err
	}
	maps.Copy(req.Header, header)
	req.Header.Set("Content-Type", "application/json")
	var response struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := doJSON(req, &response); err != nil {
		return err
	}
	if len(response.Errors) > 0 {
		return errors.New(response.Errors[0].Message)
	}
	return json.Unmarshal(response.Data, out)
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
	endpoint := fmt.Sprintf("https://%s/api/v2.0/projects/%s/repositories/%s/artifacts/%s?with_tag=true",
		img.host, url.PathEscape(project), url.PathEscape(url.PathEscape(repository)), url.PathEscape(img.digest))
	var artifact struct {
		Tags []struct {
			Name     string     `json:"name"`
			PushTime *time.Time `json:"push_time"`
		} `json:"tags"`
	}
	if err := getJSON(ctx, endpoint, header, &artifact); err != nil {
		return time.Time{}, err
	}
	// The push time of the artifact is when its digest was first pushed, maybe
	// under another tag long before the tag was moved to it, so the time the tag
	// was pushed is used.
	for _, tag := range artifact.Tags {
		if tag.Name != img.tag {
			continue
		}
		if tag.PushTime == nil {
			return time.Time{}, errors.New("no push time in the response")
		}
		return *tag.PushTime, nil
	}
	return time.Time{}, fmt.Errorf("the tag %s no longer points to the digest", img.tag)
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
