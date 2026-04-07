package repository

import (
	"context"
	"fmt"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
)

type Provider interface {
	ValidateURL(url string) error
	SupportedAuthMethods() []models.RepositoryAuthMethod
	TestConnection(ctx context.Context, repo *models.Repository) error
}

var registry = map[models.RepositoryProvider]Provider{}

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
