package commitsigning

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// Fixtures were created with git 2.56 using throwaway keys, see testdata/.
const (
	sshCommitSHA       = "45845b34bb6dff50ff93518db435c7d6944496ef"
	pgpCommitSHA       = "31ea419444df6e85c0bc06d6c1b6bab6c41abd33"
	unsignedCommitSHA  = "7814c370243473a63a0c7155d5f9c5bd2cc360bb"
	sshSHA256CommitSHA = "59c3e3ea686a91e9fb0c2f098bcc919f0eea97eae5c8b77d7baea2dd548ad5ee"

	trustedPGPFingerprint = "7181B8DD33C16E2FB4768A91E776AAD894292F76"
	trustedPGPKeyID       = "E776AAD894292F76"
	trustedSSHFingerprint = "SHA256:yIa501Nr65OG7YIn37YygYUHzAEEbJ+qhJLorL9gD8I"
)

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name)) //nolint:gosec // fixed test fixture names
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}

// splitCommit separates a raw commit object into the payload and signature
// the way Git providers report them.
func splitCommit(t *testing.T, raw string) (payload, signature string) {
	t.Helper()
	var payloadBuilder, signatureBuilder strings.Builder
	inSignature, inMessage := false, false
	for line := range strings.SplitAfterSeq(raw, "\n") {
		switch {
		case inMessage:
			payloadBuilder.WriteString(line)
		case inSignature && strings.HasPrefix(line, " "):
			signatureBuilder.WriteString(line[1:])
		case strings.HasPrefix(line, "gpgsig "), strings.HasPrefix(line, "gpgsig-sha256 "):
			inSignature = true
			_, value, _ := strings.Cut(line, " ")
			signatureBuilder.WriteString(value)
		default:
			inSignature = false
			inMessage = line == "\n"
			payloadBuilder.WriteString(line)
		}
	}
	return payloadBuilder.String(), signatureBuilder.String()
}

func mustParseKeys(t *testing.T, entries ...string) *TrustedKeys {
	t.Helper()
	keys, err := ParseTrustedKeys(entries)
	if err != nil {
		t.Fatalf("ParseTrustedKeys: %v", err)
	}
	return keys
}

func TestParseTrustedKeys(t *testing.T) {
	keys := mustParseKeys(t,
		readFixture(t, "ssh_trusted.pub"),
		"\n"+readFixture(t, "pgp_trusted.asc")+"\n",
		readFixture(t, "ssh_other.pub"),
	)
	signers := keys.Signers()
	if len(signers) != 3 {
		t.Fatalf("expected 3 keys, got %d: %+v", len(signers), signers)
	}
	if signers[0] != (Signer{Type: KeyTypeSSH, Fingerprint: trustedSSHFingerprint, Identity: "trusted@orcacd.test"}) {
		t.Errorf("unexpected SSH signer: %+v", signers[0])
	}
	if signers[1] != (Signer{Type: KeyTypePGP, Fingerprint: trustedPGPFingerprint, Identity: "OrcaCD Test <trusted@orcacd.test>"}) {
		t.Errorf("unexpected PGP signer: %+v", signers[1])
	}
}

func TestParseTrustedKeysEmpty(t *testing.T) {
	keys := mustParseKeys(t)
	if keys.Len() != 0 || len(keys.Signers()) != 0 {
		t.Fatalf("expected no keys, got %d", keys.Len())
	}
}

func TestParseTrustedKeysErrors(t *testing.T) {
	sshKey := readFixture(t, "ssh_trusted.pub")
	pgp := strings.TrimSpace(readFixture(t, "pgp_trusted.asc"))
	pgpWithoutEnd, _, _ := strings.Cut(pgp, "-----END")
	tests := map[string]struct {
		entries []string
		want    string
	}{
		"invalid ssh key":    {[]string{"ssh-ed25519 not-base64"}, "key 1: invalid SSH public key"},
		"empty key":          {[]string{sshKey, " \n"}, "key 2: key is empty"},
		"two ssh keys":       {[]string{sshKey + "\n" + readFixture(t, "ssh_other.pub")}, "key 1: expected a single SSH public key"},
		"garbage after pgp":  {[]string{pgp + "\nnope"}, "key 1: PGP public key block must end with"},
		"unterminated pgp":   {[]string{pgpWithoutEnd}, "key 1: PGP public key block must end with"},
		"corrupt pgp":        {[]string{strings.Replace(pgp, "mDMEa", "mDMEb", 1)}, "key 1: invalid PGP public key block"},
		"two pgp keys":       {[]string{pgp + "\n" + strings.TrimSpace(readFixture(t, "pgp_other.asc"))}, "key 1: PGP public key block must contain exactly one key"},
		"private key block":  {[]string{"-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----"}, "key 1: unsupported key block"},
		"ssh key options":    {[]string{`command="x" ` + sshKey}, "key 1: SSH key options are not supported"},
		"allowed signers id": {[]string{"trusted@orcacd.test " + sshKey}, "key 1: SSH key options are not supported"},
		"duplicate":          {[]string{sshKey, pgp, sshKey}, "key 3 is a duplicate of key 1"},
		"too large":          {[]string{strings.Repeat("a", MaxKeyLength+1)}, "key 1 exceeds"},
		"too many":           {make([]string, MaxKeys+1), "at most"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := ParseTrustedKeys(tt.entries)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestVerifyCommit(t *testing.T) {
	trusted := mustParseKeys(t, readFixture(t, "ssh_trusted.pub"), readFixture(t, "pgp_trusted.asc"))
	untrusted := mustParseKeys(t, readFixture(t, "ssh_other.pub"), readFixture(t, "pgp_other.asc"))

	tests := []struct {
		name    string
		fixture string
		sha     string
		keys    *TrustedKeys
		want    KeyType
		wantErr error
	}{
		{name: "ssh signed", fixture: "ssh_commit.txt", sha: sshCommitSHA, keys: trusted, want: KeyTypeSSH},
		{name: "ssh signed uppercase sha", fixture: "ssh_commit.txt", sha: strings.ToUpper(sshCommitSHA), keys: trusted, want: KeyTypeSSH},
		{name: "ssh signed sha256 repository", fixture: "ssh_sha256_commit.txt", sha: sshSHA256CommitSHA, keys: trusted, want: KeyTypeSSH},
		{name: "pgp signed", fixture: "pgp_commit.txt", sha: pgpCommitSHA, keys: trusted, want: KeyTypePGP},
		{name: "ssh untrusted", fixture: "ssh_commit.txt", sha: sshCommitSHA, keys: untrusted, wantErr: ErrUntrustedSigner},
		{name: "pgp untrusted", fixture: "pgp_commit.txt", sha: pgpCommitSHA, keys: untrusted, wantErr: ErrUntrustedSigner},
		{name: "unsigned", fixture: "unsigned_commit.txt", sha: unsignedCommitSHA, keys: trusted, wantErr: ErrUnsigned},
		{name: "wrong sha", fixture: "ssh_commit.txt", sha: pgpCommitSHA, keys: trusted, wantErr: errCommitMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, signature := splitCommit(t, readFixture(t, tt.fixture))
			signer, err := tt.keys.VerifyCommit(tt.sha, payload, signature)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected %v, got signer %+v, err %v", tt.wantErr, signer, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("VerifyCommit: %v", err)
			}
			if signer.Type != tt.want {
				t.Fatalf("expected %s signer, got %+v", tt.want, signer)
			}
		})
	}
}

func TestVerifyCommitTrailingNewlineStripped(t *testing.T) {
	keys := mustParseKeys(t, readFixture(t, "ssh_trusted.pub"))
	payload, signature := splitCommit(t, readFixture(t, "ssh_commit.txt"))
	if _, err := keys.VerifyCommit(sshCommitSHA, payload, strings.TrimSuffix(signature, "\n")); err != nil {
		t.Fatalf("VerifyCommit: %v", err)
	}
}

// A provider that returns a validly signed payload for a different commit must
// not be able to vouch for the requested commit.
func TestVerifyCommitRejectsTamperedPayload(t *testing.T) {
	keys := mustParseKeys(t, readFixture(t, "ssh_trusted.pub"))
	payload, signature := splitCommit(t, readFixture(t, "ssh_commit.txt"))

	tampered := strings.Replace(payload, "Deploy app", "Deploy evil", 1)
	if _, err := keys.VerifyCommit(sshCommitSHA, tampered, signature); !errors.Is(err, errCommitMismatch) {
		t.Fatalf("expected commit mismatch, got %v", err)
	}
	if err := checkCommitHash(sshCommitSHA, tampered, signature); err == nil {
		t.Fatal("expected tampered payload to fail the hash check")
	}
}

func TestVerifySSHRejectsTamperedPayload(t *testing.T) {
	keys := mustParseKeys(t, readFixture(t, "ssh_trusted.pub"))
	payload, signature := splitCommit(t, readFixture(t, "ssh_commit.txt"))
	if _, err := keys.verifySSH([]byte(payload+"x"), strings.TrimSpace(signature)); err == nil || err.Error() != "invalid SSH signature" {
		t.Fatalf("expected invalid SSH signature, got %v", err)
	}
}

func TestVerifyPGPRejectsTamperedPayload(t *testing.T) {
	keys := mustParseKeys(t, readFixture(t, "pgp_trusted.asc"))
	payload, signature := splitCommit(t, readFixture(t, "pgp_commit.txt"))
	if _, err := keys.verifyPGP([]byte(payload+"x"), strings.TrimSpace(signature)); err == nil || errors.Is(err, ErrUntrustedSigner) {
		t.Fatalf("expected invalid PGP signature, got %v", err)
	}
}

func TestVerifyCommitWithoutKeys(t *testing.T) {
	payload, signature := splitCommit(t, readFixture(t, "ssh_commit.txt"))
	if _, err := (&TrustedKeys{}).VerifyCommit(sshCommitSHA, payload, signature); !errors.Is(err, errNoTrustedKeys) {
		t.Fatalf("expected no trusted keys error, got %v", err)
	}
}

func TestVerifyCommitUnsupportedSignature(t *testing.T) {
	keys := mustParseKeys(t, readFixture(t, "ssh_trusted.pub"))
	payload, _ := splitCommit(t, readFixture(t, "unsigned_commit.txt"))
	if _, err := keys.VerifyCommit(unsignedCommitSHA, payload, "-----BEGIN SIGNED MESSAGE-----\nabc\n-----END SIGNED MESSAGE-----\n"); err == nil {
		t.Fatal("expected error")
	}
}

func TestPGPSignerByKeyID(t *testing.T) {
	keys := mustParseKeys(t, readFixture(t, "pgp_trusted.asc"))
	for _, id := range []string{trustedPGPKeyID, strings.ToLower(trustedPGPKeyID), "0x" + trustedPGPKeyID} {
		signer, ok := keys.PGPSignerByKeyID(id)
		if !ok || signer.Fingerprint != trustedPGPFingerprint {
			t.Errorf("PGPSignerByKeyID(%q) = %+v, %v", id, signer, ok)
		}
	}
	for _, id := range []string{"", "F4DCE6935A83094F", "94292F76", "zzzzzzzzzzzzzzzz"} {
		if _, ok := keys.PGPSignerByKeyID(id); ok {
			t.Errorf("PGPSignerByKeyID(%q) unexpectedly matched", id)
		}
	}
}

func TestPGPSignerByKeyIDRejectsExpiredKey(t *testing.T) {
	created := time.Now().Add(-48 * time.Hour)
	entity, err := openpgp.NewEntity("Expired", "", "expired@orcacd.test", &packet.Config{
		Time:            func() time.Time { return created },
		KeyLifetimeSecs: 3600,
	})
	if err != nil {
		t.Fatalf("NewEntity: %v", err)
	}
	var armored bytes.Buffer
	w, err := armor.Encode(&armored, openpgp.PublicKeyType, nil)
	if err != nil {
		t.Fatalf("armor.Encode: %v", err)
	}
	if err := entity.Serialize(w); err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close armor: %v", err)
	}

	keys := mustParseKeys(t, armored.String())
	if _, ok := keys.PGPSignerByKeyID(entity.PrimaryKey.KeyIdString()); ok {
		t.Fatal("expired key unexpectedly matched")
	}
}

func TestSSHSignerByAuthorizedKey(t *testing.T) {
	keys := mustParseKeys(t, readFixture(t, "ssh_trusted.pub"))
	// GitLab reports the key with its own comment.
	key := strings.Fields(readFixture(t, "ssh_trusted.pub"))
	signer, ok := keys.SSHSignerByAuthorizedKey(key[0] + " " + key[1] + " GitLab title")
	if !ok || signer.Fingerprint != trustedSSHFingerprint {
		t.Fatalf("SSHSignerByAuthorizedKey = %+v, %v", signer, ok)
	}
	if _, ok := keys.SSHSignerByAuthorizedKey(readFixture(t, "ssh_other.pub")); ok {
		t.Fatal("untrusted key unexpectedly matched")
	}
	if _, ok := keys.SSHSignerByAuthorizedKey("garbage"); ok {
		t.Fatal("garbage unexpectedly matched")
	}
}

func TestIsCommitHash(t *testing.T) {
	for _, sha := range []string{sshCommitSHA, strings.ToUpper(sshCommitSHA), sshSHA256CommitSHA} {
		if !IsCommitHash(sha) {
			t.Errorf("IsCommitHash(%q) = false", sha)
		}
	}
	for _, sha := range []string{"", "main", sshCommitSHA[:7], sshCommitSHA[:39] + "g", "refs/heads/main0000000000000000000000000"} {
		if IsCommitHash(sha) {
			t.Errorf("IsCommitHash(%q) = true", sha)
		}
	}
}
