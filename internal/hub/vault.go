package hub

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"
)

// pbkdfIter follows the OWASP 2023 recommendation for PBKDF2-HMAC-SHA256;
// tests lower it.
var pbkdfIter = 600_000

// MinPasswordLen is the shortest admin password accepted.
const MinPasswordLen = 10

// NewAuth creates the admin record for user/password.
func NewAuth(user, password string) (*AuthRecord, error) {
	if user == "" {
		return nil, errors.New("user is required")
	}
	if len(password) < MinPasswordLen {
		return nil, fmt.Errorf("password must have at least %d characters", MinPasswordLen)
	}
	a := &AuthRecord{User: user, Salt: random(16), KeySalt: random(16), Iter: pbkdfIter}
	h, err := pbkdf2.Key(sha256.New, password, a.Salt, a.Iter, 32)
	if err != nil {
		return nil, err
	}
	a.Hash = h
	return a, nil
}

// Verify checks user/password in constant time and returns the vault key.
func (a *AuthRecord) Verify(user, password string) ([]byte, bool) {
	h, err := pbkdf2.Key(sha256.New, password, a.Salt, a.Iter, 32)
	if err != nil {
		return nil, false
	}
	okUser := subtle.ConstantTimeCompare([]byte(user), []byte(a.User)) == 1
	okPass := subtle.ConstantTimeCompare(h, a.Hash) == 1
	if !okUser || !okPass {
		return nil, false
	}
	k, err := a.Key(password)
	return k, err == nil
}

// Key derives the 256-bit key that seals host secrets.
func (a *AuthRecord) Key(password string) ([]byte, error) {
	return pbkdf2.Key(sha256.New, password, a.KeySalt, a.Iter, 32)
}

// Seal encrypts plain with AES-256-GCM; the result is base64(nonce|ciphertext).
func Seal(key []byte, plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce := random(gcm.NonceSize())
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), nil)), nil
}

// Open decrypts what Seal produced.
func Open(key []byte, sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("sealed secret too short")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", errors.New("cannot decrypt the stored secret (wrong key)")
	}
	return string(plain), nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("vault is locked: log in first")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func random(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

// sessions are the logged-in browsers. One admin account; its vault key is
// kept in memory from the first login until the hub stops.
type sessions struct {
	mu       sync.Mutex
	m        map[string]time.Time // token → expiry
	key      []byte               // vault key, nil until someone logged in
	failures int
	lockedTo time.Time
}

const sessionIdle = 12 * time.Hour

func newSessions() *sessions { return &sessions{m: map[string]time.Time{}} }

func (s *sessions) create(key []byte) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	tok := base64.RawURLEncoding.EncodeToString(random(32))
	s.m[tok] = time.Now().Add(sessionIdle)
	s.key = key
	s.failures = 0
	return tok
}

func (s *sessions) valid(tok string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.m[tok]
	if !ok || time.Now().After(exp) {
		delete(s.m, tok)
		return false
	}
	s.m[tok] = time.Now().Add(sessionIdle)
	return true
}

func (s *sessions) drop(tok string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, tok)
}

func (s *sessions) vaultKey() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.key
}

// failed counts a bad login; after five in a row logins pause for a minute.
func (s *sessions) failed() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures++
	if s.failures >= 5 {
		s.lockedTo = time.Now().Add(time.Minute)
		s.failures = 0
	}
}

func (s *sessions) locked() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Now().Before(s.lockedTo)
}
