package repositories

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
)

type stubProvider struct {
	parseURLErr      error
	authMethods      []models.RepositoryAuthMethod
	testConnectionFn func(ctx context.Context, repo *models.Repository) error
	listBranchesFn   func(ctx context.Context, repo *models.Repository) ([]string, error)
	listTreeFn       func(ctx context.Context, repo *models.Repository, branch string) ([]TreeEntry, error)
}

func (s *stubProvider) ParseURL(url string) (string, string, error) {
	return "", "", s.parseURLErr
}

func (s *stubProvider) SupportedAuthMethods() []models.RepositoryAuthMethod {
	return s.authMethods
}

func (s *stubProvider) TestConnection(ctx context.Context, repo *models.Repository) error {
	if s.testConnectionFn != nil {
		return s.testConnectionFn(ctx, repo)
	}
	return nil
}

func (s *stubProvider) ListBranches(ctx context.Context, repo *models.Repository) ([]string, error) {
	if s.listBranchesFn != nil {
		return s.listBranchesFn(ctx, repo)
	}
	return []string{}, nil
}

func (s *stubProvider) ListTree(ctx context.Context, repo *models.Repository, branch string) ([]TreeEntry, error) {
	if s.listTreeFn != nil {
		return s.listTreeFn(ctx, repo, branch)
	}
	return []TreeEntry{}, nil
}

func withIsolatedRegistry(t *testing.T, fn func()) {
	t.Helper()
	original := registry
	registry = map[models.RepositoryProvider]Provider{}
	t.Cleanup(func() { registry = original })
	fn()
}

func TestRegisterAndGet(t *testing.T) {
	withIsolatedRegistry(t, func() {
		stub := &stubProvider{}
		Register(models.GitHub, stub)

		p, err := Get(models.GitHub)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p != stub {
			t.Error("Get returned a different provider than the one registered")
		}
	})
}

func TestGet_UnknownProvider(t *testing.T) {
	withIsolatedRegistry(t, func() {
		_, err := Get(models.GitHub)
		if err == nil {
			t.Fatal("expected error for unregistered provider, got nil")
		}
	})
}

func TestGet_ErrorMessage(t *testing.T) {
	withIsolatedRegistry(t, func() {
		_, err := Get("unknown-provider")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		expected := `no provider registered for repository type "unknown-provider"`
		if err.Error() != expected {
			t.Errorf("expected error %q, got %q", expected, err.Error())
		}
	})
}

func TestRegister_Overwrite(t *testing.T) {
	withIsolatedRegistry(t, func() {
		first := &stubProvider{parseURLErr: errors.New("first")}
		second := &stubProvider{parseURLErr: errors.New("second")}

		Register(models.GitHub, first)
		Register(models.GitHub, second)

		p, err := Get(models.GitHub)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p != second {
			t.Error("expected second registration to overwrite the first")
		}
	})
}

func TestRegister_MultipleProviders(t *testing.T) {
	withIsolatedRegistry(t, func() {
		gh := &stubProvider{}
		gl := &stubProvider{}

		Register(models.GitHub, gh)
		Register(models.GitLab, gl)

		got, err := Get(models.GitHub)
		if err != nil || got != gh {
			t.Errorf("expected GitHub provider, err=%v", err)
		}

		got, err = Get(models.GitLab)
		if err != nil || got != gl {
			t.Errorf("expected GitLab provider, err=%v", err)
		}
	})
}

func TestStubProvider_Interface(t *testing.T) {
	// Compile-time check: *stubProvider must satisfy Provider.
	var _ Provider = (*stubProvider)(nil)
}

func TestSortBranches_PrioritizesCommonBranches(t *testing.T) {
	branches := []string{"release", "production", "feature/auth", "master", "main", "develop"}

	sortBranches(branches)

	expected := []string{"main", "master", "production", "develop", "feature/auth", "release"}
	if !reflect.DeepEqual(branches, expected) {
		t.Fatalf("expected %v, got %v", expected, branches)
	}
}
