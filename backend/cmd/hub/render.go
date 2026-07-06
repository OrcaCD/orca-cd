package main

import (
	"io"
	"strings"

	"charm.land/lipgloss/v2"
)

func renderDatabaseBusyInfo(out io.Writer) {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	bodyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

	body := strings.Join([]string{
		"The database is currently busy.",
		"Retry this command in a few seconds.",
		"If this persists, stop other processes using the database and try again.",
	}, "\n")

	card := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("214")).
		Padding(1, 2).
		Render(titleStyle.Render("Database Busy") + "\n\n" + bodyStyle.Render(body))

	_, _ = lipgloss.Fprintln(out, card)
}
