package db

import (
	"fmt"
	"io"
	"os"
)

func Restore(backupPath string) error {
	if DB == nil {
		return fmt.Errorf("database not connected")
	}

	// Close the database connection
	sqlDB, err := DB.DB()
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}
	if err := sqlDB.Close(); err != nil {
		return fmt.Errorf("failed to close database connection: %w", err)
	}

	// Backup current database
	currentDBPath := sqliteFilePath
	backupCurrentPath := currentDBPath + ".bak"
	
	// Only backup if the current database exists
	if _, err := os.Stat(currentDBPath); err == nil {
		if err := copyFile(currentDBPath, backupCurrentPath); err != nil {
			return fmt.Errorf("failed to backup current database: %w", err)
		}
	}

	// Restore from backup
	if err := copyFile(backupPath, currentDBPath); err != nil {
		// Restore the previous backup if restore fails
		if _, err := os.Stat(backupCurrentPath); err == nil {
			_ = copyFile(backupCurrentPath, currentDBPath)
		}
		return fmt.Errorf("failed to restore database: %w", err)
	}

	// Clean up backup file
	_ = os.Remove(backupCurrentPath)

	return nil
}

func copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		_ = source.Close()
	}()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		_ = destination.Close()
	}()

	_, err = io.Copy(destination, source)
	return err
}
