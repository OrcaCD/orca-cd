package docker

import (
	"encoding/json"
	"strings"
	"testing"
)

const inspectFixture = `{
  "Id": "0123456789abcdef",
  "Name": "/app-web-1",
  "Args": ["--password=argsecret", "--listen", ":8080"],
  "State": {
    "Status": "running",
    "Health": {
      "Status": "unhealthy",
      "Log": [{"ExitCode": 1, "Output": "login failed for postgres://dbuser:healthsecret@db:5432/app"}]
    }
  },
  "HostConfig": {
    "LogConfig": {"Type": "syslog", "Config": {"syslog-address": "tcp://logs:514"}}
  },
  "Config": {
    "Env": ["DB_PASSWORD=envsecret", "API_TOKEN=envtoken"],
    "Image": "ghcr.io/org/app:1",
    "Cmd": ["serve", "--api-key", "cmdsecret"],
    "Labels": {
      "traefik.http.routers.app.rule": "Host(` + "`app.example.com`" + `)",
      "traefik.http.middlewares.auth.basicauth.users": "admin:$apr1$labelhash",
      "traefik.http.middlewares.auth.digestauth.users": "admin:digesthash",
      "traefik.http.middlewares.auth.basicauth.usersfile": "/run/secrets/users",
      "traefik.http.services.app.loadbalancer.passhostheader": "true",
      "traefik.http.middlewares.fwd.forwardauth.address": "http://auth:9091/api/verify",
      "com.docker.compose.service": "web"
    }
  }
}`

func sanitizeFixture(t *testing.T) (string, map[string]any) {
	t.Helper()
	out, err := sanitizeInspect([]byte(inspectFixture))
	if err != nil {
		t.Fatalf("sanitizeInspect: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	return out, decoded
}

func TestSanitizeInspect_RemovesEnv(t *testing.T) {
	out, decoded := sanitizeFixture(t)

	if _, ok := decoded["Config"].(map[string]any)["Env"]; ok {
		t.Error("Config.Env must be removed entirely")
	}
	for _, leaked := range []string{"envsecret", "envtoken", "DB_PASSWORD", "API_TOKEN"} {
		if strings.Contains(out, leaked) {
			t.Errorf("output still contains %q", leaked)
		}
	}
}

func TestSanitizeInspect_RedactsSensitiveLabels(t *testing.T) {
	out, decoded := sanitizeFixture(t)
	labels := decoded["Config"].(map[string]any)["Labels"].(map[string]any)

	for _, key := range []string{
		"traefik.http.middlewares.auth.basicauth.users",
		"traefik.http.middlewares.auth.digestauth.users",
	} {
		if labels[key] != redactedValue {
			t.Errorf("label %q = %v, want %q", key, labels[key], redactedValue)
		}
	}
	for _, leaked := range []string{"labelhash", "digesthash"} {
		if strings.Contains(out, leaked) {
			t.Errorf("output still contains %q", leaked)
		}
	}
}

func TestSanitizeInspect_KeepsEverythingElse(t *testing.T) {
	out, decoded := sanitizeFixture(t)
	labels := decoded["Config"].(map[string]any)["Labels"].(map[string]any)

	for key, want := range map[string]string{
		"traefik.http.routers.app.rule":                         "Host(`app.example.com`)",
		"traefik.http.middlewares.auth.basicauth.usersfile":     "/run/secrets/users",
		"traefik.http.services.app.loadbalancer.passhostheader": "true",
		"traefik.http.middlewares.fwd.forwardauth.address":      "http://auth:9091/api/verify",
		"com.docker.compose.service":                            "web",
	} {
		if labels[key] != want {
			t.Errorf("label %q = %v, want %q", key, labels[key], want)
		}
	}

	for _, want := range []string{
		"--password=argsecret", "cmdsecret", "healthsecret", "tcp://logs:514", "ghcr.io/org/app:1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lost %q", want)
		}
	}
}

func TestIsSecretLabelKey(t *testing.T) {
	secret := []string{
		"traefik.http.middlewares.m.basicauth.users",
		"traefik.http.middlewares.m.digestauth.users",
		"Traefik.HTTP.Middlewares.M.BasicAuth.Users",
	}
	for _, key := range secret {
		if !isSecretLabelKey(key) {
			t.Errorf("isSecretLabelKey(%q) = false, want true", key)
		}
	}
	harmless := []string{
		"traefik.http.middlewares.m.basicauth.usersfile",
		"traefik.http.middlewares.m.forwardauth.address",
		"traefik.http.services.s.loadbalancer.passhostheader",
		"com.example.password",
		"com.docker.compose.service",
	}
	for _, key := range harmless {
		if isSecretLabelKey(key) {
			t.Errorf("isSecretLabelKey(%q) = true, want false", key)
		}
	}
}

func TestSanitizeInspect_PreservesLargeNumbers(t *testing.T) {
	out, err := sanitizeInspect([]byte(`{"Config":{},"Big":9007199254740993}`))
	if err != nil {
		t.Fatalf("sanitizeInspect: %v", err)
	}
	if !strings.Contains(out, "9007199254740993") {
		t.Errorf("number precision lost: %s", out)
	}
}

func TestSanitizeInspect_MissingSections(t *testing.T) {
	out, err := sanitizeInspect([]byte(`{"Id":"abc","Config":null,"State":null,"HostConfig":null}`))
	if err != nil {
		t.Fatalf("sanitizeInspect: %v", err)
	}
	if !strings.Contains(out, `"Id": "abc"`) {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestSanitizeInspect_InvalidJSON(t *testing.T) {
	if _, err := sanitizeInspect([]byte("not json")); err == nil {
		t.Error("expected an error for invalid JSON")
	}
}

func TestSanitizeInspect_OutputIsStableAndIndented(t *testing.T) {
	first, _ := sanitizeFixture(t)
	second, _ := sanitizeFixture(t)
	if first != second {
		t.Error("output must be deterministic")
	}
	if !strings.Contains(first, "\n  ") {
		t.Error("output must be indented")
	}
	if strings.Contains(first, `<`) || strings.Contains(first, `&`) {
		t.Error("output must not HTML-escape")
	}
}
