package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/OrcaCD/orca-cd/internal/shared/httpclient"
)

// A client can name itself with the URL of a JSON document that describes it,
// a Client ID Metadata Document (draft-ietf-oauth-client-id-metadata-document).
// The hub fetches the document instead of the client registering. Like with
// registered clients, nothing is stored: the document is only cached.
const (
	maxOAuthClientMetadataURLLength = 1024
	// The draft recommends reading at most 5 KB.
	maxOAuthClientMetadataBytes = 5 << 10
	oauthClientMetadataTimeout  = 5 * time.Second

	// How long a document is used without fetching it again. Its Cache-Control
	// max-age counts, within these bounds.
	oauthClientMetadataDefaultCache = 5 * time.Minute
	oauthClientMetadataMinCache     = time.Minute
	oauthClientMetadataMaxCache     = 24 * time.Hour
	// A document that was valid is still used when its site can't be reached
	// for a while, so an outage there doesn't stop logins (draft section 6.1).
	oauthClientMetadataMaxStale  = 24 * time.Hour
	maxOAuthClientMetadataCached = 1000
)

var (
	errOAuthClientMetadataUnavailable = errors.New("the description of the app couldn't be loaded from its client_id URL, try again later")
	errOAuthClientMetadataInvalid     = errors.New("the description of the app at its client_id URL is invalid")
)

// oauthClientMetadataHTTPClient fetches the documents. It refuses private and
// local addresses, as any logged in user can make the hub fetch a URL, and
// follows no redirects (draft section 6). Tests replace it.
var oauthClientMetadataHTTPClient = &http.Client{
	Timeout:   oauthClientMetadataTimeout,
	Transport: httpclient.Default.Transport,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

var oauthClientMetadataCache = clientMetadataCache{entries: map[string]*cachedClientMetadata{}}

// isOAuthClientMetadataURL tells clients with a document from registered
// ones, whose client IDs start with oauthClientIdPrefix.
func isOAuthClientMetadataURL(clientId string) bool {
	return strings.HasPrefix(clientId, "https://")
}

// resolveOAuthClient returns the client of a client ID, from its document or
// from the registration it encodes.
func resolveOAuthClient(ctx context.Context, clientId string) (*oauthClient, error) {
	if isOAuthClientMetadataURL(clientId) {
		return fetchOAuthClientMetadata(ctx, clientId)
	}
	client, ok := decodeOAuthClientId(clientId)
	if !ok {
		return nil, errors.New("the client is not registered, it has to register again")
	}
	return client, nil
}

// oauthClientHost returns the host that published the document of a client, or
// "" for a registered client.
func oauthClientHost(clientId string) string {
	if !isOAuthClientMetadataURL(clientId) {
		return ""
	}
	u, err := url.Parse(clientId)
	if err != nil {
		return ""
	}
	return u.Host
}

// validateOAuthClientMetadataURL checks the rules of the draft (section 3) for
// a client ID URL. The URL is compared as it is, never normalized.
func validateOAuthClientMetadataURL(clientId string) error {
	invalid := func(reason string) error {
		return fmt.Errorf("the client_id URL %s", reason)
	}
	if len(clientId) > maxOAuthClientMetadataURLLength {
		return invalid("is too long")
	}
	if strings.ContainsAny(clientId, "#?") {
		return invalid("must not have a query or a fragment")
	}
	u, err := url.Parse(clientId)
	if err != nil || u.Scheme != "https" || u.Opaque != "" || u.Host == "" {
		return invalid("must be an https URL")
	}
	if u.User != nil {
		return invalid("must not contain a user name or password")
	}
	if u.Path == "" || u.Path == "/" {
		return invalid("must have a path")
	}
	for segment := range strings.SplitSeq(u.EscapedPath(), "/") {
		if unescaped, err := url.PathUnescape(segment); err != nil || unescaped == "." || unescaped == ".." {
			return invalid("must not have . or .. segments")
		}
	}
	// The hub would fetch from itself, and a client could pose as the hub
	// (draft section 10.1).
	if issuer, err := url.Parse(oauthIssuer); err == nil && strings.EqualFold(u.Host, issuer.Host) {
		return invalid("must not be on the OrcaCD hub")
	}
	return nil
}

// oauthClientMetadataDocument holds the fields of a document the hub uses or
// refuses. Other fields are ignored.
type oauthClientMetadataDocument struct {
	ClientId                          string          `json:"client_id"`
	ClientName                        string          `json:"client_name"`
	RedirectURIs                      []string        `json:"redirect_uris"`
	GrantTypes                        []string        `json:"grant_types"`
	ResponseTypes                     []string        `json:"response_types"`
	TokenEndpointAuthMethod           string          `json:"token_endpoint_auth_method"`
	TokenEndpointAuthMethodsSupported []string        `json:"token_endpoint_auth_methods_supported"`
	ClientSecret                      json.RawMessage `json:"client_secret"`
	ClientSecretExpiresAt             json.RawMessage `json:"client_secret_expires_at"`
}

// client checks the document against its URL and returns the client it
// describes. The hub only supports public clients, which authenticate with
// PKCE instead of a secret or a key.
func (d *oauthClientMetadataDocument) client(clientId string) (*oauthClient, error) {
	invalid := func(reason string) error {
		return fmt.Errorf("%w: %s", errOAuthClientMetadataInvalid, reason)
	}
	if d.ClientId != clientId {
		return nil, invalid("its client_id is not the URL it is served from")
	}
	if len(d.ClientSecret) > 0 || len(d.ClientSecretExpiresAt) > 0 {
		return nil, invalid("it must not contain a client secret")
	}
	// Without these, the method is none (draft section 5).
	if len(d.TokenEndpointAuthMethodsSupported) > 0 {
		if !slices.Contains(d.TokenEndpointAuthMethodsSupported, "none") ||
			(d.TokenEndpointAuthMethod != "" && !slices.Contains(d.TokenEndpointAuthMethodsSupported, d.TokenEndpointAuthMethod)) {
			return nil, invalid("OrcaCD only supports the token_endpoint_auth_method none")
		}
	} else if d.TokenEndpointAuthMethod != "" && d.TokenEndpointAuthMethod != "none" {
		return nil, invalid("OrcaCD only supports the token_endpoint_auth_method none")
	}
	if len(d.GrantTypes) > 0 && !slices.Contains(d.GrantTypes, grantTypeAuthorizationCode) {
		return nil, invalid("its grant_types must include authorization_code")
	}
	if len(d.ResponseTypes) > 0 && !slices.Contains(d.ResponseTypes, "code") {
		return nil, invalid("its response_types must include code")
	}

	// Redirect URIs the hub doesn't accept are left out, so a client that
	// lists one still works with the others.
	var redirectURIs []string
	for _, redirectURI := range d.RedirectURIs {
		if isValidRedirectURI(redirectURI) && !slices.Contains(redirectURIs, redirectURI) {
			redirectURIs = append(redirectURIs, redirectURI)
		}
	}
	if len(redirectURIs) == 0 {
		return nil, invalid("it has no redirect_uris OrcaCD accepts")
	}

	name := strings.TrimSpace(d.ClientName)
	if name == "" {
		name = oauthClientHost(clientId)
	}
	return &oauthClient{Name: oauthClientName(name), RedirectURIs: redirectURIs}, nil
}

type cachedClientMetadata struct {
	client     *oauthClient
	fetchedAt  time.Time
	freshUntil time.Time
}

type clientMetadataCache struct {
	mu      sync.Mutex
	entries map[string]*cachedClientMetadata
}

func (s *clientMetadataCache) get(clientId string) *cachedClientMetadata {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.entries[clientId]
}

func (s *clientMetadataCache) set(clientId string, entry *cachedClientMetadata) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.entries) >= maxOAuthClientMetadataCached {
		now := oauthNow()
		for key, existing := range s.entries {
			if now.Sub(existing.fetchedAt) > oauthClientMetadataMaxStale {
				delete(s.entries, key)
			}
		}
		if len(s.entries) >= maxOAuthClientMetadataCached {
			return
		}
	}
	s.entries[clientId] = entry
}

func (s *clientMetadataCache) delete(clientId string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, clientId)
}

// fetchOAuthClientMetadata returns the client a document describes, from the
// cache while it is fresh. Errors are fit to show to the user: details, like
// whether a host resolves to a private address, are only logged.
func fetchOAuthClientMetadata(ctx context.Context, clientId string) (*oauthClient, error) {
	if err := validateOAuthClientMetadataURL(clientId); err != nil {
		return nil, err
	}
	now := oauthNow()
	cached := oauthClientMetadataCache.get(clientId)
	if cached != nil && now.Before(cached.freshUntil) {
		return cached.client, nil
	}

	client, cacheFor, err := downloadOAuthClientMetadata(ctx, clientId)
	if err != nil {
		if transient, ok := errors.AsType[*transientClientMetadataError](err); ok {
			if cached != nil && now.Sub(cached.fetchedAt) < oauthClientMetadataMaxStale {
				return cached.client, nil
			}
			utils.Log.Warn().Err(transient.err).Str("client_id", clientId).Msg("Failed to fetch the metadata document of an OAuth client")
			return nil, errOAuthClientMetadataUnavailable
		}
		// The document was fetched and is invalid, so the cached one is gone too.
		oauthClientMetadataCache.delete(clientId)
		return nil, err
	}
	if cacheFor > 0 {
		oauthClientMetadataCache.set(clientId, &cachedClientMetadata{client: client, fetchedAt: now, freshUntil: now.Add(cacheFor)})
	} else {
		oauthClientMetadataCache.delete(clientId)
	}
	return client, nil
}

// transientClientMetadataError is a failure to fetch a document that may pass,
// like a timeout. A cached document is used instead for a while.
type transientClientMetadataError struct {
	err error
}

func (e *transientClientMetadataError) Error() string {
	return e.err.Error()
}

func downloadOAuthClientMetadata(ctx context.Context, clientId string) (*oauthClient, time.Duration, error) {
	req, err := httpclient.NewRequest(ctx, http.MethodGet, clientId, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := oauthClientMetadataHTTPClient.Do(req)
	if err != nil {
		// Refused addresses are not transient, a timeout or a DNS error may be.
		var netErr net.Error
		var dnsErr *net.DNSError
		if (errors.As(err, &netErr) && netErr.Timeout()) || (errors.As(err, &dnsErr) && dnsErr.IsTemporary) {
			return nil, 0, &transientClientMetadataError{err: err}
		}
		utils.Log.Warn().Err(err).Str("client_id", clientId).Msg("Failed to fetch the metadata document of an OAuth client")
		return nil, 0, errOAuthClientMetadataUnavailable
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError:
		return nil, 0, &transientClientMetadataError{err: fmt.Errorf("status %d", resp.StatusCode)}
	default:
		// Including redirects, which the hub doesn't follow.
		return nil, 0, fmt.Errorf("%w: it was served with status %d instead of 200", errOAuthClientMetadataInvalid, resp.StatusCode)
	}
	if mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err != nil || !isJSONMediaType(mediaType) {
		return nil, 0, fmt.Errorf("%w: it must be served as application/json", errOAuthClientMetadataInvalid)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxOAuthClientMetadataBytes+1))
	if err != nil {
		return nil, 0, &transientClientMetadataError{err: err}
	}
	if len(body) > maxOAuthClientMetadataBytes {
		return nil, 0, fmt.Errorf("%w: it is larger than %d bytes", errOAuthClientMetadataInvalid, maxOAuthClientMetadataBytes)
	}
	var document oauthClientMetadataDocument
	if err := json.Unmarshal(body, &document); err != nil {
		return nil, 0, fmt.Errorf("%w: it is not a JSON object", errOAuthClientMetadataInvalid)
	}
	client, err := document.client(clientId)
	if err != nil {
		return nil, 0, err
	}
	return client, clientMetadataCacheDuration(resp.Header.Get("Cache-Control")), nil
}

// isJSONMediaType accepts application/json and its subtypes, like
// application/client-metadata+json (draft section 4).
func isJSONMediaType(mediaType string) bool {
	return mediaType == "application/json" || (strings.HasPrefix(mediaType, "application/") && strings.HasSuffix(mediaType, "+json"))
}

// clientMetadataCacheDuration follows the max-age of a document within the
// hub's bounds. no-store and no-cache aren't cached at all.
func clientMetadataCacheDuration(cacheControl string) time.Duration {
	duration := oauthClientMetadataDefaultCache
	for directive := range strings.SplitSeq(cacheControl, ",") {
		name, value, _ := strings.Cut(strings.TrimSpace(directive), "=")
		switch strings.ToLower(name) {
		case "no-store", "no-cache":
			return 0
		case "max-age":
			if seconds, err := strconv.ParseInt(strings.Trim(value, `"`), 10, 64); err == nil && seconds >= 0 {
				duration = time.Duration(min(seconds, int64(oauthClientMetadataMaxCache/time.Second))) * time.Second
			}
		}
	}
	return max(duration, oauthClientMetadataMinCache)
}
