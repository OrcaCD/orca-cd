package commitsigning

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func mustParseKeys(t *testing.T, text string) *TrustedKeys {
	t.Helper()
	keys, err := ParseTrustedKeys(text)
	if err != nil {
		t.Fatalf("ParseTrustedKeys: %v", err)
	}
	return keys
}

func TestParseTrustedKeys(t *testing.T) {
	text := strings.Join([]string{
		"# deploy keys",
		readFixture(t, "ssh_trusted.pub"),
		readFixture(t, "pgp_trusted.asc"),
		"",
		readFixture(t, "ssh_trusted.pub"), // duplicates are dropped
		readFixture(t, "ssh_other.pub"),
	}, "\n")

	keys := mustParseKeys(t, text)
	signers := keys.Signers()
	if len(signers) != 3 {
		t.Fatalf("expected 3 keys, got %d: %+v", len(signers), signers)
	}
	if signers[0] != (Signer{Type: KeyTypePGP, Fingerprint: trustedPGPFingerprint, Identity: "OrcaCD Test <trusted@orcacd.test>"}) {
		t.Errorf("unexpected PGP signer: %+v", signers[0])
	}
	if signers[1] != (Signer{Type: KeyTypeSSH, Fingerprint: trustedSSHFingerprint, Identity: "trusted@orcacd.test"}) {
		t.Errorf("unexpected SSH signer: %+v", signers[1])
	}
}

func TestParseTrustedKeysEmpty(t *testing.T) {
	keys := mustParseKeys(t, "\n  \n# nothing\n")
	if keys.Len() != 0 {
		t.Fatalf("expected no keys, got %d", keys.Len())
	}
}

func TestParseTrustedKeysErrors(t *testing.T) {
	pgp := readFixture(t, "pgp_trusted.asc")
	pgpWithoutEnd, _, _ := strings.Cut(pgp, "-----END")
	tests := map[string]struct {
		text string
		want string
	}{
		"invalid ssh key":    {"ssh-ed25519 not-base64", "line 1: invalid SSH public key"},
		"garbage after pgp":  {pgp + "\nnope", "line 12: invalid SSH public key"},
		"unterminated pgp":   {"\n" + pgpWithoutEnd, "line 2: unterminated PGP public key block"},
		"corrupt pgp":        {strings.Replace(pgp, "mDMEa", "mDMEb", 1), "line 1: invalid PGP public key block"},
		"private key block":  {"-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----", "line 1: unsupported key block"},
		"ssh key options":    {`command="x" ` + readFixture(t, "ssh_trusted.pub"), "line 1: SSH key options are not supported"},
		"too large":          {strings.Repeat("#", MaxKeysTextLength+1), "exceed"},
		"allowed signers id": {"trusted@orcacd.test " + readFixture(t, "ssh_trusted.pub"), "line 1: SSH key options are not supported"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := ParseTrustedKeys(tt.text)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestVerifyCommit(t *testing.T) {
	trusted := mustParseKeys(t, readFixture(t, "ssh_trusted.pub")+"\n"+readFixture(t, "pgp_trusted.asc"))
	untrusted := mustParseKeys(t, readFixture(t, "ssh_other.pub")+"\n"+readFixture(t, "pgp_other.asc"))

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
