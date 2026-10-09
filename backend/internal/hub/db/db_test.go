package db

import (
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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
	required := []string{"id", "name", "icon", "key_id", "status", "last_seen", "created_at", "updated_at"}
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

	// migration 002: base; 004: role; 006: oidc fields; 007: auth_provider dropped; 010: password_change_required
	required := []string{
		"id", "email", "name", "password_hash",
		"role",
		"oidc_subject", "oidc_issuer",
		"password_change_required",
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
	if err := m.Migrate(26); err != nil {
		t.Fatalf("failed to migrate to version 26: %v", err)
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
	if err := m.Migrate(28); err != nil {
		t.Fatalf("failed to migrate to version 28: %v", err)
	}

	if _, err := sqlDB.Exec(`INSERT INTO notifications (id, name, status, type, config, events)
		VALUES ('n', 'n', 'unknown', 'discord', '{}', '["application.deployment.failed","agent.offline","repository.sync.failed","application.health.unhealthy"]')`); err != nil {
		t.Fatalf("failed to seed notification: %v", err)
	}

	if err := m.Migrate(27); err != nil {
		t.Fatalf("failed to migrate down to version 27: %v", err)
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
		t.Errorf("expected events added in version 28 to be removed, got %s", events)
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

func TestSeedDemoData_SkipsWhenMoreThanOneUserExists(t *testing.T) {
	initTestCrypto(t)

	gormDB := openTestDB(t)
	if err := runMigrations(gormDB); err != nil {
		t.Fatalf("runMigrations() error: %v", err)
	}

	users := []models.User{
		{Email: "first@orcacd.dev", Name: "First", Role: models.UserRoleAdmin, PasswordChangeRequired: false},
		{Email: "second@orcacd.dev", Name: "Second", Role: models.UserRoleUser, PasswordChangeRequired: false},
	}
	if err := gormDB.Create(&users).Error; err != nil {
		t.Fatalf("failed to create initial users: %v", err)
	}

	if err := seedDemoData(gormDB); err != nil {
		t.Fatalf("seedDemoData() error: %v", err)
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
	if got := q.Get("_cache_size"); got != "-12000" {
		t.Errorf("sqliteDSN(true): _cache_size = %q, want %q", got, "-12000")
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

	closeGlobalDB(t)

	if err := Connect(zerolog.Nop(), zerolog.InfoLevel, true); err != nil {
		t.Fatalf("second Connect() error: %v", err)
	}
	assertDemoSeedCounts(t, DB)
}

func TestSqliteFilePath_ReturnsExpectedPath(t *testing.T) {
	expectedSuffix := "data/hub.db"
	if got := GetSQLiteFilePath(); got != expectedSuffix {
		t.Errorf("GetSQLiteFilePath() = %q, want %q", got, expectedSuffix)
	}
}
