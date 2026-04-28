package utils

import "testing"

func TestFormatFileSize(t *testing.T) {
	tests := []struct {
		size int64
		want string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{1023, "1023 B"},
		{1024, "1.00 KB"},
		{1536, "1.50 KB"},
		{1024 * 1024, "1.00 MB"},
		{1024*1024 + 512*1024, "1.50 MB"},
		{1024 * 1024 * 1024, "1.00 GB"},
		{int64(1.5 * 1024 * 1024 * 1024), "1.50 GB"},
	}

	for _, tt := range tests {
		got := FormatFileSize(tt.size)
		if got != tt.want {
			t.Errorf("FormatFileSize(%d) = %q, want %q", tt.size, got, tt.want)
		}
	}
}
