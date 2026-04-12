package db

import (
	"database/sql"
	"path/filepath"
	"testing"

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
	required := []string{"id", "name", "key_id", "status", "last_seen", "created_at", "updated_at"}
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
