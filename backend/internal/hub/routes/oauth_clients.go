package routes

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin"
)

const (
	oauthClientIdPrefix = "mcp_"
	// The limits keep client IDs short enough for a URL.
	maxOAuthRedirectURIs      = 5
	maxOAuthRedirectURILength = 512
	maxOAuthClientIdLength    = 4096
	// Leaves room for the number that makes the token name unique.
	maxOAuthClientNameLength = maxAPITokenNameLength - 5
	defaultOAuthClientName   = "MCP client"

	grantTypeAuthorizationCode = "authorization_code"
	grantTypeRefreshToken      = "refresh_token"
)

// blockedRedirectSchemes can't be the redirect of a native app. Some would run
// code in the consent page, others open apps of the system with arguments from
// the URL. Schemes starting with "ms-" are blocked as well.
var blockedRedirectSchemes = []string{
	"javascript", "data", "vbscript", "file", "about", "blob", "filesystem", "view-source", "jar",
	"ftp", "ws", "wss", "mailto", "tel", "sms", "intent", "search", "search-ms", "shell", "res",
}

// oauthClient is a registered client. Clients aren't stored: the client ID is
// the client, signed by the hub, so anyone can register one without filling
// the database.
type oauthClient struct {
	Name         string   `json:"n"`
	RedirectURIs []string `json:"r"`
	// Nonce makes every registration its own client, so authorizing one again
	// only replaces its own grant.
	Nonce string `json:"x"`
}

func encodeOAuthClientId(client *oauthClient) string {
	payload, _ := json.Marshal(client) // only strings, can't fail
	signed := oauthClientIdPrefix + base64.RawURLEncoding.EncodeToString(payload)
	return signed + "." + base64.RawURLEncoding.EncodeToString(oauthClientMAC(signed))
}

func decodeOAuthClientId(clientId string) (*oauthClient, bool) {
	if len(clientId) > maxOAuthClientIdLength {
		return nil, false
	}
	signed, signature, ok := strings.Cut(clientId, ".")
	if !ok {
		return nil, false
	}
	encoded, ok := strings.CutPrefix(signed, oauthClientIdPrefix)
	if !ok {
		return nil, false
	}
	// Strict, so every client has one ID.
	mac, err := base64.RawURLEncoding.Strict().DecodeString(signature)
	if err != nil || !hmac.Equal(mac, oauthClientMAC(signed)) {
		return nil, false
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return nil, false
	}
	var client oauthClient
	if err := json.Unmarshal(payload, &client); err != nil || len(client.RedirectURIs) == 0 {
		return nil, false
	}
	return &client, true
}

func oauthClientMAC(signed string) []byte {
	mac := hmac.New(sha256.New, oauthClientKey)
	mac.Write([]byte(signed))
	return mac.Sum(nil)
}

type oauthClientRegistrationRequest struct {
	RedirectURIs  []string `json:"redirect_uris"`
	ClientName    string   `json:"client_name"`
	GrantTypes    []string `json:"grant_types"`
	ResponseTypes []string `json:"response_types"`
}

type oauthClientRegistrationResponse struct {
	ClientId                string   `json:"client_id"`
	ClientIdIssuedAt        int64    `json:"client_id_issued_at"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

// OAuthRegisterHandler registers a client (RFC 7591). Clients are public: they
// get no secret, PKCE proves that the token goes to the client that asked.
func OAuthRegisterHandler(c *gin.Context) {
	var req oauthClientRegistrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		oauthError(c, http.StatusBadRequest, "invalid_client_metadata", "invalid request body")
		return
	}

	if len(req.RedirectURIs) == 0 || len(req.RedirectURIs) > maxOAuthRedirectURIs {
		oauthError(c, http.StatusBadRequest, "invalid_redirect_uri", "between 1 and 5 redirect_uris are required")
		return
	}
	for _, redirectURI := range req.RedirectURIs {
		if !isValidRedirectURI(redirectURI) {
			oauthError(c, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uris must use https, http on a loopback address, or the scheme of an app, and have no fragment")
			return
		}
	}
	for _, grantType := range req.GrantTypes {
		if grantType != grantTypeAuthorizationCode && grantType != grantTypeRefreshToken {
			oauthError(c, http.StatusBadRequest, "invalid_client_metadata", "only the authorization_code and refresh_token grant types are supported")
			return
		}
	}
	for _, responseType := range req.ResponseTypes {
		if responseType != "code" {
			oauthError(c, http.StatusBadRequest, "invalid_client_metadata", "only the code response type is supported")
			return
		}
	}

	client := oauthClient{
		Name:         oauthClientName(req.ClientName),
		RedirectURIs: slices.Compact(slices.Clone(req.RedirectURIs)),
		Nonce:        rand.Text(),
	}
	// JSON escapes some characters, so the URIs alone don't bound the length.
	clientId := encodeOAuthClientId(&client)
	if len(clientId) > maxOAuthClientIdLength {
		oauthError(c, http.StatusBadRequest, "invalid_redirect_uri", "the redirect_uris are too long")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, oauthClientRegistrationResponse{
		ClientId:         clientId,
		ClientIdIssuedAt: oauthNow().Unix(),
		ClientName:       client.Name,
		RedirectURIs:     client.RedirectURIs,
		GrantTypes:       []string{grantTypeAuthorizationCode, grantTypeRefreshToken},
		ResponseTypes:    []string{"code"},
		// The client may have asked for a secret, RFC 7591 lets the server
		// answer with what it supports.
		TokenEndpointAuthMethod: "none",
	})
}

// oauthClientName returns the name a client gave itself, cleaned up to show
// on the consent page and as the name of its token.
func oauthClientName(name string) string {
	// Control and bidi characters could make the name look like something else.
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, name)
	name = strings.Join(strings.Fields(name), " ")
	if runes := []rune(name); len(runes) > maxOAuthClientNameLength {
		name = strings.TrimSpace(string(runes[:maxOAuthClientNameLength]))
	}
	if name == "" {
		return defaultOAuthClientName
	}
	return name
}

// isValidRedirectURI accepts https, http on a loopback address for apps on the
// user's machine, and the schemes apps register for themselves (RFC 8252).
func isValidRedirectURI(raw string) bool {
	if len(raw) > maxOAuthRedirectURILength || strings.Contains(raw, "#") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || u.User != nil {
		return false
	}
	switch u.Scheme {
	case "https":
		return u.Host != ""
	case "http":
		return isLoopbackHost(u.Hostname())
	case "":
		return false
	default:
		return !slices.Contains(blockedRedirectSchemes, u.Scheme) && !strings.HasPrefix(u.Scheme, "ms-")
	}
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// redirectURI returns the redirect URI of an authorization request. A
// registered URI must match exactly, except for the port of a loopback
// address, as apps listen on whatever port is free (RFC 8252 section 7.3).
func (client *oauthClient) redirectURI(requested string) (string, bool) {
	if requested == "" {
		if len(client.RedirectURIs) == 1 {
			return client.RedirectURIs[0], true
		}
		return "", false
	}
	for _, registered := range client.RedirectURIs {
		if requested == registered || isSameLoopbackRedirect(requested, registered) {
			return requested, true
		}
	}
	return "", false
}

func isSameLoopbackRedirect(a, b string) bool {
	ua, errA := url.Parse(a)
	ub, errB := url.Parse(b)
	if errA != nil || errB != nil || ua.Scheme != "http" || ub.Scheme != "http" {
		return false
	}
	return isLoopbackHost(ua.Hostname()) &&
		ua.Hostname() == ub.Hostname() &&
		ua.User == nil &&
		ua.Fragment == "" &&
		ua.EscapedPath() == ub.EscapedPath() &&
		ua.RawQuery == ub.RawQuery
}
