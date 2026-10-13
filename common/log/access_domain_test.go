package log

import (
	"strings"
	"testing"
)

func TestAccessDomainCannotInjectLogFields(t *testing.T) {
	m := &AccessMessage{To: "tcp:192.0.2.1:443", Status: AccessAccepted, SniffedDomain: "example.com\n email: forged", Uplink: 10, Downlink: 20}
	line := m.String()
	if strings.ContainsAny(line, "\r\n") || !strings.Contains(line, "sniffed_domain=example.com%0A+email%3A+forged") {
		t.Fatalf("unsafe domain encoding: %q", line)
	}
	if !strings.Contains(line, "accepted tcp:192.0.2.1:443") || !strings.HasSuffix(line, "uplink=10 downlink=20") {
		t.Fatalf("domain changed existing fields: %q", line)
	}
	m.SniffedDomain = ""
	if strings.Contains(m.String(), "sniffed_domain=") {
		t.Fatal("legacy access records must omit the optional field")
	}
}
