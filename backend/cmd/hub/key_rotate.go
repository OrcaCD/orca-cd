package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/OrcaCD/orca-cd/internal/hub"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

const minKeyRotateSecretLength = 32
const keyRotationBatchSize = 100

var (
	errMissingOldSecret = errors.New("old secret is required")
	errMissingNewSecret = errors.New("new secret is required")
	errSameSecrets      = errors.New("old and new secrets must be different")
)

type keyRotateOptions struct {
	OldSecret        string
	SkipConfirmation bool
}

type keyRotationSecrets struct {
	oldCipher *crypto.Cipher
	newCipher *crypto.Cipher
}

type keyRotationResult struct {
	Agents        int
	Applications  int
	Repositories  int
	Notifications int
	OIDCProviders int
}

type encryptedModelSchema struct {
	table            string
	primaryColumn    string
	primaryField     *schema.Field
	encryptedColumns []string
	encryptedFields  []*schema.Field
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
	cfg, err := hub.DefaultConfig()
	if err != nil {
		return fmt.Errorf("failed to load hub configuration: %w", err)
	}

	if cfg.Demo {
		return fmt.Errorf("key rotation is not available in demo mode")
	}

	secrets, err := prepareKeyRotationSecrets(options.OldSecret, cfg.AppSecret)
	if err != nil {
		return err
	}

	warningPoints := []string{
		"The hub server must be stopped before rotating the key",
		"Create a database export before rotating the key",
		"The database will be re-encrypted with the current APP_SECRET",
		"Sessions and agent tokens signed with the old APP_SECRET need to be re-issued",
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

	result, err := rotateDatabaseEncryptionKeyWithSecrets(ctx, secrets)
	if err != nil {
		return fmt.Errorf(
			"key rotation failed: %w. Restore the database export created before rotation, keep the old APP_SECRET configured, and retry after resolving the error",
			err,
		)
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

func prepareKeyRotationSecrets(oldSecret, newSecret string) (keyRotationSecrets, error) {
	oldSecret = strings.TrimSpace(oldSecret)
	newSecret = strings.TrimSpace(newSecret)

	if err := validateKeyRotateSecrets(oldSecret, newSecret); err != nil {
		return keyRotationSecrets{}, err
	}

	oldCipher, err := crypto.New(oldSecret)
	if err != nil {
		return keyRotationSecrets{}, fmt.Errorf("initialize old encryption key: %w", err)
	}
	newCipher, err := crypto.New(newSecret)
	if err != nil {
		return keyRotationSecrets{}, fmt.Errorf("initialize new encryption key: %w", err)
	}

	return keyRotationSecrets{
		oldCipher: oldCipher,
		newCipher: newCipher,
	}, nil
}

func rotateDatabaseEncryptionKey(ctx context.Context, oldSecret, newSecret string) (keyRotationResult, error) {
	secrets, err := prepareKeyRotationSecrets(oldSecret, newSecret)
	if err != nil {
		return keyRotationResult{}, err
	}
	return rotateDatabaseEncryptionKeyWithSecrets(ctx, secrets)
}

func rotateDatabaseEncryptionKeyWithSecrets(ctx context.Context, secrets keyRotationSecrets) (keyRotationResult, error) {
	if db.DB == nil {
		return keyRotationResult{}, errors.New("database is not connected")
	}

	var result keyRotationResult
	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := rotateEncryptedModel[models.Agent](ctx, tx, secrets, "agents", func(rows int) {
			result.Agents += rows
		}); err != nil {
			return err
		}
		if err := rotateEncryptedModel[models.Application](ctx, tx, secrets, "applications", func(rows int) {
			result.Applications += rows
		}); err != nil {
			return err
		}
		if err := rotateEncryptedModel[models.Repository](ctx, tx, secrets, "repositories", func(rows int) {
			result.Repositories += rows
		}); err != nil {
			return err
		}
		if err := rotateEncryptedModel[models.Notification](ctx, tx, secrets, "notifications", func(rows int) {
			result.Notifications += rows
		}); err != nil {
			return err
		}
		if err := rotateEncryptedModel[models.OIDCProvider](ctx, tx, secrets, "oidc providers", func(rows int) {
			result.OIDCProviders += rows
		}); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		crypto.SetDefault(secrets.oldCipher)
		return keyRotationResult{}, err
	}

	crypto.SetDefault(secrets.newCipher)
	return result, nil
}

func rotateEncryptedModel[T any](
	ctx context.Context,
	tx *gorm.DB,
	secrets keyRotationSecrets,
	label string,
	addRows func(int),
) error {
	modelSchema, err := encryptedModelSchemaFor[T](tx)
	if err != nil {
		return fmt.Errorf("inspect encrypted fields for %s: %w", label, err)
	}
	if len(modelSchema.encryptedFields) == 0 {
		return nil
	}

	return rotateEncryptedBatches[T](ctx, tx, secrets.oldCipher, secrets.newCipher, func(batch []T) error {
		rows, err := rotateEncryptedBatch(ctx, tx, batch, modelSchema, label)
		if err != nil {
			return err
		}
		addRows(rows)
		return nil
	})
}

func encryptedModelSchemaFor[T any](tx *gorm.DB) (encryptedModelSchema, error) {
	var model T
	stmt := &gorm.Statement{DB: tx}
	if err := stmt.Parse(&model); err != nil {
		return encryptedModelSchema{}, err
	}

	primaryField := stmt.Schema.PrioritizedPrimaryField
	if primaryField == nil || primaryField.DBName == "" {
		return encryptedModelSchema{}, fmt.Errorf("model %s has no primary key", stmt.Schema.Name)
	}

	encryptedStringType := reflect.TypeFor[crypto.EncryptedString]()
	encryptedFields := make([]*schema.Field, 0)
	for _, field := range stmt.Schema.Fields {
		if field.DBName == "" || !field.Updatable {
			continue
		}
		if field.IndirectFieldType == encryptedStringType {
			encryptedFields = append(encryptedFields, field)
		}
	}

	sort.Slice(encryptedFields, func(i, j int) bool {
		return encryptedFields[i].DBName < encryptedFields[j].DBName
	})

	encryptedColumns := make([]string, 0, len(encryptedFields))
	for _, field := range encryptedFields {
		encryptedColumns = append(encryptedColumns, field.DBName)
	}

	return encryptedModelSchema{
		table:            stmt.Schema.Table,
		primaryColumn:    primaryField.DBName,
		primaryField:     primaryField,
		encryptedColumns: encryptedColumns,
		encryptedFields:  encryptedFields,
	}, nil
}

func rotateEncryptedBatches[T any](
	ctx context.Context,
	tx *gorm.DB,
	oldCipher *crypto.Cipher,
	newCipher *crypto.Cipher,
	rotate func([]T) error,
) error {
	crypto.SetDefault(oldCipher)

	return gorm.G[T](tx).FindInBatches(ctx, keyRotationBatchSize, func(batch []T, _ int) error {
		crypto.SetDefault(newCipher)
		if err := rotate(batch); err != nil {
			crypto.SetDefault(oldCipher)
			return err
		}
		crypto.SetDefault(oldCipher)
		return nil
	})
}

func rotateEncryptedBatch[T any](
	ctx context.Context,
	tx *gorm.DB,
	batch []T,
	modelSchema encryptedModelSchema,
	label string,
) (int, error) {
	rowsRotated := 0
	selectColumn := modelSchema.encryptedColumns[0]
	selectArgs := make([]any, 0, len(modelSchema.encryptedColumns)-1)
	for _, column := range modelSchema.encryptedColumns[1:] {
		selectArgs = append(selectArgs, column)
	}

	for _, record := range batch {
		updates, primaryValue, hasEncryptedValue, err := encryptedFieldUpdate(ctx, record, modelSchema)
		if err != nil {
			return 0, fmt.Errorf("prepare %s update: %w", label, err)
		}
		if !hasEncryptedValue {
			continue
		}

		rows, err := gorm.G[T](tx).
			Select(selectColumn, selectArgs...).
			Where(modelSchema.primaryColumn+" = ?", primaryValue).
			Updates(ctx, updates)
		if err != nil {
			return rowsRotated, fmt.Errorf("rotate %s %v: %w", label, primaryValue, err)
		}
		if rows == 0 {
			return rowsRotated, fmt.Errorf("rotate %s %v: %w", label, primaryValue, gorm.ErrRecordNotFound)
		}
		rowsRotated += rows
	}

	return rowsRotated, nil
}

func encryptedFieldUpdate[T any](
	ctx context.Context,
	record T,
	modelSchema encryptedModelSchema,
) (T, any, bool, error) {
	var updates T
	source := reflect.ValueOf(record)
	target := reflect.ValueOf(&updates).Elem()

	primaryValue, primaryZero := modelSchema.primaryField.ValueOf(ctx, source)
	if primaryZero {
		return updates, nil, false, fmt.Errorf("%s primary key is empty", modelSchema.table)
	}

	hasEncryptedValue := false
	for _, field := range modelSchema.encryptedFields {
		value, _ := field.ValueOf(ctx, source)
		if hasRotatableEncryptedValue(value) {
			hasEncryptedValue = true
		}
		if err := field.Set(ctx, target, value); err != nil {
			return updates, nil, false, err
		}
	}

	return updates, primaryValue, hasEncryptedValue, nil
}

func hasRotatableEncryptedValue(value any) bool {
	switch v := value.(type) {
	case crypto.EncryptedString:
		return v != ""
	case *crypto.EncryptedString:
		return v != nil && *v != ""
	default:
		return value != nil
	}
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
