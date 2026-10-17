package conf

import (
	"encoding/json"

	"github.com/xtls/xray-core/common/errors"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/proxy/socks"
	"google.golang.org/protobuf/proto"
)

type SocksAccount struct {
	Username string `json:"user"`
	Password string `json:"pass"`
}

func (v *SocksAccount) Build() *socks.Account {
	return &socks.Account{
		Username: v.Username,
		Password: v.Password,
	}
}

const (
	AuthMethodNoAuth   = "noauth"
	AuthMethodUserPass = "password"
)

// FORK(mixed-clients): Authentication names must stay independent of panel billing identities.
type MixedClientConfig struct {
	SocksAccount
	Email string `json:"email"`
	Level uint32 `json:"level"`
	RateLimitConfig
}

type SocksServerConfig struct {
	// FORK(mixed-clients): An explicit clients array selects managed password authentication.
	Clients    []MixedClientConfig `json:"clients"`
	AuthMethod string              `json:"auth"`
	Users      []*SocksAccount     `json:"users"`
	Accounts   []*SocksAccount     `json:"accounts"`
	UDP        bool                `json:"udp"`
	Host       *Address            `json:"ip"`
	UserLevel  uint32              `json:"userLevel"`
}

func (v *SocksServerConfig) Build() (proto.Message, error) {
	config := new(socks.ServerConfig)
	switch v.AuthMethod {
	case AuthMethodNoAuth:
		config.AuthType = socks.AuthType_NO_AUTH
	case AuthMethodUserPass:
		config.AuthType = socks.AuthType_PASSWORD
	default:
		config.AuthType = socks.AuthType_NO_AUTH
	}

	if v.Accounts != nil {
		v.Users = v.Accounts
	}
	// TODO: PB
	if len(v.Users) > 0 {
		config.Accounts = make(map[string]string, len(v.Users))
		for _, account := range v.Users {
			config.Accounts[account.Username] = account.Password
		}
	}

	config.UdpEnabled = v.UDP
	if v.Host != nil {
		config.Address = v.Host.Build()
	}

	// FORK(mixed-clients): Empty managed users must deny access rather than falling back to noauth.
	if v.Clients != nil {
		if v.AuthMethod != AuthMethodUserPass {
			return nil, errors.New("mixed clients require password authentication")
		}
		if len(v.Accounts) > 0 || len(v.Users) > 0 {
			return nil, errors.New("mixed clients cannot be combined with legacy accounts/users")
		}
		for _, client := range v.Clients {
			user := &protocol.User{Email: client.Email, Level: client.Level, Account: serial.ToTypedMessage(client.SocksAccount.Build())}
			client.RateLimitConfig.Apply(user)
			config.Users = append(config.Users, user)
		}
	}
	config.UserLevel = v.UserLevel
	return config, nil
}

type SocksRemoteConfig struct {
	Address *Address          `json:"address"`
	Port    uint16            `json:"port"`
	Users   []json.RawMessage `json:"users"`
}

type SocksClientConfig struct {
	Address  *Address             `json:"address"`
	Port     uint16               `json:"port"`
	Level    uint32               `json:"level"`
	Email    string               `json:"email"`
	Username string               `json:"user"`
	Password string               `json:"pass"`
	Servers  []*SocksRemoteConfig `json:"servers"`
}

func (v *SocksClientConfig) Build() (proto.Message, error) {
	config := new(socks.ClientConfig)
	if v.Address != nil {
		v.Servers = []*SocksRemoteConfig{
			{
				Address: v.Address,
				Port:    v.Port,
			},
		}
		if len(v.Username) > 0 {
			v.Servers[0].Users = []json.RawMessage{{}}
		}
	}
	if len(v.Servers) != 1 {
		return nil, errors.New(`SOCKS settings: "servers" should have one and only one member. Multiple endpoints in "servers" should use multiple SOCKS outbounds and routing balancer instead`)
	}
	for _, serverConfig := range v.Servers {
		if len(serverConfig.Users) > 1 {
			return nil, errors.New(`SOCKS servers: "users" should have one member at most. Multiple members in "users" should use multiple SOCKS outbounds and routing balancer instead`)
		}
		server := &protocol.ServerEndpoint{
			Address: serverConfig.Address.Build(),
			Port:    uint32(serverConfig.Port),
		}
		for _, rawUser := range serverConfig.Users {
			user := new(protocol.User)
			if v.Address != nil {
				user.Level = v.Level
				user.Email = v.Email
			} else {
				if err := json.Unmarshal(rawUser, user); err != nil {
					return nil, errors.New("failed to parse Socks user").Base(err)
				}
			}
			account := new(SocksAccount)
			if v.Address != nil {
				account.Username = v.Username
				account.Password = v.Password
			} else {
				if err := json.Unmarshal(rawUser, account); err != nil {
					return nil, errors.New("failed to parse socks account").Base(err)
				}
			}
			user.Account = serial.ToTypedMessage(account.Build())
			server.User = user
			break
		}
		config.Server = server
		break
	}
	return config, nil
}
