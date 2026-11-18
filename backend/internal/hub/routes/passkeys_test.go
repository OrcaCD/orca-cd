package routes

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/passkeys"
	"github.com/OrcaCD/orca-cd/internal/hub/setupcode"
	"github.com/fxamacker/cbor/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/webauthn"
	"gorm.io/gorm"
)

const testBrowserOrigin = "http://localhost:8080"

type testAuthenticator struct {
	key     *ecdsa.PrivateKey
	id      []byte
	handle  string
	counter uint32
}

func setupPasskeyTest(t *testing.T) *gin.Engine {
	t.Helper()
	setupTestDB(t)
	if err := db.DB.AutoMigrate(&models.Passkey{}, &models.PasskeyEnrollment{}); err != nil {
		t.Fatal(err)
	}
	if err := InitPasskeys(testBrowserOrigin); err != nil {
		t.Fatal(err)
	}
	LocalAuthDisabled, DemoMode = false, false
	t.Cleanup(func() { LocalAuthDisabled, DemoMode = false, false })
	initTestSetupCode(t)
	r := gin.New()
	r.POST("/begin", BeginPasskeyRegistrationHandler)
	r.POST("/finish", FinishPasskeyRegistrationHandler)
	r.POST("/login/begin", BeginPasskeyLoginHandler)
	r.POST("/login/finish", FinishPasskeyLoginHandler)
	return r
}

func passkeyRequest(t *testing.T, r *gin.Engine, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func ceremonyCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == ceremonyCookieName && c.Value != "" {
			return c
		}
	}
	t.Fatalf("no ceremony cookie: %d %s", w.Code, w.Body.String())
	return nil
}

func decodeOptions(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("begin failed: %d %s", w.Code, w.Body.String())
	}
	var options map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &options); err != nil {
		t.Fatal(err)
	}
	if hasAuthCookie(w) {
		t.Fatal("begin must never grant an auth session")
	}
	return options
}

func b64(data []byte) string { return base64.RawURLEncoding.EncodeToString(data) }

func (a *testAuthenticator) registration(t *testing.T, options map[string]any, origin string, verified bool) map[string]any {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	a.key, a.id = key, []byte("credential-"+options["challenge"].(string))
	a.handle = options["user"].(map[string]any)["id"].(string)
	clientData, _ := json.Marshal(map[string]any{"type": "webauthn.create", "challenge": options["challenge"], "origin": origin})
	publicKey, err := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: key.X.FillBytes(make([]byte, 32)), -3: key.Y.FillBytes(make([]byte, 32))})
	if err != nil {
		t.Fatal(err)
	}
	rpHash := sha256.Sum256([]byte("localhost"))
	flags := byte(0x41) // user present and attested credential data
	if verified {
		flags |= 0x04
	}
	data := append(rpHash[:], flags, 0, 0, 0, 0)
	data = append(data, make([]byte, 16)...)
	if len(a.id) > 65535 {
		t.Fatal("credential ID too long")
	}
	data = binary.BigEndian.AppendUint16(data, uint16(len(a.id))) //nolint:gosec // Credential ID length is bounded above.
	data = append(data, a.id...)
	data = append(data, publicKey...)
	attestation, err := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": data})
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"id": b64(a.id), "rawId": b64(a.id), "type": "public-key", "response": map[string]any{"clientDataJSON": b64(clientData), "attestationObject": b64(attestation)}, "clientExtensionResults": map[string]any{}}
}

func (a *testAuthenticator) assertion(t *testing.T, options map[string]any, origin string, verified bool) map[string]any {
	t.Helper()
	clientData, _ := json.Marshal(map[string]any{"type": "webauthn.get", "challenge": options["challenge"], "origin": origin})
	rpHash := sha256.Sum256([]byte("localhost"))
	flags := byte(0x01)
	if verified {
		flags |= 0x04
	}
	a.counter++
	authData := binary.BigEndian.AppendUint32(append(rpHash[:], flags), a.counter)
	clientHash := sha256.Sum256(clientData)
	digest := sha256.Sum256(append(bytes.Clone(authData), clientHash[:]...))
	signature, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"id": b64(a.id), "rawId": b64(a.id), "type": "public-key", "response": map[string]any{"clientDataJSON": b64(clientData), "authenticatorData": b64(authData), "signature": b64(signature), "userHandle": a.handle}, "clientExtensionResults": map[string]any{}}
}

func registerTestPasskey(t *testing.T, r *gin.Engine) testAuthenticator {
	t.Helper()
	w := passkeyRequest(t, r, "/begin", map[string]any{"name": "Admin", "email": "admin@example.com", "setupCode": testSetupCode}, nil)
	options := decodeOptions(t, w)
	var a testAuthenticator
	response := a.registration(t, options, testBrowserOrigin, true)
	w = passkeyRequest(t, r, "/finish", response, ceremonyCookie(t, w))
	if w.Code != http.StatusCreated || !hasAuthCookie(w) {
		t.Fatalf("registration failed: %d %s", w.Code, w.Body.String())
	}
	return a
}

func TestPasskeysSetupLoginAndReplay(t *testing.T) {
	r := setupPasskeyTest(t)
	a := registerTestPasskey(t, r)
	w := passkeyRequest(t, r, "/login/begin", nil, nil)
	options, cookie := decodeOptions(t, w), ceremonyCookie(t, w)
	response := a.assertion(t, options, testBrowserOrigin, true)
	w = passkeyRequest(t, r, "/login/finish", response, cookie)
	if w.Code != http.StatusOK || !hasAuthCookie(w) {
		t.Fatalf("login failed: %d %s", w.Code, w.Body.String())
	}
	w = passkeyRequest(t, r, "/login/finish", response, cookie)
	if w.Code != http.StatusBadRequest || hasAuthCookie(w) {
		t.Fatal("accepted replay")
	}
	keys, err := gorm.G[models.Passkey](db.DB).Find(t.Context())
	if err != nil || len(keys) != 1 || keys[0].LastUsedAt == nil {
		t.Fatal("missing persisted login state")
	}
	user, err := passkeys.LoadUser(t.Context(), db.DB, keys[0].UserId)
	if err != nil || !user.LocalAuthEnabled || user.Credentials[0].Authenticator.SignCount != 1 {
		t.Fatal("invalid user or signature counter")
	}
	admin, err := userIsAdmin(t, user.Id)
	if err != nil || !admin {
		t.Fatal("setup must assign admin role")
	}
}

func TestPasskeysRejectInvalidAssertions(t *testing.T) {
	for _, scenario := range []string{"origin", "verification", "signature", "handle", "challenge", "deleted", "disabled"} {
		t.Run(scenario, func(t *testing.T) {
			r := setupPasskeyTest(t)
			a := registerTestPasskey(t, r)
			w := passkeyRequest(t, r, "/login/begin", nil, nil)
			options, cookie := decodeOptions(t, w), ceremonyCookie(t, w)
			origin := testBrowserOrigin
			if scenario == "origin" {
				origin = "https://evil.example.com"
			}
			if scenario == "handle" {
				a.handle = b64([]byte("somebody-else"))
			}
			if scenario == "challenge" {
				options["challenge"] = "different-challenge"
			}
			response := a.assertion(t, options, origin, scenario != "verification")
			if scenario == "signature" {
				response["response"].(map[string]any)["signature"] = b64([]byte("invalid"))
			}
			if scenario == "deleted" {
				if _, err := gorm.G[models.Passkey](db.DB).Where("credential_id = ?", b64(a.id)).Delete(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "disabled" {
				LocalAuthDisabled = true
			}
			w = passkeyRequest(t, r, "/login/finish", response, cookie)
			if w.Code < 400 || hasAuthCookie(w) {
				t.Fatalf("accepted %s: %d %s", scenario, w.Code, w.Body.String())
			}
		})
	}
}

func TestPasskeysEnrollmentAndConcurrentReset(t *testing.T) {
	r := setupPasskeyTest(t)
	u := models.User{Name: "Invited", Email: "invited@example.com", LocalAuthEnabled: true}
	if err := gorm.G[models.User](db.DB).Create(t.Context(), &u); err != nil {
		t.Fatal(err)
	}
	token, err := passkeys.IssueEnrollment(t.Context(), db.DB, &u)
	if err != nil {
		t.Fatal(err)
	}
	begin := func() *httptest.ResponseRecorder {
		return passkeyRequest(t, r, "/begin", map[string]any{"enrollmentToken": token}, nil)
	}
	w := begin()
	options, cookie := decodeOptions(t, w), ceremonyCookie(t, w)
	var a testAuthenticator
	response := a.registration(t, options, testBrowserOrigin, true)
	if err := db.DB.Transaction(func(tx *gorm.DB) error { _, err := passkeys.ResetLocal(t.Context(), tx, &u); return err }); err != nil {
		t.Fatal(err)
	}
	w = passkeyRequest(t, r, "/finish", response, cookie)
	if w.Code < 400 || hasAuthCookie(w) {
		t.Fatal("accepted enrollment started before reset")
	}
	if begin().Code != http.StatusForbidden {
		t.Fatal("old enrollment code still works")
	}
	token, err = passkeys.IssueEnrollment(t.Context(), db.DB, &u)
	if err != nil {
		t.Fatal(err)
	}
	w = begin()
	options, cookie = decodeOptions(t, w), ceremonyCookie(t, w)
	response = a.registration(t, options, testBrowserOrigin, true)
	w = passkeyRequest(t, r, "/finish", response, cookie)
	if w.Code != http.StatusCreated || !hasAuthCookie(w) {
		t.Fatalf("enrollment failed: %s", w.Body.String())
	}
	if begin().Code != http.StatusForbidden {
		t.Fatal("enrollment code reused")
	}
}

func TestPasskeysRegistrationRequiresVerifiedUserAndTrustedOrigin(t *testing.T) {
	for _, origin := range []string{testBrowserOrigin, "https://evil.example.com"} {
		t.Run(origin, func(t *testing.T) {
			r := setupPasskeyTest(t)
			w := passkeyRequest(t, r, "/begin", map[string]any{"name": "Admin", "email": "admin@example.com", "setupCode": testSetupCode}, nil)
			options, cookie := decodeOptions(t, w), ceremonyCookie(t, w)
			var a testAuthenticator
			response := a.registration(t, options, origin, origin != testBrowserOrigin)
			w = passkeyRequest(t, r, "/finish", response, cookie)
			if w.Code < 400 || hasAuthCookie(w) {
				t.Fatal("accepted unverified registration")
			}
			count, err := gorm.G[models.User](db.DB).Count(t.Context(), "*")
			if err != nil || count != 0 {
				t.Fatal("failed registration must not create an account")
			}
		})
	}
}

func TestPasskeysRequireSetupAuthorization(t *testing.T) {
	r := setupPasskeyTest(t)
	for _, body := range []map[string]any{
		{"name": "Admin", "email": "admin@example.com"},
		{"name": "Admin", "email": "admin@example.com", "setupCode": "wrong"},
		{"enrollmentToken": "unknown"},
	} {
		w := passkeyRequest(t, r, "/begin", body, nil)
		if w.Code != http.StatusForbidden || hasAuthCookie(w) {
			t.Fatal("registration lacked authorization")
		}
	}
	invalid := passkeyRequest(t, r, "/begin", map[string]any{"name": "äö", "email": "admin@example.com", "setupCode": testSetupCode}, nil)
	if invalid.Code != http.StatusBadRequest {
		t.Fatal("accepted a name shorter than three characters")
	}
	_ = registerTestPasskey(t, r)
	w := passkeyRequest(t, r, "/begin", map[string]any{"name": "Other", "email": "other@example.com", "setupCode": testSetupCode}, nil)
	if w.Code != http.StatusForbidden {
		t.Fatal("allowed second setup account")
	}
}

func TestPasskeysManagementOwnershipAndLastKey(t *testing.T) {
	r := setupPasskeyTest(t)
	a := registerTestPasskey(t, r)
	handle, err := base64.RawURLEncoding.DecodeString(a.handle)
	if err != nil {
		t.Fatal(err)
	}
	user, err := passkeys.LoadUser(t.Context(), db.DB, string(handle))
	if err != nil {
		t.Fatal(err)
	}
	r.Use(func(c *gin.Context) { setUserClaims(t, c, &user.User) })
	r.GET("/keys", ListPasskeysHandler)
	r.DELETE("/keys/:id", DeletePasskeyHandler)
	keys, err := gorm.G[models.Passkey](db.DB).Find(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/keys/"+keys[0].Id, nil))
	if w.Code != http.StatusBadRequest {
		t.Fatal("deleted last passkey")
	}
	other := models.User{Name: "Other", Email: "other@example.com", LocalAuthEnabled: true}
	if err := gorm.G[models.User](db.DB).Create(t.Context(), &other); err != nil {
		t.Fatal(err)
	}
	user.User = other
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/keys/"+keys[0].Id, nil))
	if w.Code != http.StatusNotFound {
		t.Fatal("deleted another user's key")
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/keys", nil))
	if w.Code != http.StatusOK || w.Body.String() != "[]" {
		t.Fatalf("leaked passkey metadata: %s", w.Body.String())
	}
}

func TestOwnPasskeyRegistrationAndRemoval(t *testing.T) {
	r := setupPasskeyTest(t)
	a := registerTestPasskey(t, r)
	handle, err := base64.RawURLEncoding.DecodeString(a.handle)
	if err != nil {
		t.Fatal(err)
	}
	u, err := passkeys.LoadUser(t.Context(), db.DB, string(handle))
	if err != nil {
		t.Fatal(err)
	}
	current := &u.User
	own := r.Group("/own", func(c *gin.Context) {
		if current != nil {
			setUserClaims(t, c, current)
		}
	})
	own.POST("/begin", BeginOwnPasskeyHandler)
	own.POST("/finish", FinishOwnPasskeyHandler)
	own.GET("/keys", ListPasskeysHandler)
	own.DELETE("/keys/:id", DeletePasskeyHandler)
	for _, name := range []string{"", " "} {
		w := passkeyRequest(t, r, "/own/begin", map[string]any{"name": name}, nil)
		if w.Code != http.StatusBadRequest {
			t.Fatal("accepted an empty passkey name")
		}
	}
	w := passkeyRequest(t, r, "/own/begin", map[string]any{"name": "Backup"}, nil)
	options, cookie := decodeOptions(t, w), ceremonyCookie(t, w)
	var backup testAuthenticator
	w = passkeyRequest(t, r, "/own/finish", backup.registration(t, options, testBrowserOrigin, true), cookie)
	if w.Code != http.StatusCreated || hasAuthCookie(w) {
		t.Fatalf("adding a key must preserve the existing session: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/own/keys", nil))
	var keys []passkeyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &keys); err != nil || len(keys) != 2 {
		t.Fatalf("missing passkey metadata: %s", w.Body.String())
	}
	for _, key := range keys {
		if key.Name == "Backup" {
			w = httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/own/keys/"+key.Id, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("could not remove backup key: %s", w.Body.String())
			}
		}
	}
	for i := 1; i < passkeys.MaxCredentials; i++ {
		if err := passkeys.SaveCredential(t.Context(), db.DB, u.Id, "Limit", &webauthn.Credential{ID: []byte(strconv.Itoa(i))}); err != nil {
			t.Fatal(err)
		}
	}
	w = passkeyRequest(t, r, "/own/begin", map[string]any{"name": "Too many"}, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatal("allowed more than the credential limit")
	}
	current = nil
	w = passkeyRequest(t, r, "/own/begin", map[string]any{"name": "Unauthorized"}, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatal("unauthenticated key registration")
	}
}

func TestOwnRegistrationCannotSwitchAccount(t *testing.T) {
	r := setupPasskeyTest(t)
	a := registerTestPasskey(t, r)
	handle, _ := base64.RawURLEncoding.DecodeString(a.handle)
	u, err := passkeys.LoadUser(t.Context(), db.DB, string(handle))
	if err != nil {
		t.Fatal(err)
	}
	current := &u.User
	own := r.Group("/own", func(c *gin.Context) { setUserClaims(t, c, current) })
	own.POST("/begin", BeginOwnPasskeyHandler)
	own.POST("/finish", FinishOwnPasskeyHandler)
	w := passkeyRequest(t, r, "/own/begin", map[string]any{"name": "Laptop"}, nil)
	options, cookie := decodeOptions(t, w), ceremonyCookie(t, w)
	other := models.User{Name: "Other", Email: "other@example.com", LocalAuthEnabled: true}
	if err := gorm.G[models.User](db.DB).Create(t.Context(), &other); err != nil {
		t.Fatal(err)
	}
	current = &other
	var key testAuthenticator
	w = passkeyRequest(t, r, "/own/finish", key.registration(t, options, testBrowserOrigin, true), cookie)
	if w.Code != http.StatusForbidden {
		t.Fatal("registered a key after switching accounts")
	}
	current.LocalAuthEnabled = false
	w = passkeyRequest(t, r, "/own/begin", map[string]any{"name": "Managed"}, nil)
	if w.Code != http.StatusForbidden {
		t.Fatal("allowed a managed account to add local credentials")
	}
}

func TestPasskeyEndpointsRejectUnavailableLocalAuthentication(t *testing.T) {
	r := setupPasskeyTest(t)
	for _, scenario := range []string{"disabled", "demo", "unconfigured"} {
		t.Run(scenario, func(t *testing.T) {
			previous := passkeyWebAuthn
			switch scenario {
			case "disabled":
				LocalAuthDisabled = true
			case "demo":
				DemoMode = true
			case "unconfigured":
				passkeyWebAuthn = nil
			}
			t.Cleanup(func() { LocalAuthDisabled, DemoMode, passkeyWebAuthn = false, false, previous })
			w := passkeyRequest(t, r, "/login/begin", nil, nil)
			want := http.StatusForbidden
			if scenario == "unconfigured" {
				want = http.StatusServiceUnavailable
			}
			if w.Code != want || hasAuthCookie(w) {
				t.Fatalf("unexpected %s response: %d", scenario, w.Code)
			}
		})
	}
	w := passkeyRequest(t, r, "/begin", map[string]any{"name": false}, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatal("accepted invalid registration JSON")
	}
}

func TestPasskeyManagementFailsClosedWhenStorageIsUnavailable(t *testing.T) {
	r := setupPasskeyTest(t)
	a := registerTestPasskey(t, r)
	handle, _ := base64.RawURLEncoding.DecodeString(a.handle)
	u, err := passkeys.LoadUser(t.Context(), db.DB, string(handle))
	if err != nil {
		t.Fatal(err)
	}
	own := r.Group("/own", func(c *gin.Context) { setUserClaims(t, c, &u.User) })
	own.POST("/begin", BeginOwnPasskeyHandler)
	own.GET("/keys", ListPasskeysHandler)
	own.DELETE("/keys/:id", DeletePasskeyHandler)
	if err := db.DB.Migrator().DropTable(&models.Passkey{}); err != nil {
		t.Fatal(err)
	}
	w := passkeyRequest(t, r, "/own/begin", map[string]any{"name": "Laptop"}, nil)
	if w.Code != http.StatusInternalServerError || hasAuthCookie(w) {
		t.Fatal("registered a key without credential storage")
	}
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		path := "/own/keys"
		if method == http.MethodDelete {
			path += "/missing"
		}
		w = httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("%s silently succeeded without credential storage", method)
		}
	}
}

func TestPasskeyLoginRejectsAccountDisabledDuringCeremony(t *testing.T) {
	r := setupPasskeyTest(t)
	a := registerTestPasskey(t, r)
	w := passkeyRequest(t, r, "/login/begin", nil, nil)
	options, cookie := decodeOptions(t, w), ceremonyCookie(t, w)
	handle, _ := base64.RawURLEncoding.DecodeString(a.handle)
	if _, err := gorm.G[models.User](db.DB).Where("id = ?", string(handle)).Update(t.Context(), "local_auth_enabled", false); err != nil {
		t.Fatal(err)
	}
	w = passkeyRequest(t, r, "/login/finish", a.assertion(t, options, testBrowserOrigin, true), cookie)
	if w.Code != http.StatusUnauthorized || hasAuthCookie(w) {
		t.Fatal("accepted credentials after local authentication was disabled for the account")
	}
}

func TestPasskeySetupRechecksAuthorizationBeforeCreatingAccount(t *testing.T) {
	for _, scenario := range []string{"code cleared", "account created"} {
		t.Run(scenario, func(t *testing.T) {
			r := setupPasskeyTest(t)
			body := map[string]any{"name": "Admin", "email": "admin@example.com", "setupCode": testSetupCode}
			w := passkeyRequest(t, r, "/begin", body, nil)
			options, cookie := decodeOptions(t, w), ceremonyCookie(t, w)
			var key testAuthenticator
			response := key.registration(t, options, testBrowserOrigin, true)
			wantUsers := int64(0)
			if scenario == "code cleared" {
				setupcode.Clear()
			} else {
				user := models.User{Name: "Other admin", Email: "other@example.com", LocalAuthEnabled: true}
				if err := gorm.G[models.User](db.DB).Create(t.Context(), &user); err != nil {
					t.Fatal(err)
				}
				wantUsers = 1
			}
			w = passkeyRequest(t, r, "/finish", response, cookie)
			if w.Code != http.StatusBadRequest || hasAuthCookie(w) {
				t.Fatal("pending setup created an account after authorization changed")
			}
			if w := passkeyRequest(t, r, "/begin", body, nil); w.Code != http.StatusForbidden {
				t.Fatal("started another setup after authorization changed")
			}
			count, err := gorm.G[models.User](db.DB).Count(t.Context(), "*")
			if err != nil || count != wantUsers {
				t.Fatalf("failed setup changed accounts: count=%d err=%v", count, err)
			}
			count, err = gorm.G[models.Passkey](db.DB).Count(t.Context(), "*")
			if err != nil || count != 0 {
				t.Fatal("failed setup persisted a credential")
			}
		})
	}
}

func TestPasskeyEnrollmentRechecksAuthorizationAtFinish(t *testing.T) {
	for _, scenario := range []string{"expired", "replaced", "managed", "deleted"} {
		t.Run(scenario, func(t *testing.T) {
			r := setupPasskeyTest(t)
			user := models.User{Name: "Invited", Email: "invited@example.com", LocalAuthEnabled: true}
			if err := gorm.G[models.User](db.DB).Create(t.Context(), &user); err != nil {
				t.Fatal(err)
			}
			token, err := passkeys.IssueEnrollment(t.Context(), db.DB, &user)
			if err != nil {
				t.Fatal(err)
			}
			body := map[string]any{"enrollmentToken": token}
			w := passkeyRequest(t, r, "/begin", body, nil)
			options, cookie := decodeOptions(t, w), ceremonyCookie(t, w)
			var key testAuthenticator
			response := key.registration(t, options, testBrowserOrigin, true)
			switch scenario {
			case "expired":
				_, err = gorm.G[models.PasskeyEnrollment](db.DB).Where("user_id = ?", user.Id).Update(t.Context(), "expires_at", time.Now().Add(-time.Second))
			case "replaced":
				_, err = passkeys.IssueEnrollment(t.Context(), db.DB, &user)
			case "managed":
				_, err = gorm.G[models.User](db.DB).Where("id = ?", user.Id).Update(t.Context(), "local_auth_enabled", false)
			case "deleted":
				_, err = gorm.G[models.User](db.DB).Where("id = ?", user.Id).Delete(t.Context())
			}
			if err != nil {
				t.Fatal(err)
			}
			w = passkeyRequest(t, r, "/finish", response, cookie)
			if w.Code != http.StatusBadRequest || hasAuthCookie(w) {
				t.Fatal("accepted a pending enrollment after authorization changed")
			}
			if w := passkeyRequest(t, r, "/begin", body, nil); w.Code != http.StatusForbidden {
				t.Fatal("started registration with an invalid enrollment")
			}
			count, err := gorm.G[models.Passkey](db.DB).Count(t.Context(), "*")
			if err != nil || count != 0 {
				t.Fatal("failed enrollment persisted a credential")
			}
		})
	}
}

func TestPasskeyLoginRejectsNonAdvancingAuthenticatorCounter(t *testing.T) {
	r := setupPasskeyTest(t)
	key := registerTestPasskey(t, r)
	for attempt := range 2 {
		w := passkeyRequest(t, r, "/login/begin", nil, nil)
		options, cookie := decodeOptions(t, w), ceremonyCookie(t, w)
		key.counter = 0
		w = passkeyRequest(t, r, "/login/finish", key.assertion(t, options, testBrowserOrigin, true), cookie)
		if attempt == 0 {
			if w.Code != http.StatusOK || !hasAuthCookie(w) {
				t.Fatal("first assertion should authenticate")
			}
		} else if w.Code != http.StatusUnauthorized || hasAuthCookie(w) {
			t.Fatal("accepted a potentially cloned authenticator")
		}
	}
}

func TestPasskeyLoginAndListingSurviveAnotherCorruptCredential(t *testing.T) {
	for _, scenario := range []string{"json", "ciphertext"} {
		t.Run(scenario, func(t *testing.T) {
			r := setupPasskeyTest(t)
			key := registerTestPasskey(t, r)
			handle, _ := base64.RawURLEncoding.DecodeString(key.handle)
			user, err := passkeys.LoadUser(t.Context(), db.DB, string(handle))
			if err != nil {
				t.Fatal(err)
			}
			bad := models.Passkey{UserId: user.Id, Name: "Damaged", CredentialId: "damaged", Credential: crypto.EncryptedString("{invalid-json}")}
			if err := gorm.G[models.Passkey](db.DB).Create(t.Context(), &bad); err != nil {
				t.Fatal(err)
			}
			if scenario == "ciphertext" {
				if err := db.DB.Exec("UPDATE passkeys SET credential = ? WHERE id = ?", "damaged-ciphertext", bad.Id).Error; err != nil {
					t.Fatal(err)
				}
			}
			w := passkeyRequest(t, r, "/login/begin", nil, nil)
			options, cookie := decodeOptions(t, w), ceremonyCookie(t, w)
			w = passkeyRequest(t, r, "/login/finish", key.assertion(t, options, testBrowserOrigin, true), cookie)
			if w.Code != http.StatusOK || !hasAuthCookie(w) {
				t.Fatal("a corrupt backup credential blocked a healthy passkey login")
			}
			r.GET("/keys", func(c *gin.Context) { setUserClaims(t, c, &user.User) }, ListPasskeysHandler)
			w = httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/keys", nil))
			var keys []passkeyResponse
			if err := json.Unmarshal(w.Body.Bytes(), &keys); err != nil || w.Code != http.StatusOK || len(keys) != 2 {
				t.Fatal("could not list passkey metadata when a credential is corrupt")
			}
		})
	}
}

func TestDuplicatePasskeyRegistrationReturnsBadRequest(t *testing.T) {
	r := setupPasskeyTest(t)
	key := registerTestPasskey(t, r)
	handle, _ := base64.RawURLEncoding.DecodeString(key.handle)
	user, err := passkeys.LoadUser(t.Context(), db.DB, string(handle))
	if err != nil {
		t.Fatal(err)
	}
	own := r.Group("/own", func(c *gin.Context) { setUserClaims(t, c, &user.User) })
	own.POST("/begin", BeginOwnPasskeyHandler)
	own.POST("/finish", FinishOwnPasskeyHandler)
	w := passkeyRequest(t, r, "/own/begin", map[string]any{"name": "Backup"}, nil)
	options, cookie := decodeOptions(t, w), ceremonyCookie(t, w)
	var backup testAuthenticator
	response := backup.registration(t, options, testBrowserOrigin, true)
	if err := passkeys.SaveCredential(t.Context(), db.DB, user.Id, "Already registered", &webauthn.Credential{ID: backup.id}); err != nil {
		t.Fatal(err)
	}
	w = passkeyRequest(t, r, "/own/finish", response, cookie)
	if w.Code != http.StatusBadRequest || hasAuthCookie(w) || w.Body.String() != "{\"error\":\"passkey registration failed, please try again\"}" {
		t.Fatalf("duplicate credential did not return a safe 4xx: %d %s", w.Code, w.Body.String())
	}
	count, err := gorm.G[models.Passkey](db.DB).Where("user_id = ?", user.Id).Count(t.Context(), "*")
	if err != nil || count != 2 {
		t.Fatal("a failed registration changed existing credentials")
	}
}
