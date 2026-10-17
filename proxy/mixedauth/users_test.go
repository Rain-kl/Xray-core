package mixedauth_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/proxy/mixedauth"
	"github.com/xtls/xray-core/proxy/socks"
)

func TestConcurrentRevocationPreservesIdentity(t *testing.T) {
	users := mixedauth.New()
	user := &protocol.MemoryUser{Email: "scoped-id", DownlinkSpeedLimit: 1234, Account: &socks.Account{Username: "login", Password: "secret"}}
	if err := users.Add(user); err != nil {
		t.Fatal(err)
	}
	if got := users.Authenticate("login", "secret"); got != user || got.DownlinkSpeedLimit != 1234 {
		t.Fatal("authentication lost billing identity or limits")
	}
	if err := users.Add(&protocol.MemoryUser{Email: "another-id", Account: &socks.Account{Username: "login", Password: "different"}}); err == nil {
		t.Fatal("duplicate login accepted")
	}
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := range 100 {
				name := fmt.Sprintf("login-%d-%d", i, j)
				u := &protocol.MemoryUser{Email: name, Account: &socks.Account{Username: name, Password: "pass"}}
				if err := users.Add(u); err != nil {
					t.Error(err)
					return
				}
				if users.Authenticate(name, "pass") != u {
					t.Error("identity mismatch")
				}
				if err := users.Remove(name); err != nil {
					t.Error(err)
				}
			}
		}(i)
	}
	wg.Wait()
	if err := users.Remove(user.Email); err != nil {
		t.Fatal(err)
	}
	if users.Authenticate("login", "secret") != nil || users.Count() != 0 {
		t.Fatal("last user remains authorized")
	}
}
