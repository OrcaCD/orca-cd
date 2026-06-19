package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/OrcaCD/orca-cd/internal/hub"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

const minKeyRotateSecretLength = 32

var (
	errMissingOldSecret = errors.New("old secret is required")
	errMissingNewSecret = errors.New("new secret is required")
	errSameSecrets      = errors.New("old and new secrets must be different")
)

type keyRotateOptions struct {
	OldSecret        string
	SkipConfirmation bool
}

type keyRotationResult struct {
	Agents        int
	Applications  int
	Repositories  int
	Notifications int
	OIDCProviders int
}

func (r keyRotationResult) Total() int {
	return r.Agents + r.Applications + r.Repositories + r.Notifications + r.OIDCProviders
}

func newKeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "key",
		Short: "Manage hub encryption keys",
	}

	cmd.AddCommand(newKeyRotateCmd())
	return cmd
}

func newKeyRotateCmd() *cobra.Command {
	options := keyRotateOptions{
		OldSecret: os.Getenv("OLD_APP_SECRET"),
	}

	cmd := &cobra.Command{
		Use:   "rotate",
		Short: "Rotate the hub database encryption key",
		Long: strings.Join([]string{
			"Re-encrypt all database fields encrypted from the previous APP_SECRET to the current APP_SECRET.",
			"The hub must be stopped before running this command.",
		}, "\n"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runKeyRotateCommandWithInput(cmd.Context(), cmd.OutOrStdout(), os.Stdin, options)
		},
	}

	cmd.Flags().StringVar(&options.OldSecret, "old-secret", options.OldSecret, "Previous APP_SECRET value. Can also be set with OLD_APP_SECRET")
	cmd.Flags().BoolVarP(&options.SkipConfirmation, "yes", "y", false, "Skip confirmation warnings")

	return cmd
}

func runKeyRotateCommandWithInput(ctx context.Context, out io.Writer, in io.Reader, options keyRotateOptions) error {
	options.OldSecret = strings.TrimSpace(options.OldSecret)

	cfg, err := hub.DefaultConfig()
	if err != nil {
		return fmt.Errorf("failed to load hub configuration: %w", err)
	}

	if cfg.Demo {
		return fmt.Errorf("key rotation is not available in demo mode")
	}

	if err := validateKeyRotateSecrets(options.OldSecret, cfg.AppSecret); err != nil {
		return err
	}

	warningPoints := []string{
		"The hub server must be stopped before rotating the key",
		"Create a database export before rotating the key",
		"The database will be re-encrypted with the current APP_SECRET",
		"Sessions and agent tokens signed with the old APP_SECRET may need to be re-issued",
	}
	confirmed, err := getUserConfirmation(out, in, warningPoints, options.SkipConfirmation)
	if err != nil {
		return fmt.Errorf("failed to read confirmation: %w", err)
	}
	if !confirmed {
		_, _ = fmt.Fprintln(out, lipgloss.NewStyle().Foreground(lipgloss.Yellow).Render("Key rotation cancelled"))
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

	result, err := rotateDatabaseEncryptionKey(ctx, options.OldSecret, cfg.AppSecret)
	if err != nil {
		return fmt.Errorf("key rotation failed: %w", err)
	}

	renderKeyRotateResult(out, result)
	return nil
}

func validateKeyRotateSecrets(oldSecret, newSecret string) error {
	oldSecret = strings.TrimSpace(oldSecret)
	newSecret = strings.TrimSpace(newSecret)

	if oldSecret == "" {
		return errMissingOldSecret
	}
	if newSecret == "" {
		return errMissingNewSecret
	}
	if len(oldSecret) < minKeyRotateSecretLength {
		return fmt.Errorf("invalid old secret: must be minimum %d characters", minKeyRotateSecretLength)
	}
	if len(newSecret) < minKeyRotateSecretLength {
		return fmt.Errorf("invalid new secret: must be minimum %d characters", minKeyRotateSecretLength)
	}
	if oldSecret == newSecret {
		return errSameSecrets
	}

	return nil
}

func rotateDatabaseEncryptionKey(ctx context.Context, oldSecret, newSecret string) (keyRotationResult, error) {
	if err := validateKeyRotateSecrets(oldSecret, newSecret); err != nil {
		return keyRotationResult{}, err
	}
	if db.DB == nil {
		return keyRotationResult{}, errors.New("database is not connected")
	}

	var result keyRotationResult
	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := crypto.Init(oldSecret); err != nil {
			return fmt.Errorf("initialize old encryption key: %w", err)
		}

		agents, err := gorm.G[models.Agent](tx).Find(ctx)
		if err != nil {
			return fmt.Errorf("load agents: %w", err)
		}
		applications, err := gorm.G[models.Application](tx).Find(ctx)
		if err != nil {
			return fmt.Errorf("load applications: %w", err)
		}
		repositories, err := gorm.G[models.Repository](tx).Find(ctx)
		if err != nil {
			return fmt.Errorf("load repositories: %w", err)
		}
		notifications, err := gorm.G[models.Notification](tx).Find(ctx)
		if err != nil {
			return fmt.Errorf("load notifications: %w", err)
		}
		oidcProviders, err := gorm.G[models.OIDCProvider](tx).Find(ctx)
		if err != nil {
			return fmt.Errorf("load oidc providers: %w", err)
		}

		if err := crypto.Init(newSecret); err != nil {
			return fmt.Errorf("initialize new encryption key: %w", err)
		}

		if err := rotateAgents(ctx, tx, agents, &result); err != nil {
			return err
		}
		if err := rotateApplications(ctx, tx, applications, &result); err != nil {
			return err
		}
		if err := rotateRepositories(ctx, tx, repositories, &result); err != nil {
			return err
		}
		if err := rotateNotifications(ctx, tx, notifications, &result); err != nil {
			return err
		}
		if err := rotateOIDCProviders(ctx, tx, oidcProviders, &result); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return keyRotationResult{}, err
	}

	return result, nil
}

func rotateAgents(ctx context.Context, tx *gorm.DB, agents []models.Agent, result *keyRotationResult) error {
	for _, agent := range agents {
		rows, err := gorm.G[models.Agent](tx).
			Where("id = ?", agent.Id).
			Updates(ctx, models.Agent{Name: agent.Name, KeyId: agent.KeyId})
		if err != nil {
			return fmt.Errorf("rotate agent %s: %w", agent.Id, err)
		}
		if rows == 0 {
			return fmt.Errorf("rotate agent %s: %w", agent.Id, gorm.ErrRecordNotFound)
		}
		result.Agents += rows
	}
	return nil
}

func rotateApplications(ctx context.Context, tx *gorm.DB, applications []models.Application, result *keyRotationResult) error {
	for _, application := range applications {
		rows, err := gorm.G[models.Application](tx).
			Where("id = ?", application.Id).
			Updates(ctx, models.Application{
				Name:                application.Name,
				ComposeFile:         application.ComposeFile,
				PreviousComposeFile: application.PreviousComposeFile,
			})
		if err != nil {
			return fmt.Errorf("rotate application %s: %w", application.Id, err)
		}
		if rows == 0 {
			return fmt.Errorf("rotate application %s: %w", application.Id, gorm.ErrRecordNotFound)
		}
		if application.ImageWebhookSecret != nil {
			if err := updateEncryptedField[models.Application](ctx, tx, application.Id, "image_webhook_secret", application.ImageWebhookSecret); err != nil {
				return fmt.Errorf("rotate application image webhook secret %s: %w", application.Id, err)
			}
		}
		result.Applications += rows
	}
	return nil
}

func rotateRepositories(ctx context.Context, tx *gorm.DB, repositories []models.Repository, result *keyRotationResult) error {
	for _, repository := range repositories {
		rotated := false
		if repository.AuthUser != nil {
			if err := updateEncryptedField[models.Repository](ctx, tx, repository.Id, "auth_user", repository.AuthUser); err != nil {
				return fmt.Errorf("rotate repository auth user %s: %w", repository.Id, err)
			}
			rotated = true
		}
		if repository.AuthToken != nil {
			if err := updateEncryptedField[models.Repository](ctx, tx, repository.Id, "auth_token", repository.AuthToken); err != nil {
				return fmt.Errorf("rotate repository auth token %s: %w", repository.Id, err)
			}
			rotated = true
		}
		if repository.WebhookSecret != nil {
			if err := updateEncryptedField[models.Repository](ctx, tx, repository.Id, "webhook_secret", repository.WebhookSecret); err != nil {
				return fmt.Errorf("rotate repository webhook secret %s: %w", repository.Id, err)
			}
			rotated = true
		}
		if rotated {
			result.Repositories++
		}
	}
	return nil
}

func rotateNotifications(ctx context.Context, tx *gorm.DB, notifications []models.Notification, result *keyRotationResult) error {
	for _, notification := range notifications {
		rows, err := gorm.G[models.Notification](tx).
			Where("id = ?", notification.Id).
			Updates(ctx, models.Notification{Name: notification.Name, Config: notification.Config})
		if err != nil {
			return fmt.Errorf("rotate notification %s: %w", notification.Id, err)
		}
		if rows == 0 {
			return fmt.Errorf("rotate notification %s: %w", notification.Id, gorm.ErrRecordNotFound)
		}
		result.Notifications += rows
	}
	return nil
}

func rotateOIDCProviders(ctx context.Context, tx *gorm.DB, providers []models.OIDCProvider, result *keyRotationResult) error {
	for _, provider := range providers {
		rows, err := gorm.G[models.OIDCProvider](tx).
			Where("id = ?", provider.Id).
			Updates(ctx, models.OIDCProvider{ClientSecret: provider.ClientSecret})
		if err != nil {
			return fmt.Errorf("rotate oidc provider %s: %w", provider.Id, err)
		}
		if rows == 0 {
			return fmt.Errorf("rotate oidc provider %s: %w", provider.Id, gorm.ErrRecordNotFound)
		}
		result.OIDCProviders += rows
	}
	return nil
}

func updateEncryptedField[T any](ctx context.Context, tx *gorm.DB, id string, column string, value any) error {
	rows, err := gorm.G[T](tx).Where("id = ?", id).Update(ctx, column, value)
	if err != nil {
		return err
	}
	if rows == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func renderKeyRotateResult(out io.Writer, result keyRotationResult) {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Green)
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.White)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.White)
	noteStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))

	body := strings.Join([]string{
		labelStyle.Render("Rows re-encrypted:") + " " + valueStyle.Render(fmt.Sprintf("%d", result.Total())),
		labelStyle.Render("Agents:") + " " + valueStyle.Render(fmt.Sprintf("%d", result.Agents)),
		labelStyle.Render("Applications:") + " " + valueStyle.Render(fmt.Sprintf("%d", result.Applications)),
		labelStyle.Render("Repositories:") + " " + valueStyle.Render(fmt.Sprintf("%d", result.Repositories)),
		labelStyle.Render("Notifications:") + " " + valueStyle.Render(fmt.Sprintf("%d", result.Notifications)),
		labelStyle.Render("OIDC providers:") + " " + valueStyle.Render(fmt.Sprintf("%d", result.OIDCProviders)),
		"",
		noteStyle.Render("Restart the hub with the new APP_SECRET."),
	}, "\n")

	card := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Blue).
		Padding(1, 2).
		Render(titleStyle.Render("Key Rotation Successful") + "\n\n" + body)

	_, _ = lipgloss.Fprintln(out, card)
}
