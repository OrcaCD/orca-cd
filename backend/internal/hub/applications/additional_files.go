package applications

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/repositories"
	"github.com/OrcaCD/orca-cd/internal/shared/deployfiles"
)

// AdditionalFileDestinations validates repository paths and returns the
// corresponding locations relative to the Compose file, in the same order.
func AdditionalFileDestinations(composePath string, paths []string) ([]string, error) {
	composePath = strings.TrimPrefix(path.Clean(composePath), "/")
	dir := path.Dir(composePath)
	destinations := make([]string, 0, len(paths))
	for _, name := range paths {
		if !deployfiles.ValidPath(name) || name != strings.TrimSpace(name) {
			return nil, fmt.Errorf("invalid repository file path %q", name)
		}
		dest := name
		if dir != "." {
			var ok bool
			dest, ok = strings.CutPrefix(name, dir+"/")
			if !ok {
				return nil, fmt.Errorf("additional file %q must be inside the Compose file's directory", name)
			}
		}
		if name == composePath {
			return nil, fmt.Errorf("the Compose file %q is already included in every deployment", name)
		}
		destinations = append(destinations, dest)
	}
	if err := deployfiles.ValidatePaths(destinations); err != nil {
		return nil, err
	}
	return destinations, nil
}

func FetchAdditionalFiles(ctx context.Context, provider repositories.Provider, repo *models.Repository, commit, composePath string, paths []string) (crypto.EncryptedString, error) {
	if len(paths) == 0 {
		return "", nil
	}
	destinations, err := AdditionalFileDestinations(composePath, paths)
	if err != nil {
		return "", err
	}
	files := make(models.DeploymentFiles, len(paths))
	size := 0
	for i, name := range paths {
		content, err := provider.GetFileContent(ctx, repo, commit, name)
		if err != nil {
			return "", fmt.Errorf("failed to fetch additional file %q: %w", name, err)
		}
		size += len(content)
		if size > deployfiles.MaxBytes {
			return "", fmt.Errorf("additional files exceed %d bytes at %q", deployfiles.MaxBytes, name)
		}
		files[destinations[i]] = []byte(content)
	}
	return models.EncodeDeploymentFiles(files)
}
