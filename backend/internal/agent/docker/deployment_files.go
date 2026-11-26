package docker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/shared/deployfiles"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/google/uuid"
)

const (
	deploymentFilesLabel       = "orca-cd.deployment-files"
	maxManifestBytes           = 256 << 10
	deploymentFilesMetadataDir = ".orcacd-managed-files"
)

type deploymentFilesManifest struct {
	Paths    []string `json:"paths"`
	Revision string   `json:"revision"`
}

type deploymentFileChange struct {
	name     string
	previous []byte
	mode     fs.FileMode
	existed  bool
	content  []byte
	selected bool
}

type deploymentFilesUpdate struct {
	root            *os.Root
	dir             string
	previous        deploymentFilesManifest
	current         deploymentFilesManifest
	changes         []deploymentFileChange
	applied         int
	manifestChanged bool
}

func openDeploymentFiles(dir string) (*os.Root, deploymentFilesManifest, error) {
	base, err := os.OpenRoot(filepath.Dir(dir))
	if err != nil {
		return nil, deploymentFilesManifest{}, err
	}
	defer func() { _ = base.Close() }()
	root, err := openDeploymentAssetDir(base, filepath.Base(dir), false)
	if err != nil {
		return nil, deploymentFilesManifest{}, err
	}
	manifest, err := loadDeploymentFilesManifest(dir)
	if err != nil {
		_ = root.Close()
		return nil, deploymentFilesManifest{}, err
	}
	return root, manifest, nil
}

func loadDeploymentFilesManifest(dir string) (deploymentFilesManifest, error) {
	var manifest deploymentFilesManifest
	root, err := os.OpenRoot(filepath.Join(filepath.Dir(dir), deploymentFilesMetadataDir))
	if errors.Is(err, fs.ErrNotExist) {
		return manifest, nil
	}
	if err != nil {
		return manifest, err
	}
	defer func() { _ = root.Close() }()
	data, exists, _, err := readDeploymentAsset(root, filepath.Base(dir)+".json", maxManifestBytes)
	if err != nil || !exists {
		return manifest, err
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, fmt.Errorf("invalid managed file manifest: %w", err)
	}
	for _, name := range manifest.Paths {
		if err := deployfiles.ValidatePath(name); err != nil {
			return manifest, fmt.Errorf("invalid managed file manifest: %w", err)
		}
	}
	return manifest, nil
}

func prepareDeploymentFiles(root *os.Root, dir string, previous deploymentFilesManifest, files map[string][]byte) (*deploymentFilesUpdate, error) {
	paths := slices.Sorted(maps.Keys(files))
	if err := deployfiles.ValidatePaths(paths); err != nil {
		return nil, err
	}
	size := 0
	for _, name := range paths {
		size += len(files[name])
		if size > deployfiles.MaxBytes {
			return nil, fmt.Errorf("additional files exceed %d bytes at %q", deployfiles.MaxBytes, name)
		}
	}
	oldPaths := make(map[string]bool, len(previous.Paths))
	allPaths := make(map[string]bool, len(previous.Paths)+len(files))
	for _, name := range previous.Paths {
		oldPaths[name], allPaths[name] = true, true
	}
	for _, name := range paths {
		allPaths[name] = true
	}

	update := &deploymentFilesUpdate{root: root, dir: dir, previous: previous, current: previous}
	oldSize := 0
	for _, name := range slices.Sorted(maps.Keys(allPaths)) {
		old, exists, mode, err := readDeploymentAsset(root, name, 2*deployfiles.MaxBytes-oldSize)
		if err != nil {
			return nil, fmt.Errorf("read deployment file %q: %w", name, err)
		}
		oldSize += len(old)
		if exists && !oldPaths[name] {
			return nil, fmt.Errorf("deployment file %q already exists and is not managed by Git sync", name)
		}
		content, selected := files[name]
		if exists != selected || (selected && !bytes.Equal(content, old)) {
			update.changes = append(update.changes, deploymentFileChange{
				name: name, previous: old, mode: mode, existed: exists, content: content, selected: selected,
			})
		}
	}
	if len(update.changes) != 0 || !slices.Equal(paths, previous.Paths) {
		update.current = deploymentFilesManifest{Paths: paths}
		if len(paths) != 0 {
			update.current.Revision = uuid.NewString()
		}
	}
	return update, nil
}

func (u *deploymentFilesUpdate) apply() error {
	if len(u.changes) == 0 && slices.Equal(u.previous.Paths, u.current.Paths) {
		return nil
	}
	// Record ownership of new paths before installing them. If the agent stops
	// before finishing, a retry can still track both new and deselected files.
	pending := u.current
	pending.Paths = slices.Clone(pending.Paths)
	for _, name := range u.previous.Paths {
		if _, found := slices.BinarySearch(u.current.Paths, name); !found {
			pending.Paths = append(pending.Paths, name)
		}
	}
	if err := storeDeploymentFilesManifest(u.dir, pending); err != nil {
		return err
	}
	u.manifestChanged = true
	for _, change := range u.changes {
		if err := setDeploymentAsset(u.root, change.name, change.content, change.selected, 0o644); err != nil {
			return fmt.Errorf("apply deployment file %q: %w", change.name, err)
		}
		u.applied++
	}
	return nil
}

func (u *deploymentFilesUpdate) finish() error {
	if !u.manifestChanged {
		return nil
	}
	for _, name := range u.previous.Paths {
		if _, found := slices.BinarySearch(u.current.Paths, name); !found {
			return storeDeploymentFilesManifest(u.dir, u.current)
		}
	}
	return nil
}

func (u *deploymentFilesUpdate) restore() error {
	var err error
	for i := u.applied - 1; i >= 0; i-- {
		change := u.changes[i]
		if restoreErr := setDeploymentAsset(u.root, change.name, change.previous, change.existed, change.mode); restoreErr != nil {
			err = errors.Join(err, fmt.Errorf("restore deployment file %q: %w", change.name, restoreErr))
		}
	}
	if u.manifestChanged && err == nil {
		err = storeDeploymentFilesManifest(u.dir, u.previous)
	}
	return err
}

func storeDeploymentFilesManifest(dir string, manifest deploymentFilesManifest) error {
	metadataDir := filepath.Join(filepath.Dir(dir), deploymentFilesMetadataDir)
	if len(manifest.Paths) != 0 {
		if err := os.MkdirAll(metadataDir, 0o700); err != nil {
			return fmt.Errorf("create managed file metadata directory: %w", err)
		}
	}
	root, err := os.OpenRoot(metadataDir)
	if len(manifest.Paths) == 0 && errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	name := filepath.Base(dir) + ".json"
	if len(manifest.Paths) == 0 {
		if err := root.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove managed file manifest: %w", err)
		}
		return nil
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("encode managed file manifest: %w", err)
	}
	if len(data) > maxManifestBytes {
		return errors.New("managed file manifest is too large")
	}
	if err := writeRootFileAtomic(root, name, data); err != nil {
		return fmt.Errorf("write managed file manifest: %w", err)
	}
	return nil
}

func readDeploymentAsset(root *os.Root, name string, limit int) ([]byte, bool, fs.FileMode, error) {
	parent, err := openDeploymentAssetParent(root, name, false)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, 0, nil
	}
	if err != nil {
		return nil, false, 0, err
	}
	defer func() { _ = parent.Close() }()
	base := filepath.Base(name)
	info, err := parent.Lstat(base)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, 0, nil
	}
	if err != nil {
		return nil, false, 0, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, 0, errors.New("not a regular file")
	}
	file, err := parent.Open(base)
	if err != nil {
		return nil, false, 0, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if len(data) > limit {
		return nil, false, 0, errors.New("managed files exceed the size limit")
	}
	return data, true, info.Mode().Perm(), err
}

func setDeploymentAsset(root *os.Root, name string, content []byte, exists bool, mode fs.FileMode) error {
	parent, err := openDeploymentAssetParent(root, name, exists)
	if err != nil {
		if !exists && errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	defer func() { _ = parent.Close() }()
	base := filepath.Base(name)
	if exists {
		return writeRootFileAtomicWithMode(parent, base, content, mode)
	}
	info, err := parent.Lstat(base)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("not a regular file")
	}
	return parent.Remove(base)
}

// Check and pin each parent directory so symlinks cannot redirect a write
// into another directory, even when their target is inside the deployment.
func openDeploymentAssetParent(root *os.Root, name string, create bool) (*os.Root, error) {
	parent, err := root.OpenRoot(".")
	if err != nil {
		return nil, err
	}
	dirs, _, nested := strings.CutLast(name, "/")
	if !nested {
		return parent, nil
	}
	for dir := range strings.SplitSeq(dirs, "/") {
		child, err := openDeploymentAssetDir(parent, dir, create)
		_ = parent.Close()
		if err != nil {
			return nil, err
		}
		parent = child
	}
	return parent, nil
}

func openDeploymentAssetDir(parent *os.Root, name string, create bool) (*os.Root, error) {
	info, err := parent.Lstat(name)
	if create && errors.Is(err, fs.ErrNotExist) {
		if err := parent.Mkdir(name, 0o750); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		info, err = parent.Lstat(name)
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%q is not a directory", name)
	}
	child, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	actual, err := child.Stat(".")
	if err != nil || !os.SameFile(info, actual) {
		_ = child.Close()
		return nil, fmt.Errorf("directory %q changed while opening it", name)
	}
	return child, nil
}

func applyDeploymentFilesLabel(project *composetypes.Project, revision string) {
	for name, service := range project.Services {
		if revision == "" {
			delete(service.Labels, deploymentFilesLabel)
		} else {
			service.Labels[deploymentFilesLabel] = revision
		}
		project.Services[name] = service
	}
}
