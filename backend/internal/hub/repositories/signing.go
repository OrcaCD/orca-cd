package repositories

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/hub/commitsigning"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/shared/httpclient"
)

// CommitSignatureVerifier is an optional capability for providers whose API
// exposes enough signature data to check a commit against trusted keys.
type CommitSignatureVerifier interface {
	VerifyCommitSignature(ctx context.Context, repo *models.Repository, sha string, keys *commitsigning.TrustedKeys) (commitsigning.Signer, error)
}

func SupportsCommitSignatures(p Provider) bool {
	_, ok := p.(CommitSignatureVerifier)
	return ok
}

// VerifyCommit enforces the repository's signed commit policy for sha. It
// succeeds without contacting the provider when signed commits are not required.
func VerifyCommit(ctx context.Context, provider Provider, repo *models.Repository, sha string) error {
	if !repo.RequireSignedCommits {
		return nil
	}
	verifier, ok := provider.(CommitSignatureVerifier)
	if !ok {
		return fmt.Errorf("commit signature verification is not supported for %s repositories", repo.Provider)
	}
	if !commitsigning.IsCommitHash(sha) {
		return fmt.Errorf("commit signature verification requires a full commit hash, got %q", sha)
	}
	keys, err := commitsigning.ParseTrustedKeys(repo.CommitSigningKeys)
	if err != nil {
		return fmt.Errorf("invalid trusted signing keys: %w", err)
	}
	if keys.Len() == 0 {
		return errors.New("signed commits are required but no trusted signing keys are configured")
	}
	if _, err := verifier.VerifyCommitSignature(ctx, repo, sha, keys); err != nil {
		return fmt.Errorf("commit %s failed signature verification: %w", sha, err)
	}
	return nil
}

// fetchCommitSignatureJSON sends req and decodes a successful response into v.
// It reports found=false for a 404 so callers can treat it as unsigned.
func fetchCommitSignatureJSON(req *http.Request, providerName string, v any) (found bool, err error) {
	resp, err := httpclient.Default.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to fetch %s commit signature: %w", providerName, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			fmt.Printf("warning: failed to close %s response body: %v\n", providerName, err)
		}
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		if err := decodeJSON(resp.Body, v); err != nil {
			return false, fmt.Errorf("failed to decode %s commit signature response: %w", providerName, err)
		}
		return true, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return false, errors.New("authentication failed or access denied")
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("%s API returned unexpected status: %d", providerName, resp.StatusCode)
	}
}

// verifyRawCommitSignature checks the signature and signed payload reported
// by providers that expose the raw signed commit data (GitHub, Gitea).
func verifyRawCommitSignature(keys *commitsigning.TrustedKeys, sha, reportedSHA string, signature, payload *string) (commitsigning.Signer, error) {
	if !strings.EqualFold(reportedSHA, sha) {
		return commitsigning.Signer{}, errors.New("provider returned a different commit")
	}
	if signature == nil || *signature == "" || payload == nil {
		return commitsigning.Signer{}, commitsigning.ErrUnsigned
	}
	return keys.VerifyCommit(sha, *payload, *signature)
}
