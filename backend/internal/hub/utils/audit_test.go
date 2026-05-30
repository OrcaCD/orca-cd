package utils

import (
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/middleware"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func fatalIfErr(t *testing.T, err error, msg string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", msg, err)
	}
}

func assertEqual[T comparable](t *testing.T, expected, actual T, msg string) {
	t.Helper()
	if expected != actual {
		t.Fatalf("%s: expected %v, got %v", msg, expected, actual)
	}
}

func assertNil(t *testing.T, v any, msg string) {
	t.Helper()

	if v == nil {
		return
	}

	val := reflect.ValueOf(v)
	switch val.Kind() {
	case
		reflect.Pointer,
		reflect.Interface,
		reflect.Slice,
		reflect.Map,
		reflect.Func,
		reflect.Chan:
		if val.IsNil() {
			return
		}
	}

	t.Fatalf("%s: expected nil, got %v", msg, v)
}

func assertNotNil(t *testing.T, v any, msg string) {
	t.Helper()

	if v == nil {
		t.Fatalf("%s: expected not nil", msg)
		return
	}

	val := reflect.ValueOf(v)
	switch val.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Slice, reflect.Map, reflect.Func, reflect.Chan:
		if val.IsNil() {
			t.Fatalf("%s: expected not nil", msg)
		}
	}
}

func assertNotPanics(t *testing.T, fn func(), msg string) {
	t.Helper()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("%s: unexpected panic: %v", msg, r)
		}
	}()

	fn()
}

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
	fatalIfErr(t, err, "failed to open test db")

	fatalIfErr(t, testDB.AutoMigrate(&models.AuditLog{}), "auto migrate failed")

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
		fatalIfErr(t, err, "db query failed")

		assertNotNil(t, audit.TargetId, "targetId should not be nil")
		assertEqual(t, "123", *audit.TargetId, "targetId mismatch")
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
		fatalIfErr(t, err, "db query failed")

		assertNil(t, audit.TargetId, "targetId should be nil")
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
		fatalIfErr(t, err, "db query failed")

		assertNil(t, audit.UserId, "userId should be nil (system fallback)")
	})

	t.Run("authenticated user", func(t *testing.T) {
		testDB := setupTestDB(t)

		fatalIfErr(
			t,
			auth.Init("test-secret-that-is-long-enough-32chars", "http://localhost:8080"),
			"auth init failed",
		)

		user := &models.User{
			Base: models.Base{Id: "user-456"},
			Name: "test-admin",
		}

		token, err := auth.GenerateUserToken(user)
		fatalIfErr(t, err, "token generation failed")

		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/", nil)
		//nolint:gosec
		req.AddCookie(&http.Cookie{Name: "orcacd_auth", Value: token})

		router := gin.New()

		var capturedCtx *gin.Context

		router.POST("/", middleware.RequireAuth(), func(ctx *gin.Context) {
			capturedCtx = ctx
			ctx.Status(http.StatusOK)
		})

		router.ServeHTTP(w, req)

		assertEqual(t, http.StatusOK, w.Code, "middleware should allow token")

		RecordAuditLog(capturedCtx, "UPDATE", "PROJECT", "789")

		var audit models.AuditLog
		err = testDB.First(&audit).Error
		fatalIfErr(t, err, "db query failed")

		assertNotNil(t, audit.UserId, "userId should not be nil")
		assertEqual(t, "user-456", *audit.UserId, "userId mismatch")
	})

	t.Run("database error case", func(t *testing.T) {
		testDB := setupTestDB(t)

		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/", nil)

		c, _ := gin.CreateTestContext(w)
		c.Request = req

		fatalIfErr(t, testDB.Migrator().DropTable(&models.AuditLog{}), "drop table failed")

		assertNotPanics(t, func() {
			RecordAuditLog(c, "DELETE", "CLUSTER", "999")
		}, "RecordAuditLog should not panic")
	})
}
