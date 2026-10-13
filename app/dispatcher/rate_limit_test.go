package dispatcher

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/common/protocol"
)

type rateLimitTestWriter struct {
	forwarded   atomic.Bool
	closed      atomic.Bool
	interrupted atomic.Bool
}

func (w *rateLimitTestWriter) WriteMultiBuffer(mb buf.MultiBuffer) error {
	w.forwarded.Store(true)
	buf.ReleaseMulti(mb)
	return nil
}

func (w *rateLimitTestWriter) Close() error {
	w.closed.Store(true)
	return nil
}

func (w *rateLimitTestWriter) Interrupt() {
	w.interrupted.Store(true)
}

type rateLimitTestReader struct {
	interrupted atomic.Bool
}

func (r *rateLimitTestReader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	return buf.MultiBuffer{buf.FromBytes([]byte("x"))}, nil
}

func (r *rateLimitTestReader) Interrupt() {
	r.interrupted.Store(true)
}

func TestRateLimitedWriterCloseCancelsWaitAndClosesWriter(t *testing.T) {
	limiter := protocol.NewRateLimiter(1)
	if err := limiter.Wait(context.Background(), 64*1024); err != nil {
		t.Fatalf("drain burst: %v", err)
	}
	downstream := &rateLimitTestWriter{}
	wrapped := newRateLimitedWriter(context.Background(), downstream, limiter).(*rateLimitedWriter)
	done := make(chan error, 1)
	go func() {
		done <- wrapped.WriteMultiBuffer(buf.MultiBuffer{buf.FromBytes([]byte("x"))})
	}()
	waitUntilBlocked(t, done)

	if err := wrapped.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("WriteMultiBuffer error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked write did not stop after Close")
	}
	if downstream.forwarded.Load() {
		t.Fatal("canceled write was forwarded")
	}
	if !downstream.closed.Load() {
		t.Fatal("Close was not forwarded to the underlying writer")
	}
}

func TestRateLimitedReaderInterruptCancelsWaitAndForwards(t *testing.T) {
	limiter := protocol.NewRateLimiter(1)
	if err := limiter.Wait(context.Background(), 64*1024); err != nil {
		t.Fatalf("drain burst: %v", err)
	}
	downstream := &rateLimitTestReader{}
	wrapped := newRateLimitedReader(context.Background(), downstream, limiter).(*rateLimitedReader)
	done := make(chan error, 1)
	go func() {
		_, err := wrapped.ReadMultiBuffer()
		done <- err
	}()
	waitUntilBlocked(t, done)

	wrapped.Interrupt()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("ReadMultiBuffer error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked read did not stop after Interrupt")
	}
	if !downstream.interrupted.Load() {
		t.Fatal("Interrupt was not forwarded to the underlying reader")
	}
}

func waitUntilBlocked(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("operation completed before cancellation: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
}
