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

var ErrCredentialLimit = errors.New("passkey limit reached")

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
	// Decode each row separately so one damaged credential cannot hide healthy keys.
	type storedCredential struct {
		Id         string
		Credential string
	}
	rows, err := gorm.G[storedCredential](database).Table("passkeys").Select("id", "credential").Where("user_id = ?", id).Find(ctx)
	if err != nil {
		return User{}, err
	}
	user := User{User: u}
	for _, row := range rows {
		var data crypto.EncryptedString
		var credential webauthn.Credential
		err := data.Scan(row.Credential)
		if err == nil {
			err = json.Unmarshal([]byte(data), &credential)
		}
		if err != nil {
			database.Logger.Error(ctx, "skipping unreadable passkey %s for user %s: %v", row.Id, id, err)
			continue
		}
		user.Credentials = append(user.Credentials, credential)
	}
	return user, nil
}

func CredentialID(id []byte) string { return base64.RawURLEncoding.EncodeToString(id) }

func SaveCredential(ctx context.Context, database *gorm.DB, userID, name string, credential *webauthn.Credential) error {
	data, err := json.Marshal(credential)
	if err != nil {
		return err
	}
	return database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// SQLite's immediate transactions serialize the count with the insert.
		count, err := gorm.G[models.Passkey](tx).Where("user_id = ?", userID).Count(ctx, "*")
		if err != nil {
			return err
		}
		if count >= MaxCredentials {
			return ErrCredentialLimit
		}
		return gorm.G[models.Passkey](tx).Create(ctx, &models.Passkey{
			UserId: userID, Name: name, CredentialId: CredentialID(credential.ID), Credential: crypto.EncryptedString(data),
		})
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
