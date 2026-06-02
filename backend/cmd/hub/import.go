package main

import (
	"bufio"
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

	// Display warning and get user confirmation
	warningPoints := []string{
		"The hub server must be stopped before importing",
		"All existing data will be permanently overridden",
	}
	confirmed, err := getUserConfirmation(out, "Import Database", warningPoints)
	if err != nil {
		return fmt.Errorf("failed to read confirmation: %w", err)
	}
	if !confirmed {
		_, _ = fmt.Fprintln(out, lipgloss.NewStyle().Foreground(lipgloss.Yellow).Render("Import cancelled"))
		return nil
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

// getUserConfirmation displays a warning message and prompts the user for confirmation
func getUserConfirmation(out io.Writer, warningTitle string, warningPoints []string) (bool, error) {
	// Display warning
	warningStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Yellow).
		Background(lipgloss.Color("52"))
	
	alertStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Yellow).
		Padding(1, 2)

	warningText := warningStyle.Render("⚠ WARNING")
	warningContent := warningText + "\n\n"
	
	for _, point := range warningPoints {
		warningContent += "• " + point + "\n"
	}

	card := alertStyle.Render(warningContent)
	_, _ = lipgloss.Fprintln(out, card)

	// Prompt for confirmation
	prompt := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.White).Render("\nProceed with this action? (yes/no): ")
	_, _ = fmt.Fprint(out, prompt)

	// Read user input
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}

	input = strings.TrimSpace(strings.ToLower(input))
	return input == "yes" || input == "y", nil
}
