package trafficlog

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xtls/xray-core/app/proxyman"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/protocol/tls/cert"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/common/uuid"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/proxy/freedom"
	httpin "github.com/xtls/xray-core/proxy/http"
	vless "github.com/xtls/xray-core/proxy/vless"
	vlessin "github.com/xtls/xray-core/proxy/vless/inbound"
	vlessout "github.com/xtls/xray-core/proxy/vless/outbound"
	"github.com/xtls/xray-core/transport/internet"
	transtcp "github.com/xtls/xray-core/transport/internet/tcp"
	xtls "github.com/xtls/xray-core/transport/internet/tls"
)

const (
	rateTestBytes = 256 * 1024
	rateTestLimit = 256 * 1024
)

type rateTestClient struct {
	port xnet.Port
}

func startRateTestServer(t *testing.T, users ...*protocol.User) (xnet.Port, xnet.Destination, []byte) {
	t.Helper()
	serverPort := pickPort(t)
	_, destination := startCountingTarget(t, 0, nil)
	serverCert, certHash := cert.MustGenerate(nil, cert.CommonName("localhost"))
	startInstance(t, "rate-limit-server", &core.Config{
		Inbound: []*core.InboundHandlerConfig{{
			ReceiverSettings: serial.ToTypedMessage(&proxyman.ReceiverConfig{
				PortList: &xnet.PortList{Range: []*xnet.PortRange{xnet.SinglePortRange(serverPort)}},
				Listen:   xnet.NewIPOrDomain(xnet.LocalHostIP),
				StreamSettings: &internet.StreamConfig{
					ProtocolName: "tcp",
					SecurityType: serial.GetMessageType(&xtls.Config{}),
					SecuritySettings: []*serial.TypedMessage{
						serial.ToTypedMessage(&xtls.Config{
							Certificate: []*xtls.Certificate{xtls.ParseCertificate(serverCert)},
						}),
					},
				},
			}),
			ProxySettings: serial.ToTypedMessage(&vlessin.Config{Users: users}),
		}},
		Outbound: []*core.OutboundHandlerConfig{{
			ProxySettings: serial.ToTypedMessage(&freedom.Config{
				FinalRules: []*freedom.FinalRuleConfig{{Action: freedom.RuleAction_Allow}},
			}),
		}},
	})
	return serverPort, destination, certHash[:]
}

func startRateTestClient(t *testing.T, serverPort xnet.Port, id string, certHash []byte) rateTestClient {
	t.Helper()
	clientPort := pickPort(t)
	startInstance(t, "rate-limit-client-"+id, &core.Config{
		Inbound: []*core.InboundHandlerConfig{{
			ReceiverSettings: serial.ToTypedMessage(&proxyman.ReceiverConfig{
				PortList: &xnet.PortList{Range: []*xnet.PortRange{xnet.SinglePortRange(clientPort)}},
				Listen:   xnet.NewIPOrDomain(xnet.LocalHostIP),
			}),
			ProxySettings: serial.ToTypedMessage(&httpin.ServerConfig{}),
		}},
		Outbound: []*core.OutboundHandlerConfig{{
			ProxySettings: serial.ToTypedMessage(&vlessout.Config{
				Vnext: &protocol.ServerEndpoint{
					Address: xnet.NewIPOrDomain(xnet.LocalHostIP),
					Port:    uint32(serverPort),
					User: &protocol.User{
						Account: serial.ToTypedMessage(&vless.Account{Id: id, Flow: vless.XRV}),
					},
				},
			}),
			SenderSettings: serial.ToTypedMessage(&proxyman.SenderConfig{
				StreamSettings: &internet.StreamConfig{
					ProtocolName: "tcp",
					TransportSettings: []*internet.TransportConfig{{
						ProtocolName: "tcp",
						Settings:     serial.ToTypedMessage(&transtcp.Config{}),
					}},
					SecurityType: serial.GetMessageType(&xtls.Config{}),
					SecuritySettings: []*serial.TypedMessage{
						serial.ToTypedMessage(&xtls.Config{PinnedPeerCertSha256: [][]byte{certHash}}),
					},
				},
			}),
		}},
	})
	return rateTestClient{port: clientPort}
}

func rateTestUser(email string, downlink uint64, group string, groupDownlink uint64) (*protocol.User, string) {
	id := protocol.NewID(uuid.New()).String()
	return &protocol.User{
		Email:                   email,
		Account:                 serial.ToTypedMessage(&vless.Account{Id: id, Flow: vless.XRV}),
		DownlinkSpeedLimit:      downlink,
		RateLimitGroup:          group,
		DownlinkGroupSpeedLimit: groupDownlink,
	}, id
}

func connectRateTestClient(t *testing.T, client rateTestClient, destination xnet.Destination) net.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	address := fmt.Sprintf("127.0.0.1:%d", client.port)
	var conn net.Conn
	var err error
	for {
		conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", address)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			t.Fatalf("dial rate-test proxy %s: %v (last error: %v)", address, ctx.Err(), err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			conn.Close()
			t.Fatalf("set CONNECT deadline: %v", err)
		}
	}
	requestTarget := destination.NetAddr()
	if _, err := fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", requestTarget, requestTarget); err != nil {
		conn.Close()
		t.Fatalf("send CONNECT: %v", err)
	}
	reader := bufio.NewReader(conn)
	status, err := reader.ReadString('\n')
	if err != nil {
		conn.Close()
		t.Fatalf("read CONNECT status: %v", err)
	}
	if !strings.Contains(status, " 200 ") {
		conn.Close()
		t.Fatalf("CONNECT status = %q, want 200", strings.TrimSpace(status))
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			conn.Close()
			t.Fatalf("read CONNECT headers: %v", err)
		}
		if strings.TrimSpace(line) == "" {
			return conn
		}
	}
}

func measureRateTestTransfers(t *testing.T, conns []net.Conn) ([]time.Duration, time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	for _, conn := range conns {
		if err := conn.SetDeadline(deadline); err != nil {
			t.Fatalf("set transfer deadline: %v", err)
		}
		t.Cleanup(func() { _ = conn.Close() })
	}

	start := make(chan struct{})
	elapsed := make([]time.Duration, len(conns))
	errCh := make(chan error, len(conns))
	var wg sync.WaitGroup
	for i, conn := range conns {
		wg.Add(1)
		go func(i int, conn net.Conn) {
			defer wg.Done()
			<-start
			began := time.Now()
			payload := bytes.Repeat([]byte{byte(i + 1)}, rateTestBytes)
			if _, err := io.Copy(conn, bytes.NewReader(payload)); err != nil {
				errCh <- fmt.Errorf("write payload %d: %w", i, err)
				return
			}
			if _, err := io.CopyN(io.Discard, conn, int64(rateTestBytes)); err != nil {
				errCh <- fmt.Errorf("read echo %d: %w", i, err)
				return
			}
			elapsed[i] = time.Since(began)
		}(i, conn)
	}
	started := time.Now()
	close(start)
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatalf("rate-limited transfers exceeded timeout: %v", ctx.Err())
	}
	select {
	case err := <-errCh:
		t.Fatal(err)
	default:
	}
	return elapsed, time.Since(started)
}

func TestRateLimitConcurrentSameUserAggregateDownlink(t *testing.T) {
	user, id := rateTestUser("aggregate@example.test", rateTestLimit, "", 0)
	serverPort, destination, certHash := startRateTestServer(t, user)
	client := startRateTestClient(t, serverPort, id, certHash)
	conns := []net.Conn{
		connectRateTestClient(t, client, destination),
		connectRateTestClient(t, client, destination),
	}

	_, total := measureRateTestTransfers(t, conns)
	if total < 1200*time.Millisecond || total >= 8*time.Second {
		t.Fatalf("aggregate transfer time = %s, want 1.2s..8s for one shared 256 KiB/s user cap", total)
	}
}

func TestRateLimitDistinctUsersAtSameIPHaveIndependentCaps(t *testing.T) {
	first, firstID := rateTestUser("independent-a@example.test", rateTestLimit, "", 0)
	second, secondID := rateTestUser("independent-b@example.test", rateTestLimit, "", 0)
	serverPort, destination, certHash := startRateTestServer(t, first, second)
	firstClient := startRateTestClient(t, serverPort, firstID, certHash)
	secondClient := startRateTestClient(t, serverPort, secondID, certHash)
	conns := []net.Conn{
		connectRateTestClient(t, firstClient, destination),
		connectRateTestClient(t, secondClient, destination),
	}

	perConnection, total := measureRateTestTransfers(t, conns)
	if total >= 1500*time.Millisecond {
		t.Fatalf("independent concurrent transfers took %s total, want under 1.5s", total)
	}
	for i, elapsed := range perConnection {
		if elapsed < 350*time.Millisecond || elapsed >= 1500*time.Millisecond {
			t.Errorf("user %d transfer time = %s, want 350ms..1.5s under its own 256 KiB/s cap", i, elapsed)
		}
	}
}

func TestRateLimitGroupSharesDownlinkCap(t *testing.T) {
	first, firstID := rateTestUser("group-a@example.test", 0, "shared-downlink", rateTestLimit)
	second, secondID := rateTestUser("group-b@example.test", 0, "shared-downlink", rateTestLimit)
	serverPort, destination, certHash := startRateTestServer(t, first, second)
	firstClient := startRateTestClient(t, serverPort, firstID, certHash)
	secondClient := startRateTestClient(t, serverPort, secondID, certHash)
	conns := []net.Conn{
		connectRateTestClient(t, firstClient, destination),
		connectRateTestClient(t, secondClient, destination),
	}

	_, total := measureRateTestTransfers(t, conns)
	if total < 1200*time.Millisecond || total >= 8*time.Second {
		t.Fatalf("group transfer time = %s, want 1.2s..8s for shared 256 KiB/s group cap", total)
	}
}

func TestRateLimitUnlimitedUserControl(t *testing.T) {
	user, id := rateTestUser("unlimited@example.test", 0, "", 0)
	serverPort, destination, certHash := startRateTestServer(t, user)
	client := startRateTestClient(t, serverPort, id, certHash)
	conns := []net.Conn{connectRateTestClient(t, client, destination)}

	perConnection, _ := measureRateTestTransfers(t, conns)
	if perConnection[0] >= 1500*time.Millisecond {
		t.Fatalf("unlimited transfer took %s, want under 1.5s", perConnection[0])
	}
}
