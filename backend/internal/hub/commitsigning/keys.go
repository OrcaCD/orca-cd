// Package commitsigning verifies Git commit signatures against a set of
// trusted OpenPGP and SSH public keys.
package commitsigning

import (
	"bytes"
	"errors"
	"fmt"
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
	MaxKeysTextLength = 64 << 10
	MaxKeys           = 50

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
	pgp openpgp.EntityList
	ssh []trustedSSHKey
}

// ParseTrustedKeys parses armored OpenPGP public key blocks and SSH public keys
// in authorized_keys format (one per line). Blank lines and lines starting
// with '#' are ignored. Errors never echo key material.
func ParseTrustedKeys(text string) (*TrustedKeys, error) {
	if len(text) > MaxKeysTextLength {
		return nil, fmt.Errorf("signing keys exceed %d bytes", MaxKeysTextLength)
	}

	keys := &TrustedKeys{}
	seen := map[string]struct{}{}
	rest := text
	lineOffset := 0
	for {
		start := strings.Index(rest, pgpPublicKeyBegin)
		if start < 0 {
			if err := keys.parseSSHLines(rest, lineOffset, seen); err != nil {
				return nil, err
			}
			break
		}
		if err := keys.parseSSHLines(rest[:start], lineOffset, seen); err != nil {
			return nil, err
		}
		lineOffset += strings.Count(rest[:start], "\n")

		end := strings.Index(rest[start:], pgpPublicKeyEnd)
		if end < 0 {
			return nil, fmt.Errorf("line %d: unterminated PGP public key block", lineOffset+1)
		}
		block := rest[start : start+end+len(pgpPublicKeyEnd)]
		if err := keys.parsePGPBlock(block, lineOffset, seen); err != nil {
			return nil, err
		}
		lineOffset += strings.Count(block, "\n")
		rest = rest[start+len(block):]
	}

	if keys.Len() > MaxKeys {
		return nil, fmt.Errorf("at most %d signing keys are allowed", MaxKeys)
	}
	return keys, nil
}

func (k *TrustedKeys) parsePGPBlock(block string, lineOffset int, seen map[string]struct{}) error {
	entities, err := openpgp.ReadArmoredKeyRing(strings.NewReader(block))
	if err != nil {
		return fmt.Errorf("line %d: invalid PGP public key block", lineOffset+1)
	}
	for _, entity := range entities {
		if entity.PrivateKey != nil {
			return fmt.Errorf("line %d: PGP key block must not contain private keys", lineOffset+1)
		}
		fingerprint := pgpFingerprint(entity)
		if _, ok := seen[fingerprint]; ok {
			continue
		}
		seen[fingerprint] = struct{}{}
		k.pgp = append(k.pgp, entity)
	}
	return nil
}

func (k *TrustedKeys) parseSSHLines(text string, lineOffset int, seen map[string]struct{}) error {
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lineNumber := lineOffset + i + 1
		if strings.HasPrefix(line, "-----BEGIN") {
			return fmt.Errorf("line %d: unsupported key block, expected a PGP public key block", lineNumber)
		}
		key, comment, options, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			return fmt.Errorf("line %d: invalid SSH public key", lineNumber)
		}
		if len(options) > 0 {
			return fmt.Errorf("line %d: SSH key options are not supported", lineNumber)
		}
		if _, ok := key.(*ssh.Certificate); ok {
			return fmt.Errorf("line %d: SSH certificates are not supported", lineNumber)
		}
		fingerprint := ssh.FingerprintSHA256(key)
		if _, ok := seen[fingerprint]; ok {
			continue
		}
		seen[fingerprint] = struct{}{}
		k.ssh = append(k.ssh, trustedSSHKey{key: key, comment: comment})
	}
	return nil
}

func (k *TrustedKeys) Len() int {
	return len(k.pgp) + len(k.ssh)
}

// Signers describes every trusted key, PGP keys first.
func (k *TrustedKeys) Signers() []Signer {
	signers := make([]Signer, 0, k.Len())
	for _, entity := range k.pgp {
		signers = append(signers, pgpSigner(entity))
	}
	for _, key := range k.ssh {
		signers = append(signers, sshSigner(key))
	}
	return signers
}

// PGPSignerByKeyID returns the trusted, unrevoked PGP key whose primary key
// or subkey has the given 64-bit hex key ID.
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
	for _, key := range k.pgp.KeysById(id) {
		if key.Entity.Revoked(now) || key.Revoked(now) {
			continue
		}
		return pgpSigner(key.Entity), true
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
