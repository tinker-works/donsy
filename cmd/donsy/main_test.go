package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/tinker-works/donsy/internal/adapters/clock"
	"github.com/tinker-works/donsy/internal/adapters/projectstore"
	"github.com/tinker-works/donsy/internal/application/usecases"
	"github.com/tinker-works/donsy/internal/httpapi"
)

func TestPrintDescription(t *testing.T) {
	var output bytes.Buffer

	if err := printDescription(&output); err != nil {
		t.Fatalf("printDescription() error = %v", err)
	}

	const want = "donsy is the go-merge daemon and host\n"
	if output.String() != want {
		t.Fatalf("printDescription() = %q, want %q", output.String(), want)
	}
}

func TestServeStopsDaemonWhenHTTPServerFails(t *testing.T) {
	registry, err := projectstore.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = registry.Close() }()
	api, err := httpapi.New(&usecases.UseCases{}, slog.New(slog.NewTextHandler(io.Discard, nil)), "token")
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("listener failed")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err = serve(ctx, api, failingListener{err: want}, registry, nil, &usecases.UseCases{}, clock.Real{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if !errors.Is(err, want) {
		t.Fatalf("serve() error = %v, want %v", err, want)
	}
}

type failingListener struct{ err error }

func (l failingListener) Accept() (net.Conn, error) { return nil, l.err }
func (failingListener) Close() error                { return nil }
func (failingListener) Addr() net.Addr              { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)} }
