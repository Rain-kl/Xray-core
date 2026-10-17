package socks

import (
	"context"
	"fmt"

	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/proxy"
	"github.com/xtls/xray-core/proxy/mixedauth"
)

var _ proxy.UserManager = (*Server)(nil)

func configuredUsers(config *ServerConfig) (*mixedauth.Users, error) {
	users := mixedauth.New()
	if config.AuthType != AuthType_PASSWORD {
		if len(config.Users) > 0 {
			return nil, fmt.Errorf("mixed managed users require password authentication")
		}
		return users, nil
	}
	for name, pass := range config.Accounts {
		if err := users.Add(&protocol.MemoryUser{Email: name, Level: config.UserLevel, Account: &Account{Username: name, Password: pass}}); err != nil {
			return nil, err
		}
	}
	for _, raw := range config.Users {
		user, err := raw.ToMemoryUser()
		if err != nil {
			return nil, fmt.Errorf("decode mixed user: %w", err)
		}
		if _, ok := user.Account.(*Account); !ok {
			return nil, fmt.Errorf("mixed user requires SOCKS credentials")
		}
		if err := users.Add(user); err != nil {
			return nil, err
		}
	}
	return users, nil
}

func (s *Server) AddUser(_ context.Context, user *protocol.MemoryUser) error {
	if s.config.AuthType != AuthType_PASSWORD {
		return fmt.Errorf("mixed user management requires password authentication")
	}
	if user == nil {
		return fmt.Errorf("mixed user is missing")
	}
	if _, ok := user.Account.(*Account); !ok {
		return fmt.Errorf("mixed user requires SOCKS credentials")
	}
	return s.users.Add(user)
}
func (s *Server) RemoveUser(_ context.Context, email string) error { return s.users.Remove(email) }
func (s *Server) GetUser(_ context.Context, email string) *protocol.MemoryUser {
	return s.users.Get(email)
}
func (s *Server) GetUsers(context.Context) []*protocol.MemoryUser { return s.users.All() }
func (s *Server) GetUsersCount(context.Context) int64             { return s.users.Count() }
