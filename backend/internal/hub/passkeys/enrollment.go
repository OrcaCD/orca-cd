package passkeys

import (
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"gorm.io/gorm"
)

const EnrollmentLifetime = 24 * time.Hour

func TokenHash(token string) string {
	digest := sha512.Sum512([]byte(token))
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
