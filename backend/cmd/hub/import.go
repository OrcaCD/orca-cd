package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/OrcaCD/orca-cd/internal/hub"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

func newImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import <file>",
		Short: "Import a database backup",
		Long:  "Import a database backup created with the backup command. The current database will be safely backed up before importing.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runImportCommand(cmd.OutOrStdout(), args[0])
		},
	}

	return cmd
}

func runImportCommand(out io.Writer, importPath string) error {
	// Validate that the import file exists
	if _, err := os.Stat(importPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("backup file not found: %s", importPath)
		}
		return fmt.Errorf("failed to check backup file: %w", err)
	}

	cfg, err := hub.DefaultConfig()
	if err != nil {
		return fmt.Errorf("failed to load hub configuration: %w", err)
	}

	if cfg.Demo {
		return fmt.Errorf("import is not available in demo mode")
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

	if err := db.Restore(importPath); err != nil {
		return fmt.Errorf("import failed: %w", err)
	}

	renderImportResult(out, importPath)
	return nil
}

func renderImportResult(out io.Writer, importPath string) {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Green)
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.White)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.White)

	body := strings.Join([]string{
		labelStyle.Render("Source:") + " " + valueStyle.Render(importPath),
		labelStyle.Render("Status:") + " " + valueStyle.Render("Database restored successfully"),
	}, "\n")

	card := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Blue).
		Padding(1, 2).
		Render(titleStyle.Render("Import Successful") + "\n\n" + body)

	_, _ = lipgloss.Fprintln(out, card)
}
