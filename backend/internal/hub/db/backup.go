package db

import "fmt"

func Backup(outputPath string) error {
	if DB == nil {
		return fmt.Errorf("database not connected")
	}
	return DB.Exec("VACUUM INTO ?", outputPath).Error
}
