package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/toradex/torizon-gateway-app/internal/store"
)

// Errors returned by the service (safe to surface to users).
var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrAlreadyConfigured  = errors.New("an administrator account already exists")
	ErrWeakPassword       = errors.New("password must be at least 10 characters")
	ErrInvalidUsername    = errors.New("username must be 1–32 characters")
)

// Service implements authentication over the store.
type Service struct {
	store      *store.Store
	sessionTTL time.Duration
}

// New builds an auth service. ttl is the session lifetime.
func New(st *store.Store, ttl time.Duration) *Service {
	return &Service{store: st, sessionTTL: ttl}
}

// SessionTTL exposes the configured session lifetime (for cookie expiry).
func (s *Service) SessionTTL() time.Duration { return s.sessionTTL }

// NeedsSetup reports whether no account exists yet (first-boot).
func (s *Service) NeedsSetup() (bool, error) {
	n, err := s.store.UserCount()
	return n == 0, err
}

// CreateAdmin creates the first administrator account. Fails if one exists.
func (s *Service) CreateAdmin(username, password string) error {
	need, err := s.NeedsSetup()
	if err != nil {
		return err
	}
	if !need {
		return ErrAlreadyConfigured
	}
	if err := validateUsername(username); err != nil {
		return err
	}
	if err := validatePassword(password); err != nil {
		return err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	_, err = s.store.CreateUser(username, hash)
	return err
}

// Login verifies credentials and creates a session, returning its id.
func (s *Service) Login(username, password string) (sessionID string, user store.User, err error) {
	u, found, err := s.store.UserByUsername(username)
	if err != nil {
		return "", store.User{}, err
	}
	// Always run a verify to keep timing similar whether or not the user exists.
	hash := u.PasswordHash
	if !found {
		hash = dummyHash
	}
	ok, _ := verifyPassword(password, hash)
	if !found || !ok {
		return "", store.User{}, ErrInvalidCredentials
	}
	sid, err := randomID()
	if err != nil {
		return "", store.User{}, err
	}
	if err := s.store.CreateSession(sid, u.ID, time.Now().Add(s.sessionTTL)); err != nil {
		return "", store.User{}, err
	}
	return sid, u, nil
}

// Validate returns the user for a valid session.
func (s *Service) Validate(sessionID string) (store.User, bool) {
	if sessionID == "" {
		return store.User{}, false
	}
	u, ok, err := s.store.SessionUser(sessionID)
	if err != nil {
		return store.User{}, false
	}
	return u, ok
}

// Logout invalidates a session.
func (s *Service) Logout(sessionID string) error {
	return s.store.DeleteSession(sessionID)
}

func validateUsername(u string) error {
	u = strings.TrimSpace(u)
	if n := utf8.RuneCountInString(u); n < 1 || n > 32 {
		return ErrInvalidUsername
	}
	return nil
}

func validatePassword(p string) error {
	if utf8.RuneCountInString(p) < 10 {
		return ErrWeakPassword
	}
	return nil
}

func randomID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// dummyHash is a valid argon2id hash used to equalize login timing when a
// username is not found (its plaintext is unknown/irrelevant).
var dummyHash = func() string {
	h, _ := hashPassword("x-nonexistent-user-placeholder-x")
	return h
}()
