package conf

import (
	"encoding/json"
	"testing"

	"github.com/xtls/xray-core/proxy/socks"
)

func TestMixedConfigIdentityAndFailClosed(t *testing.T) {
	var cfg SocksServerConfig
	if err := json.Unmarshal([]byte(`{"auth":"password","clients":[{"user":"login","pass":"secret","email":"billing-id","downlink_speed_limit":2048}]}`), &cfg); err != nil {
		t.Fatal(err)
	}
	built, err := cfg.Build()
	if err != nil {
		t.Fatal(err)
	}
	user := built.(*socks.ServerConfig).Users[0]
	account, err := user.GetTypedAccount()
	if err != nil {
		t.Fatal(err)
	}
	if user.Email != "billing-id" || user.DownlinkSpeedLimit != 2048 || account.(*socks.Account).Username != "login" {
		t.Fatalf("identity/limit mapping: %+v", user)
	}
	for _, raw := range []string{`{"auth":"noauth","clients":[]}`, `{"auth":"password","clients":[],"accounts":[{"user":"legacy","pass":"legacy"}]}`} {
		var cfg SocksServerConfig
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			t.Fatal(err)
		}
		if _, err := cfg.Build(); err == nil {
			t.Fatalf("accepted ambiguous authentication: %s", raw)
		}
	}
}
