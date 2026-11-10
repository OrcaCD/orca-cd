package repositories

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/commitsigning"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/shared/httpclient"
)

const (
	signedFixtureSHA   = "45845b34bb6dff50ff93518db435c7d6944496ef"
	unsignedFixtureSHA = "7814c370243473a63a0c7155d5f9c5bd2cc360bb"
)

func readSigningFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("../commitsigning/testdata/" + name) //nolint:gosec // fixed test fixture names
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}

// signedFixture returns the payload and signature of the SSH signed fixture
// commit, as Git providers report them.
func signedFixture(t *testing.T) (payload, signature string) {
	t.Helper()
	var payloadBuilder, signatureBuilder strings.Builder
	inSignature := false
	for line := range strings.SplitAfterSeq(readSigningFixture(t, "ssh_commit.txt"), "\n") {
		switch {
		case strings.HasPrefix(line, "gpgsig "):
			inSignature = true
			signatureBuilder.WriteString(strings.TrimPrefix(line, "gpgsig "))
		case inSignature && strings.HasPrefix(line, " "):
			signatureBuilder.WriteString(line[1:])
		default:
			inSignature = false
			payloadBuilder.WriteString(line)
		}
	}
	return payloadBuilder.String(), signatureBuilder.String()
}

func trustedFixtureKeys(t *testing.T, names ...string) *commitsigning.TrustedKeys {
	t.Helper()
	entries := make([]string, len(names))
	for i, name := range names {
		entries[i] = readSigningFixture(t, name)
	}
	keys, err := commitsigning.ParseTrustedKeys(entries)
	if err != nil {
		t.Fatalf("ParseTrustedKeys: %v", err)
	}
	return keys
}

func mockJSON(t *testing.T, expectedURL string, status int, body any) {
	t.Helper()
	originalClient := httpclient.Default
	t.Cleanup(func() { httpclient.Default = originalClient })

	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != expectedURL {
			t.Errorf("unexpected URL: %s", req.URL.String())
		}
		return jsonResponseWithBody(status, string(encoded)), nil
	})
}

func TestGitHubVerifyCommitSignature(t *testing.T) {
	payload, signature := signedFixture(t)
	url := testRepoAPIURL + "/git/commits/" + signedFixtureSHA
	repo := &models.Repository{Url: testRepoURL, AuthMethod: models.AuthMethodNone}

	verification := map[string]any{"verified": true, "signature": signature, "payload": payload}
	tests := []struct {
		name    string
		status  int
		body    map[string]any
		keys    []string
		wantErr error
		wantMsg string
	}{
		{name: "trusted", status: http.StatusOK, body: map[string]any{"sha": signedFixtureSHA, "verification": verification}, keys: []string{"ssh_trusted.pub"}},
		{name: "untrusted", status: http.StatusOK, body: map[string]any{"sha": signedFixtureSHA, "verification": verification}, keys: []string{"ssh_other.pub"}, wantErr: commitsigning.ErrUntrustedSigner},
		{name: "unsigned", status: http.StatusOK, body: map[string]any{"sha": signedFixtureSHA, "verification": map[string]any{"verified": false, "reason": "unsigned", "signature": nil, "payload": nil}}, keys: []string{"ssh_trusted.pub"}, wantErr: commitsigning.ErrUnsigned},
		{name: "different commit", status: http.StatusOK, body: map[string]any{"sha": unsignedFixtureSHA, "verification": verification}, keys: []string{"ssh_trusted.pub"}, wantMsg: "different commit"},
		{name: "not found", status: http.StatusNotFound, body: map[string]any{}, keys: []string{"ssh_trusted.pub"}, wantMsg: "not found"},
		{name: "forbidden", status: http.StatusForbidden, body: map[string]any{}, keys: []string{"ssh_trusted.pub"}, wantMsg: "access denied"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockJSON(t, url, tt.status, tt.body)
			signer, err := githubProvider{}.VerifyCommitSignature(context.Background(), repo, signedFixtureSHA, trustedFixtureKeys(t, tt.keys...))
			assertSignatureResult(t, signer, err, tt.wantErr, tt.wantMsg)
		})
	}
}

func TestGiteaVerifyCommitSignature(t *testing.T) {
	payload, signature := signedFixture(t)
	url := testGiteaRepoAPIURL + "/git/commits/" + signedFixtureSHA + "?stat=false&verification=true&files=false"
	repo := &models.Repository{Url: testGiteaRepoURL, AuthMethod: models.AuthMethodNone}

	t.Run("trusted", func(t *testing.T) {
		mockJSON(t, url, http.StatusOK, map[string]any{
			"sha":    signedFixtureSHA,
			"commit": map[string]any{"verification": map[string]any{"verified": false, "signature": signature, "payload": payload}},
		})
		signer, err := giteaProvider{}.VerifyCommitSignature(context.Background(), repo, signedFixtureSHA, trustedFixtureKeys(t, "ssh_trusted.pub"))
		assertSignatureResult(t, signer, err, nil, "")
	})
	t.Run("unsigned", func(t *testing.T) {
		mockJSON(t, url, http.StatusOK, map[string]any{
			"sha":    signedFixtureSHA,
			"commit": map[string]any{"verification": map[string]any{"verified": false, "reason": "gpg.error.not_signed_commit", "signature": "", "payload": ""}},
		})
		signer, err := giteaProvider{}.VerifyCommitSignature(context.Background(), repo, signedFixtureSHA, trustedFixtureKeys(t, "ssh_trusted.pub"))
		assertSignatureResult(t, signer, err, commitsigning.ErrUnsigned, "")
	})
}

func TestGitLabVerifyCommitSignature(t *testing.T) {
	url := testGitLabRepoAPIURL + "/repository/commits/" + signedFixtureSHA + "/signature"
	repo := &models.Repository{Url: testGitLabRepoURL, AuthMethod: models.AuthMethodNone}
	sshKey := strings.Fields(readSigningFixture(t, "ssh_trusted.pub"))

	tests := []struct {
		name    string
		status  int
		body    map[string]any
		wantErr error
		wantMsg string
	}{
		{name: "trusted ssh", status: http.StatusOK, body: map[string]any{"signature_type": "SSH", "verification_status": "verified", "key": map[string]any{"key": sshKey[0] + " " + sshKey[1] + " Laptop"}}},
		{name: "trusted pgp", status: http.StatusOK, body: map[string]any{"signature_type": "PGP", "verification_status": "verified", "gpg_key_primary_keyid": "E776AAD894292F76"}},
		{name: "expired ssh", status: http.StatusOK, body: map[string]any{"signature_type": "SSH", "verification_status": "verified", "key": map[string]any{"key": sshKey[0] + " " + sshKey[1], "expires_at": "2020-01-01T00:00:00.000Z"}}, wantErr: commitsigning.ErrUntrustedSigner},
		{name: "unexpired ssh", status: http.StatusOK, body: map[string]any{"signature_type": "SSH", "verification_status": "verified", "key": map[string]any{"key": sshKey[0] + " " + sshKey[1], "expires_at": "2999-01-01T00:00:00.000Z"}}},
		{name: "untrusted pgp", status: http.StatusOK, body: map[string]any{"signature_type": "PGP", "verification_status": "verified", "gpg_key_primary_keyid": "F4DCE6935A83094F"}, wantErr: commitsigning.ErrUntrustedSigner},
		{name: "untrusted ssh", status: http.StatusOK, body: map[string]any{"signature_type": "SSH", "verification_status": "verified", "key": map[string]any{"key": readSigningFixture(t, "ssh_other.pub")}}, wantErr: commitsigning.ErrUntrustedSigner},
		{name: "ssh without key", status: http.StatusOK, body: map[string]any{"signature_type": "SSH", "verification_status": "verified"}, wantErr: commitsigning.ErrUntrustedSigner},
		{name: "not verified", status: http.StatusOK, body: map[string]any{"signature_type": "PGP", "verification_status": "unknown_key", "gpg_key_primary_keyid": "E776AAD894292F76"}, wantMsg: `"unknown_key"`},
		{name: "x509", status: http.StatusOK, body: map[string]any{"signature_type": "X509", "verification_status": "verified"}, wantMsg: "unsupported signature type"},
		{name: "unsigned", status: http.StatusNotFound, body: map[string]any{"message": "404 GPG Signature Not Found"}, wantErr: commitsigning.ErrUnsigned},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockJSON(t, url, tt.status, tt.body)
			signer, err := gitlabProvider{}.VerifyCommitSignature(context.Background(), repo, signedFixtureSHA, trustedFixtureKeys(t, "ssh_trusted.pub", "pgp_trusted.asc"))
			assertSignatureResult(t, signer, err, tt.wantErr, tt.wantMsg)
		})
	}
}

func TestSupportsCommitSignatures(t *testing.T) {
	for provider, want := range map[models.RepositoryProvider]bool{
		models.GitHub:      true,
		models.Gitea:       true,
		models.GitLab:      true,
		models.AzureDevOps: false,
	} {
		p, err := Get(provider)
		if err != nil {
			t.Fatalf("Get(%s): %v", provider, err)
		}
		if got := SupportsCommitSignatures(p); got != want {
			t.Errorf("SupportsCommitSignatures(%s) = %v, want %v", provider, got, want)
		}
	}
}

func assertSignatureResult(t *testing.T, signer commitsigning.Signer, err, wantErr error, wantMsg string) {
	t.Helper()
	switch {
	case wantErr != nil:
		if !errors.Is(err, wantErr) {
			t.Fatalf("expected %v, got signer %+v, err %v", wantErr, signer, err)
		}
	case wantMsg != "":
		if err == nil || !strings.Contains(err.Error(), wantMsg) {
			t.Fatalf("expected error containing %q, got signer %+v, err %v", wantMsg, signer, err)
		}
	default:
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if signer.Fingerprint == "" {
			t.Fatal("expected signer fingerprint")
		}
	}
}
