package reality

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

const maxRecordedClientHelloBytes = maxHandshake + 4096

var errClientHelloTooLarge = errors.New("REALITY: client hello exceeds maximum recorded size")

// clientHelloRecorderConn captures raw bytes read during ClientHello parsing so
// target dialing can wait until SNI, ECH, and REALITY checks decide whether to relay.
type clientHelloRecorderConn struct {
	net.Conn
	buf      bytes.Buffer
	maxBytes int
}

func (c *clientHelloRecorderConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if n > 0 {
		if c.maxBytes > 0 && c.buf.Len()+n > c.maxBytes {
			return 0, errClientHelloTooLarge
		}
		c.buf.Write(b[:n])
	}
	return n, err
}

func (c *clientHelloRecorderConn) Write(b []byte) (int, error) {
	return 0, fmt.Errorf("Write(%v)", len(b))
}

func (c *clientHelloRecorderConn) Close() error {
	return fmt.Errorf("Close()")
}

func (c *clientHelloRecorderConn) SetDeadline(t time.Time) error {
	return nil
}

func (c *clientHelloRecorderConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (c *clientHelloRecorderConn) SetWriteDeadline(t time.Time) error {
	return nil
}

func (c *clientHelloRecorderConn) Recorded() []byte {
	return c.buf.Bytes()
}

func matchConfiguredServerName(config *Config, serverName string) (string, bool) {
	if serverName == "" || config == nil {
		return "", false
	}
	if config.ServerNames[serverName] {
		return serverName, true
	}
	for allowed, ok := range config.ServerNames {
		if ok && allowed != "" && strings.EqualFold(allowed, serverName) {
			return allowed, true
		}
	}
	return "", false
}

func sendFatalTLSAlert(conn net.Conn, code alert) {
	_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, _ = conn.Write([]byte{byte(recordTypeAlert), 0x03, 0x03, 0x00, 0x02, alertLevelError, byte(code)})
	_ = conn.SetWriteDeadline(time.Time{})
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

func relayFallback(underlying CloseWriteConn, target net.Conn, config *Config) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := io.Copy(target, NewRatelimitedConn(underlying, &config.LimitFallbackUpload))
		if err == nil {
			if targetWriterCloser, ok := target.(CloseWriteConn); ok {
				_ = targetWriterCloser.CloseWrite()
			}
			return
		}
		_ = target.Close()
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(underlying, NewRatelimitedConn(target, &config.LimitFallbackDownload))
		_ = underlying.CloseWrite()
	}()
	wg.Wait()
}
