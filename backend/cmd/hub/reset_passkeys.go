package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/OrcaCD/orca-cd/internal/hub"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/passkeys"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

var (
	errMissingUserIdentifier = errors.New("either --email or --id must be provided")
	errTooManyIdentifiers    = errors.New("provide either --email or --id, not both")
)

type resetPasskeysTarget struct {
	Email string
	ID    string
}

func newResetPasskeysCmd() *cobra.Command {
	target := resetPasskeysTarget{}

	cmd := &cobra.Command{
		Use:   "reset-passkeys",
		Short: "Reset a local user's passkeys",
		Long:  "Revoke a local user's passkeys and sessions, then issue a single-use enrollment code valid for 24 hours.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runResetPasskeysCommand(cmd.Context(), cmd.OutOrStdout(), target)
		},
	}

	cmd.Flags().StringVar(&target.Email, "email", "", "Email of the user")
	cmd.Flags().StringVar(&target.ID, "id", "", "ID of the user")

	return cmd
}

func runResetPasskeysCommand(ctx context.Context, out io.Writer, target resetPasskeysTarget) error {
	target, err := normalizeResetPasskeysTarget(target)
	if err != nil {
		return err
	}

	cfg, err := hub.DefaultConfig()
	if err != nil {
		return fmt.Errorf("failed to load hub configuration: %w", err)
	}

	if cfg.Demo {
		return fmt.Errorf("passkey recovery is not available in demo mode")
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

	user, enrollmentToken, err := resetUserPasskeys(ctx, target)
	if err != nil {
		return err
	}

	renderResetPasskeysResult(out, user, enrollmentToken)
	return nil
}

func normalizeResetPasskeysTarget(target resetPasskeysTarget) (resetPasskeysTarget, error) {
	target.Email = strings.ToLower(strings.TrimSpace(target.Email))
	target.ID = strings.TrimSpace(target.ID)

	hasEmail := target.Email != ""
	hasID := target.ID != ""

	if !hasEmail && !hasID {
		return resetPasskeysTarget{}, errMissingUserIdentifier
	}

	if hasEmail && hasID {
		return resetPasskeysTarget{}, errTooManyIdentifiers
	}

	return target, nil
}

func resetUserPasskeys(ctx context.Context, target resetPasskeysTarget) (models.User, string, error) {
	normalizedTarget, err := normalizeResetPasskeysTarget(target)
	if err != nil {
		return models.User{}, "", err
	}
	target = normalizedTarget

	var user models.User
	var enrollmentToken string
	err = db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := gorm.G[models.User](tx)
		var err error
		if target.ID != "" {
			user, err = query.Where("id = ?", target.ID).First(ctx)
		} else {
			user, err = query.Where("email = ?", target.Email).First(ctx)
		}
		if err != nil {
			return err
		}
		if !user.LocalAuthEnabled {
			return passkeys.ErrManagedUser
		}
		enrollmentToken, err = passkeys.ResetLocal(ctx, tx, &user)
		return err
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if target.ID != "" {
				return models.User{}, "", fmt.Errorf("user not found with id %q", target.ID)
			}
			return models.User{}, "", fmt.Errorf("user not found with email %q", target.Email)
		}
		return models.User{}, "", fmt.Errorf("reset passkeys: %w", err)
	}
	return user, enrollmentToken, nil
}

func renderResetPasskeysResult(out io.Writer, user models.User, enrollmentToken string) {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Green)
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.White)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.White)
	codeStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("24")).Padding(0, 1)
	noteStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))

	body := strings.Join([]string{
		labelStyle.Render("User ID:") + " " + valueStyle.Render(user.Id),
		labelStyle.Render("Email:") + " " + valueStyle.Render(user.Email),
		labelStyle.Render("Enrollment code:") + " " + codeStyle.Render(enrollmentToken),
		"",
		noteStyle.Render("Use this code on the enrollment page linked from sign-in within 24 hours."),
	}, "\n")

	card := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Blue).
		Padding(1, 2).
		Render(titleStyle.Render("Passkey Recovery Ready") + "\n\n" + body)

	_, _ = lipgloss.Fprintln(out, card)
}
