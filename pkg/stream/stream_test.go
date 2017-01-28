package stream

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"
)

func TestReadOnce(t *testing.T) {
	t.Parallel()

	n := 3
	rf := func(ctx context.Context, addr string) (io.Reader, error) {
		return &ctxReader{ctx, []byte(addr), int32(10 * n)}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	sink := make(chan []byte)
	addr := "my.address.co"

	// Take n records from the sink, then cancel the context.
	go func() {
		defer cancel()
		var want, have []byte = []byte(addr), nil
		for i := 0; i < n; i++ {
			select {
			case have = <-sink:
			case <-time.After(100 * time.Millisecond):
				t.Fatal("timeout waiting for record")
			}
			if have = bytes.TrimSpace(have); !bytes.Equal(want, have) {
				t.Errorf("want %q, have %q", want, have)
			}
		}
	}()

	// Make sure the context cancelation terminates the function.
	if want, have := context.Canceled, readOnce(ctx, rf, addr, sink); want != have {
		t.Errorf("want %v, have %v", want, have)
	}
}

func TestReadUntilCanceled(t *testing.T) {
	t.Parallel()

	rf := func(ctx context.Context, addr string) (io.Reader, error) {
		return &ctxReader{ctx, []byte(addr), 1}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	sink := make(chan []byte)
	addr := "some.addr.local"

	// Each ctxReader will die after 1 record.
	// So we want to take at least 3, to test it's reconnecting.
	// Once those 3 have been drained, cancel the context.
	go func() {
		defer cancel()
		var want, have []byte = []byte(addr), nil
		for i := 0; i < 3; i++ {
			select {
			case have = <-sink:
			case <-time.After(100 * time.Second):
				t.Fatal("timeout waiting for record")
			}
			if have = bytes.TrimSpace(have); !bytes.Equal(want, have) {
				t.Errorf("want %q, have %q", want, have)
			}
		}
	}()

	// Read until the context has been canceled.
	done := make(chan struct{})
	go func() {
		noSleep := func(time.Duration) { /* no delay pls */ }
		readUntilCanceled(ctx, rf, "some.addr.local", sink, noSleep)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Errorf("timeout waiting for read loop to finish")
	}
}

type ctxReader struct {
	ctx context.Context
	rec []byte
	cnt int32
}

func (r *ctxReader) Read(p []byte) (int, error) {
	if atomic.AddInt32(&r.cnt, -1) < 0 {
		return 0, errors.New("count exceeded")
	}
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return copy(p, append(r.rec, '\n')), nil
	}
}
