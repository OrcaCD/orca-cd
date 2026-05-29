package utils

import (
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test_utils.db")

	testDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: gormlogger.New(
			log.New(os.Stderr, "\n", log.LstdFlags),
			gormlogger.Config{
				LogLevel: gormlogger.Warn,
			},
		),
	})

	require.NoError(t, err)

	err = testDB.AutoMigrate(&models.AuditLog{})
	require.NoError(t, err)

	old := db.DB
	db.DB = testDB

	t.Cleanup(func() {
		sqlDB, _ := testDB.DB()

		if sqlDB != nil {
			_ = sqlDB.Close()
		}

		db.DB = old
	})

	return testDB
}

func TestRecordAuditLog(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("happy case", func(t *testing.T) {
		testDB := setupTestDB(t)

		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/", nil)

		c, _ := gin.CreateTestContext(w)
		c.Request = req

		RecordAuditLog(c, "LOGIN", "USER", "123")

		var audit models.AuditLog
		err := testDB.First(&audit).Error
		require.NoError(t, err)

		require.NotNil(t, audit.TargetId)
		require.Equal(t, "123", *audit.TargetId)
	})

	t.Run("empty targetId", func(t *testing.T) {
		testDB := setupTestDB(t)

		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/", nil)

		c, _ := gin.CreateTestContext(w)
		c.Request = req

		RecordAuditLog(c, "LOGIN", "USER", "")

		var audit models.AuditLog
		err := testDB.First(&audit).Error
		require.NoError(t, err)

		require.Nil(t, audit.TargetId)
	})
	t.Run("system user fallback", func(t *testing.T) {
		testDB := setupTestDB(t)

		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/", nil)

		c, _ := gin.CreateTestContext(w)
		c.Request = req

		RecordAuditLog(c, "LOGIN", "USER", "123")

		var audit models.AuditLog
		err := testDB.First(&audit).Error
		require.NoError(t, err)

		require.Nil(t, audit.UserId)
	})
}
