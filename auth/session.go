package auth

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

const (
	DefaultAdminID       = "admin"
	DefaultAdminUsername = "admin"
	DefaultAdminPassword = "admin"
)

// Session holds an authenticated session.
type Session struct {
	UserID    string
	Username  string
	ExpiresAt time.Time
}

// Store manages in-memory auth sessions.
type Store struct {
	mu       sync.RWMutex
	sessions map[string]Session
	ttl      time.Duration
}

// NewStore creates a session store.
func NewStore(ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &Store{
		sessions: make(map[string]Session),
		ttl:      ttl,
	}
}

// Create issues a new session token for the user.
func (s *Store) Create(userID, username string) string {
	token := randomToken()
	s.mu.Lock()
	s.sessions[token] = Session{
		UserID:    userID,
		Username:  username,
		ExpiresAt: time.Now().Add(s.ttl),
	}
	s.mu.Unlock()
	return token
}

// Get returns session info if the token is valid.
func (s *Store) Get(token string) (Session, bool) {
	if token == "" {
		return Session{}, false
	}
	s.mu.RLock()
	sess, ok := s.sessions[token]
	s.mu.RUnlock()
	if !ok || time.Now().After(sess.ExpiresAt) {
		if ok {
			s.Delete(token)
		}
		return Session{}, false
	}
	return sess, true
}

// Delete removes a session token.
func (s *Store) Delete(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

func randomToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte(time.Now().String()))
	}
	return hex.EncodeToString(buf)
}
