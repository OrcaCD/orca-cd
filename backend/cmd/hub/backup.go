package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/OrcaCD/orca-cd/internal/hub"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

func newBackupCmd() *cobra.Command {
	var outputPath string

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Create a backup of the hub database",
		Long:  "Create a consistent backup of the hub database using VACUUM INTO. Safe to run while the hub is running.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runBackupCommand(cmd.OutOrStdout(), outputPath)
		},
	}

	// .Format() is strange in Go, resulting format is YYYYMMDD-HHMMSS
	defaultOutput := "hub-backup-" + time.Now().Format("20060102-150405") + ".db"
	cmd.Flags().StringVarP(&outputPath, "output", "o", defaultOutput, "Output path for the backup file")

	return cmd
}

func resolveBackupPath(outputPath string) string {
	if filepath.IsAbs(outputPath) {
		return outputPath
	}
	return filepath.Join("data", outputPath)
}

func runBackupCommand(out io.Writer, outputPath string) error {
	outputPath = resolveBackupPath(outputPath)

	if err := os.MkdirAll(filepath.Dir(outputPath), 0750); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	if _, err := os.Stat(outputPath); err == nil {
		return fmt.Errorf("output file already exists: %s", outputPath)
	}

	cfg, err := hub.DefaultConfig()
	if err != nil {
		return fmt.Errorf("failed to load hub configuration: %w", err)
	}

	if cfg.Demo {
		return fmt.Errorf("backup is not available in demo mode")
	}

	dbLogger := zerolog.New(io.Discard)
	if err := db.Connect(dbLogger, cfg.LogLevel, false); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "database is locked") {
			renderDatabaseBusyInfo(out)
		}
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer func() {
		_ = db.Close()
	}()

	if err := db.Backup(outputPath); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	info, err := os.Stat(outputPath)
	if err != nil {
		return fmt.Errorf("backup created but could not read file info: %w", err)
	}

	renderBackupResult(out, outputPath, info.Size())
	return nil
}

func renderBackupResult(out io.Writer, outputPath string, size int64) {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Green)
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.White)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.White)

	body := strings.Join([]string{
		labelStyle.Render("Output:") + " " + valueStyle.Render(outputPath),
		labelStyle.Render("Size:") + " " + valueStyle.Render(utils.FormatFileSize(size)),
	}, "\n")

	card := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Blue).
		Padding(1, 2).
		Render(titleStyle.Render("Backup Successful") + "\n\n" + body)

	_, _ = lipgloss.Fprintln(out, card)
}
