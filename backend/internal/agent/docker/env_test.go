package docker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/agent/dotenv"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/shared/envrules"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
)

const envTestApp = "billing"

type envTestFile struct {
	path     string
	required bool
	format   string
}

func required(path string) envTestFile { return envTestFile{path: path, required: true} }

func setupEnvApp(t *testing.T, c *Client, services map[string][]envTestFile) string {
	t.Helper()
	c.deploymentsDir = t.TempDir()
	dir := filepath.Join(c.deploymentsDir, envTestApp)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, composeFileName), "services: {}\n")
	stubEnvProject(t, dir, services)
	return dir
}

func stubEnvProject(t *testing.T, dir string, services map[string][]envTestFile) {
	t.Helper()
	original := loadProject
	t.Cleanup(func() { loadProject = original })
	loadProject = func(_ context.Context, _ api.Compose, opts api.ProjectLoadOptions) (*composetypes.Project, error) {
		project := &composetypes.Project{Name: opts.ProjectName, Services: composetypes.Services{}}
		for name, files := range services {
			service := composetypes.ServiceConfig{Name: name}
			for _, file := range files {
				path := file.path
				if !filepath.IsAbs(path) {
					path = filepath.Join(dir, path)
				}
				service.EnvFiles = append(service.EnvFiles, composetypes.EnvFile{Path: path, Required: composetypes.OptOut(file.required), Format: file.format})
			}
			project.Services[name] = service
		}
		return project, nil
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // path is built from t.TempDir()
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	return string(data)
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}
}

func listEnvFile(t *testing.T, c *Client, path string) *messages.EnvFile {
	t.Helper()
	files, err := c.ListEnv(t.Context(), envTestApp)
	if err != nil {
		t.Fatalf("ListEnv: %v", err)
	}
	for _, file := range files {
		if file.Path == path {
			return file
		}
	}
	t.Fatalf("env file %q not listed in %v", path, files)
	return nil
}

func TestListEnv_ReturnsVariablesAndSecrets(t *testing.T) {
	c := newTestClient(t)
	dir := setupEnvApp(t, c, map[string][]envTestFile{
		"web":    {required(".env"), required("shared.env")},
		"worker": {required("shared.env")},
	})
	writeTestFile(t, filepath.Join(dir, ".env"), "# orcacd:public\nLOG_LEVEL=debug\nDB_PASSWORD=hunter2\nEMPTY_SECRET=\n")
	writeTestFile(t, filepath.Join(dir, "shared.env"), "SHARED=1\n")

	files, err := c.ListEnv(t.Context(), envTestApp)
	if err != nil {
		t.Fatalf("ListEnv: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 env files, got %d", len(files))
	}

	env := listEnvFile(t, c, ".env")
	if strings.Join(env.Services, ",") != "web" {
		t.Fatalf("unexpected services %v", env.Services)
	}
	if len(env.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %+v", env.Entries)
	}
	if e := env.Entries[0]; e.Key != "LOG_LEVEL" || e.IsSecret || e.Value != "debug" {
		t.Fatalf("unexpected variable %+v", e)
	}
	if e := env.Entries[1]; e.Key != "DB_PASSWORD" || !e.IsSecret || !e.IsSet || e.Value != "" {
		t.Fatalf("expected the secret to report only that it is set, got %+v", e)
	}
	if e := env.Entries[2]; !e.IsSecret || e.IsSet {
		t.Fatalf("expected an empty secret not to be set, got %+v", e)
	}

	if shared := listEnvFile(t, c, "shared.env"); strings.Join(shared.Services, ",") != "web,worker" {
		t.Fatalf("expected a shared env file to list all of its services, got %v", shared.Services)
	}
}

func TestListEnv_NeverReturnsSecretValues(t *testing.T) {
	c := newTestClient(t)
	dir := setupEnvApp(t, c, map[string][]envTestFile{"web": {required(".env")}})
	writeTestFile(t, filepath.Join(dir, ".env"), "TOKEN=super-secret-value\n")

	files, err := c.ListEnv(t.Context(), envTestApp)
	if err != nil {
		t.Fatalf("ListEnv: %v", err)
	}
	for _, file := range files {
		if strings.Contains(file.String(), "super-secret-value") {
			t.Fatal("the secret value is part of the list result")
		}
	}
}

func TestListEnv_OnlyEditableEnvFiles(t *testing.T) {
	c := newTestClient(t)
	outside := filepath.Join(t.TempDir(), "outside.env")
	setupEnvApp(t, c, map[string][]envTestFile{
		"web": {
			required(".env"),
			required(outside),
			required("../other-app/.env"),
			{path: "raw.env", required: true, format: "raw"},
			{path: "config/optional.env"},
		},
	})

	files, err := c.ListEnv(t.Context(), envTestApp)
	if err != nil {
		t.Fatalf("ListEnv: %v", err)
	}
	var paths []string
	for _, file := range files {
		paths = append(paths, file.Path)
	}
	if got := strings.Join(paths, ","); got != ".env,config/optional.env" {
		t.Fatalf("expected only editable env files, got %q", got)
	}
}

func TestListEnv_MissingEnvFileIsEmpty(t *testing.T) {
	c := newTestClient(t)
	dir := setupEnvApp(t, c, map[string][]envTestFile{"web": {{path: "optional.env"}}})

	before := listEnvFile(t, c, "optional.env")
	if len(before.Entries) != 0 || before.Revision == "" {
		t.Fatalf("unexpected result for a missing file: %+v", before)
	}
	if _, err := os.Stat(filepath.Join(dir, "optional.env")); err == nil {
		t.Fatal("listing must not create the file")
	}

	writeTestFile(t, filepath.Join(dir, "optional.env"), "")
	if after := listEnvFile(t, c, "optional.env"); after.Revision == before.Revision {
		t.Fatal("expected creating the file to change the revision")
	}
}

func TestListEnv_Errors(t *testing.T) {
	t.Run("compose file missing", func(t *testing.T) {
		c := newTestClient(t)
		dir := setupEnvApp(t, c, map[string][]envTestFile{"web": {required(".env")}})
		if err := os.Remove(filepath.Join(dir, composeFileName)); err != nil {
			t.Fatal(err)
		}

		_, err := c.ListEnv(t.Context(), envTestApp)
		if !errors.Is(err, errComposeFileMissing) {
			t.Fatalf("expected errComposeFileMissing, got %v", err)
		}
	})

	t.Run("not deployed", func(t *testing.T) {
		c := newTestClient(t)
		c.deploymentsDir = t.TempDir()

		_, err := c.ListEnv(t.Context(), envTestApp)
		if !errors.Is(err, errApplicationNotDeployed) {
			t.Fatalf("expected errApplicationNotDeployed, got %v", err)
		}
	})

	t.Run("unsafe application name", func(t *testing.T) {
		c := newTestClient(t)
		c.deploymentsDir = t.TempDir()

		if _, err := c.ListEnv(t.Context(), "../bad"); err == nil {
			t.Fatal("expected an error")
		}
	})

	t.Run("compose file does not load", func(t *testing.T) {
		c := newTestClient(t)
		setupEnvApp(t, c, nil)
		loadProject = func(context.Context, api.Compose, api.ProjectLoadOptions) (*composetypes.Project, error) {
			return nil, errors.New("yaml: invalid")
		}

		if _, err := c.ListEnv(t.Context(), envTestApp); err == nil || !strings.Contains(err.Error(), "yaml: invalid") {
			t.Fatalf("expected the load error, got %v", err)
		}
	})
}

func TestListEnv_LoadsOfflineWithoutResolvingEnvironment(t *testing.T) {
	c := newTestClient(t)
	setupEnvApp(t, c, nil)

	var got api.ProjectLoadOptions
	loadProject = func(_ context.Context, _ api.Compose, opts api.ProjectLoadOptions) (*composetypes.Project, error) {
		got = opts
		return &composetypes.Project{Name: opts.ProjectName}, nil
	}

	if _, err := c.ListEnv(t.Context(), envTestApp); err != nil {
		t.Fatalf("ListEnv: %v", err)
	}
	if !got.Offline {
		t.Error("expected remote compose resources not to be fetched")
	}
	if len(got.ProjectOptionsFns) != 1 {
		t.Errorf("expected the environment resolution to be skipped, got %d project options", len(got.ProjectOptionsFns))
	}
}

func TestRevision_DoesNotRevealContent(t *testing.T) {
	content := []byte("PASSWORD=hunter2\n")
	revision := envRevision(".env", content, true)

	for _, plain := range [][]byte{content} {
		sum := sha256.Sum256(plain)
		if strings.HasPrefix(hex.EncodeToString(sum[:]), revision) || revision == hex.EncodeToString(sum[:])[:len(revision)] {
			t.Fatal("the revision must not be a plain hash of the content")
		}
	}
	if envRevision(".env", content, true) != revision {
		t.Fatal("expected the revision to be stable")
	}
	if envRevision(".env", []byte("PASSWORD=hunter3\n"), true) == revision {
		t.Fatal("expected different content to have a different revision")
	}
	if envRevision("other.env", content, true) == revision {
		t.Fatal("expected a different file to have a different revision")
	}
	if envRevision(".env", nil, false) == envRevision(".env", nil, true) {
		t.Fatal("expected a missing file to differ from an empty one")
	}
}

func envUpdate(path, revision string, changes ...*messages.EnvChange) *messages.EnvFileChanges {
	return &messages.EnvFileChanges{Path: path, ExpectedRevision: revision, Changes: changes}
}

func setChange(key, value string, secret bool) *messages.EnvChange {
	return &messages.EnvChange{Key: key, Value: value, IsSecret: secret}
}

func deleteChange(key string) *messages.EnvChange {
	return &messages.EnvChange{Key: key, Delete: true}
}

func numberedKey(i int) string {
	return fmt.Sprintf("K%04d", i)
}

func TestUpdateEnv_AppliesChangesToSeveralFiles(t *testing.T) {
	c := newTestClient(t)
	dir := setupEnvApp(t, c, map[string][]envTestFile{
		"web": {required(".env"), required("config/db.env"), {path: "optional.env"}},
	})
	writeTestFile(t, filepath.Join(dir, ".env"), "# Settings\n# orcacd:public\nLOG_LEVEL=info\nOLD=1\n")
	writeTestFile(t, filepath.Join(dir, "config", "db.env"), "PASSWORD=old\n")

	err := c.UpdateEnv(t.Context(), envTestApp, []*messages.EnvFileChanges{
		envUpdate(".env", listEnvFile(t, c, ".env").Revision,
			setChange("LOG_LEVEL", "debug", false), setChange("TOKEN", "abc def", true), deleteChange("OLD")),
		envUpdate("config/db.env", listEnvFile(t, c, "config/db.env").Revision,
			setChange("PASSWORD", "new", true)),
		envUpdate("optional.env", listEnvFile(t, c, "optional.env").Revision,
			setChange("FEATURE", "on", false)),
	})
	if err != nil {
		t.Fatalf("UpdateEnv: %v", err)
	}

	if got, want := readTestFile(t, filepath.Join(dir, ".env")), "# Settings\n# orcacd:public\nLOG_LEVEL=debug\nTOKEN=\"abc def\"\n"; got != want {
		t.Fatalf(".env:\n%q\nwant:\n%q", got, want)
	}
	if got := readTestFile(t, filepath.Join(dir, "config", "db.env")); got != "PASSWORD=new\n" {
		t.Fatalf("db.env: %q", got)
	}
	if got := readTestFile(t, filepath.Join(dir, "optional.env")); got != "# orcacd:public\nFEATURE=on\n" {
		t.Fatalf("optional.env: %q", got)
	}
}

func TestEnv_KeysComposeAccepts(t *testing.T) {
	c := newTestClient(t)
	dir := setupEnvApp(t, c, map[string][]envTestFile{"web": {required(".env")}})
	writeTestFile(t, filepath.Join(dir, ".env"), "# orcacd:public\nspring.profile=dev\nmy-secret=1\nlist[0]=hidden\n1ABC=x\nключ=y\n")

	env := listEnvFile(t, c, ".env")
	wantKeys := []string{"spring.profile", "my-secret", "list[0]", "1ABC", "ключ"}
	if len(env.Entries) != len(wantKeys) {
		t.Fatalf("expected %v to be listed, got %+v", wantKeys, env.Entries)
	}
	for i, key := range wantKeys {
		if env.Entries[i].Key != key {
			t.Fatalf("entry %d: expected key %q, got %+v", i, key, env.Entries[i])
		}
	}
	if e := env.Entries[0]; e.IsSecret || e.Value != "dev" {
		t.Fatalf("unexpected entry %+v", e)
	}
	if e := env.Entries[1]; !e.IsSecret {
		t.Fatalf("unexpected entry %+v", e)
	}

	err := c.UpdateEnv(t.Context(), envTestApp, []*messages.EnvFileChanges{
		envUpdate(".env", env.Revision,
			setChange("spring.profile", "prod", false), setChange("list[0]", "shown", true), setChange("new-key.v2", "x", true)),
	})
	if err != nil {
		t.Fatalf("UpdateEnv: %v", err)
	}
	if got, want := readTestFile(t, filepath.Join(dir, ".env")), "# orcacd:public\nspring.profile=prod\nmy-secret=1\nlist[0]=shown\n1ABC=x\nключ=y\nnew-key.v2=x\n"; got != want {
		t.Fatalf(".env:\n%q\nwant:\n%q", got, want)
	}
}

func TestUpdateEnv_RejectsTurningSecretIntoVariable(t *testing.T) {
	c := newTestClient(t)
	dir := setupEnvApp(t, c, map[string][]envTestFile{"web": {required(".env")}})
	content := "KEY=secret-value\n# orcacd:public\nOTHER=1\n"
	writeTestFile(t, filepath.Join(dir, ".env"), content)

	err := c.UpdateEnv(t.Context(), envTestApp, []*messages.EnvFileChanges{
		envUpdate(".env", listEnvFile(t, c, ".env").Revision,
			setChange("OTHER", "2", false), setChange("KEY", "now-public", false)),
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "secret-value") || strings.Contains(err.Error(), "now-public") {
		t.Fatalf("error must not contain values: %v", err)
	}
	if got := readTestFile(t, filepath.Join(dir, ".env")); got != content {
		t.Fatalf("expected nothing to be written, got %q", got)
	}
}

func TestUpdateEnv_SecretCanBeReplacedAndVariableCanBecomeSecret(t *testing.T) {
	c := newTestClient(t)
	dir := setupEnvApp(t, c, map[string][]envTestFile{"web": {required(".env")}})
	writeTestFile(t, filepath.Join(dir, ".env"), "SECRET=old\n# orcacd:public\nVAR=visible\n")

	err := c.UpdateEnv(t.Context(), envTestApp, []*messages.EnvFileChanges{
		envUpdate(".env", listEnvFile(t, c, ".env").Revision, setChange("SECRET", "new", true), setChange("VAR", "hidden", true)),
	})
	if err != nil {
		t.Fatalf("UpdateEnv: %v", err)
	}
	if got, want := readTestFile(t, filepath.Join(dir, ".env")), "SECRET=new\nVAR=hidden\n"; got != want {
		t.Fatalf(".env:\n%q\nwant:\n%q", got, want)
	}
}

func TestUpdateEnv_DeletedSecretCanBeAddedAsVariableLater(t *testing.T) {
	c := newTestClient(t)
	dir := setupEnvApp(t, c, map[string][]envTestFile{"web": {required(".env")}})
	writeTestFile(t, filepath.Join(dir, ".env"), "KEY=secret-value\n")

	if err := c.UpdateEnv(t.Context(), envTestApp, []*messages.EnvFileChanges{
		envUpdate(".env", listEnvFile(t, c, ".env").Revision, deleteChange("KEY")),
	}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := c.UpdateEnv(t.Context(), envTestApp, []*messages.EnvFileChanges{
		envUpdate(".env", listEnvFile(t, c, ".env").Revision, setChange("KEY", "public-value", false)),
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	entry := listEnvFile(t, c, ".env").Entries[0]
	if entry.IsSecret || entry.Value != "public-value" {
		t.Fatalf("unexpected entry %+v", entry)
	}
}

func TestUpdateEnv_NewFilesHaveRestrictedMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are not meaningful on Windows")
	}
	c := newTestClient(t)
	dir := setupEnvApp(t, c, map[string][]envTestFile{"web": {{path: "config/optional.env"}}})

	err := c.UpdateEnv(t.Context(), envTestApp, []*messages.EnvFileChanges{
		envUpdate("config/optional.env", listEnvFile(t, c, "config/optional.env").Revision, setChange("A", "1", true)),
	})
	if err != nil {
		t.Fatalf("UpdateEnv: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "config", "optional.env"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected mode 0600, got %v", info.Mode().Perm())
	}
}

func TestUpdateEnv_StaleRevisionWritesNothing(t *testing.T) {
	c := newTestClient(t)
	dir := setupEnvApp(t, c, map[string][]envTestFile{"web": {required("a.env"), required("b.env")}})
	writeTestFile(t, filepath.Join(dir, "a.env"), "A=1\n")
	writeTestFile(t, filepath.Join(dir, "b.env"), "B=1\n")
	revA := listEnvFile(t, c, "a.env").Revision
	revB := listEnvFile(t, c, "b.env").Revision

	// Somebody else changes b.env after the lists.
	writeTestFile(t, filepath.Join(dir, "b.env"), "B=2\n")

	err := c.UpdateEnv(t.Context(), envTestApp, []*messages.EnvFileChanges{
		envUpdate("a.env", revA, setChange("A", "changed", true)),
		envUpdate("b.env", revB, setChange("B", "changed", true)),
	})
	if !errors.Is(err, ErrEnvConflict) {
		t.Fatalf("expected ErrEnvConflict, got %v", err)
	}
	if got := readTestFile(t, filepath.Join(dir, "a.env")); got != "A=1\n" {
		t.Fatalf("a.env must stay untouched when another file conflicts, got %q", got)
	}
	if got := readTestFile(t, filepath.Join(dir, "b.env")); got != "B=2\n" {
		t.Fatalf("b.env must stay untouched, got %q", got)
	}
}

func TestUpdateEnv_RevisionIsRequired(t *testing.T) {
	c := newTestClient(t)
	dir := setupEnvApp(t, c, map[string][]envTestFile{"web": {required(".env")}})
	writeTestFile(t, filepath.Join(dir, ".env"), "A=1\n")

	err := c.UpdateEnv(t.Context(), envTestApp, []*messages.EnvFileChanges{envUpdate(".env", "", setChange("A", "2", true))})
	if !errors.Is(err, ErrEnvConflict) {
		t.Fatalf("expected ErrEnvConflict, got %v", err)
	}
}

func TestUpdateEnv_RejectsUndeclaredPathsWithoutWriting(t *testing.T) {
	c := newTestClient(t)
	dir := setupEnvApp(t, c, map[string][]envTestFile{"web": {required(".env")}})
	writeTestFile(t, filepath.Join(dir, ".env"), "A=1\n")
	writeTestFile(t, filepath.Join(dir, "undeclared.env"), "U=1\n")
	outside := filepath.Join(t.TempDir(), "outside.env")
	rev := listEnvFile(t, c, ".env").Revision

	for _, path := range []string{"undeclared.env", "../outside.env", "../" + envTestApp + "/.env", outside, "./.env", "sub/../.env", ""} {
		t.Run(path, func(t *testing.T) {
			err := c.UpdateEnv(t.Context(), envTestApp, []*messages.EnvFileChanges{
				envUpdate(".env", rev, setChange("A", "changed", true)),
				envUpdate(path, rev, setChange("X", "1", true)),
			})
			if err == nil || errors.Is(err, ErrEnvConflict) {
				t.Fatalf("expected a validation error, got %v", err)
			}
			if got := readTestFile(t, filepath.Join(dir, ".env")); got != "A=1\n" {
				t.Fatalf("the declared file must stay untouched, got %q", got)
			}
			if got := readTestFile(t, filepath.Join(dir, "undeclared.env")); got != "U=1\n" {
				t.Fatalf("undeclared.env was modified: %q", got)
			}
			if _, err := os.Stat(outside); err == nil {
				t.Fatal("a file outside the application directory was created")
			}
		})
	}
}

func TestUpdateEnv_ValidatesBeforeWriting(t *testing.T) {
	const secretValue = "do-not-leak-me"
	tooLarge := strings.Repeat("x", envrules.MaxValueBytes+1)

	tests := []struct {
		name    string
		changes []*messages.EnvChange
	}{
		{"key with space", []*messages.EnvChange{setChange("A B", "v", true)}},
		{"key with equals", []*messages.EnvChange{setChange("A=B", "v", true)}},
		{"empty key", []*messages.EnvChange{setChange("", "v", true)}},
		{"key with newline", []*messages.EnvChange{setChange("A\nB", "v", true)}},
		{"duplicate key", []*messages.EnvChange{setChange("A", "1", true), deleteChange("A")}},
		{"value too large", []*messages.EnvChange{setChange("A", tooLarge, true)}},
		{"invalid UTF-8", []*messages.EnvChange{setChange("A", secretValue+"\xff", true)}},
		{"control character", []*messages.EnvChange{setChange("A", secretValue+"\x00", true)}},
		{"too many changes", func() []*messages.EnvChange {
			var changes []*messages.EnvChange
			for i := range envrules.MaxKeysPerFile + 1 {
				changes = append(changes, setChange(numberedKey(i), "v", true))
			}
			return changes
		}()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t)
			dir := setupEnvApp(t, c, map[string][]envTestFile{"web": {required(".env")}})
			writeTestFile(t, filepath.Join(dir, ".env"), "KEEP=1\n")
			rev := listEnvFile(t, c, ".env").Revision

			err := c.UpdateEnv(t.Context(), envTestApp, []*messages.EnvFileChanges{
				envUpdate(".env", rev, append([]*messages.EnvChange{setChange("VALID", "1", true)}, tt.changes...)...),
			})
			if err == nil || errors.Is(err, ErrEnvConflict) {
				t.Fatalf("expected a validation error, got %v", err)
			}
			if strings.Contains(err.Error(), secretValue) {
				t.Fatalf("the error contains a value: %v", err)
			}
			if got := readTestFile(t, filepath.Join(dir, ".env")); got != "KEEP=1\n" {
				t.Fatalf("a rejected update must not change the file, got %q", got)
			}
		})
	}
}

func TestUpdateEnv_LimitsKeysPerFile(t *testing.T) {
	c := newTestClient(t)
	dir := setupEnvApp(t, c, map[string][]envTestFile{"web": {required(".env")}})
	var content strings.Builder
	for i := range envrules.MaxKeysPerFile {
		fmt.Fprintf(&content, "%s=1\n", numberedKey(i))
	}
	writeTestFile(t, filepath.Join(dir, ".env"), content.String())

	err := c.UpdateEnv(t.Context(), envTestApp, []*messages.EnvFileChanges{
		envUpdate(".env", listEnvFile(t, c, ".env").Revision, setChange("ONE_TOO_MANY", "1", true)),
	})
	if err == nil || errors.Is(err, ErrEnvConflict) {
		t.Fatalf("expected the key limit to be enforced, got %v", err)
	}
	if readTestFile(t, filepath.Join(dir, ".env")) != content.String() {
		t.Fatal("the file must stay untouched")
	}

	err = c.UpdateEnv(t.Context(), envTestApp, []*messages.EnvFileChanges{
		envUpdate(".env", listEnvFile(t, c, ".env").Revision, deleteChange(numberedKey(0)), setChange("REPLACEMENT", "1", true)),
	})
	if err != nil {
		t.Fatalf("expected replacing a key at the limit to work: %v", err)
	}
}

func TestUpdateEnv_RejectsEmptyAndDuplicateFiles(t *testing.T) {
	c := newTestClient(t)
	dir := setupEnvApp(t, c, map[string][]envTestFile{"web": {required(".env")}})
	writeTestFile(t, filepath.Join(dir, ".env"), "A=1\n")
	rev := listEnvFile(t, c, ".env").Revision

	if err := c.UpdateEnv(t.Context(), envTestApp, nil); err == nil {
		t.Fatal("expected an update without files to be rejected")
	}
	err := c.UpdateEnv(t.Context(), envTestApp, []*messages.EnvFileChanges{
		envUpdate(".env", rev, setChange("A", "2", true)),
		envUpdate(".env", rev, setChange("B", "2", true)),
	})
	if err == nil {
		t.Fatal("expected a file named twice to be rejected")
	}
	if readTestFile(t, filepath.Join(dir, ".env")) != "A=1\n" {
		t.Fatal("the file must stay untouched")
	}
}

func TestUpdateEnv_DoesNotRewriteUnchangedFiles(t *testing.T) {
	c := newTestClient(t)
	dir := setupEnvApp(t, c, map[string][]envTestFile{"web": {required(".env")}})
	writeTestFile(t, filepath.Join(dir, ".env"), "A=\"x y\"\n")
	rev := listEnvFile(t, c, ".env").Revision

	err := c.UpdateEnv(t.Context(), envTestApp, []*messages.EnvFileChanges{
		envUpdate(".env", rev, setChange("A", "x y", true), deleteChange("MISSING")),
	})
	if err != nil {
		t.Fatalf("UpdateEnv: %v", err)
	}
	if got := listEnvFile(t, c, ".env").Revision; got != rev {
		t.Fatal("expected a no-op update to keep the revision")
	}
}

func TestUpdateEnv_ComposeFileMissing(t *testing.T) {
	c := newTestClient(t)
	dir := setupEnvApp(t, c, map[string][]envTestFile{"web": {required(".env")}})
	if err := os.Remove(filepath.Join(dir, composeFileName)); err != nil {
		t.Fatal(err)
	}

	err := c.UpdateEnv(t.Context(), envTestApp, []*messages.EnvFileChanges{envUpdate(".env", "x", setChange("A", "1", true))})
	if !errors.Is(err, errComposeFileMissing) {
		t.Fatalf("expected errComposeFileMissing, got %v", err)
	}
}

func TestEnvAccess_SymlinkedEnvFileOutsideApplicationDir(t *testing.T) {
	for name, c := range map[string]*Client{
		"regular application":    newTestClient(t),
		"privileged application": newTestClientWithAllowlist(t, map[string]struct{}{"app-1": {}}),
	} {
		t.Run(name, func(t *testing.T) {
			dir := setupEnvApp(t, c, map[string][]envTestFile{"web": {required(".env")}})
			outside := filepath.Join(t.TempDir(), "outside.env")
			writeTestFile(t, outside, "OUTSIDE=secret\n")
			symlinkOrSkip(t, outside, filepath.Join(dir, ".env"))

			if _, err := c.ListEnv(t.Context(), envTestApp); err == nil {
				t.Fatal("expected listing through a symlink that leaves the application directory to fail")
			}
			err := c.UpdateEnv(t.Context(), envTestApp, []*messages.EnvFileChanges{envUpdate(".env", "x", setChange("A", "1", true))})
			if err == nil || errors.Is(err, ErrEnvConflict) {
				t.Fatalf("expected an error, got %v", err)
			}
			if got := readTestFile(t, outside); got != "OUTSIDE=secret\n" {
				t.Fatalf("the file behind the symlink was modified: %q", got)
			}
		})
	}
}

func TestEnvAccess_SymlinkedDirectoryOutsideApplicationDir(t *testing.T) {
	c := newTestClient(t)
	dir := setupEnvApp(t, c, map[string][]envTestFile{"web": {required("config/app.env"), {path: "config/new.env"}}})
	outsideDir := t.TempDir()
	writeTestFile(t, filepath.Join(outsideDir, "app.env"), "OUTSIDE=secret\n")
	symlinkOrSkip(t, outsideDir, filepath.Join(dir, "config"))

	if _, err := c.ListEnv(t.Context(), envTestApp); err == nil {
		t.Fatal("expected listing through a symlinked directory to fail")
	}
	err := c.UpdateEnv(t.Context(), envTestApp, []*messages.EnvFileChanges{envUpdate("config/new.env", "x", setChange("A", "1", true))})
	if err == nil {
		t.Fatal("expected an error")
	}
	entries, readErr := os.ReadDir(outsideDir)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 1 {
		t.Fatalf("nothing may be created outside the application directory, found %d entries", len(entries))
	}
}

func TestEnvAccess_ReplacesSymlinkInsideApplicationDir(t *testing.T) {
	c := newTestClient(t)
	dir := setupEnvApp(t, c, map[string][]envTestFile{"web": {required(".env")}})
	writeTestFile(t, filepath.Join(dir, "real.env"), "A=1\n")
	// Absolute targets always count as leaving the directory.
	symlinkOrSkip(t, "real.env", filepath.Join(dir, ".env"))

	err := c.UpdateEnv(t.Context(), envTestApp, []*messages.EnvFileChanges{
		envUpdate(".env", listEnvFile(t, c, ".env").Revision, setChange("A", "2", true)),
	})
	if err != nil {
		t.Fatalf("UpdateEnv: %v", err)
	}
	if got := readTestFile(t, filepath.Join(dir, ".env")); got != "A=2\n" {
		t.Fatalf(".env: %q", got)
	}
	if got := readTestFile(t, filepath.Join(dir, "real.env")); got != "A=1\n" {
		t.Fatalf("expected the update to replace the link, not write through it, got %q", got)
	}
}

func TestEnvAccess_RejectsSymlinkedApplicationDir(t *testing.T) {
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()
	target := t.TempDir()
	writeTestFile(t, filepath.Join(target, composeFileName), "services: {}\n")
	symlinkOrSkip(t, target, filepath.Join(c.deploymentsDir, envTestApp))
	stubEnvProject(t, target, map[string][]envTestFile{"web": {required(".env")}})

	if _, err := c.ListEnv(t.Context(), envTestApp); err == nil {
		t.Fatal("expected a symlinked application directory to be rejected")
	}
}

func TestEnvAccess_IgnoresFilesThatAreNotRegular(t *testing.T) {
	c := newTestClient(t)
	dir := setupEnvApp(t, c, map[string][]envTestFile{"web": {required("dir.env")}})
	if err := os.Mkdir(filepath.Join(dir, "dir.env"), 0o750); err != nil {
		t.Fatal(err)
	}

	if _, err := c.ListEnv(t.Context(), envTestApp); err == nil {
		t.Fatal("expected a directory not to be read as an env file")
	}
}

func TestEnvAccess_RejectsOversizedFile(t *testing.T) {
	c := newTestClient(t)
	dir := setupEnvApp(t, c, map[string][]envTestFile{"web": {required(".env")}})
	writeTestFile(t, filepath.Join(dir, ".env"), strings.Repeat("A=1\n", envrules.MaxFileBytes/4+1))

	if _, err := c.ListEnv(t.Context(), envTestApp); err == nil {
		t.Fatal("expected an oversized env file to be rejected")
	}
}

func TestRollbackEnvWrites(t *testing.T) {
	c := newTestClient(t)
	dir := setupEnvApp(t, c, nil)
	writeTestFile(t, filepath.Join(dir, "existing.env"), "NEW=1\n")
	writeTestFile(t, filepath.Join(dir, "created.env"), "NEW=1\n")
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()

	c.rollbackEnvWrites(root, []envWrite{
		{path: "existing.env", old: []byte("OLD=1\n"), existed: true},
		{path: "created.env"},
	})

	if got := readTestFile(t, filepath.Join(dir, "existing.env")); got != "OLD=1\n" {
		t.Fatalf("existing.env: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "created.env")); err == nil {
		t.Fatal("expected a file that did not exist before to be removed")
	}
}

func TestWriteRootFileAtomic_LeavesNoTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()

	for _, content := range []string{"A=1\n", "A=2\n"} {
		if err := writeRootFileAtomic(root, "sub/dir/app.env", []byte(content)); err != nil {
			t.Fatalf("writeRootFileAtomic: %v", err)
		}
	}

	entries, err := os.ReadDir(filepath.Join(dir, "sub", "dir"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "app.env" {
		t.Fatalf("expected only app.env, got %v", entries)
	}
	if got := readTestFile(t, filepath.Join(dir, "sub", "dir", "app.env")); got != "A=2\n" {
		t.Fatalf("app.env: %q", got)
	}
}

func TestRedeploy_DeploysComposeFileFromDisk(t *testing.T) {
	c := newTestClient(t)
	dir := setupEnvApp(t, c, map[string][]envTestFile{"web": {required(".env")}})
	writeTestFile(t, filepath.Join(dir, composeFileName), "services:\n  web:\n    image: img:1\n")
	c.mu.Lock()
	c.ready = true
	c.hostDirDetectionComplete = true
	c.mu.Unlock()

	originalUp := upProject
	t.Cleanup(func() { upProject = originalUp })
	var gotProject string
	var gotOptions api.UpOptions
	upProject = func(_ context.Context, _ api.Compose, project *composetypes.Project, options api.UpOptions) error {
		gotProject = project.Name
		gotOptions = options
		return nil
	}

	if err := c.Redeploy(t.Context(), "app-1", envTestApp); err != nil {
		t.Fatalf("Redeploy: %v", err)
	}
	if gotProject != envTestApp {
		t.Fatalf("expected %q to be deployed, got %q", envTestApp, gotProject)
	}
	if gotOptions.Create.Recreate != api.RecreateDiverged {
		t.Fatalf("expected only changed services to be recreated, got %q", gotOptions.Create.Recreate)
	}
	if got := readTestFile(t, filepath.Join(dir, composeFileName)); got != "services:\n  web:\n    image: img:1\n" {
		t.Fatalf("the compose file changed: %q", got)
	}
}

func TestRedeploy_ComposeFileMissing(t *testing.T) {
	c := newTestClient(t)
	dir := setupEnvApp(t, c, nil)
	if err := os.Remove(filepath.Join(dir, composeFileName)); err != nil {
		t.Fatal(err)
	}

	if err := c.Redeploy(t.Context(), "app-1", envTestApp); !errors.Is(err, errComposeFileMissing) {
		t.Fatalf("expected errComposeFileMissing, got %v", err)
	}
}

// deployEnvApp runs Deploy for a project whose services declare the given env
// files, and returns the application directory.
func deployEnvApp(t *testing.T, c *Client, services map[string][]envTestFile) (string, error) {
	t.Helper()
	dir := setupEnvApp(t, c, services)
	if err := os.Remove(filepath.Join(dir, composeFileName)); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	c.ready = true
	c.hostDirDetectionComplete = true
	c.mu.Unlock()

	originalUp := upProject
	t.Cleanup(func() { upProject = originalUp })
	upProject = func(context.Context, api.Compose, *composetypes.Project, api.UpOptions) error { return nil }

	err := c.Deploy(t.Context(), DeployRequest{ApplicationID: "app-1", ApplicationName: envTestApp, ComposeFile: "services: {}\n"})
	return dir, err
}

func TestDeploy_CreatesMissingRequiredEnvFiles(t *testing.T) {
	c := newTestClient(t)

	dir, err := deployEnvApp(t, c, map[string][]envTestFile{
		"web": {required(".env"), required("config/app.env"), {path: "optional.env"}},
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	for _, name := range []string{".env", "config/app.env"} {
		info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("expected %s to be created: %v", name, err)
		}
		if info.Size() != 0 {
			t.Fatalf("expected %s to be empty", name)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
			t.Fatalf("expected mode 0600 for %s, got %v", name, info.Mode().Perm())
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "optional.env")); err == nil {
		t.Fatal("an optional env file must not be created")
	}
}

func TestDeploy_DoesNotCreateEnvFilesOutsideApplicationDir(t *testing.T) {
	c := newTestClient(t)
	outside := filepath.Join(t.TempDir(), "outside.env")

	_, err := deployEnvApp(t, c, map[string][]envTestFile{"web": {required(outside)}})
	if err == nil {
		t.Fatal("expected Compose to report the missing env file")
	}
	if _, statErr := os.Stat(outside); statErr == nil {
		t.Fatal("an env file outside the application directory must not be created")
	}
}

func TestDeploy_KeepsExistingEnvFiles(t *testing.T) {
	c := newTestClient(t)
	dir := setupEnvApp(t, c, map[string][]envTestFile{"web": {required(".env")}})
	writeTestFile(t, filepath.Join(dir, ".env"), "KEEP=1\n")
	c.mu.Lock()
	c.ready = true
	c.hostDirDetectionComplete = true
	c.mu.Unlock()
	originalUp := upProject
	t.Cleanup(func() { upProject = originalUp })
	upProject = func(context.Context, api.Compose, *composetypes.Project, api.UpOptions) error { return nil }

	if err := c.Deploy(t.Context(), DeployRequest{ApplicationID: "app-1", ApplicationName: envTestApp, ComposeFile: "services: {}\n"}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if got := readTestFile(t, filepath.Join(dir, ".env")); got != "KEEP=1\n" {
		t.Fatalf(".env: %q", got)
	}
}

func TestDeploy_DoesNotCreateEnvFilesOfRejectedComposeFile(t *testing.T) {
	c := newTestClient(t)
	setupDir := func() string {
		dir := setupEnvApp(t, c, nil)
		stubEnvProject(t, dir, nil)
		loadProject = func(_ context.Context, _ api.Compose, opts api.ProjectLoadOptions) (*composetypes.Project, error) {
			return &composetypes.Project{
				Name: opts.ProjectName,
				Services: composetypes.Services{"web": composetypes.ServiceConfig{
					Privileged: true,
					EnvFiles:   []composetypes.EnvFile{{Path: filepath.Join(dir, ".env"), Required: true}},
				}},
			}, nil
		}
		return dir
	}
	dir := setupDir()
	if err := os.Remove(filepath.Join(dir, composeFileName)); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	c.ready = true
	c.hostDirDetectionComplete = true
	c.mu.Unlock()

	err := c.Deploy(t.Context(), DeployRequest{ApplicationID: "app-1", ApplicationName: envTestApp, ComposeFile: "services: {}\n"})
	if err == nil {
		t.Fatal("expected the privileged service to be rejected")
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".env")); statErr == nil {
		t.Fatal("the env file of a rejected compose file must not be created")
	}
}

func TestDeploy_LoadsProjectWithoutResolvingEnvironment(t *testing.T) {
	c := newTestClient(t)
	var got api.ProjectLoadOptions
	dir, err := deployEnvApp(t, c, nil)
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	_ = dir

	loadProject = func(_ context.Context, _ api.Compose, opts api.ProjectLoadOptions) (*composetypes.Project, error) {
		got = opts
		return &composetypes.Project{Name: opts.ProjectName}, nil
	}
	if err := c.Deploy(t.Context(), DeployRequest{ApplicationID: "app-1", ApplicationName: envTestApp, ComposeFile: "services: {}\n"}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if len(got.ProjectOptionsFns) != 1 {
		t.Fatalf("expected the environment resolution to be skipped until the env files exist, got %d project options", len(got.ProjectOptionsFns))
	}
}

// Uses the real Compose loader instead of a stub, to cover how it resolves
// env_file paths and that a missing env file does not fail the load.
func TestListEnv_WithRealComposeLoader(t *testing.T) {
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()
	dir := filepath.Join(c.deploymentsDir, envTestApp)
	writeTestFile(t, filepath.Join(dir, composeFileName), `services:
  web:
    image: img:1
    env_file:
      - .env
      - path: ./config/../optional.env
        required: false
      - path: raw.env
        format: raw
      - ../elsewhere.env
  worker:
    image: img:1
    env_file: .env
`)
	writeTestFile(t, filepath.Join(dir, ".env"), "# orcacd:public\nA=1\n")

	files, err := c.ListEnv(t.Context(), envTestApp)
	if err != nil {
		t.Fatalf("ListEnv: %v", err)
	}
	var got []string
	for _, file := range files {
		got = append(got, file.Path+"="+strings.Join(file.Services, "+"))
	}
	if want := ".env=web+worker,optional.env=web"; strings.Join(got, ",") != want {
		t.Fatalf("expected %q, got %q", want, strings.Join(got, ","))
	}
}

func TestDeploy_WithRealComposeLoaderCreatesRequiredEnvFile(t *testing.T) {
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()
	dir := filepath.Join(c.deploymentsDir, envTestApp)
	c.mu.Lock()
	c.ready = true
	c.hostDirDetectionComplete = true
	c.mu.Unlock()

	originalUp := upProject
	t.Cleanup(func() { upProject = originalUp })
	var environment map[string]*string
	upProject = func(_ context.Context, _ api.Compose, project *composetypes.Project, _ api.UpOptions) error {
		environment = project.Services["web"].Environment
		return nil
	}

	err := c.Deploy(t.Context(), DeployRequest{
		ApplicationID:   "app-1",
		ApplicationName: envTestApp,
		ComposeFile:     "services:\n  web:\n    image: img:1\n    env_file:\n      - .env\n      - path: optional.env\n        required: false\n",
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".env")); err != nil {
		t.Fatalf("expected the required env file to be created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "optional.env")); err == nil {
		t.Fatal("an optional env file must not be created")
	}

	// A second deploy picks up the values written in between.
	writeTestFile(t, filepath.Join(dir, ".env"), "GREETING=hello world\n")
	if err := c.Redeploy(t.Context(), "app-1", envTestApp); err != nil {
		t.Fatalf("Redeploy: %v", err)
	}
	if value := environment["GREETING"]; value == nil || *value != "hello world" {
		t.Fatalf("expected the env file to be part of the deployed environment, got %v", environment)
	}
}

func TestOpenApplicationDir_Errors(t *testing.T) {
	t.Run("deployments directory not configured", func(t *testing.T) {
		c := newTestClient(t)
		c.deploymentsDir = ""

		if _, err := c.ListEnv(t.Context(), envTestApp); err == nil || !strings.Contains(err.Error(), "not configured") {
			t.Fatalf("expected a configuration error, got %v", err)
		}
	})

	t.Run("name without usable characters", func(t *testing.T) {
		c := newTestClient(t)
		c.deploymentsDir = t.TempDir()

		if _, err := c.ListEnv(t.Context(), "!!!"); err == nil {
			t.Fatal("expected an error")
		}
	})

	t.Run("application directory is a file", func(t *testing.T) {
		c := newTestClient(t)
		c.deploymentsDir = t.TempDir()
		writeTestFile(t, filepath.Join(c.deploymentsDir, envTestApp), "not a directory")

		if _, err := c.ListEnv(t.Context(), envTestApp); err == nil || !strings.Contains(err.Error(), "not a directory") {
			t.Fatalf("expected a not-a-directory error, got %v", err)
		}
	})
}

func TestEnvEntries_SkipsEntriesThatCannotBeShown(t *testing.T) {
	entries := envEntries(dotenv.Parse("# " + dotenv.PublicMarker + "\nVALID=1\n# " + dotenv.PublicMarker + "\nBAD:KEY=2\n# " + dotenv.PublicMarker + "\nINVALID_UTF8=\xff\xfe\nSECRET=hidden\n"))

	got := map[string]*messages.EnvEntry{}
	for _, entry := range entries {
		got[entry.Key] = entry
	}
	if _, ok := got["BAD:KEY"]; ok {
		t.Fatal("expected a key compose does not accept to be skipped")
	}
	if _, ok := got["INVALID_UTF8"]; ok {
		t.Fatal("expected a value that is not valid UTF-8 to be skipped")
	}
	if got["VALID"] == nil || got["VALID"].Value != "1" {
		t.Fatalf("expected VALID to be listed, got %v", entries)
	}
}

func TestUpdateEnv_NotDeployed(t *testing.T) {
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	err := c.UpdateEnv(t.Context(), envTestApp, []*messages.EnvFileChanges{envUpdate(".env", "rev")})
	if !errors.Is(err, errApplicationNotDeployed) {
		t.Fatalf("expected errApplicationNotDeployed, got %v", err)
	}
}

func TestUpdateEnv_ComposeFileDoesNotLoad(t *testing.T) {
	c := newTestClient(t)
	setupEnvApp(t, c, nil)
	loadProject = func(context.Context, api.Compose, api.ProjectLoadOptions) (*composetypes.Project, error) {
		return nil, errors.New("yaml: invalid")
	}

	err := c.UpdateEnv(t.Context(), envTestApp, []*messages.EnvFileChanges{envUpdate(".env", "rev")})
	if err == nil || !strings.Contains(err.Error(), "yaml: invalid") {
		t.Fatalf("expected the load error, got %v", err)
	}
}

func TestUpdateEnv_CanceledContextWritesNothing(t *testing.T) {
	c := newTestClient(t)
	dir := setupEnvApp(t, c, map[string][]envTestFile{"web": {required(".env")}})
	writeTestFile(t, filepath.Join(dir, ".env"), "A=1\n")
	revision := listEnvFile(t, c, ".env").Revision

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := c.UpdateEnv(ctx, envTestApp, []*messages.EnvFileChanges{envUpdate(".env", revision, setChange("A", "2", true))})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if got := readTestFile(t, filepath.Join(dir, ".env")); got != "A=1\n" {
		t.Fatalf("expected the file to stay untouched, got %q", got)
	}
}

func TestUpdateEnv_RejectsResultLargerThanLimit(t *testing.T) {
	c := newTestClient(t)
	dir := setupEnvApp(t, c, map[string][]envTestFile{"web": {required(".env")}})
	value := strings.Repeat("a", envrules.MaxValueBytes-100)
	var existing strings.Builder
	for i := range 60 {
		fmt.Fprintf(&existing, "%s=%s\n", numberedKey(i), value)
	}
	writeTestFile(t, filepath.Join(dir, ".env"), existing.String())
	revision := listEnvFile(t, c, ".env").Revision

	var changes []*messages.EnvChange
	for i := 100; i < 106; i++ {
		changes = append(changes, setChange(numberedKey(i), value, false))
	}

	err := c.UpdateEnv(t.Context(), envTestApp, []*messages.EnvFileChanges{envUpdate(".env", revision, changes...)})
	if err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("expected a size error, got %v", err)
	}
	if got := readTestFile(t, filepath.Join(dir, ".env")); got != existing.String() {
		t.Fatal("expected the file to stay untouched")
	}
}

func TestRedeploy_Errors(t *testing.T) {
	t.Run("not deployed", func(t *testing.T) {
		c := newTestClient(t)
		c.deploymentsDir = t.TempDir()

		if err := c.Redeploy(t.Context(), "app-1", envTestApp); !errors.Is(err, errApplicationNotDeployed) {
			t.Fatalf("expected errApplicationNotDeployed, got %v", err)
		}
	})

	t.Run("compose file cannot be read", func(t *testing.T) {
		c := newTestClient(t)
		dir := setupEnvApp(t, c, nil)
		if err := os.Remove(filepath.Join(dir, composeFileName)); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(dir, composeFileName), 0o750); err != nil {
			t.Fatal(err)
		}

		err := c.Redeploy(t.Context(), "app-1", envTestApp)
		if err == nil || errors.Is(err, errComposeFileMissing) {
			t.Fatalf("expected a read error, got %v", err)
		}
	})
}

func TestCreateMissingEnvFiles_Errors(t *testing.T) {
	t.Run("application directory does not exist", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "missing")
		project := &composetypes.Project{Services: composetypes.Services{
			"web": {Name: "web", EnvFiles: []composetypes.EnvFile{{Path: filepath.Join(dir, ".env"), Required: true}}},
		}}

		if err := createMissingEnvFiles(dir, project); err == nil {
			t.Fatal("expected an error")
		}
	})

	t.Run("parent of the env file is a file", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, "blocker"), "x")
		project := &composetypes.Project{Services: composetypes.Services{
			"web": {Name: "web", EnvFiles: []composetypes.EnvFile{{Path: filepath.Join(dir, "blocker", "sub", ".env"), Required: true}}},
		}}

		if err := createMissingEnvFiles(dir, project); err == nil {
			t.Fatal("expected an error")
		}
		if _, err := resolveEnvFiles(dir, project); err == nil || !strings.Contains(err.Error(), "create missing env files") {
			t.Fatalf("expected resolveEnvFiles to report the error, got %v", err)
		}
	})
}
