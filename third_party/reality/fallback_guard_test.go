package reality

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	gotls "crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"errors"
	"io"
	"math/big"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/crypto/cryptobyte"
)

func startMockDestTLSServer(t *testing.T, certDNSNames []string, responseBody string) (string, *x509.CertPool, *atomic.Int32) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate dest cert key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: certDNSNames[0]},
		DNSNames:     certDNSNames,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create dest cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse dest cert: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)

	tlsCfg := &gotls.Config{
		MinVersion: gotls.VersionTLS13,
		MaxVersion: gotls.VersionTLS13,
		NextProtos: []string{"h2", "http/1.1"},
		Certificates: []gotls.Certificate{{
			Certificate: [][]byte{der},
			PrivateKey:  priv,
		}},
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen dest: %v", err)
	}
	var dialCount atomic.Int32
	var wg sync.WaitGroup
	t.Cleanup(func() {
		_ = ln.Close()
		wg.Wait()
	})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			wg.Add(1)
			go func(raw net.Conn) {
				defer wg.Done()
				defer raw.Close()
				tc := gotls.Server(raw, tlsCfg)
				_ = tc.SetDeadline(time.Now().Add(5 * time.Second))
				if err := tc.Handshake(); err != nil {
					return
				}
				buf := make([]byte, 128)
				n, _ := tc.Read(buf)
				if n > 0 {
					_, _ = tc.Write([]byte(responseBody))
				}
				_ = tc.Close()
			}(c)
		}
	}()

	return ln.Addr().String(), pool, &dialCount
}

func newRealityTestConfig(destAddr string, serverNames []string, dialCount *atomic.Int32) (*Config, *ecdh.PrivateKey, [8]byte) {
	privKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	shortID := [8]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88}
	snMap := make(map[string]bool, len(serverNames))
	for _, s := range serverNames {
		snMap[s] = true
	}
	var baseDialer net.Dialer
	cfg := &Config{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialCount.Add(1)
			return baseDialer.DialContext(ctx, network, addr)
		},
		Type:                   "tcp",
		Dest:                   destAddr,
		ServerNames:            snMap,
		PrivateKey:             privKey,
		ShortIds:               map[[8]byte]bool{shortID: true},
		SessionTicketsDisabled: true,
	}
	return cfg, privKey, shortID
}

func seedPostHandshakeCache(t *testing.T, destAddr string, serverNames []string) {
	t.Helper()
	for _, sni := range serverNames {
		for _, alpn := range []string{"0", "1", "2"} {
			key := destAddr + " " + sni + " " + alpn
			GlobalPostHandshakeRecordsLens.Store(key, []int{})
			t.Cleanup(func() {
				GlobalPostHandshakeRecordsLens.Delete(key)
			})
		}
	}
}

type serverRunResult struct {
	conn *Conn
	err  error
}

func runSingleServerAccept(t *testing.T, cfg *Config) (string, <-chan serverRunResult) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen reality server: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	resCh := make(chan serverRunResult, 1)
	go func() {
		rawConn, acceptErr := ln.Accept()
		if acceptErr != nil {
			resCh <- serverRunResult{err: acceptErr}
			return
		}
		serverConn, serverErr := Server(context.Background(), rawConn, cfg)
		resCh <- serverRunResult{conn: serverConn, err: serverErr}
	}()
	return ln.Addr().String(), resCh
}

func buildRawClientHelloRecord(t *testing.T, serverName string, includeECH bool) []byte {
	t.Helper()
	random := make([]byte, 32)
	_, _ = rand.Read(random)
	sessionID := make([]byte, 32)
	_, _ = rand.Read(sessionID)

	var exts cryptobyte.Builder
	if serverName != "" {
		exts.AddUint16(extensionServerName)
		exts.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
			b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
				b.AddUint8(0)
				b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
					b.AddBytes([]byte(serverName))
				})
			})
		})
	}
	exts.AddUint16(extensionSupportedVersions)
	exts.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
		b.AddUint8LengthPrefixed(func(b *cryptobyte.Builder) {
			b.AddUint16(VersionTLS13)
		})
	})
	exts.AddUint16(extensionSupportedCurves)
	exts.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
		b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
			b.AddUint16(uint16(X25519))
		})
	})
	exts.AddUint16(extensionKeyShare)
	exts.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
		b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
			b.AddUint16(uint16(X25519))
			b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
				b.AddBytes(make([]byte, 32))
			})
		})
	})
	if includeECH {
		exts.AddUint16(extensionEncryptedClientHello)
		exts.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
			// Outer ECH type (0), KDF/AEAD ciphersuite, config_id, encap, payload
			b.AddUint8(0)
			b.AddUint16(0x0001)
			b.AddUint16(0x0001)
			b.AddUint8(0x01)
			b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
				b.AddBytes(make([]byte, 32))
			})
			b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
				b.AddBytes(make([]byte, 32))
			})
		})
	}

	var body cryptobyte.Builder
	body.AddUint16(VersionTLS12)
	body.AddBytes(random)
	body.AddUint8LengthPrefixed(func(b *cryptobyte.Builder) {
		b.AddBytes(sessionID)
	})
	body.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
		b.AddUint16(TLS_AES_128_GCM_SHA256)
	})
	body.AddUint8LengthPrefixed(func(b *cryptobyte.Builder) {
		b.AddUint8(compressionNone)
	})
	extsBytes, err := exts.Bytes()
	if err != nil {
		t.Fatalf("marshal extensions: %v", err)
	}
	body.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
		b.AddBytes(extsBytes)
	})

	bodyBytes, err := body.Bytes()
	if err != nil {
		t.Fatalf("marshal client hello body: %v", err)
	}

	var handshake cryptobyte.Builder
	handshake.AddUint8(typeClientHello)
	handshake.AddUint24(uint32(len(bodyBytes)))
	handshake.AddBytes(bodyBytes)
	hsBytes, err := handshake.Bytes()
	if err != nil {
		t.Fatalf("marshal handshake: %v", err)
	}

	record := make([]byte, recordHeaderLen+len(hsBytes))
	record[0] = byte(recordTypeHandshake)
	record[1] = 0x03
	record[2] = 0x01
	binary.BigEndian.PutUint16(record[3:5], uint16(len(hsBytes)))
	copy(record[recordHeaderLen:], hsBytes)
	return record
}

func assertFatalTLSAlert(t *testing.T, conn net.Conn, expectedAlert alert) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 64)
	n, err := io.ReadFull(conn, buf[:7])
	if err != nil {
		t.Fatalf("read TLS alert: n=%d err=%v", n, err)
	}
	want := []byte{byte(recordTypeAlert), 0x03, 0x03, 0x00, 0x02, alertLevelError, byte(expectedAlert)}
	if !bytes.Equal(buf[:7], want) {
		t.Fatalf("TLS alert = %x, want %x", buf[:7], want)
	}
	extra := make([]byte, 1)
	if _, eofErr := conn.Read(extra); !errors.Is(eofErr, io.EOF) {
		t.Fatalf("expected EOF after alert, got %v", eofErr)
	}
}

func TestServerRejectsFallbackDisallowedAndEmptySNIWithoutDialing(t *testing.T) {
	destAddr, _, dialCount := startMockDestTLSServer(t, []string{"allowed.example"}, "ok")

	cases := []struct {
		name          string
		serverNames   []string
		clientSNI     string
		wantAlert     alert
		wantErrSubstr string
	}{
		{
			name:          "disallowed SNI",
			serverNames:   []string{"allowed.example"},
			clientSNI:     "other-cf.example",
			wantAlert:     alertUnrecognizedName,
			wantErrSubstr: "server name mismatch",
		},
		{
			name:          "empty SNI without empty serverName configured",
			serverNames:   []string{"allowed.example"},
			clientSNI:     "",
			wantAlert:     alertHandshakeFailure,
			wantErrSubstr: "server name mismatch",
		},
		{
			name:          "empty SNI rejected on unauthenticated fallback even when configured",
			serverNames:   []string{"", "allowed.example"},
			clientSNI:     "",
			wantAlert:     alertHandshakeFailure,
			wantErrSubstr: "empty server name is not allowed on fallback",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dialCount.Store(0)
			cfg, _, _ := newRealityTestConfig(destAddr, tc.serverNames, dialCount)
			srvAddr, resCh := runSingleServerAccept(t, cfg)

			c, err := net.DialTimeout("tcp", srvAddr, 3*time.Second)
			if err != nil {
				t.Fatalf("dial server: %v", err)
			}
			defer c.Close()

			if err := writeAll(c, buildRawClientHelloRecord(t, tc.clientSNI, false)); err != nil {
				t.Fatalf("write ClientHello: %v", err)
			}
			assertFatalTLSAlert(t, c, tc.wantAlert)

			res := <-resCh
			if res.err == nil || !strings.Contains(res.err.Error(), tc.wantErrSubstr) {
				t.Fatalf("server err = %v, want substring %q", res.err, tc.wantErrSubstr)
			}
			if got := dialCount.Load(); got != 0 {
				t.Fatalf("dialCount = %d, want 0", got)
			}
		})
	}
}

func TestServerRejectsNonTLSWithoutDialingOrAlert(t *testing.T) {
	destAddr, _, dialCount := startMockDestTLSServer(t, []string{"allowed.example"}, "ok")
	cfg, _, _ := newRealityTestConfig(destAddr, []string{"allowed.example"}, dialCount)
	srvAddr, resCh := runSingleServerAccept(t, cfg)

	c, err := net.DialTimeout("tcp", srvAddr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial server: %v", err)
	}
	defer c.Close()

	if err := writeAll(c, []byte("GET / HTTP/1.1\r\nHost: other-cf.example\r\n\r\n")); err != nil {
		t.Fatalf("write HTTP payload: %v", err)
	}
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 32)
	n, readErr := c.Read(buf)
	if n != 0 || !errors.Is(readErr, io.EOF) {
		t.Fatalf("non-TLS response = (%d bytes, %v), want (0, EOF)", n, readErr)
	}

	res := <-resCh
	if res.err == nil || !strings.Contains(res.err.Error(), "failed to read client hello") {
		t.Fatalf("server err = %v, want failed to read client hello", res.err)
	}
	if got := dialCount.Load(); got != 0 {
		t.Fatalf("dialCount = %d, want 0", got)
	}
}

func TestServerRejectsUnauthenticatedFallbackWithECHWithoutDialing(t *testing.T) {
	destAddr, _, dialCount := startMockDestTLSServer(t, []string{"allowed.example"}, "ok")

	t.Run("raw ECH extension", func(t *testing.T) {
		dialCount.Store(0)
		cfg, _, _ := newRealityTestConfig(destAddr, []string{"allowed.example"}, dialCount)
		srvAddr, resCh := runSingleServerAccept(t, cfg)

		c, err := net.DialTimeout("tcp", srvAddr, 3*time.Second)
		if err != nil {
			t.Fatalf("dial server: %v", err)
		}
		defer c.Close()

		if err := writeAll(c, buildRawClientHelloRecord(t, "allowed.example", true)); err != nil {
			t.Fatalf("write ClientHello with ECH: %v", err)
		}
		assertFatalTLSAlert(t, c, alertHandshakeFailure)

		res := <-resCh
		if res.err == nil || !strings.Contains(res.err.Error(), "encrypted client hello is not allowed on fallback") {
			t.Fatalf("server err = %v, want ECH rejection", res.err)
		}
		if got := dialCount.Load(); got != 0 {
			t.Fatalf("dialCount = %d, want 0", got)
		}
	})

	t.Run("Chrome uTLS GREASE ECH without REALITY auth", func(t *testing.T) {
		dialCount.Store(0)
		cfg, _, _ := newRealityTestConfig(destAddr, []string{"allowed.example"}, dialCount)
		srvAddr, resCh := runSingleServerAccept(t, cfg)

		c, err := net.DialTimeout("tcp", srvAddr, 3*time.Second)
		if err != nil {
			t.Fatalf("dial server: %v", err)
		}
		defer c.Close()

		uc := utls.UClient(c, &utls.Config{
			ServerName:         "allowed.example",
			InsecureSkipVerify: true,
		}, utls.HelloChrome_Auto)
		_ = uc.SetDeadline(time.Now().Add(3 * time.Second))
		if hsErr := uc.Handshake(); hsErr == nil {
			t.Fatal("expected unauthenticated Chrome GREASE ECH fallback handshake to fail")
		}

		res := <-resCh
		if res.err == nil || !strings.Contains(res.err.Error(), "encrypted client hello is not allowed on fallback") {
			t.Fatalf("server err = %v, want ECH rejection", res.err)
		}
		if got := dialCount.Load(); got != 0 {
			t.Fatalf("dialCount = %d, want 0", got)
		}
	})
}

func TestServerAllowsUnauthenticatedFallbackForAllowedSNIWithoutECH(t *testing.T) {
	destAddr, pool, dialCount := startMockDestTLSServer(t, []string{"allowed.example"}, "fallback-ok")

	for _, clientSNI := range []string{"allowed.example", "Allowed.Example"} {
		t.Run(clientSNI, func(t *testing.T) {
			dialCount.Store(0)
			cfg, _, _ := newRealityTestConfig(destAddr, []string{"allowed.example"}, dialCount)
			srvAddr, resCh := runSingleServerAccept(t, cfg)

			c, err := net.DialTimeout("tcp", srvAddr, 3*time.Second)
			if err != nil {
				t.Fatalf("dial server: %v", err)
			}
			tc := gotls.Client(c, &gotls.Config{
				ServerName: clientSNI,
				RootCAs:    pool,
				MinVersion: gotls.VersionTLS13,
				MaxVersion: gotls.VersionTLS13,
			})
			_ = tc.SetDeadline(time.Now().Add(5 * time.Second))
			if err := tc.Handshake(); err != nil {
				t.Fatalf("fallback TLS handshake failed: %v", err)
			}
			if _, err := tc.Write([]byte("ping")); err != nil {
				t.Fatalf("write fallback data: %v", err)
			}
			buf := make([]byte, 32)
			n, err := tc.Read(buf)
			if err != nil && !errors.Is(err, io.EOF) {
				t.Fatalf("read fallback data: %v", err)
			}
			if string(buf[:n]) != "fallback-ok" {
				t.Fatalf("fallback reply = %q, want %q", string(buf[:n]), "fallback-ok")
			}
			_ = tc.Close()

			res := <-resCh
			if res.err == nil || !strings.Contains(res.err.Error(), "authentication failed") {
				t.Fatalf("server err = %v, want authentication failed on unauthenticated fallback", res.err)
			}
			if got := dialCount.Load(); got != 1 {
				t.Fatalf("dialCount = %d, want 1", got)
			}
		})
	}
}

func dialAuthenticatedReality(t *testing.T, srvAddr, sni string, fp utls.ClientHelloID, privKey *ecdh.PrivateKey, shortID [8]byte) *utls.UConn {
	t.Helper()
	rawConn, err := net.DialTimeout("tcp", srvAddr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial server: %v", err)
	}
	var authKey []byte
	var verified bool
	uConn := utls.UClient(rawConn, &utls.Config{
		ServerName:             sni,
		InsecureSkipVerify:     true,
		SessionTicketsDisabled: true,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return errors.New("missing peer cert")
			}
			cert, parseErr := x509.ParseCertificate(rawCerts[0])
			if parseErr != nil {
				return parseErr
			}
			pub, ok := cert.PublicKey.(ed25519.PublicKey)
			if !ok {
				return errors.New("unexpected non-ed25519 key")
			}
			mac := hmac.New(sha512.New, authKey)
			mac.Write(pub)
			if !bytes.Equal(mac.Sum(nil), cert.Signature) {
				return errors.New("invalid REALITY cert HMAC")
			}
			verified = true
			return nil
		},
	}, fp)
	if err := uConn.BuildHandshakeState(); err != nil {
		_ = rawConn.Close()
		t.Fatalf("BuildHandshakeState: %v", err)
	}
	hello := uConn.HandshakeState.Hello
	hello.SessionId = make([]byte, 32)
	copy(hello.Raw[39:], hello.SessionId)
	hello.SessionId[0] = 26
	hello.SessionId[1] = 11
	hello.SessionId[2] = 9
	binary.BigEndian.PutUint32(hello.SessionId[4:], uint32(time.Now().Unix()))
	copy(hello.SessionId[8:], shortID[:])

	ecdhe := uConn.HandshakeState.State13.KeyShareKeys.Ecdhe
	if ecdhe == nil {
		ecdhe = uConn.HandshakeState.State13.KeyShareKeys.MlkemEcdhe
	}
	if ecdhe == nil {
		_ = rawConn.Close()
		t.Fatalf("fingerprint %v missing TLS 1.3 ECDHE keyshare", fp)
	}
	sharedKey, err := ecdhe.ECDH(privKey.PublicKey())
	if err != nil {
		_ = rawConn.Close()
		t.Fatalf("ECDH: %v", err)
	}
	authKey, err = hkdf.Key(sha256.New, sharedKey, hello.Random[:20], "REALITY", 32)
	if err != nil {
		_ = rawConn.Close()
		t.Fatalf("HKDF: %v", err)
	}
	block, _ := aes.NewCipher(authKey)
	aead, _ := cipher.NewGCM(block)
	aead.Seal(hello.SessionId[:0], hello.Random[20:], hello.SessionId[:16], hello.Raw)
	copy(hello.Raw[39:], hello.SessionId)

	_ = uConn.SetDeadline(time.Now().Add(5 * time.Second))
	if err := uConn.Handshake(); err != nil {
		_ = rawConn.Close()
		t.Fatalf("authenticated REALITY handshake: %v", err)
	}
	if !verified {
		_ = uConn.Close()
		t.Fatal("REALITY temporary cert HMAC was not verified")
	}
	_ = uConn.SetDeadline(time.Time{})
	return uConn
}

func TestServerAllowsAuthenticatedRealityWithGREASEECHMixedCaseAndX25519Only(t *testing.T) {
	destAddr, _, dialCount := startMockDestTLSServer(t, []string{"allowed.example"}, "unused")
	seedPostHandshakeCache(t, destAddr, []string{"allowed.example"})

	cases := []struct {
		name string
		sni  string
		fp   utls.ClientHelloID
	}{
		{
			name: "Chrome GREASE ECH with mixed-case SNI",
			sni:  "Allowed.Example",
			fp:   utls.HelloChrome_Auto,
		},
		{
			name: "classical X25519-only fingerprint",
			sni:  "allowed.example",
			fp:   utls.HelloChrome_120,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dialCount.Store(0)
			cfg, privKey, shortID := newRealityTestConfig(destAddr, []string{"allowed.example"}, dialCount)
			srvAddr, resCh := runSingleServerAccept(t, cfg)

			uConn := dialAuthenticatedReality(t, srvAddr, tc.sni, tc.fp, privKey, shortID)
			defer uConn.Close()

			res := <-resCh
			if res.err != nil {
				t.Fatalf("Server() error = %v", res.err)
			}
			defer res.conn.Close()
			if got := dialCount.Load(); got != 1 {
				t.Fatalf("dialCount = %d, want 1", got)
			}

			msg := []byte("hello-reality")
			done := make(chan error, 1)
			go func() {
				buf := make([]byte, len(msg))
				if _, err := io.ReadFull(res.conn, buf); err != nil {
					done <- err
					return
				}
				if !bytes.Equal(buf, msg) {
					done <- errors.New("unexpected payload on server")
					return
				}
				_, err := res.conn.Write([]byte("ack-reality"))
				done <- err
			}()

			if err := writeAll(uConn, msg); err != nil {
				t.Fatalf("client write: %v", err)
			}
			reply := make([]byte, len("ack-reality"))
			if _, err := io.ReadFull(uConn, reply); err != nil {
				t.Fatalf("client read: %v", err)
			}
			if string(reply) != "ack-reality" {
				t.Fatalf("reply = %q, want ack-reality", string(reply))
			}
			if err := <-done; err != nil {
				t.Fatalf("server echo error: %v", err)
			}
		})
	}
}

func TestServerRejectsOversizedFragmentedClientHelloWithoutDialing(t *testing.T) {
	destAddr, _, dialCount := startMockDestTLSServer(t, []string{"allowed.example"}, "ok")
	cfg, _, _ := newRealityTestConfig(destAddr, []string{"allowed.example"}, dialCount)
	srvAddr, resCh := runSingleServerAccept(t, cfg)

	c, err := net.DialTimeout("tcp", srvAddr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial server: %v", err)
	}
	defer c.Close()

	// Declare a ClientHello larger than maxRecordedClientHelloBytes across multiple TLS records.
	chunk := make([]byte, 16384)
	first := make([]byte, recordHeaderLen+len(chunk))
	first[0] = byte(recordTypeHandshake)
	first[1] = 0x03
	first[2] = 0x01
	binary.BigEndian.PutUint16(first[3:5], uint16(len(chunk)))
	first[5] = typeClientHello
	first[6] = 0x01
	first[7] = 0x20
	first[8] = 0x00

	for i := 0; i < 6; i++ {
		pkt := first
		if i > 0 {
			pkt = make([]byte, recordHeaderLen+len(chunk))
			pkt[0] = byte(recordTypeHandshake)
			pkt[1] = 0x03
			pkt[2] = 0x01
			binary.BigEndian.PutUint16(pkt[3:5], uint16(len(chunk)))
		}
		if err := writeAll(c, pkt); err != nil {
			break
		}
	}

	res := <-resCh
	if res.err == nil {
		t.Fatal("expected oversized ClientHello to be rejected")
	}
	if got := dialCount.Load(); got != 0 {
		t.Fatalf("dialCount = %d, want 0", got)
	}
}
