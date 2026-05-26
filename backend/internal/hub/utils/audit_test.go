package utils

import (
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRecordAuditLog(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	err = testDB.AutoMigrate(&models.AuditLog{})
	require.NoError(t, err)

	old := db.DB
	db.DB = testDB
	defer func() { db.DB = old }()

	t.Run("happy case", func(t *testing.T) {
		testDB := newTestDB(t)
		db.DB = testDB

		c, _ := gin.CreateTestContext(nil)

		RecordAuditLog(c, "LOGIN", "USER", "123")

		var audit models.AuditLog
		err := testDB.First(&audit).Error
		require.NoError(t, err)

		require.Equal(t, "123", *audit.TargetId)
	})

	t.Run("empty targetId", func(t *testing.T) {
		testDB := newTestDB(t)
		db.DB = testDB

		c, _ := gin.CreateTestContext(nil)

		RecordAuditLog(c, "LOGIN", "USER", "")

		var audit models.AuditLog
		err := testDB.Last(&audit).Error
		require.NoError(t, err)

		require.Nil(t, audit.TargetId)
	})
	t.Run("system user fallback", func(t *testing.T) {
		testDB := newTestDB(t)
		db.DB = testDB

		c, _ := gin.CreateTestContext(nil)

		RecordAuditLog(c, "LOGIN", "USER", "123")

		var audit models.AuditLog
		err := testDB.First(&audit).Error
		require.NoError(t, err)

		require.Nil(t, audit.UserId)
	})
}

func newTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	err = db.AutoMigrate(&models.AuditLog{})
	require.NoError(t, err)

	return db
}
