package passkeys

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/go-webauthn/webauthn/webauthn"
)

const CeremonyLifetime = 5 * time.Minute
const maxSessions = 1024

// Ceremony binds a WebAuthn challenge to its purpose, account and authorization.
type Ceremony struct {
	Session        webauthn.SessionData
	Purpose        string
	User           models.User
	Name           string
	EnrollmentHash string
	SetupCode      string
	expires        time.Time
}

// Sessions is bounded, expiring, and consumes challenges even on failed finishes.
// A restart cancels pending ceremonies; enrollment codes remain usable until redeemed.
type Sessions struct {
	mu      sync.Mutex
	entries map[string]Ceremony
}

func (s *Sessions) Put(c Ceremony) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, entry := range s.entries {
		if !now.Before(entry.expires) {
			delete(s.entries, id)
		}
	}
	if len(s.entries) >= maxSessions {
		return "", errors.New("too many pending passkey requests")
	}
	if s.entries == nil {
		s.entries = make(map[string]Ceremony)
	}
	id := base64.RawURLEncoding.EncodeToString(randomBytes())
	c.expires = now.Add(CeremonyLifetime)
	s.entries[id] = c
	return id, nil
}

func (s *Sessions) Take(id, purpose string) (Ceremony, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.entries[id]
	delete(s.entries, id)
	return c, ok && c.Purpose == purpose && time.Now().Before(c.expires)
}

func randomBytes() []byte {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return b
}
