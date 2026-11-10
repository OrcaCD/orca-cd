package commitsigning

import (
	"bytes"
	"crypto/sha1" //nolint:gosec // SHA-1 is the object ID algorithm of SHA-1 Git repositories
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"
	pgperrors "github.com/ProtonMail/go-crypto/openpgp/errors"
	"golang.org/x/crypto/ssh"
)

var (
	ErrUnsigned        = errors.New("commit is not signed")
	ErrUntrustedSigner = errors.New("commit is not signed by a trusted key")
	errCommitMismatch  = errors.New("signed commit data does not match the commit hash")
)

const (
	pgpSignatureBegin   = "-----BEGIN PGP SIGNATURE-----"
	sshSignatureBegin   = "-----BEGIN SSH SIGNATURE-----"
	sshSignatureEnd     = "-----END SSH SIGNATURE-----"
	sshSignatureMagic   = "SSHSIG"
	sshSignatureVersion = 1
	gitSSHNamespace     = "git"
)

// IsCommitHash reports whether sha is a full lowercase or uppercase hex SHA-1
// or SHA-256 Git object ID. Abbreviated hashes and ref names are rejected so a
// verified commit cannot change between verification and checkout.
func IsCommitHash(sha string) bool {
	if len(sha) != sha1.Size*2 && len(sha) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(sha)
	return err == nil
}

// VerifyCommit checks that payload (the raw commit object without its
// signature header) and signature together form the commit identified by sha,
// and that signature is a valid signature over payload by a trusted key.
func (k *TrustedKeys) VerifyCommit(sha, payload, signature string) (Signer, error) {
	if k.Len() == 0 {
		return Signer{}, errNoTrustedKeys
	}
	if strings.TrimSpace(signature) == "" {
		return Signer{}, ErrUnsigned
	}
	if err := checkCommitHash(sha, payload, signature); err != nil {
		return Signer{}, err
	}

	trimmed := strings.TrimSpace(signature)
	switch {
	case strings.HasPrefix(trimmed, pgpSignatureBegin):
		return k.verifyPGP([]byte(payload), trimmed)
	case strings.HasPrefix(trimmed, sshSignatureBegin):
		return k.verifySSH([]byte(payload), trimmed)
	default:
		return Signer{}, errors.New("unsupported signature format, only PGP and SSH signatures are supported")
	}
}

// checkCommitHash rebuilds the commit object the way Git embeds signatures
// (see add_header_signature in Git's commit.c) and compares its object ID.
func checkCommitHash(sha, payload, signature string) error {
	var h hash.Hash
	var header string
	switch len(sha) {
	case sha1.Size * 2:
		h, header = sha1.New(), "gpgsig" //nolint:gosec // see import
	case sha256.Size * 2:
		h, header = sha256.New(), "gpgsig-sha256"
	default:
		return fmt.Errorf("invalid commit hash %q", sha)
	}

	insertAt := len(payload)
	if end := strings.Index(payload, "\n\n"); end >= 0 {
		insertAt = end + 1
	}
	if !strings.HasSuffix(signature, "\n") {
		signature += "\n"
	}

	var object strings.Builder
	object.WriteString(payload[:insertAt])
	object.WriteString(header)
	for line := range strings.SplitAfterSeq(signature, "\n") {
		if line == "" {
			continue
		}
		object.WriteByte(' ')
		object.WriteString(line)
	}
	object.WriteString(payload[insertAt:])

	_, _ = fmt.Fprintf(h, "commit %d\x00", object.Len())
	_, _ = h.Write([]byte(object.String()))
	if hex.EncodeToString(h.Sum(nil)) != strings.ToLower(sha) {
		return errCommitMismatch
	}
	return nil
}

func (k *TrustedKeys) verifyPGP(payload []byte, signature string) (Signer, error) {
	if len(k.pgp) == 0 {
		return Signer{}, ErrUntrustedSigner
	}
	entity, err := openpgp.CheckArmoredDetachedSignature(k.pgp, bytes.NewReader(payload), strings.NewReader(signature), nil)
	if err != nil {
		if errors.Is(err, pgperrors.ErrUnknownIssuer) {
			return Signer{}, ErrUntrustedSigner
		}
		return Signer{}, fmt.Errorf("invalid PGP signature: %w", err)
	}
	return pgpSigner(entity), nil
}

// sshSignature is the SSHSIG wire format following the magic preamble,
// see https://github.com/openssh/openssh-portable/blob/master/PROTOCOL.sshsig
type sshSignature struct {
	Version       uint32
	PublicKey     []byte
	Namespace     string
	Reserved      string
	HashAlgorithm string
	Signature     []byte
}

type sshSignedData struct {
	Namespace     string
	Reserved      string
	HashAlgorithm string
	Hash          []byte
}

func (k *TrustedKeys) verifySSH(payload []byte, armored string) (Signer, error) {
	body, ok := strings.CutPrefix(armored, sshSignatureBegin)
	if ok {
		body, ok = strings.CutSuffix(body, sshSignatureEnd)
	}
	if !ok {
		return Signer{}, errors.New("malformed SSH signature armor")
	}
	blob, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(body), ""))
	if err != nil {
		return Signer{}, errors.New("malformed SSH signature encoding")
	}

	rest, ok := bytes.CutPrefix(blob, []byte(sshSignatureMagic))
	if !ok {
		return Signer{}, errors.New("malformed SSH signature")
	}
	var sig sshSignature
	if err := ssh.Unmarshal(rest, &sig); err != nil {
		return Signer{}, errors.New("malformed SSH signature")
	}
	if sig.Version != sshSignatureVersion {
		return Signer{}, fmt.Errorf("unsupported SSH signature version %d", sig.Version)
	}
	if sig.Namespace != gitSSHNamespace {
		return Signer{}, fmt.Errorf("SSH signature namespace must be %q", gitSSHNamespace)
	}

	var digest hash.Hash
	switch sig.HashAlgorithm {
	case "sha256":
		digest = sha256.New()
	case "sha512":
		digest = sha512.New()
	default:
		return Signer{}, errors.New("unsupported SSH signature hash algorithm")
	}

	trusted, ok := k.sshKeyByWireKey(sig.PublicKey)
	if !ok {
		return Signer{}, ErrUntrustedSigner
	}

	var keySig ssh.Signature
	if err := ssh.Unmarshal(sig.Signature, &keySig); err != nil {
		return Signer{}, errors.New("malformed SSH signature")
	}
	// PROTOCOL.sshsig forbids SHA-1 based RSA signatures.
	if keySig.Format == ssh.KeyAlgoRSA {
		return Signer{}, errors.New("SSH signatures using ssh-rsa (SHA-1) are not supported")
	}

	_, _ = digest.Write(payload)
	signed := append([]byte(sshSignatureMagic), ssh.Marshal(sshSignedData{
		Namespace:     sig.Namespace,
		Reserved:      sig.Reserved,
		HashAlgorithm: sig.HashAlgorithm,
		Hash:          digest.Sum(nil),
	})...)
	if err := trusted.key.Verify(signed, &keySig); err != nil {
		return Signer{}, errors.New("invalid SSH signature")
	}
	return sshSigner(trusted), nil
}
