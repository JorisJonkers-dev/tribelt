package oidc

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// MemStore is an in-memory Store for tests and local tooling; it keeps refresh tokens in plain memory.
type MemStore struct {
	mu       sync.Mutex
	nextID   int64
	accounts map[string]*Account // by issuer + sub
	sessions map[string]*memSession
	Now      func() time.Time
}

type memSession struct {
	Session
	refresh string
}

// NewMemStore returns an empty MemStore.
func NewMemStore() *MemStore {
	return &MemStore{accounts: map[string]*Account{}, sessions: map[string]*memSession{}, Now: time.Now}
}

func (m *MemStore) byID(id int64) *Account {
	for _, a := range m.accounts {
		if a.ID == id {
			return a
		}
	}
	return nil
}

// SignIn implements Store.
func (m *MemStore) SignIn(_ context.Context, issuer string, id Identity, refreshToken, userAgent string, expires time.Time) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.Now()
	acc, ok := m.accounts[issuer+"\x00"+id.Sub]
	if !ok {
		m.nextID++
		acc = &Account{ID: m.nextID, Sub: id.Sub, CreatedAt: now}
		m.accounts[issuer+"\x00"+id.Sub] = acc
	}
	acc.Name, acc.Email, acc.Roles, acc.Admin, acc.LastLoginAt = id.Name, id.Email, id.Roles, IsAdmin(id.Roles), now
	sid := NewSessionID()
	m.sessions[sid] = &memSession{Session: Session{ID: sid, Account: Account{ID: acc.ID}, UserAgent: userAgent, CreatedAt: now, LastSeenAt: now, RolesCheckedAt: now, ExpiresAt: expires}, refresh: refreshToken}
	return sid, nil
}

func (m *MemStore) snapshot(s *memSession) Session {
	out := s.Session
	if acc := m.byID(s.Account.ID); acc != nil {
		out.Account = *acc
	}
	return out
}

func (m *MemStore) live(id string) (*memSession, bool) {
	s, ok := m.sessions[id]
	if !ok || !m.Now().Before(s.ExpiresAt) {
		return nil, false
	}
	return s, true
}

// Load implements Store.
func (m *MemStore) Load(_ context.Context, sessionID string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.live(sessionID)
	if !ok {
		return Session{}, ErrNoSession
	}
	return m.snapshot(s), nil
}

// Refresh implements Store; the mutex stands in for the row lock.
func (m *MemStore) Refresh(ctx context.Context, sessionID string, seen time.Time, fn RefreshFunc) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.live(sessionID)
	if !ok {
		return Session{}, ErrNoSession
	}
	if !s.RolesCheckedAt.Equal(seen) {
		return m.snapshot(s), nil
	}
	next, id, err := fn(ctx, s.refresh)
	if errors.Is(err, ErrSessionGone) {
		delete(m.sessions, sessionID)
		return Session{}, ErrSessionGone
	}
	if err != nil {
		return Session{}, err
	}
	s.refresh, s.RolesCheckedAt = next, m.Now()
	if s.RolesCheckedAt.Equal(seen) {
		s.RolesCheckedAt = seen.Add(time.Nanosecond)
	}
	if acc := m.byID(s.Account.ID); acc != nil {
		acc.Name, acc.Email, acc.Roles, acc.Admin = id.Name, id.Email, id.Roles, IsAdmin(id.Roles)
	}
	return m.snapshot(s), nil
}

// SignOut implements Store.
func (m *MemStore) SignOut(_ context.Context, sessionID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[sessionID]
	if !ok {
		return "", ErrNoSession
	}
	delete(m.sessions, sessionID)
	return s.refresh, nil
}

// SignOutAccount implements Store.
func (m *MemStore) SignOutAccount(_ context.Context, accountID int64) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var tokens []string
	for id, s := range m.sessions {
		if s.Account.ID == accountID {
			tokens = append(tokens, s.refresh)
			delete(m.sessions, id)
		}
	}
	return tokens, nil
}

// Sessions implements Store.
func (m *MemStore) Sessions(_ context.Context, accountID int64) ([]Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Session
	for id := range m.sessions {
		if s, ok := m.live(id); ok && s.Account.ID == accountID {
			out = append(out, m.snapshot(s))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeenAt.After(out[j].LastSeenAt) })
	return out, nil
}

// RefreshToken returns the stored refresh token of a session, for tests.
func (m *MemStore) RefreshToken(sessionID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[sessionID]; ok {
		return s.refresh
	}
	return ""
}
