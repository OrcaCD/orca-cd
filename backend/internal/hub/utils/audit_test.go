package utils

import (
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/middleware"
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

	t.Run("authenticated user", func(t *testing.T) {
		testDB := setupTestDB(t)

		err := auth.Init("test-secret-that-is-long-enough-32chars", "http://localhost:8080")
		require.NoError(t, err)

		user := &models.User{
			Base: models.Base{Id: "user-456"},
			Name: "test-admin",
		}
		token, err := auth.GenerateUserToken(user)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/", nil)

		req.AddCookie(&http.Cookie{Name: "orcacd_auth", Value: token})

		router := gin.New()

		var capturedCtx *gin.Context
		router.POST("/", middleware.RequireAuth(), func(ctx *gin.Context) {
			capturedCtx = ctx
			ctx.Status(http.StatusOK)
		})

		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, "Middleware hätte den Token erlauben müssen")

		RecordAuditLog(capturedCtx, "UPDATE", "PROJECT", "789")

		var audit models.AuditLog
		err = testDB.First(&audit).Error
		require.NoError(t, err)

		require.NotNil(t, audit.UserId)
		require.Equal(t, "user-456", *audit.UserId)
	})

	t.Run("database error case", func(t *testing.T) {
		testDB := setupTestDB(t)

		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/", nil)

		c, _ := gin.CreateTestContext(w)
		c.Request = req

		err := testDB.Migrator().DropTable(&models.AuditLog{})
		require.NoError(t, err)

		require.NotPanics(t, func() {
			RecordAuditLog(c, "DELETE", "CLUSTER", "999")
		})
	})
}
