package repositories

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
)

type Provider interface {
	// Validates and parses the repository URL, returning the owner and repo name if valid.
	ParseURL(url string) (string, string, error)
	// Returns the list of supported authentication methods for this provider.
	SupportedAuthMethods() []models.RepositoryAuthMethod
	// Tests the connection to the repository using the provided credentials.
	TestConnection(ctx context.Context, repo *models.Repository) error
	// Lists available branches for the repository.
	ListBranches(ctx context.Context, repo *models.Repository) ([]string, error)
	// Lists repository tree entries for a branch.
	ListTree(ctx context.Context, repo *models.Repository, branch string) ([]TreeEntry, error)
	// Returns decoded file content for a repository file at the given branch.
	GetFileContent(ctx context.Context, repo *models.Repository, branch, path string) (string, error)
	// Returns latest commit metadata for the given branch.
	GetLatestCommit(ctx context.Context, repo *models.Repository, branch string) (CommitInfo, error)
}

type CommitInfo struct {
	Hash    string
	Message string
}

type TreeEntryType string

const (
	TreeEntryTypeFile TreeEntryType = "file"
	TreeEntryTypeDir  TreeEntryType = "dir"
	httpsScheme                     = "https"
	providerPageSize                = 100
)

type TreeEntry struct {
	Path string        `json:"path"`
	Type TreeEntryType `json:"type"`
}

var registry = map[models.RepositoryProvider]Provider{}

// ownerRe matches usernames/org names: 1–39 alphanumeric chars or hyphens,
// not starting or ending with a hyphen
var ownerRe = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,37}[a-zA-Z0-9])?$`)

// repoRe matches repository names: 1–100 chars, alphanumeric, hyphens,
// underscores, or dots
var repoRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,100}$`)

var commonBranchesByPriority = map[string]int{
	"main":        0,
	"master":      1,
	"production":  2,
	"prod":        3,
	"staging":     4,
	"develop":     5,
	"development": 6,
	"dev":         7,
}

// sortBranches orders common default branches first, then the rest alphabetically.
func sortBranches(branches []string) {
	sort.Slice(branches, func(i, j int) bool {
		leftBranch := strings.ToLower(branches[i])
		rightBranch := strings.ToLower(branches[j])

		leftPriority, leftIsCommon := commonBranchesByPriority[leftBranch]
		rightPriority, rightIsCommon := commonBranchesByPriority[rightBranch]

		if leftIsCommon && rightIsCommon {
			if leftPriority != rightPriority {
				return leftPriority < rightPriority
			}
		}

		if leftIsCommon != rightIsCommon {
			return leftIsCommon
		}

		if leftBranch == rightBranch {
			return branches[i] < branches[j]
		}

		return leftBranch < rightBranch
	})
}

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
