package dispatcher

import (
	"context"
	"time"

	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/common/protocol"
)

type rateLimitedWriter struct {
	buf.Writer
	ctx     context.Context
	cancel  context.CancelFunc
	limiter *protocol.RateLimiter
}

func newRateLimitedWriter(ctx context.Context, writer buf.Writer, limiter *protocol.RateLimiter) buf.Writer {
	if limiter == nil {
		return writer
	}
	ctx, cancel := context.WithCancel(ctx)
	return &rateLimitedWriter{Writer: writer, ctx: ctx, cancel: cancel, limiter: limiter}
}

func (w *rateLimitedWriter) WriteMultiBuffer(mb buf.MultiBuffer) error {
	if err := w.limiter.Wait(w.ctx, int(mb.Len())); err != nil {
		buf.ReleaseMulti(mb)
		return err
	}
	return w.Writer.WriteMultiBuffer(mb)
}

func (w *rateLimitedWriter) Close() error {
	w.cancel()
	return common.Close(w.Writer)
}

func (w *rateLimitedWriter) Interrupt() {
	w.cancel()
	common.Interrupt(w.Writer)
}

type rateLimitedReader struct {
	reader  buf.Reader
	ctx     context.Context
	cancel  context.CancelFunc
	limiter *protocol.RateLimiter
}

func newRateLimitedReader(ctx context.Context, reader buf.Reader, limiter *protocol.RateLimiter) buf.Reader {
	if limiter == nil {
		return reader
	}
	ctx, cancel := context.WithCancel(ctx)
	return &rateLimitedReader{reader: reader, ctx: ctx, cancel: cancel, limiter: limiter}
}

func (r *rateLimitedReader) limit(mb buf.MultiBuffer, err error) (buf.MultiBuffer, error) {
	if waitErr := r.limiter.Wait(r.ctx, int(mb.Len())); waitErr != nil {
		buf.ReleaseMulti(mb)
		return nil, waitErr
	}
	return mb, err
}

func (r *rateLimitedReader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	return r.limit(r.reader.ReadMultiBuffer())
}

func (r *rateLimitedReader) ReadMultiBufferTimeout(timeout time.Duration) (buf.MultiBuffer, error) {
	reader, ok := r.reader.(buf.TimeoutReader)
	if !ok {
		return nil, buf.ErrNotTimeoutReader
	}
	return r.limit(reader.ReadMultiBufferTimeout(timeout))
}

func (r *rateLimitedReader) Close() error {
	r.cancel()
	return common.Close(r.reader)
}

func (r *rateLimitedReader) Interrupt() {
	r.cancel()
	common.Interrupt(r.reader)
}
