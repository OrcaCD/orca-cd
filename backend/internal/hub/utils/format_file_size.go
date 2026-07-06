package utils

import "fmt"

func FormatFileSize(size int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)

	switch {
	case size >= gb:
		return fmt.Sprintf("%.2f GB", float64(size)/gb)
	case size >= mb:
		return fmt.Sprintf("%.2f MB", float64(size)/mb)
	case size >= kb:
		return fmt.Sprintf("%.2f KB", float64(size)/kb)
	default:
		return fmt.Sprintf("%d B", size)
	}
}
