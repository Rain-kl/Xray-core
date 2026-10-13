package conf

import "github.com/xtls/xray-core/common/protocol"

type RateLimitConfig struct {
	UplinkSpeedLimit        uint64 `json:"uplink_speed_limit"`
	DownlinkSpeedLimit      uint64 `json:"downlink_speed_limit"`
	RateLimitGroup          string `json:"rate_limit_group"`
	DownlinkGroupSpeedLimit uint64 `json:"downlink_group_speed_limit"`
}

func (c RateLimitConfig) Apply(user *protocol.User) {
	user.UplinkSpeedLimit = c.UplinkSpeedLimit
	user.DownlinkSpeedLimit = c.DownlinkSpeedLimit
	user.RateLimitGroup = c.RateLimitGroup
	user.DownlinkGroupSpeedLimit = c.DownlinkGroupSpeedLimit
}

func (c RateLimitConfig) HasLimits() bool {
	return c.UplinkSpeedLimit != 0 || c.DownlinkSpeedLimit != 0 ||
		c.RateLimitGroup != "" || c.DownlinkGroupSpeedLimit != 0
}
