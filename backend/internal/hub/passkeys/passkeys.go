package passkeys

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"gorm.io/gorm"
)

const MaxCredentials = 20

type User struct {
	models.User
	Credentials []webauthn.Credential
}

func (u User) WebAuthnID() []byte                         { return []byte(u.Id) }
func (u User) WebAuthnName() string                       { return u.Email }
func (u User) WebAuthnDisplayName() string                { return u.Name }
func (u User) WebAuthnCredentials() []webauthn.Credential { return u.Credentials }

// New trusts only the configured application origin, never request Host headers.
func New(appURL string) (*webauthn.WebAuthn, error) {
	u, err := url.Parse(appURL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("invalid passkey application URL")
	}
	ip := net.ParseIP(u.Hostname())
	loopback := u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())
	if u.Scheme != "https" && (u.Scheme != "http" || !loopback) {
		return nil, errors.New("passkeys require HTTPS in APP_URL (HTTP is allowed on localhost)")
	}
	return webauthn.New(&webauthn.Config{
		RPID: u.Hostname(), RPDisplayName: "OrcaCD", RPOrigins: []string{u.Scheme + "://" + u.Host},
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			RequireResidentKey: protocol.ResidentKeyRequired(),
			UserVerification:   protocol.VerificationRequired,
		},
		AttestationPreference: protocol.PreferNoAttestation,
	})
}

func LoadUser(ctx context.Context, database *gorm.DB, id string) (User, error) {
	u, err := gorm.G[models.User](database).Where("id = ?", id).First(ctx)
	if err != nil {
		return User{}, err
	}
	rows, err := gorm.G[models.Passkey](database).Where("user_id = ?", id).Find(ctx)
	if err != nil {
		return User{}, err
	}
	user := User{User: u}
	for _, row := range rows {
		var credential webauthn.Credential
		if err := json.Unmarshal([]byte(row.Credential), &credential); err != nil {
			return User{}, err
		}
		user.Credentials = append(user.Credentials, credential)
	}
	return user, nil
}

func CredentialID(id []byte) string { return base64.RawURLEncoding.EncodeToString(id) }

func SaveCredential(ctx context.Context, database *gorm.DB, userID, name string, credential *webauthn.Credential) error {
	count, err := gorm.G[models.Passkey](database).Where("user_id = ?", userID).Count(ctx, "*")
	if err != nil {
		return err
	}
	if count >= MaxCredentials {
		return errors.New("passkey limit reached")
	}
	data, err := json.Marshal(credential)
	if err != nil {
		return err
	}
	return gorm.G[models.Passkey](database).Create(ctx, &models.Passkey{
		UserId: userID, Name: name, CredentialId: CredentialID(credential.ID), Credential: crypto.EncryptedString(data),
	})
}

func UpdateCredential(ctx context.Context, database *gorm.DB, userID string, credential *webauthn.Credential) error {
	data, err := json.Marshal(credential)
	if err != nil {
		return err
	}
	// Persist counters and backup flags after every assertion. A concurrent deletion must not recreate a key.
	rows, err := gorm.G[models.Passkey](database).Where("user_id = ? AND credential_id = ?", userID, CredentialID(credential.ID)).
		Select("credential", "last_used_at").Updates(ctx, models.Passkey{Credential: crypto.EncryptedString(data), LastUsedAt: new(time.Now())})
	if err != nil {
		return err
	}
	if rows != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
