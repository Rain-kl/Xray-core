package mixedauth

import (
	"crypto/subtle"
	"fmt"
	"strings"
	"sync"

	"github.com/xtls/xray-core/common/protocol"
)

type Credentials interface {
	GetUsername() string
	GetPassword() string
}

type Users struct {
	mu      sync.RWMutex
	byName  map[string]*protocol.MemoryUser
	byEmail map[string]*protocol.MemoryUser
}

func New() *Users {
	return &Users{byName: make(map[string]*protocol.MemoryUser), byEmail: make(map[string]*protocol.MemoryUser)}
}

func (s *Users) Add(user *protocol.MemoryUser) error {
	if user == nil {
		return fmt.Errorf("mixed user is missing")
	}
	credentials, ok := user.Account.(Credentials)
	if !ok {
		return fmt.Errorf("mixed user requires username/password credentials")
	}
	name, pass := credentials.GetUsername(), credentials.GetPassword()
	if len(name) == 0 || len(name) > 255 || strings.ContainsAny(name, ":\r\n") {
		return fmt.Errorf("mixed username must be 1..255 bytes without colon or line breaks")
	}
	if len(pass) == 0 || len(pass) > 255 {
		return fmt.Errorf("mixed password must be 1..255 bytes")
	}
	if user.Email == "" {
		return fmt.Errorf("mixed user email is missing")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.byName[name]; exists {
		return fmt.Errorf("mixed username already exists")
	}
	if _, exists := s.byEmail[user.Email]; exists {
		return fmt.Errorf("mixed user identity already exists")
	}
	s.byName[name] = user
	s.byEmail[user.Email] = user
	return nil
}

func (s *Users) Authenticate(name, pass string) *protocol.MemoryUser {
	s.mu.RLock()
	defer s.mu.RUnlock()
	user := s.byName[name]
	if user == nil {
		return nil
	}
	credentials := user.Account.(Credentials)
	if subtle.ConstantTimeCompare([]byte(credentials.GetPassword()), []byte(pass)) != 1 {
		return nil
	}
	return user
}

func (s *Users) Get(email string) *protocol.MemoryUser {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byEmail[email]
}

func (s *Users) Remove(email string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	user := s.byEmail[email]
	if user == nil {
		return fmt.Errorf("mixed user not found")
	}
	delete(s.byEmail, email)
	delete(s.byName, user.Account.(Credentials).GetUsername())
	return nil
}

func (s *Users) All() []*protocol.MemoryUser {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*protocol.MemoryUser, 0, len(s.byEmail))
	for _, user := range s.byEmail {
		result = append(result, user)
	}
	return result
}

func (s *Users) Count() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return int64(len(s.byEmail))
}
