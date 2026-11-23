package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/apitoken"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: gormlogger.Discard,
	})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})
	return db
}

func TestRunMigrations_Succeeds(t *testing.T) {
	db := openTestDB(t)
	if err := runMigrations(db); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}
}

func TestRunMigrations_Idempotent(t *testing.T) {
	db := openTestDB(t)
	if err := runMigrations(db); err != nil {
		t.Fatalf("first runMigrations() error: %v", err)
	}
	if err := runMigrations(db); err != nil {
		t.Fatalf("second runMigrations() error: %v", err)
	}
}

func TestRunMigrations_AllTablesExist(t *testing.T) {
	db := openTestDB(t)
	if err := runMigrations(db); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}

	tables := []string{
		"agents",
		"users",
		"oidc_providers",
		"user_oidc_identities",
		"repositories",
		"applications",
		"notifications",
		"application_notifications",
		"agent_notifications",
		"repository_notifications",
		"application_events",
		"github_actions_triggers",
		"github_actions_trigger_skips",
		"application_deployments",
		"roles",
		"role_permissions",
		"user_roles",
		"api_tokens",
	}
	for _, table := range tables {
		if !db.Migrator().HasTable(table) {
			t.Errorf("expected table %q to exist after migrations", table)
		}
	}
}

func columnNames(t *testing.T, sqlDB *sql.DB, table string) map[string]bool {
	t.Helper()
	rows, err := sqlDB.Query("SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		t.Fatalf("failed to query columns for table %q: %v", table, err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Fatalf("failed to close rows: %v", err)
		}
	}()

	cols := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("failed to scan column name: %v", err)
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows error: %v", err)
	}
	return cols
}

func TestRunMigrations_AgentsTableSchema(t *testing.T) {
	gormDB := openTestDB(t)
	if err := runMigrations(gormDB); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}
	cols := columnNames(t, sqlDB, "agents")

	// migration 001: base columns; migration 003: secret renamed to key_id
	required := []string{"id", "name", "icon", "key_id", "status", "last_seen", "created_at", "updated_at", "allowed_ips", "last_ip"}
	for _, col := range required {
		if !cols[col] {
			t.Errorf("agents table missing column %q", col)
		}
	}
	if cols["secret"] {
		t.Error("agents table should not have column 'secret' (renamed to key_id by migration 003)")
	}
}

func TestRunMigrations_UsersTableSchema(t *testing.T) {
	gormDB := openTestDB(t)
	if err := runMigrations(gormDB); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}
	cols := columnNames(t, sqlDB, "users")
	if cols["password_hash"] || cols["password_change_required"] {
		t.Fatal("legacy password columns remain")
	}

	// Local authentication is explicit after the passkey migration.
	required := []string{
		"id", "email", "name", "local_auth_enabled",
		"oidc_subject", "oidc_issuer",
		"created_at", "updated_at",
	}
	for _, col := range required {
		if !cols[col] {
			t.Errorf("users table missing column %q", col)
		}
	}
	if cols["auth_provider"] {
		t.Error("users table should not have column 'auth_provider' (dropped by migration 007)")
	}
	if cols["role"] {
		t.Error("users table should not have column 'role' (replaced by user_roles in migration 041)")
	}
}

func TestPasskeyMigrationPreservesAccountTypesAndCascades(t *testing.T) {
	database := openTestDB(t)
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	m := newTestMigrator(t, sqlDB)
	if err := m.Migrate(46); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO users (id, name, email, password_hash, token_version)
        VALUES ('local', 'Local', 'local@example.com', 'legacy-hash', 3), ('sso', 'SSO', 'sso@example.com', NULL, 4)`); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO api_tokens (id, user_id, name, token_hash, prefix, expires_at)
        VALUES ('existing-token', 'local', 'Automation', 'token-digest', 'orca_', datetime('now', '+1 day'))`); err != nil {
		t.Fatal(err)
	}
	if err := m.Up(); err != nil {
		t.Fatal(err)
	}
	var local, sso bool
	if err := sqlDB.QueryRow("SELECT local_auth_enabled FROM users WHERE id = 'local'").Scan(&local); err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.QueryRow("SELECT local_auth_enabled FROM users WHERE id = 'sso'").Scan(&sso); err != nil {
		t.Fatal(err)
	}
	if !local || sso {
		t.Fatal("account type lost during migration")
	}
	var version int
	if err := sqlDB.QueryRow("SELECT token_version FROM users WHERE id = 'local'").Scan(&version); err != nil || version != 3 {
		t.Fatal("existing sessions should survive the upgrade")
	}
	var tokenCount int
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM api_tokens WHERE id = 'existing-token' AND user_id = 'local'").Scan(&tokenCount); err != nil || tokenCount != 1 {
		t.Fatal("existing API tokens should survive the upgrade")
	}
	if _, err := sqlDB.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO passkeys (id, user_id, credential_id, credential, name, created_at, updated_at)
        VALUES ('key', 'local', 'credential', 'encrypted', 'Laptop', datetime('now'), datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO passkey_enrollments (user_id, token_hash, token_version, expires_at)
        VALUES ('local', 'digest', 3, datetime('now', '+1 day'))`); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec("DELETE FROM users WHERE id = 'local'"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"passkeys", "passkey_enrollments", "api_tokens"} {
		var count int
		if err := sqlDB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("orphan in %s: %v", table, err)
		}
	}
	if err := m.Migrate(49); err != nil {
		t.Fatal(err)
	}
	if !columnNames(t, sqlDB, "users")["password_hash"] {
		t.Fatal("down migration did not restore schema")
	}
	if !columnNames(t, sqlDB, "audit_logs")["api_token_id"] {
		t.Fatal("password cleanup rollback removed API token audit data")
	}
	if err := m.Up(); err != nil {
		t.Fatal(err)
	}
}

func newTestMigrator(t *testing.T, sqlDB *sql.DB) *migrate.Migrate {
	t.Helper()
	m, err := newMigrator(sqlDB, migrationFiles, "migrations")
	if err != nil {
		t.Fatalf("failed to create migrator: %v", err)
	}
	return m
}

func TestRunMigrations_RolesKeepExistingAccess(t *testing.T) {
	gormDB := openTestDB(t)
	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}

	m := newTestMigrator(t, sqlDB)
	if err := m.Migrate(40); err != nil {
		t.Fatalf("failed to migrate to version 40: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO users (id, email, name, role, password_change_required)
		VALUES ('admin', 'admin@example.com', 'Admin', 'admin', 0), ('user', 'user@example.com', 'User', 'user', 0)`); err != nil {
		t.Fatalf("failed to seed users: %v", err)
	}

	if err := m.Migrate(41); err != nil {
		t.Fatalf("failed to migrate to version 41: %v", err)
	}

	roleOf := func(userID string) string {
		t.Helper()
		var builtIn string
		if err := sqlDB.QueryRow(`SELECT roles.built_in FROM user_roles JOIN roles ON roles.id = user_roles.role_id
			WHERE user_roles.user_id = ? AND user_roles.source = 'manual'`, userID).Scan(&builtIn); err != nil {
			t.Fatalf("failed to query role of %s: %v", userID, err)
		}
		return builtIn
	}
	if got := roleOf("admin"); got != "admin" {
		t.Errorf("expected admin to get the admin role, got %q", got)
	}
	if got := roleOf("user"); got != "user" {
		t.Errorf("expected user to get the user role, got %q", got)
	}

	// The user role must allow every action on every resource, like before roles existed.
	var grants int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM role_permissions JOIN roles ON roles.id = role_permissions.role_id
		WHERE roles.built_in = 'user' AND role_permissions.resource_id IS NULL`).Scan(&grants); err != nil {
		t.Fatalf("failed to count grants: %v", err)
	}
	want := 0
	for resource, actions := range models.PermissionActions {
		// Added after roles and not granted to the user role.
		if resource != models.ResourceAPIToken {
			want += len(actions)
		}
	}
	if grants != want {
		t.Errorf("expected the user role to have %d grants, got %d", want, grants)
	}

	if err := m.Migrate(40); err != nil {
		t.Fatalf("failed to migrate down to version 40: %v", err)
	}
	var role string
	if err := sqlDB.QueryRow(`SELECT role FROM users WHERE id = 'admin'`).Scan(&role); err != nil {
		t.Fatalf("failed to query role: %v", err)
	}
	if role != "admin" {
		t.Errorf("expected the admin to keep the admin role after migrating down, got %q", role)
	}
	if gormDB.Migrator().HasTable("roles") {
		t.Error("expected roles table to be dropped")
	}
}

func TestRunMigrations_OIDCProvidersTableSchema(t *testing.T) {
	gormDB := openTestDB(t)
	if err := runMigrations(gormDB); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}
	cols := columnNames(t, sqlDB, "oidc_providers")

	// migration 005: base; 008: require_verified_email + auto_signup
	required := []string{
		"id", "name", "issuer_url", "client_id", "client_secret", "scopes", "enabled",
		"require_verified_email", "auto_signup",
		"created_at", "updated_at",
	}
	for _, col := range required {
		if !cols[col] {
			t.Errorf("oidc_providers table missing column %q", col)
		}
	}
}

func TestRunMigrations_UserOIDCIdentitiesTableSchema(t *testing.T) {
	gormDB := openTestDB(t)
	if err := runMigrations(gormDB); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}
	cols := columnNames(t, sqlDB, "user_oidc_identities")

	required := []string{"id", "user_id", "provider_id", "subject", "created_at", "updated_at"}
	for _, col := range required {
		if !cols[col] {
			t.Errorf("user_oidc_identities table missing column %q", col)
		}
	}
}

func TestRunMigrations_RepositoriesTableSchema(t *testing.T) {
	gormDB := openTestDB(t)
	if err := runMigrations(gormDB); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}
	cols := columnNames(t, sqlDB, "repositories")

	required := []string{
		"id", "name", "url", "provider",
		"auth_method", "auth_user", "auth_token",
		"sync_type", "sync_status", "last_sync_error",
		"polling_interval", "webhook_secret", "last_synced_at",
		"created_by", "created_at", "updated_at",
	}
	for _, col := range required {
		if !cols[col] {
			t.Errorf("repositories table missing column %q", col)
		}
	}
}

func TestRunMigrations_ApplicationsTableSchema(t *testing.T) {
	gormDB := openTestDB(t)
	if err := runMigrations(gormDB); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}
	cols := columnNames(t, sqlDB, "applications")

	required := []string{
		"id", "name", "icon", "repository_id", "agent_id",
		"sync_status", "health_status",
		"branch", "commit", "commit_message",
		"last_synced_at", "path", "compose_file", "previous_compose_file",
		"created_at", "updated_at",
	}
	for _, col := range required {
		if !cols[col] {
			t.Errorf("applications table missing column %q", col)
		}
	}
}

func TestRunMigrations_NotificationsTableSchema(t *testing.T) {
	gormDB := openTestDB(t)
	if err := runMigrations(gormDB); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}
	cols := columnNames(t, sqlDB, "notifications")

	required := []string{
		"id", "name", "enabled", "all_applications", "all_agents", "all_repositories", "events",
		"status", "type", "config",
		"created_at", "updated_at",
	}
	for _, col := range required {
		if !cols[col] {
			t.Errorf("notifications table missing column %q", col)
		}
	}
}

func TestRunMigrations_NotificationEventsBackfill(t *testing.T) {
	gormDB := openTestDB(t)
	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}

	driver, err := sqlite3.WithInstance(sqlDB, &sqlite3.Config{})
	if err != nil {
		t.Fatalf("failed to create migrate driver: %v", err)
	}
	src, err := iofs.New(migrationFiles, "migrations")
	if err != nil {
		t.Fatalf("failed to open migration source: %v", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "sqlite3", driver)
	if err != nil {
		t.Fatalf("failed to create migrator: %v", err)
	}
	if err := m.Migrate(28); err != nil {
		t.Fatalf("failed to migrate to version 28: %v", err)
	}

	seed := []string{
		`INSERT INTO agents (id, name, key_id) VALUES ('agent', 'a', 'k')`,
		`INSERT INTO repositories (id, name, url, provider, auth_method, sync_type, created_by)
			VALUES ('repo', 'r', 'https://example.com/r.git', 'generic', 'none', 'manual', 'user')`,
		`INSERT INTO applications (id, name, repository_id, agent_id, sync_status, health_status, branch, "commit", commit_message, path)
			VALUES ('app-1', 'a', 'repo', 'agent', 'unknown', 'unknown', 'main', 'c', 'm', 'p'),
				('app-2', 'a', 'repo', 'agent', 'unknown', 'unknown', 'main', 'c', 'm', 'p')`,
		`INSERT INTO notifications (id, name, enabled, enable_by_default, status, type, config)
			VALUES ('covering', 'n', 1, 1, 'unknown', 'discord', '{}'),
				('partial', 'n', 1, 1, 'unknown', 'discord', '{}'),
				('assigned', 'n', 1, 0, 'unknown', 'discord', '{}')`,
		`INSERT INTO application_notifications (application_id, notification_id)
			VALUES ('app-1', 'covering'), ('app-2', 'covering'), ('app-1', 'partial'), ('app-2', 'assigned')`,
	}
	for _, stmt := range seed {
		if _, err := sqlDB.Exec(stmt); err != nil {
			t.Fatalf("failed to seed data: %v", err)
		}
	}

	if err := runMigrations(gormDB); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}

	// Raw query: the seeded names are not encrypted, so loading the model would fail.
	rows, err := sqlDB.Query(`SELECT id, all_applications, events FROM notifications WHERE all_agents = 0 AND all_repositories = 0`)
	if err != nil {
		t.Fatalf("failed to query notifications: %v", err)
	}
	defer func() { _ = rows.Close() }()

	// Only a default notification already attached to every application becomes all_applications.
	wantAllApplications := map[string]bool{"covering": true, "partial": false, "assigned": false}
	// Existing notifications keep the application events they received before
	// events became selectable; events added later are not backfilled.
	wantEvents, err := json.Marshal([]models.NotificationEvent{
		models.NotificationEventDeploymentSucceeded,
		models.NotificationEventDeploymentFailed,
		models.NotificationEventImageUpdateSucceeded,
		models.NotificationEventImageUpdateFailed,
		models.NotificationEventSyncFailed,
	})
	if err != nil {
		t.Fatalf("failed to marshal events: %v", err)
	}
	count := 0
	for rows.Next() {
		var id, events string
		var allApplications bool
		if err := rows.Scan(&id, &allApplications, &events); err != nil {
			t.Fatalf("failed to scan notification: %v", err)
		}
		count++
		if allApplications != wantAllApplications[id] {
			t.Errorf("notification %q: expected all_applications=%v, got %v", id, wantAllApplications[id], allApplications)
		}
		if events != string(wantEvents) {
			t.Errorf("notification %q: expected events %s, got %s", id, wantEvents, events)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("failed to iterate notifications: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3 notifications, got %d", count)
	}

	assocRows, err := sqlDB.Query(`SELECT application_id || ':' || notification_id FROM application_notifications ORDER BY 1`)
	if err != nil {
		t.Fatalf("failed to query application_notifications: %v", err)
	}
	defer func() { _ = assocRows.Close() }()

	var associations []string
	for assocRows.Next() {
		var association string
		if err := assocRows.Scan(&association); err != nil {
			t.Fatalf("failed to scan association: %v", err)
		}
		associations = append(associations, association)
	}
	if err := assocRows.Err(); err != nil {
		t.Fatalf("failed to iterate associations: %v", err)
	}
	wantAssociations := []string{"app-1:partial", "app-2:assigned"}
	if !slices.Equal(associations, wantAssociations) {
		t.Fatalf("expected associations %v, got %v", wantAssociations, associations)
	}
}

func TestRunMigrations_ApplicationNotificationsTableSchema(t *testing.T) {
	gormDB := openTestDB(t)
	if err := runMigrations(gormDB); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}
	cols := columnNames(t, sqlDB, "application_notifications")

	required := []string{"application_id", "notification_id"}
	for _, col := range required {
		if !cols[col] {
			t.Errorf("application_notifications table missing column %q", col)
		}
	}
}

func TestRunMigrations_AgentAndRepositoryNotificationsTableSchema(t *testing.T) {
	gormDB := openTestDB(t)
	if err := runMigrations(gormDB); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}

	for table, required := range map[string][]string{
		"agent_notifications":      {"agent_id", "notification_id"},
		"repository_notifications": {"repository_id", "notification_id"},
	} {
		cols := columnNames(t, sqlDB, table)
		for _, col := range required {
			if !cols[col] {
				t.Errorf("%s table missing column %q", table, col)
			}
		}
	}
}

func TestRunMigrations_AgentAndRepositoryNotificationsCascade(t *testing.T) {
	// Cascades need foreign keys enabled, like the production DSN does.
	gormDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")+"?_foreign_keys=ON"), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := gormDB.DB()
		_ = sqlDB.Close()
	})
	if err := runMigrations(gormDB); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}

	statements := []string{
		`INSERT INTO agents (id, name, key_id) VALUES ('agent', 'a', 'k')`,
		`INSERT INTO repositories (id, name, url, provider, auth_method, sync_type, sync_status, created_by)
			VALUES ('repo', 'r', 'https://example.com/r', 'github', 'none', 'manual', 'unknown', 'u')`,
		`INSERT INTO notifications (id, name, status, type, config) VALUES ('n1', 'n', 'unknown', 'discord', '{}'), ('n2', 'n', 'unknown', 'discord', '{}')`,
		`INSERT INTO agent_notifications (agent_id, notification_id) VALUES ('agent', 'n1'), ('agent', 'n2')`,
		`INSERT INTO repository_notifications (repository_id, notification_id) VALUES ('repo', 'n1'), ('repo', 'n2')`,
		`DELETE FROM notifications WHERE id = 'n1'`,
	}
	for _, stmt := range statements {
		if _, err := sqlDB.Exec(stmt); err != nil {
			t.Fatalf("failed to execute %q: %v", stmt, err)
		}
	}

	assertRowCount := func(query string, want int) {
		t.Helper()
		var got int
		if err := sqlDB.QueryRow(query).Scan(&got); err != nil {
			t.Fatalf("failed to query %q: %v", query, err)
		}
		if got != want {
			t.Errorf("%q: expected %d rows, got %d", query, want, got)
		}
	}
	assertRowCount(`SELECT COUNT(*) FROM agent_notifications`, 1)
	assertRowCount(`SELECT COUNT(*) FROM repository_notifications`, 1)

	if _, err := sqlDB.Exec(`DELETE FROM agents WHERE id = 'agent'`); err != nil {
		t.Fatalf("failed to delete agent: %v", err)
	}
	if _, err := sqlDB.Exec(`DELETE FROM repositories WHERE id = 'repo'`); err != nil {
		t.Fatalf("failed to delete repository: %v", err)
	}
	assertRowCount(`SELECT COUNT(*) FROM agent_notifications`, 0)
	assertRowCount(`SELECT COUNT(*) FROM repository_notifications`, 0)
}

func TestRunMigrations_APITokens(t *testing.T) {
	// Cascades need foreign keys enabled, like the production DSN does.
	gormDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")+"?_foreign_keys=ON"), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := gormDB.DB()
		_ = sqlDB.Close()
	})
	if err := runMigrations(gormDB); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}
	ctx := t.Context()

	users := []models.User{{Email: "a@example.com", Name: "A"}, {Email: "b@example.com", Name: "B"}}
	if err := gorm.G[models.User](gormDB).CreateInBatches(ctx, &users, len(users)); err != nil {
		t.Fatalf("failed to create users: %v", err)
	}

	expiresAt := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	token := models.APIToken{
		UserId:    users[0].Id,
		Name:      "CI",
		TokenHash: "hash-1",
		Prefix:    "orca_abcdefgh",
		Scopes:    []models.APITokenScope{"application:deploy", models.APITokenScopeAdmin},
		ExpiresAt: expiresAt,
	}
	if err := gorm.G[models.APIToken](gormDB).Create(ctx, &token); err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	got, err := gorm.G[models.APIToken](gormDB).Where("token_hash = ?", "hash-1").First(ctx)
	if err != nil {
		t.Fatalf("failed to fetch token: %v", err)
	}
	if !slices.Equal(got.Scopes, token.Scopes) {
		t.Errorf("expected scopes %v, got %v", token.Scopes, got.Scopes)
	}
	if !got.ExpiresAt.Equal(expiresAt) {
		t.Errorf("expected expires_at %v, got %v", expiresAt, got.ExpiresAt)
	}
	if got.LastUsedAt != nil {
		t.Errorf("expected last_used_at to be empty, got %v", got.LastUsedAt)
	}

	sameHash := models.APIToken{UserId: users[1].Id, Name: "Other", TokenHash: "hash-1", Prefix: "p", ExpiresAt: expiresAt}
	if err := gorm.G[models.APIToken](gormDB).Create(ctx, &sameHash); err == nil {
		t.Error("expected creating a token with an existing hash to fail")
	}
	sameName := models.APIToken{UserId: users[0].Id, Name: "ci", TokenHash: "hash-2", Prefix: "p", ExpiresAt: expiresAt}
	if err := gorm.G[models.APIToken](gormDB).Create(ctx, &sameName); err == nil {
		t.Error("expected creating a second token with the same name for a user to fail")
	}
	otherUser := models.APIToken{UserId: users[1].Id, Name: "CI", TokenHash: "hash-3", Prefix: "p", ExpiresAt: expiresAt}
	if err := gorm.G[models.APIToken](gormDB).Create(ctx, &otherUser); err != nil {
		t.Errorf("expected another user to be able to use the same token name: %v", err)
	}

	if _, err := gorm.G[models.User](gormDB).Where("id = ?", users[0].Id).Delete(ctx); err != nil {
		t.Fatalf("failed to delete user: %v", err)
	}
	remaining, err := gorm.G[models.APIToken](gormDB).Find(ctx)
	if err != nil {
		t.Fatalf("failed to list tokens: %v", err)
	}
	if len(remaining) != 1 || remaining[0].UserId != users[1].Id {
		t.Errorf("expected only the token of the remaining user to be left, got %+v", remaining)
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}
	if err := newTestMigrator(t, sqlDB).Migrate(45); err != nil {
		t.Fatalf("failed to migrate down to version 45: %v", err)
	}
	if gormDB.Migrator().HasTable("api_tokens") {
		t.Error("expected api_tokens table to be dropped")
	}
}

func TestRunMigrations_AuditLogsAPITokenId(t *testing.T) {
	gormDB := openTestDB(t)
	if err := runMigrations(gormDB); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}
	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}

	if !columnNames(t, sqlDB, "audit_logs")["api_token_id"] {
		t.Fatal("audit_logs table missing column \"api_token_id\"")
	}

	if err := newTestMigrator(t, sqlDB).Migrate(48); err != nil {
		t.Fatalf("failed to migrate down to version 48: %v", err)
	}
	if columnNames(t, sqlDB, "audit_logs")["api_token_id"] {
		t.Error("expected api_token_id to be dropped")
	}
}

func TestRunMigrations_AgentAndRepositoryNotificationsDown(t *testing.T) {
	gormDB := openTestDB(t)
	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}

	driver, err := sqlite3.WithInstance(sqlDB, &sqlite3.Config{})
	if err != nil {
		t.Fatalf("failed to create migrate driver: %v", err)
	}
	src, err := iofs.New(migrationFiles, "migrations")
	if err != nil {
		t.Fatalf("failed to open migration source: %v", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "sqlite3", driver)
	if err != nil {
		t.Fatalf("failed to create migrator: %v", err)
	}
	if err := m.Migrate(30); err != nil {
		t.Fatalf("failed to migrate to version 30: %v", err)
	}

	if _, err := sqlDB.Exec(`INSERT INTO notifications (id, name, status, type, config, events)
		VALUES ('n', 'n', 'unknown', 'discord', '{}', '["application.deployment.failed","agent.offline","repository.sync.failed","application.health.unhealthy"]')`); err != nil {
		t.Fatalf("failed to seed notification: %v", err)
	}

	if err := m.Migrate(29); err != nil {
		t.Fatalf("failed to migrate down to version 29: %v", err)
	}

	for _, table := range []string{"agent_notifications", "repository_notifications"} {
		if gormDB.Migrator().HasTable(table) {
			t.Errorf("expected table %q to be dropped", table)
		}
	}
	cols := columnNames(t, sqlDB, "notifications")
	if cols["all_agents"] || cols["all_repositories"] {
		t.Errorf("expected all_agents and all_repositories to be dropped, got %v", cols)
	}

	var events string
	if err := sqlDB.QueryRow(`SELECT events FROM notifications WHERE id = 'n'`).Scan(&events); err != nil {
		t.Fatalf("failed to query events: %v", err)
	}
	if events != `["application.deployment.failed"]` {
		t.Errorf("expected events added in version 30 to be removed, got %s", events)
	}
}

func TestRunMigrations_GitHubActionsTriggersDown(t *testing.T) {
	gormDB := openTestDB(t)
	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatal(err)
	}
	m := newTestMigrator(t, sqlDB)
	if err := m.Migrate(51); err != nil {
		t.Fatalf("migrate to trigger schema: %v", err)
	}
	if err := m.Migrate(50); err != nil {
		t.Fatalf("revert trigger schema: %v", err)
	}
	for _, table := range []string{"github_actions_triggers", "github_actions_trigger_skips"} {
		if gormDB.Migrator().HasTable(table) {
			t.Errorf("expected %q to be dropped", table)
		}
	}
	if columnNames(t, sqlDB, "application_events")["trigger_id"] {
		t.Error("expected application_events.trigger_id to be dropped")
	}
}

func TestRunMigrations_GitHubActionsTriggerSkipsCascadeOnApplicationDelete(t *testing.T) {
	gormDB := openTestDB(t)
	if err := runMigrations(gormDB); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatal(err)
	}
	var onDelete string
	if err := sqlDB.QueryRow(`SELECT on_delete FROM pragma_foreign_key_list('github_actions_trigger_skips')
		WHERE "from" = 'application_id' AND "table" = 'applications' AND "to" = 'id'`).Scan(&onDelete); err != nil {
		t.Fatalf("load application foreign key: %v", err)
	}
	if onDelete != "CASCADE" {
		t.Errorf("application foreign key ON DELETE = %q, want CASCADE", onDelete)
	}
}

func TestRunMigrations_ApplicationEventsTableSchema(t *testing.T) {
	gormDB := openTestDB(t)
	if err := runMigrations(gormDB); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}
	if !gormDB.Migrator().HasTable("application_events") {
		t.Fatal("expected application_events table")
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}
	cols := columnNames(t, sqlDB, "application_events")
	if !cols["trigger_id"] {
		t.Error("application events must have a trigger ID column")
	}
	required := []string{
		"id", "application_id", "request_id", "type", "source", "status",
		"actor_user_id", "actor_name", "commit_hash", "commit_message",
		"error_message", "completed_at", "created_at", "updated_at",
	}
	for _, col := range required {
		if !cols[col] {
			t.Errorf("application_events table missing column %q", col)
		}
	}

	var createSQL string
	if err := sqlDB.QueryRow(
		"SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'application_events'",
	).Scan(&createSQL); err != nil {
		t.Fatalf("failed to load application_events schema: %v", err)
	}
	if strings.Contains(strings.ToUpper(createSQL), "CHECK (") {
		t.Errorf("application_events schema contains application-level CHECK constraints: %s", createSQL)
	}

	rows, err := sqlDB.Query(`SELECT "from", on_delete FROM pragma_foreign_key_list('application_events')`)
	if err != nil {
		t.Fatalf("failed to query application_events foreign keys: %v", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Fatalf("failed to close rows: %v", err)
		}
	}()

	deleteActions := make(map[string]string)
	for rows.Next() {
		var column, action string
		if err := rows.Scan(&column, &action); err != nil {
			t.Fatalf("failed to scan application_events foreign key: %v", err)
		}
		deleteActions[column] = action
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows error: %v", err)
	}

	wantDeleteActions := map[string]string{
		"application_id": "CASCADE",
		"actor_user_id":  "SET NULL",
	}
	for column, want := range wantDeleteActions {
		if got := deleteActions[column]; got != want {
			t.Errorf("application_events foreign key %q ON DELETE = %q, want %q", column, got, want)
		}
	}
}

func TestRunMigrations_UserOIDCIdentitiesIndexes(t *testing.T) {
	gormDB := openTestDB(t)
	if err := runMigrations(gormDB); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}

	rows, err := sqlDB.Query("SELECT name FROM pragma_index_list('user_oidc_identities')")
	if err != nil {
		t.Fatalf("failed to query indexes: %v", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Fatalf("failed to close rows: %v", err)
		}
	}()

	indexes := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("failed to scan index name: %v", err)
		}
		indexes[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows error: %v", err)
	}

	required := []string{
		"idx_user_oidc_identities_provider_subject",
		"idx_user_oidc_identities_user_provider",
		"idx_user_oidc_identities_user_id",
		"idx_user_oidc_identities_provider_id",
	}
	for _, idx := range required {
		if !indexes[idx] {
			t.Errorf("user_oidc_identities missing index %q", idx)
		}
	}
}

func initTestCrypto(t *testing.T) {
	t.Helper()
	if err := crypto.Init("test-secret-that-is-long-enough-32chars"); err != nil {
		t.Fatalf("crypto.Init() error: %v", err)
	}
}

func assertDemoSeedCounts(t *testing.T, gormDB *gorm.DB) {
	t.Helper()

	var userCount int64
	if err := gormDB.Model(&models.User{}).Where("email = ?", demoSeedUserEmail).Count(&userCount).Error; err != nil {
		t.Fatalf("failed to count demo users: %v", err)
	}
	if userCount != 1 {
		t.Fatalf("expected exactly 1 demo user, got %d", userCount)
	}

	var agentCount int64
	if err := gormDB.Model(&models.Agent{}).Where("id = ?", demoSeedAgentID).Count(&agentCount).Error; err != nil {
		t.Fatalf("failed to count demo agents: %v", err)
	}
	if agentCount != 1 {
		t.Fatalf("expected exactly 1 demo agent, got %d", agentCount)
	}

	var repoCount int64
	if err := gormDB.Model(&models.Repository{}).Where("url = ? AND sync_type = ?", demoSeedRepositoryURL, models.SyncTypeManual).Count(&repoCount).Error; err != nil {
		t.Fatalf("failed to count demo repositories: %v", err)
	}
	if repoCount != 1 {
		t.Fatalf("expected exactly 1 demo repository, got %d", repoCount)
	}
}

func closeGlobalDB(t *testing.T) {
	t.Helper()
	if DB == nil {
		return
	}

	sqlDB, err := DB.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB from global DB: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("failed to close global DB: %v", err)
	}
	DB = nil
}

func TestSeedDemoData_SeedsExpectedRecords(t *testing.T) {
	initTestCrypto(t)

	gormDB := openTestDB(t)
	if err := runMigrations(gormDB); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}

	if err := seedDemoData(gormDB); err != nil {
		t.Fatalf("seedDemoData() error: %v", err)
	}

	assertDemoSeedCounts(t, gormDB)

	var user models.User
	if err := gormDB.Where("email = ?", demoSeedUserEmail).First(&user).Error; err != nil {
		t.Fatalf("failed to fetch demo user: %v", err)
	}
	if !user.LocalAuthEnabled {
		t.Fatal("demo login account must be explicitly local")
	}

	var agent models.Agent
	if err := gormDB.Where("id = ?", demoSeedAgentID).First(&agent).Error; err != nil {
		t.Fatalf("failed to fetch demo agent: %v", err)
	}
	if agent.Name.String() != demoSeedAgentName {
		t.Fatalf("expected demo agent name %q, got %q", demoSeedAgentName, agent.Name.String())
	}
	if agent.KeyId.String() != demoSeedAgentKeyID {
		t.Fatalf("expected demo agent key_id %q, got %q", demoSeedAgentKeyID, agent.KeyId.String())
	}

	var repository models.Repository
	if err := gormDB.Where("url = ? AND sync_type = ?", demoSeedRepositoryURL, models.SyncTypeManual).First(&repository).Error; err != nil {
		t.Fatalf("failed to fetch demo repository: %v", err)
	}
	if repository.CreatedBy != user.Id {
		t.Fatalf("expected demo repository created_by to be %q, got %q", user.Id, repository.CreatedBy)
	}
}

func TestSeedDemoData_IsIdempotent(t *testing.T) {
	initTestCrypto(t)

	gormDB := openTestDB(t)
	if err := runMigrations(gormDB); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}

	if err := seedDemoData(gormDB); err != nil {
		t.Fatalf("first seedDemoData() error: %v", err)
	}
	if err := seedDemoData(gormDB); err != nil {
		t.Fatalf("second seedDemoData() error: %v", err)
	}

	assertDemoSeedCounts(t, gormDB)
}

func TestSeedDemoData_SeedsNewerEntities(t *testing.T) {
	initTestCrypto(t)

	gormDB := openTestDB(t)
	if err := runMigrations(gormDB); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}

	if err := seedDemoData(gormDB); err != nil {
		t.Fatalf("seedDemoData() error: %v", err)
	}

	counts := map[string]struct {
		model any
		want  int64
	}{
		"users":              {&models.User{}, 2},
		"agents":             {&models.Agent{}, 2},
		"repositories":       {&models.Repository{}, 2},
		"applications":       {&models.Application{}, 3},
		"application events": {&models.ApplicationEvent{}, 8},
		"deployments":        {&models.ApplicationDeployment{}, 4},
		"notifications":      {&models.Notification{}, 2},
		"audit logs":         {&models.AuditLog{}, 9},
		"api tokens":         {&models.APIToken{}, 4},
	}
	for name, c := range counts {
		var got int64
		if err := gormDB.Model(c.model).Count(&got).Error; err != nil {
			t.Fatalf("failed to count %s: %v", name, err)
		}
		if got != c.want {
			t.Errorf("expected %d %s, got %d", c.want, name, got)
		}
	}

	var signedRepo models.Repository
	if err := gormDB.Where("id = ?", demoSeedSignedRepositoryID).First(&signedRepo).Error; err != nil {
		t.Fatalf("failed to fetch signed demo repository: %v", err)
	}
	if !signedRepo.RequireSignedCommits || len(signedRepo.CommitSigningKeys) != 1 {
		t.Errorf("expected signed commits to be required with one key, got %v / %d keys", signedRepo.RequireSignedCommits, len(signedRepo.CommitSigningKeys))
	}

	var agent models.Agent
	if err := gormDB.Where("id = ?", demoSeedAgentID).First(&agent).Error; err != nil {
		t.Fatalf("failed to fetch demo agent: %v", err)
	}
	if agent.Status != models.AgentStatusOnline || agent.LastSeen == nil || agent.Version == "" || agent.DockerVersion == "" {
		t.Errorf("expected demo agent to look connected, got %+v", agent)
	}

	var token models.APIToken
	if err := gormDB.Where("id = ?", demoSeedAPITokenID).First(&token).Error; err != nil {
		t.Fatalf("failed to fetch demo api token: %v", err)
	}
	if token.UserId != demoSeedUserID || token.IsExpired(time.Now()) || token.LastUsedAt == nil {
		t.Errorf("expected an active, used demo api token of the demo user, got %+v", token)
	}
	// The stored hash must not belong to a token anyone could send
	if apitoken.IsWellFormed("demo:"+token.Id) || token.TokenHash != apitoken.Hash("demo:"+token.Id) {
		t.Errorf("expected the demo token hash to be of a malformed token, got %q", token.TokenHash)
	}

	var firstUse models.AuditLog
	if err := gormDB.Where("event_type = ?", "first-used-api-token").First(&firstUse).Error; err != nil {
		t.Fatalf("failed to fetch demo first use entry: %v", err)
	}
	if firstUse.APITokenId == nil || *firstUse.APITokenId != demoSeedAPITokenID {
		t.Errorf("expected the first use to be made with %q, got %v", demoSeedAPITokenID, firstUse.APITokenId)
	}
}

func TestSeedDemoData_RefusesDatabaseWithRealUsers(t *testing.T) {
	initTestCrypto(t)

	gormDB := openTestDB(t)
	if err := runMigrations(gormDB); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}

	users := []models.User{
		{Email: "first@orcacd.dev", Name: "First"},
		{Email: "second@orcacd.dev", Name: "Second"},
	}
	if err := gormDB.Create(&users).Error; err != nil {
		t.Fatalf("failed to create initial users: %v", err)
	}

	if err := seedDemoData(gormDB); !errors.Is(err, ErrDemoOnExistingDatabase) {
		t.Fatalf("seedDemoData() error = %v, want %v", err, ErrDemoOnExistingDatabase)
	}

	var demoUserCount int64
	if err := gormDB.Model(&models.User{}).Where("email = ?", demoSeedUserEmail).Count(&demoUserCount).Error; err != nil {
		t.Fatalf("failed to count demo users: %v", err)
	}
	if demoUserCount != 0 {
		t.Fatalf("expected no demo user to be seeded, got %d", demoUserCount)
	}

	var demoAgentCount int64
	if err := gormDB.Model(&models.Agent{}).Where("id = ?", demoSeedAgentID).Count(&demoAgentCount).Error; err != nil {
		t.Fatalf("failed to count demo agents: %v", err)
	}
	if demoAgentCount != 0 {
		t.Fatalf("expected no demo agent to be seeded, got %d", demoAgentCount)
	}

	var demoRepositoryCount int64
	if err := gormDB.Model(&models.Repository{}).Where("id = ?", demoSeedRepositoryID).Count(&demoRepositoryCount).Error; err != nil {
		t.Fatalf("failed to count demo repositories: %v", err)
	}
	if demoRepositoryCount != 0 {
		t.Fatalf("expected no demo repository to be seeded, got %d", demoRepositoryCount)
	}
}

// setupGlobalDB sets the package-level DB and logger for the duration of a test,
// restoring the originals via t.Cleanup.
func setupGlobalDB(t *testing.T) {
	t.Helper()
	gormDB := openTestDB(t)
	if err := runMigrations(gormDB); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}
	originalDB := DB
	originalLogger := logger
	DB = gormDB
	logger = zerolog.Nop()
	t.Cleanup(func() {
		DB = originalDB
		logger = originalLogger
	})
}

func TestSqliteDSN_ReadWriteParams(t *testing.T) {
	dsn := sqliteDSN(false)
	_, rawQuery, _ := strings.Cut(dsn, "?")
	q, err := url.ParseQuery(rawQuery)
	if err != nil {
		t.Fatalf("url.ParseQuery() error: %v", err)
	}

	cases := map[string]string{
		"_busy_timeout": "5000",
		"_foreign_keys": "ON",
		"_journal_mode": "WAL",
		"_synchronous":  "NORMAL",
		"_auto_vacuum":  "2",
		"_cache_size":   "-12000",
		"_txlock":       "immediate",
	}
	for param, want := range cases {
		if got := q.Get(param); got != want {
			t.Errorf("sqliteDSN(false): %s = %q, want %q", param, got, want)
		}
	}
	if got := q.Get("mode"); got != "" {
		t.Errorf("sqliteDSN(false): mode = %q, want empty", got)
	}
}

func TestSqliteDSN_ReadOnlyParams(t *testing.T) {
	dsn := sqliteDSN(true)
	_, rawQuery, _ := strings.Cut(dsn, "?")
	q, err := url.ParseQuery(rawQuery)
	if err != nil {
		t.Fatalf("url.ParseQuery() error: %v", err)
	}

	if got := q.Get("mode"); got != "ro" {
		t.Errorf("sqliteDSN(true): mode = %q, want %q", got, "ro")
	}
	if got := q.Get("_query_only"); got != "1" {
		t.Errorf("sqliteDSN(true): _query_only = %q, want %q", got, "1")
	}
	if got := q.Get("_cache_size"); got != "-12000" {
		t.Errorf("sqliteDSN(true): _cache_size = %q, want %q", got, "-12000")
	}
	// go-sqlite3 ignores URI parameters of DSNs without the file: prefix
	if !strings.HasPrefix(dsn, "file:") {
		t.Errorf("sqliteDSN(true) = %q, want file: prefix", dsn)
	}
}

func TestSqliteDSN_AlwaysUsesFileURI(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		if dsn := sqliteDSN(readOnly); !strings.HasPrefix(dsn, "file:"+sqliteFilePath+"?") {
			t.Errorf("sqliteDSN(%v) = %q, want file: prefix", readOnly, dsn)
		}
	}
}

func TestIncrementalVacuum_Succeeds(t *testing.T) {
	setupGlobalDB(t)
	if err := IncrementalVacuum(); err != nil {
		t.Fatalf("IncrementalVacuum() unexpected error: %v", err)
	}
}

func TestIncrementalVacuum_ReturnsErrorOnClosedDB(t *testing.T) {
	setupGlobalDB(t)

	sqlDB, err := DB.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("failed to close sql.DB: %v", err)
	}

	if err := IncrementalVacuum(); err == nil {
		t.Fatal("IncrementalVacuum() expected error on closed DB, got nil")
	}
}

func TestStartVacuumScheduler_StopDoesNotBlock(t *testing.T) {
	stop := StartVacuumScheduler()

	done := make(chan struct{})
	go func() {
		stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stop() blocked for more than 1 second")
	}
}

func TestStartVacuumScheduler_StopIsIdempotent(t *testing.T) {
	stop := StartVacuumScheduler()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("calling stop() twice panicked: %v", r)
		}
	}()
	stop()
	stop()
}

func TestConnect_DemoModeSeedsDataOnce(t *testing.T) {
	initTestCrypto(t)

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to read current working directory: %v", err)
	}

	workDir := t.TempDir()
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("failed to change working directory: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workDir, "data"), 0750); err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
		if DB != nil {
			if sqlDB, err := DB.DB(); err == nil {
				_ = sqlDB.Close()
			}
			DB = nil
		}
	})

	if err := Connect(zerolog.Nop(), zerolog.InfoLevel, true); err != nil {
		t.Fatalf("first Connect() error: %v", err)
	}
	assertDemoSeedCounts(t, DB)

	if err := DB.Exec("DELETE FROM users").Error; err == nil {
		t.Fatal("expected write to the demo database to fail")
	}
	if err := DB.Create(&models.AuditLog{EventType: "created", TargetType: "agent"}).Error; err == nil {
		t.Fatal("expected insert into the demo database to fail")
	}
	assertDemoSeedCounts(t, DB)

	closeGlobalDB(t)

	if err := Connect(zerolog.Nop(), zerolog.InfoLevel, true); err != nil {
		t.Fatalf("second Connect() error: %v", err)
	}
	assertDemoSeedCounts(t, DB)
}

func TestConnect_RefusesNonDemoStartOnDemoDatabase(t *testing.T) {
	initTestCrypto(t)

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to read current working directory: %v", err)
	}

	workDir := t.TempDir()
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("failed to change working directory: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workDir, "data"), 0750); err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
		if DB != nil {
			if sqlDB, err := DB.DB(); err == nil {
				_ = sqlDB.Close()
			}
			DB = nil
		}
	})

	if err := Connect(zerolog.Nop(), zerolog.InfoLevel, true); err != nil {
		t.Fatalf("demo Connect() error: %v", err)
	}
	closeGlobalDB(t)

	if err := Connect(zerolog.Nop(), zerolog.InfoLevel, false); !errors.Is(err, ErrNonDemoOnDemoDatabase) {
		t.Fatalf("non-demo Connect() error = %v, want %v", err, ErrNonDemoOnDemoDatabase)
	}
	if DB != nil {
		t.Fatal("expected the global DB to stay unset when the start is refused")
	}

	if err := Connect(zerolog.Nop(), zerolog.InfoLevel, true); err != nil {
		t.Fatalf("demo Connect() after refused start error: %v", err)
	}
}

func TestSqliteFilePath_ReturnsExpectedPath(t *testing.T) {
	expectedSuffix := "data/hub.db"
	if got := GetSQLiteFilePath(); got != expectedSuffix {
		t.Errorf("GetSQLiteFilePath() = %q, want %q", got, expectedSuffix)
	}
}
