package passkeys

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"gorm.io/gorm"
)

const EnrollmentLifetime = 24 * time.Hour

func TokenHash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

// IssueEnrollment must run in the caller's transaction, along with account creation
// or revocation. The raw code is returned once and is never stored.
func IssueEnrollment(ctx context.Context, tx *gorm.DB, user *models.User) (string, error) {
	token := base64.RawURLEncoding.EncodeToString(randomBytes())
	if _, err := gorm.G[models.PasskeyEnrollment](tx).Where("user_id = ?", user.Id).Delete(ctx); err != nil {
		return "", err
	}
	err := gorm.G[models.PasskeyEnrollment](tx).Create(ctx, &models.PasskeyEnrollment{
		UserId: user.Id, TokenHash: TokenHash(token), TokenVersion: user.TokenVersion, ExpiresAt: time.Now().Add(EnrollmentLifetime),
	})
	return token, err
}

func ConsumeEnrollment(ctx context.Context, tx *gorm.DB, user *models.User, hash string) error {
	rows, err := gorm.G[models.PasskeyEnrollment](tx).
		Where("user_id = ? AND token_hash = ? AND token_version = ? AND expires_at > ?", user.Id, hash, user.TokenVersion, time.Now()).Delete(ctx)
	if err != nil {
		return err
	}
	if rows != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// ResetLocal is shared by admin recovery and the offline CLI. The caller
// owns the transaction so credential/session revocation and code issuance are atomic.
func ResetLocal(ctx context.Context, tx *gorm.DB, user *models.User) (string, error) {
	current, err := gorm.G[models.User](tx).Where("id = ?", user.Id).First(ctx)
	if err != nil {
		return "", err
	}
	*user = current
	if !user.LocalAuthEnabled {
		return "", errors.New("cannot reset passkeys for a managed user")
	}
	if _, err := gorm.G[models.Passkey](tx).Where("user_id = ?", user.Id).Delete(ctx); err != nil {
		return "", err
	}
	rows, err := gorm.G[models.User](tx).Where("id = ?", user.Id).Update(ctx, "token_version", gorm.Expr("token_version + 1"))
	if err != nil {
		return "", err
	}
	if rows != 1 {
		return "", gorm.ErrRecordNotFound
	}
	user.TokenVersion++
	return IssueEnrollment(ctx, tx, user)
}
