package conf

import (
	"strings"

	"github.com/xtls/xray-core/common/errors"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/common/task"
	"github.com/xtls/xray-core/proxy/shadowsocks"
	"github.com/xtls/xray-core/proxy/shadowsocks_2022"
	"google.golang.org/protobuf/proto"
)

func cipherFromString(c string) shadowsocks.CipherType {
	switch strings.ToLower(c) {
	case "aes-128-gcm", "aead_aes_128_gcm":
		return shadowsocks.CipherType_AES_128_GCM
	case "aes-256-gcm", "aead_aes_256_gcm":
		return shadowsocks.CipherType_AES_256_GCM
	case "chacha20-poly1305", "aead_chacha20_poly1305", "chacha20-ietf-poly1305":
		return shadowsocks.CipherType_CHACHA20_POLY1305
	case "xchacha20-poly1305", "aead_xchacha20_poly1305", "xchacha20-ietf-poly1305":
		return shadowsocks.CipherType_XCHACHA20_POLY1305
	default:
		return shadowsocks.CipherType_UNKNOWN
	}
}

type ShadowsocksUserConfig struct {
	Cipher   string   `json:"method"`
	Password string   `json:"password"`
	Level    byte     `json:"level"`
	Email    string   `json:"email"`
	Address  *Address `json:"address"`
	Port     uint16   `json:"port"`
	// FORK(native-rate-limits): Preserve authenticated user caps across upstream sync; see FEATURES.md.
	RateLimitConfig
}

type ShadowsocksServerConfig struct {
	Cipher      string                   `json:"method"`
	Password    string                   `json:"password"`
	Level       byte                     `json:"level"`
	Email       string                   `json:"email"`
	Users       []*ShadowsocksUserConfig `json:"users"`
	Clients     []*ShadowsocksUserConfig `json:"clients"`
	NetworkList *NetworkList             `json:"network"`
	// FORK(native-rate-limits): Preserve authenticated user caps across upstream sync; see FEATURES.md.
	RateLimitConfig
}

func (v *ShadowsocksServerConfig) Build() (proto.Message, error) {
	errors.PrintNonRemovalDeprecatedFeatureWarning("Shadowsocks (with no Forward Secrecy, etc.)", "VLESS Encryption")

	if v.Clients != nil {
		v.Users = v.Clients
	}
	// FORK(native-rate-limits): Preserve authenticated user caps across upstream sync; see FEATURES.md.
	if len(v.Users) > 0 && v.RateLimitConfig.HasLimits() {
		return nil, errors.New("shadowsocks multi-user: use per-user rate limit fields")
	}

	if _, err := shadowsocks_2022.GetCipherMethod(v.Cipher); err == nil {
		return buildShadowsocks2022(v)
	}

	config := new(shadowsocks.ServerConfig)
	config.Network = v.NetworkList.Build()

	if v.Users != nil {
		if len(v.Users) > 0 {
			config.Users = make([]*protocol.User, len(v.Users))
			processUser := func(idx int) error {
				user := v.Users[idx]
				account := &shadowsocks.Account{
					Password:   user.Password,
					CipherType: cipherFromString(user.Cipher),
				}
				if account.Password == "" {
					return errors.New("Shadowsocks password is not specified.")
				}
				if account.CipherType < shadowsocks.CipherType_AES_128_GCM ||
					account.CipherType > shadowsocks.CipherType_XCHACHA20_POLY1305 {
					return errors.New("unsupported cipher method: ", user.Cipher)
				}
				config.Users[idx] = &protocol.User{
					Email:   user.Email,
					Level:   uint32(user.Level),
					Account: serial.ToTypedMessage(account),
				}
				// FORK(native-rate-limits): Preserve authenticated user caps across upstream sync; see FEATURES.md.
				user.RateLimitConfig.Apply(config.Users[idx])
				return nil
			}
			if err := task.ParallelForN(len(v.Users), processUser); err != nil {
				return nil, err
			}
		}
	} else {
		account := &shadowsocks.Account{
			Password:   v.Password,
			CipherType: cipherFromString(v.Cipher),
		}
		if account.Password == "" {
			return nil, errors.New("Shadowsocks password is not specified.")
		}
		if account.CipherType == shadowsocks.CipherType_UNKNOWN {
			return nil, errors.New("unknown cipher method: ", v.Cipher)
		}
		user := &protocol.User{
			Email:   v.Email,
			Level:   uint32(v.Level),
			Account: serial.ToTypedMessage(account),
		}
		// FORK(native-rate-limits): Preserve authenticated user caps across upstream sync; see FEATURES.md.
		v.RateLimitConfig.Apply(user)
		config.Users = append(config.Users, user)
	}

	return config, nil
}

func buildShadowsocks2022(v *ShadowsocksServerConfig) (proto.Message, error) {
	v.Cipher = strings.ToLower(v.Cipher)
	if len(v.Users) == 0 {
		// FORK(native-rate-limits): Preserve authenticated user caps across upstream sync; see FEATURES.md.
		if v.RateLimitConfig.HasLimits() {
			return nil, errors.New("shadowsocks 2022 (single-user): rate limits are unsupported")
		}
		config := new(shadowsocks_2022.ServerConfig)
		config.Method = v.Cipher
		config.Key = v.Password
		config.Network = v.NetworkList.Build()
		config.Email = v.Email
		config.Level = int32(v.Level)
		return config, nil
	}
	if v.Cipher == "" {
		return nil, errors.New("shadowsocks 2022 (multi-user): missing server method")
	}
	if !strings.Contains(v.Cipher, "aes") {
		return nil, errors.New("shadowsocks 2022 (multi-user): only blake3-aes-*-gcm methods are supported")
	}

	if v.Users[0].Address == nil {
		config := new(shadowsocks_2022.MultiUserServerConfig)
		config.Method = v.Cipher
		config.Key = v.Password
		config.Network = v.NetworkList.Build()

		config.Users = make([]*protocol.User, len(v.Users))
		processUser := func(idx int) error {
			user := v.Users[idx]
			if user.Cipher != "" {
				return errors.New("shadowsocks 2022 (multi-user): users must have empty method")
			}
			account := &shadowsocks_2022.Account{
				Key: user.Password,
			}
			config.Users[idx] = &protocol.User{
				Email:   user.Email,
				Level:   uint32(user.Level),
				Account: serial.ToTypedMessage(account),
			}
			// FORK(native-rate-limits): Preserve authenticated user caps across upstream sync; see FEATURES.md.
			user.RateLimitConfig.Apply(config.Users[idx])
			return nil
		}
		if err := task.ParallelForN(len(v.Users), processUser); err != nil {
			return nil, err
		}
		return config, nil
	}

	config := new(shadowsocks_2022.RelayServerConfig)
	config.Method = v.Cipher
	config.Key = v.Password
	config.Network = v.NetworkList.Build()
	for _, user := range v.Users {
		// FORK(native-rate-limits): Preserve authenticated user caps across upstream sync; see FEATURES.md.
		if user.RateLimitConfig.HasLimits() {
			return nil, errors.New("shadowsocks 2022 (relay): rate limits are unsupported")
		}
		if user.Cipher != "" {
			return nil, errors.New("shadowsocks 2022 (relay): users must have empty method")
		}
		if user.Address == nil {
			return nil, errors.New("shadowsocks 2022 (relay): all users must have relay address")
		}
		config.Destinations = append(config.Destinations, &shadowsocks_2022.RelayDestination{
			Key:     user.Password,
			Email:   user.Email,
			Address: user.Address.Build(),
			Port:    uint32(user.Port),
			Level:   int32(user.Level),
		})
	}
	return config, nil
}

type ShadowsocksServerTarget struct {
	Address  *Address `json:"address"`
	Port     uint16   `json:"port"`
	Level    byte     `json:"level"`
	Email    string   `json:"email"`
	Cipher   string   `json:"method"`
	Password string   `json:"password"`
	// FORK(native-rate-limits): Preserve authenticated user caps across upstream sync; see FEATURES.md.
	RateLimitConfig
}

type ShadowsocksClientConfig struct {
	Address  *Address                   `json:"address"`
	Port     uint16                     `json:"port"`
	Level    byte                       `json:"level"`
	Email    string                     `json:"email"`
	Cipher   string                     `json:"method"`
	Password string                     `json:"password"`
	Servers  []*ShadowsocksServerTarget `json:"servers"`
	// FORK(native-rate-limits): Preserve authenticated user caps across upstream sync; see FEATURES.md.
	RateLimitConfig
}

func (v *ShadowsocksClientConfig) Build() (proto.Message, error) {
	errors.PrintNonRemovalDeprecatedFeatureWarning("Shadowsocks (with no Forward Secrecy, etc.)", "VLESS Encryption")

	// FORK(native-rate-limits): Preserve authenticated user caps across upstream sync; see FEATURES.md.
	if len(v.Servers) > 0 && v.RateLimitConfig.HasLimits() {
		return nil, errors.New("shadowsocks servers: set rate limit fields on the target")
	}
	if v.Address != nil {
		v.Servers = []*ShadowsocksServerTarget{
			{
				Address:         v.Address,
				Port:            v.Port,
				Level:           v.Level,
				Email:           v.Email,
				Cipher:          v.Cipher,
				Password:        v.Password,
				RateLimitConfig: v.RateLimitConfig,
			},
		}
	}
	if len(v.Servers) != 1 {
		return nil, errors.New(`Shadowsocks settings: "servers" should have one and only one member. Multiple endpoints in "servers" should use multiple Shadowsocks outbounds and routing balancer instead`)
	}

	server := v.Servers[0]
	if server.Address == nil {
		return nil, errors.New("Shadowsocks server address is not set.")
	}
	if server.Port == 0 {
		return nil, errors.New("Invalid Shadowsocks port.")
	}
	if server.Password == "" {
		return nil, errors.New("Shadowsocks password is not specified.")
	}

	if _, err := shadowsocks_2022.GetCipherMethod(server.Cipher); err == nil {
		// FORK(native-rate-limits): Preserve authenticated user caps across upstream sync; see FEATURES.md.
		if server.RateLimitConfig.HasLimits() {
			return nil, errors.New("shadowsocks 2022 client: rate limits are unsupported")
		}
		config := new(shadowsocks_2022.ClientConfig)
		config.Address = server.Address.Build()
		config.Port = uint32(server.Port)
		config.Method = server.Cipher
		config.Key = server.Password
		return config, nil
	}
	config := new(shadowsocks.ClientConfig)
	account := &shadowsocks.Account{
		Password: server.Password,
	}
	account.CipherType = cipherFromString(server.Cipher)
	if account.CipherType == shadowsocks.CipherType_UNKNOWN {
		return nil, errors.New("unknown cipher method: ", server.Cipher)
	}
	user := &protocol.User{
		Level:   uint32(server.Level),
		Email:   server.Email,
		Account: serial.ToTypedMessage(account),
	}
	// FORK(native-rate-limits): Preserve authenticated user caps across upstream sync; see FEATURES.md.
	server.RateLimitConfig.Apply(user)
	ss := &protocol.ServerEndpoint{
		Address: server.Address.Build(),
		Port:    uint32(server.Port),
		User:    user,
	}
	config.Server = ss

	return config, nil
}
