package repositories

import (
	"context"
	"fmt"
	"regexp"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
)

type Provider interface {
	// Validates and parses the repository URL, returning the owner and repo name if valid.
	ParseURL(url string) (string, string, error)
	// Returns the list of supported authentication methods for this provider.
	SupportedAuthMethods() []models.RepositoryAuthMethod
	// Tests the connection to the repository using the provided credentials.
	TestConnection(ctx context.Context, repo *models.Repository) error
	// ListBranches(ctx context.Context, repo *models.Repository) ([]string, error)
	// ...
}

var registry = map[models.RepositoryProvider]Provider{}

// ownerRe matches usernames/org names: 1–39 alphanumeric chars or hyphens,
// not starting or ending with a hyphen
var ownerRe = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,37}[a-zA-Z0-9])?$`)

// repoRe matches repository names: 1–100 chars, alphanumeric, hyphens,
// underscores, or dots
var repoRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,100}$`)

func Register(t models.RepositoryProvider, p Provider) {
	registry[t] = p
}

func Get(t models.RepositoryProvider) (Provider, error) {
	p, ok := registry[t]
	if !ok {
		return nil, fmt.Errorf("no provider registered for repository type %q", t)
	}
	return p, nil
}
