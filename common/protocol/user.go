package protocol

import (
	"context"

	"github.com/xtls/xray-core/common/errors"
	"github.com/xtls/xray-core/common/serial"
)

func (u *User) GetTypedAccount() (Account, error) {
	if u.GetAccount() == nil {
		return nil, errors.New("Account is missing")
	}

	rawAccount, err := u.Account.GetInstance()
	if err != nil {
		return nil, err
	}
	if asAccount, ok := rawAccount.(AsAccount); ok {
		return asAccount.AsAccount()
	}
	if account, ok := rawAccount.(Account); ok {
		return account, nil
	}
	return nil, errors.New("Unknown account type: ", u.Account.Type)
}

// FORK(native-rate-limits): Preserve authenticated user caps across upstream sync; see FEATURES.md.
func (u *User) ToMemoryUser() (*MemoryUser, error) {
	account, err := u.GetTypedAccount()
	if err != nil {
		return nil, err
	}
	mu := &MemoryUser{
		Account:                 account,
		Email:                   u.Email,
		Level:                   u.Level,
		UplinkSpeedLimit:        u.GetUplinkSpeedLimit(),
		DownlinkSpeedLimit:      u.GetDownlinkSpeedLimit(),
		RateLimitGroup:          u.GetRateLimitGroup(),
		DownlinkGroupSpeedLimit: u.GetDownlinkGroupSpeedLimit(),
	}
	globalLimiterRegistry.Update(mu.limiterKey(), "uplink", mu.UplinkSpeedLimit)
	globalLimiterRegistry.Update(mu.limiterKey(), "downlink", mu.DownlinkSpeedLimit)
	globalLimiterRegistry.Update(mu.RateLimitGroup, "group-downlink", mu.DownlinkGroupSpeedLimit)
	return mu, nil
}

// FORK(native-rate-limits): Preserve authenticated user caps across upstream sync; see FEATURES.md.
func ToProtoUser(mu *MemoryUser) *User {
	if mu == nil {
		return nil
	}
	return &User{
		Account:                 serial.ToTypedMessage(mu.Account.ToProto()),
		Email:                   mu.Email,
		Level:                   mu.Level,
		UplinkSpeedLimit:        mu.UplinkSpeedLimit,
		DownlinkSpeedLimit:      mu.DownlinkSpeedLimit,
		RateLimitGroup:          mu.RateLimitGroup,
		DownlinkGroupSpeedLimit: mu.DownlinkGroupSpeedLimit,
	}
}

// MemoryUser is a parsed form of User, to reduce number of parsing of Account proto.
type MemoryUser struct {
	// Account is the parsed account of the protocol.
	Account Account
	Email   string
	Level   uint32
	// FORK(native-rate-limits): Preserve authenticated user caps across upstream sync; see FEATURES.md.
	UplinkSpeedLimit   uint64
	DownlinkSpeedLimit uint64

	RateLimitGroup          string
	DownlinkGroupSpeedLimit uint64
}

// FORK(native-rate-limits): Preserve authenticated user caps across upstream sync; see FEATURES.md.
func (u *MemoryUser) GetUplinkLimiter(ctx context.Context) *RateLimiter {
	if u == nil {
		return nil
	}
	return globalLimiterRegistry.Acquire(ctx, u.limiterKey(), "uplink", u.UplinkSpeedLimit)
}

func (u *MemoryUser) GetDownlinkLimiter(ctx context.Context) *RateLimiter {
	if u == nil {
		return nil
	}
	return globalLimiterRegistry.Acquire(ctx, u.limiterKey(), "downlink", u.DownlinkSpeedLimit)
}

func (u *MemoryUser) GetDownlinkGroupLimiter(ctx context.Context) *RateLimiter {
	if u == nil || u.RateLimitGroup == "" {
		return nil
	}
	return globalLimiterRegistry.Acquire(ctx, u.RateLimitGroup, "group-downlink", u.DownlinkGroupSpeedLimit)
}

func (u *MemoryUser) HasRateLimits() bool {
	return u != nil && (u.UplinkSpeedLimit > 0 || u.DownlinkSpeedLimit > 0 || u.DownlinkGroupSpeedLimit > 0)
}

func (u *MemoryUser) limiterKey() string {
	if u.RateLimitGroup == "" {
		return u.Email
	}
	return u.RateLimitGroup + "\x00" + u.Email
}
