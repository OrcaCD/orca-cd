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

	if err := Close(); err != nil {
		return fmt.Errorf("failed to close database connection: %w", err)
	}

	_ = os.Remove(sqliteFilePath + "-shm")
	_ = os.Remove(sqliteFilePath + "-wal")

	currentDBPath := sqliteFilePath
	backupCurrentPath := currentDBPath + ".bak"

	if _, err := os.Stat(currentDBPath); err == nil {
		if err := copyFile(currentDBPath, backupCurrentPath); err != nil {
			return fmt.Errorf("failed to backup current database: %w", err)
		}
	}

	if err := copyFile(backupPath, currentDBPath); err != nil {
		if _, err := os.Stat(backupCurrentPath); err == nil {
			_ = copyFile(backupCurrentPath, currentDBPath)
		}
		return fmt.Errorf("failed to restore database: %w", err)
	}

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
