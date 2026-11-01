package docker

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"unicode/utf8"

	"github.com/OrcaCD/orca-cd/internal/agent/dotenv"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/shared/envrules"
	"github.com/OrcaCD/orca-cd/internal/shared/utils"
	"github.com/compose-spec/compose-go/v2/cli"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
)

var (
	ErrEnvConflict = errors.New("the environment settings changed in the meantime")

	errApplicationNotDeployed = errors.New("the application has not been deployed to this agent yet")
	errComposeFileMissing     = errors.New("the compose file of the application was not found on the agent, deploy the application first")
)

var envRevisionKey = sync.OnceValue(func() []byte {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	return key
})

// envRevision identifies the state of an env file for optimistic concurrency.
func envRevision(path string, content []byte, exists bool) string {
	mac := hmac.New(sha256.New, envRevisionKey())
	mac.Write([]byte(path))
	mac.Write([]byte{0})
	if exists {
		mac.Write([]byte{1})
		mac.Write(content)
	} else {
		mac.Write([]byte{0})
	}
	return hex.EncodeToString(mac.Sum(nil)[:16])
}

type declaredEnvFile struct {
	path     string
	services []string
}

func (c *Client) openApplicationDir(appName string) (root *os.Root, projectName, dir string, err error) {
	if c.deploymentsDir == "" {
		return nil, "", "", errors.New("deployments directory is not configured")
	}
	if err := utils.DoesNotLookLikeFilePath(appName); err != nil {
		return nil, "", "", fmt.Errorf("invalid application name: %w", err)
	}
	projectName, err = utils.NormalizeProjectName(appName)
	if err != nil {
		return nil, "", "", err
	}
	dir = filepath.Join(c.deploymentsDir, projectName)
	if err := utils.IsPathWithinBase(c.deploymentsDir, dir); err != nil {
		return nil, "", "", fmt.Errorf("invalid deployment directory: %w", err)
	}

	info, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, "", "", errApplicationNotDeployed
	}
	if err != nil {
		return nil, "", "", err
	}
	if !info.IsDir() {
		return nil, "", "", fmt.Errorf("%s is not a directory", dir)
	}

	root, err = os.OpenRoot(dir)
	if err != nil {
		return nil, "", "", err
	}
	return root, projectName, dir, nil
}

func (c *Client) declaredEnvFiles(ctx context.Context, root *os.Root, projectName, dir string) ([]declaredEnvFile, error) {
	if _, err := root.Stat(composeFileName); errors.Is(err, fs.ErrNotExist) {
		return nil, errComposeFileMissing
	} else if err != nil {
		return nil, fmt.Errorf("check compose file: %w", err)
	}

	// Skipping the environment resolution keeps a missing env file from failing
	// the load.
	project, err := loadProject(ctx, c.compose, api.ProjectLoadOptions{
		ProjectName:       projectName,
		ConfigPaths:       []string{filepath.Join(dir, composeFileName)},
		WorkingDir:        dir,
		Offline:           true,
		ProjectOptionsFns: []cli.ProjectOptionsFn{cli.WithoutEnvironmentResolution},
	})
	if err != nil {
		return nil, fmt.Errorf("load compose project: %w", err)
	}

	var declared []declaredEnvFile
	indexByPath := make(map[string]int)
	for _, serviceName := range slices.Sorted(maps.Keys(project.Services)) {
		for _, envFile := range serviceEnvFiles(project.Services[serviceName]) {
			if envFile.Format == "raw" {
				continue
			}
			rel, ok := utils.RelativePathInDir(dir, envFile.Path)
			if !ok {
				continue
			}
			idx, seen := indexByPath[rel]
			if !seen {
				idx = len(declared)
				indexByPath[rel] = idx
				declared = append(declared, declaredEnvFile{path: rel})
			}
			if !slices.Contains(declared[idx].services, serviceName) {
				declared[idx].services = append(declared[idx].services, serviceName)
			}
		}
	}
	return declared, nil
}

func readEnvFile(root *os.Root, path string) ([]byte, bool, error) {
	name := filepath.FromSlash(path)
	info, err := root.Stat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, errors.New("not a regular file")
	}

	file, err := root.Open(name)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(io.LimitReader(file, envrules.MaxFileBytes+1))
	if err != nil {
		return nil, false, err
	}
	if len(data) > envrules.MaxFileBytes {
		return nil, false, fmt.Errorf("larger than %d bytes", envrules.MaxFileBytes)
	}
	return data, true, nil
}

// ListEnv returns the declared env files of an application. Values are only
// included for variables, a secret reports whether it is set.
func (c *Client) ListEnv(ctx context.Context, appName string) ([]*messages.EnvFile, error) {
	root, projectName, dir, err := c.openApplicationDir(appName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()

	declared, err := c.declaredEnvFiles(ctx, root, projectName, dir)
	if err != nil {
		return nil, err
	}

	files := make([]*messages.EnvFile, 0, len(declared))
	for _, file := range declared {
		data, exists, err := readEnvFile(root, file.path)
		if err != nil {
			return nil, fmt.Errorf("read env file %q: %w", file.path, err)
		}
		files = append(files, &messages.EnvFile{
			Path:     file.path,
			Services: file.services,
			Entries:  envEntries(dotenv.Parse(string(data))),
			Revision: envRevision(file.path, data, exists),
		})
	}
	return files, nil
}

func envEntries(doc *dotenv.Doc) []*messages.EnvEntry {
	var entries []*messages.EnvEntry
	for _, entry := range doc.Entries() {
		if envrules.ValidateKey(entry.Key) != nil {
			continue
		}
		if !entry.Public {
			entries = append(entries, &messages.EnvEntry{Key: entry.Key, IsSecret: true, IsSet: entry.Value != ""})
			continue
		}
		if !utf8.ValidString(entry.Value) {
			continue
		}
		entries = append(entries, &messages.EnvEntry{Key: entry.Key, Value: entry.Value})
	}
	return entries
}

func validateEnvUpdate(files []*messages.EnvFileChanges, declared []declaredEnvFile) error {
	if len(files) == 0 {
		return errors.New("the update contains no env files")
	}

	seenFiles := make(map[string]struct{}, len(files))
	for _, file := range files {
		if !slices.ContainsFunc(declared, func(d declaredEnvFile) bool { return d.path == file.Path }) {
			return fmt.Errorf("env file %q is not declared in the compose file", file.Path)
		}
		if _, dup := seenFiles[file.Path]; dup {
			return fmt.Errorf("env file %q appears more than once", file.Path)
		}
		seenFiles[file.Path] = struct{}{}

		if len(file.Changes) > envrules.MaxKeysPerFile {
			return fmt.Errorf("env file %q: more than %d changes", file.Path, envrules.MaxKeysPerFile)
		}
		seenKeys := make(map[string]struct{}, len(file.Changes))
		for i, change := range file.Changes {
			if err := envrules.ValidateKey(change.Key); err != nil {
				return fmt.Errorf("env file %q: key of change %d %w", file.Path, i+1, err)
			}
			if _, dup := seenKeys[change.Key]; dup {
				return fmt.Errorf("env file %q: key %q is changed more than once", file.Path, change.Key)
			}
			seenKeys[change.Key] = struct{}{}

			if change.Delete {
				continue
			}
			if err := envrules.ValidateValue(change.Value); err != nil {
				return fmt.Errorf("env file %q: value of key %q %w", file.Path, change.Key, err)
			}
		}
	}
	return nil
}

func rejectSecretReveal(doc *dotenv.Doc, changes []*messages.EnvChange) error {
	secrets := make(map[string]struct{})
	for _, entry := range doc.Entries() {
		if !entry.Public {
			secrets[entry.Key] = struct{}{}
		}
	}
	for _, change := range changes {
		if _, isSecret := secrets[change.Key]; isSecret && !change.Delete && !change.IsSecret {
			return fmt.Errorf("key %q is a secret and cannot be made a variable", change.Key)
		}
	}
	return nil
}

type envWrite struct {
	path    string
	old     []byte
	existed bool
	data    []byte
}

func (c *Client) UpdateEnv(ctx context.Context, appName string, files []*messages.EnvFileChanges) error {
	root, projectName, dir, err := c.openApplicationDir(appName)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	declared, err := c.declaredEnvFiles(ctx, root, projectName, dir)
	if err != nil {
		return err
	}
	if err := validateEnvUpdate(files, declared); err != nil {
		return err
	}

	var writes []envWrite
	for _, file := range files {
		data, exists, err := readEnvFile(root, file.Path)
		if err != nil {
			return fmt.Errorf("read env file %q: %w", file.Path, err)
		}
		if !hmac.Equal([]byte(file.ExpectedRevision), []byte(envRevision(file.Path, data, exists))) {
			return ErrEnvConflict
		}

		doc := dotenv.Parse(string(data))
		if err := rejectSecretReveal(doc, file.Changes); err != nil {
			return fmt.Errorf("env file %q: %w", file.Path, err)
		}
		edits := make([]dotenv.Change, 0, len(file.Changes))
		for _, change := range file.Changes {
			if change.Delete {
				doc.Remove(change.Key)
			} else {
				doc.Set(change.Key, change.Value, !change.IsSecret)
			}
			edits = append(edits, dotenv.Change{Key: change.Key, Value: change.Value, Delete: change.Delete})
		}
		if doc.KeyCount() > envrules.MaxKeysPerFile {
			return fmt.Errorf("env file %q: more than %d keys", file.Path, envrules.MaxKeysPerFile)
		}
		updated := doc.String()
		if len(updated) > envrules.MaxFileBytes {
			return fmt.Errorf("env file %q: larger than %d bytes", file.Path, envrules.MaxFileBytes)
		}
		if updated != string(data) {
			if err := dotenv.VerifyEdit(string(data), updated, edits); err != nil {
				return fmt.Errorf("env file %q: %w", file.Path, err)
			}
			writes = append(writes, envWrite{path: file.Path, old: data, existed: exists, data: []byte(updated)})
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	for i, write := range writes {
		if err := writeRootFileAtomic(root, write.path, write.data); err != nil {
			c.rollbackEnvWrites(root, writes[:i])
			return fmt.Errorf("write env file %q: %w", write.path, err)
		}
	}

	c.log.Info().Int("files_changed", len(writes)).Msg("env files updated")
	return nil
}

func (c *Client) rollbackEnvWrites(root *os.Root, writes []envWrite) {
	for _, write := range writes {
		var err error
		if write.existed {
			err = writeRootFileAtomic(root, write.path, write.old)
		} else {
			err = root.Remove(filepath.FromSlash(write.path))
		}
		if err != nil {
			c.log.Error().Err(err).Str("path", write.path).Msg("failed to roll back env file")
		}
	}
}

func (c *Client) Redeploy(ctx context.Context, appID, appName string) error {
	root, _, _, err := c.openApplicationDir(appName)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	composeFile, err := root.ReadFile(composeFileName)
	if errors.Is(err, fs.ErrNotExist) {
		return errComposeFileMissing
	}
	if err != nil {
		return fmt.Errorf("read compose file: %w", err)
	}

	return c.Deploy(ctx, DeployRequest{
		ApplicationID:   appID,
		ApplicationName: appName,
		ComposeFile:     string(composeFile),
	})
}

// resolveEnvFiles reads the env_files of a project that was loaded without
// resolving them, after creating the missing required ones inside the
// application directory, so the first deploy does not fail on them.
func resolveEnvFiles(dir string, project *composetypes.Project) (*composetypes.Project, error) {
	if err := createMissingEnvFiles(dir, project); err != nil {
		return nil, fmt.Errorf("create missing env files: %w", err)
	}
	resolved, err := project.WithServicesEnvironmentResolved(false)
	if err != nil {
		return nil, fmt.Errorf("load compose project: %w", err)
	}
	return resolved, nil
}

func serviceEnvFiles(service composetypes.ServiceConfig) []composetypes.EnvFile {
	envFiles := slices.Clone(service.EnvFiles)
	for _, hook := range service.PreStart {
		envFiles = append(envFiles, hook.EnvFiles...)
	}
	return envFiles
}

func createMissingEnvFiles(dir string, project *composetypes.Project) error {
	var root *os.Root
	defer func() {
		if root != nil {
			_ = root.Close()
		}
	}()

	for _, service := range project.Services {
		for _, envFile := range serviceEnvFiles(service) {
			rel, ok := utils.RelativePathInDir(dir, envFile.Path)
			if !ok || !bool(envFile.Required) {
				continue
			}

			if root == nil {
				var err error
				if root, err = os.OpenRoot(dir); err != nil {
					return err
				}
			}
			name := filepath.FromSlash(rel)
			if _, err := root.Lstat(name); err == nil {
				continue
			} else if !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("check %q: %w", rel, err)
			}

			if parent := filepath.Dir(name); parent != "." {
				if err := root.MkdirAll(parent, 0o750); err != nil {
					return fmt.Errorf("create directory of %q: %w", rel, err)
				}
			}
			file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return fmt.Errorf("create %q: %w", rel, err)
			}
			if err := file.Close(); err != nil {
				return fmt.Errorf("create %q: %w", rel, err)
			}
		}
	}
	return nil
}
