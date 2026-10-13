package trafficlog

import (
	"os"
	"strings"
	"testing"

	"github.com/xtls/xray-core/app/proxyman"
	"github.com/xtls/xray-core/common/serial"
)

func TestSniffedDomainPreservesOriginalDestination(t *testing.T) {
	for _, tc := range []struct {
		name      string
		enabled   bool
		routeOnly bool
		override  []string
	}{
		{"route-only", true, true, []string{"http"}},
		{"override-suppressed", true, false, []string{"tls"}},
		{"disabled", false, false, []string{"http"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := []byte("GET / HTTP/1.1\r\nHost: www.audit.example.com\r\nConnection: close\r\n\r\n")
			_, destination := startCountingTarget(t, int64(len(payload)), nil)
			accessLog := accessLogPath(t, "sniffed-access.log")
			port := pickPort(t)
			config := plainServerConfig(t, port, accessLog)
			message, err := config.Inbound[0].ReceiverSettings.GetInstance()
			if err != nil {
				t.Fatal(err)
			}
			receiver := message.(*proxyman.ReceiverConfig)
			receiver.SniffingSettings = &proxyman.SniffingConfig{Enabled: tc.enabled, RouteOnly: tc.routeOnly, DestinationOverride: tc.override}
			config.Inbound[0].ReceiverSettings = serial.ToTypedMessage(receiver)
			startInstance(t, "sniffed-server", config)
			conn := httpConnect(t, port, destination)
			writeAll(t, conn, payload)
			readAll(t, conn, len(payload))
			conn.Close()
			records := waitAccessRecords(t, accessLog, 1)
			if records[0].uplink != int64(len(payload)) || records[0].downlink != int64(len(payload)) {
				t.Fatalf("sniffing altered accounting: %+v", records[0])
			}
			data, err := os.ReadFile(accessLog)
			if err != nil {
				t.Fatal(err)
			}
			line := string(data)
			if !strings.Contains(line, "accepted //"+destination.NetAddr()) {
				t.Fatalf("original target lost: %s", line)
			}
			if got := strings.Contains(line, "sniffed_domain=www.audit.example.com"); got != tc.enabled {
				t.Fatalf("domain presence = %v, want %v: %s", got, tc.enabled, line)
			}
		})
	}
}
