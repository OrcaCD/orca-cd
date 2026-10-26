// Package commitsigning verifies Git commit signatures against a set of
// trusted OpenPGP and SSH public keys.
package commitsigning

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"golang.org/x/crypto/ssh"
)

type KeyType string

const (
	KeyTypePGP KeyType = "pgp"
	KeyTypeSSH KeyType = "ssh"
)

const (
	MaxKeys      = 50
	MaxKeyLength = 32 << 10

	pgpPublicKeyBegin = "-----BEGIN PGP PUBLIC KEY BLOCK-----"
	pgpPublicKeyEnd   = "-----END PGP PUBLIC KEY BLOCK-----"
)

var errNoTrustedKeys = errors.New("no trusted signing keys configured")

// Signer describes a trusted key.
type Signer struct {
	Type        KeyType `json:"type"`
	Fingerprint string  `json:"fingerprint"`
	Identity    string  `json:"identity"`
}

func (s Signer) String() string {
	if s.Identity == "" {
		return fmt.Sprintf("%s key %s", s.Type, s.Fingerprint)
	}
	return fmt.Sprintf("%s key %s (%s)", s.Type, s.Fingerprint, s.Identity)
}

type trustedSSHKey struct {
	key     ssh.PublicKey
	comment string
}

// TrustedKeys is the set of public keys whose commit signatures are accepted.
type TrustedKeys struct {
	pgp     openpgp.EntityList
	ssh     []trustedSSHKey
	signers []Signer
}

// ParseTrustedKeys parses trusted keys, each either one armored OpenPGP public
// key block or one SSH public key in authorized_keys format. Errors never echo
// key material.
func ParseTrustedKeys(entries []string) (*TrustedKeys, error) {
	if len(entries) > MaxKeys {
		return nil, fmt.Errorf("at most %d signing keys are allowed", MaxKeys)
	}

	keys := &TrustedKeys{}
	seen := map[string]int{}
	for i, entry := range entries {
		number := i + 1
		entry = strings.TrimSpace(entry)
		if len(entry) > MaxKeyLength {
			return nil, fmt.Errorf("key %d exceeds %d bytes", number, MaxKeyLength)
		}

		var signer Signer
		if strings.HasPrefix(entry, pgpPublicKeyBegin) {
			entity, err := parsePGPKey(entry)
			if err != nil {
				return nil, fmt.Errorf("key %d: %w", number, err)
			}
			signer = pgpSigner(entity)
			keys.pgp = append(keys.pgp, entity)
		} else {
			key, err := parseSSHKey(entry)
			if err != nil {
				return nil, fmt.Errorf("key %d: %w", number, err)
			}
			signer = sshSigner(key)
			keys.ssh = append(keys.ssh, key)
		}

		if previous, ok := seen[signer.Fingerprint]; ok {
			return nil, fmt.Errorf("key %d is a duplicate of key %d", number, previous)
		}
		seen[signer.Fingerprint] = number
		keys.signers = append(keys.signers, signer)
	}
	return keys, nil
}

func parsePGPKey(block string) (*openpgp.Entity, error) {
	if !strings.HasSuffix(block, pgpPublicKeyEnd) {
		return nil, errors.New("PGP public key block must end with " + pgpPublicKeyEnd)
	}
	// The armor reader stops after the first block and would ignore any further ones.
	if strings.Count(block, pgpPublicKeyBegin) != 1 {
		return nil, errors.New("PGP public key block must contain exactly one key")
	}
	entities, err := openpgp.ReadArmoredKeyRing(strings.NewReader(block))
	if err != nil {
		return nil, errors.New("invalid PGP public key block")
	}
	if len(entities) != 1 {
		return nil, errors.New("PGP public key block must contain exactly one key")
	}
	if entities[0].PrivateKey != nil {
		return nil, errors.New("PGP key block must not contain a private key")
	}
	return entities[0], nil
}

func parseSSHKey(line string) (trustedSSHKey, error) {
	switch {
	case line == "":
		return trustedSSHKey{}, errors.New("key is empty")
	case strings.HasPrefix(line, "-----BEGIN"):
		return trustedSSHKey{}, errors.New("unsupported key block, expected a PGP public key block")
	case strings.Contains(line, "\n"):
		return trustedSSHKey{}, errors.New("expected a single SSH public key")
	}
	key, comment, options, _, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		return trustedSSHKey{}, errors.New("invalid SSH public key")
	}
	if len(options) > 0 {
		return trustedSSHKey{}, errors.New("SSH key options are not supported")
	}
	if _, ok := key.(*ssh.Certificate); ok {
		return trustedSSHKey{}, errors.New("SSH certificates are not supported")
	}
	return trustedSSHKey{key: key, comment: comment}, nil
}

func (k *TrustedKeys) Len() int {
	return len(k.pgp) + len(k.ssh)
}

// Signers describes every trusted key in the order they were parsed.
func (k *TrustedKeys) Signers() []Signer {
	return slices.Clone(k.signers)
}

// PGPSignerByKeyID returns the trusted PGP key whose primary key has the given
// 64-bit hex key ID. Keys that are expired, revoked or have no usable signing
// key are not returned, since the signature time is unknown to the caller.
func (k *TrustedKeys) PGPSignerByKeyID(keyID string) (Signer, bool) {
	keyID = strings.TrimPrefix(strings.TrimSpace(keyID), "0x")
	if len(keyID) != 16 {
		return Signer{}, false
	}
	id, err := strconv.ParseUint(keyID, 16, 64)
	if err != nil {
		return Signer{}, false
	}
	now := time.Now()
	for _, entity := range k.pgp {
		if entity.PrimaryKey.KeyId != id {
			continue
		}
		// SigningKey also rejects an expired or revoked primary key.
		if _, ok := entity.SigningKey(now); ok {
			return pgpSigner(entity), true
		}
	}
	return Signer{}, false
}

// SSHSignerByAuthorizedKey returns the trusted SSH key equal to the given
// authorized_keys formatted public key.
func (k *TrustedKeys) SSHSignerByAuthorizedKey(authorizedKey string) (Signer, bool) {
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.TrimSpace(authorizedKey)))
	if err != nil {
		return Signer{}, false
	}
	trusted, ok := k.sshKeyByWireKey(key.Marshal())
	if !ok {
		return Signer{}, false
	}
	return sshSigner(trusted), true
}

func (k *TrustedKeys) sshKeyByWireKey(wireKey []byte) (trustedSSHKey, bool) {
	for _, key := range k.ssh {
		if bytes.Equal(key.key.Marshal(), wireKey) {
			return key, true
		}
	}
	return trustedSSHKey{}, false
}

func pgpSigner(entity *openpgp.Entity) Signer {
	signer := Signer{Type: KeyTypePGP, Fingerprint: pgpFingerprint(entity)}
	if identity := entity.PrimaryIdentity(); identity != nil {
		signer.Identity = identity.Name
	}
	return signer
}

func pgpFingerprint(entity *openpgp.Entity) string {
	return strings.ToUpper(fmt.Sprintf("%x", entity.PrimaryKey.Fingerprint))
}

func sshSigner(key trustedSSHKey) Signer {
	return Signer{Type: KeyTypeSSH, Fingerprint: ssh.FingerprintSHA256(key.key), Identity: key.comment}
}
