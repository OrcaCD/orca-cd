package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/OrcaCD/orca-cd/internal/hub"
	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

var (
	errMissingUserIdentifier = errors.New("either --email or --id must be provided")
	errTooManyIdentifiers    = errors.New("provide either --email or --id, not both")
	errManagedUser           = errors.New("cannot reset password for a managed user")
)

type resetPasswordTarget struct {
	Email string
	ID    string
}

func newResetPasswordCmd() *cobra.Command {
	target := resetPasswordTarget{}

	cmd := &cobra.Command{
		Use:   "reset-password",
		Short: "Reset a local user's password",
		Long:  "Generate a new password for a local user and require a password change on next login.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runResetPasswordCommand(cmd.Context(), cmd.OutOrStdout(), target)
		},
	}

	cmd.Flags().StringVar(&target.Email, "email", "", "Email of the user")
	cmd.Flags().StringVar(&target.ID, "id", "", "ID of the user")

	return cmd
}

func runResetPasswordCommand(ctx context.Context, out io.Writer, target resetPasswordTarget) error {
	target, err := normalizeResetPasswordTarget(target)
	if err != nil {
		return err
	}

	cfg, err := hub.DefaultConfig()
	if err != nil {
		return fmt.Errorf("failed to load hub configuration: %w", err)
	}

	if cfg.Demo {
		return fmt.Errorf("password reset is not available in demo mode")
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

	user, generatedPassword, err := resetUserPassword(ctx, target)
	if err != nil {
		return err
	}

	renderResetPasswordResult(out, user, generatedPassword)
	return nil
}

func normalizeResetPasswordTarget(target resetPasswordTarget) (resetPasswordTarget, error) {
	target.Email = strings.ToLower(strings.TrimSpace(target.Email))
	target.ID = strings.TrimSpace(target.ID)

	hasEmail := target.Email != ""
	hasID := target.ID != ""

	if !hasEmail && !hasID {
		return resetPasswordTarget{}, errMissingUserIdentifier
	}

	if hasEmail && hasID {
		return resetPasswordTarget{}, errTooManyIdentifiers
	}

	return target, nil
}

func resetUserPassword(ctx context.Context, target resetPasswordTarget) (models.User, string, error) {
	normalizedTarget, err := normalizeResetPasswordTarget(target)
	if err != nil {
		return models.User{}, "", err
	}
	target = normalizedTarget

	user, err := findUserForPasswordReset(ctx, target)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if target.ID != "" {
				return models.User{}, "", fmt.Errorf("user not found with id %q", target.ID)
			}
			return models.User{}, "", fmt.Errorf("user not found with email %q", target.Email)
		}
		return models.User{}, "", fmt.Errorf("failed to load user: %w", err)
	}

	if user.PasswordHash == nil {
		return models.User{}, "", errManagedUser
	}

	generatedPassword, err := auth.GenerateRandomPassword()
	if err != nil {
		return models.User{}, "", fmt.Errorf("failed to generate password: %w", err)
	}

	hash, err := auth.HashPassword(generatedPassword)
	if err != nil {
		return models.User{}, "", fmt.Errorf("failed to hash password: %w", err)
	}

	rowsAffected, err := gorm.G[models.User](db.DB).Where("id = ?", user.Id).Updates(ctx, models.User{
		PasswordHash:           &hash,
		PasswordChangeRequired: true,
	})
	if err != nil {
		return models.User{}, "", fmt.Errorf("failed to update user password: %w", err)
	}
	if rowsAffected == 0 {
		return models.User{}, "", fmt.Errorf("failed to update user password: %w", gorm.ErrRecordNotFound)
	}

	user.PasswordHash = &hash
	user.PasswordChangeRequired = true

	return user, generatedPassword, nil
}

func findUserForPasswordReset(ctx context.Context, target resetPasswordTarget) (models.User, error) {
	query := gorm.G[models.User](db.DB)

	if target.ID != "" {
		return query.Where("id = ?", target.ID).First(ctx)
	}

	return query.Where("email = ?", target.Email).First(ctx)
}

func renderResetPasswordResult(out io.Writer, user models.User, generatedPassword string) {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Green)
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.White)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.White)
	passwordStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("24")).Padding(0, 1)
	noteStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))

	body := strings.Join([]string{
		labelStyle.Render("User ID:") + " " + valueStyle.Render(user.Id),
		labelStyle.Render("Email:") + " " + valueStyle.Render(user.Email),
		labelStyle.Render("New temporary password:") + " " + passwordStyle.Render(generatedPassword),
		"",
		noteStyle.Render("Important: The user must change this password on the next login."),
	}, "\n")

	card := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Blue).
		Padding(1, 2).
		Render(titleStyle.Render("Password Reset Successful") + "\n\n" + body)

	_, _ = lipgloss.Fprintln(out, card)
}
