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

	// Close the database connection to release all file handles
	// This ensures the database file can be safely replaced
	if err := Close(); err != nil {
		return fmt.Errorf("failed to close database connection: %w", err)
	}

	// Clean up WAL files to ensure a clean state for the new database
	_ = os.Remove(sqliteFilePath + "-shm")
	_ = os.Remove(sqliteFilePath + "-wal")

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
	source, err := os.Open(src) // #nosec G304 - paths are controlled internally
	if err != nil {
		return err
	}
	defer func() {
		_ = source.Close()
	}()

	destination, err := os.Create(dst) // #nosec G304 - paths are controlled internally
	if err != nil {
		return err
	}
	defer func() {
		_ = destination.Close()
	}()

	_, err = io.Copy(destination, source)
	return err
}
