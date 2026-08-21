package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/tinker-works/donsy/internal/adapters/clock"
	"github.com/tinker-works/donsy/internal/adapters/instancelock"
	"github.com/tinker-works/donsy/internal/adapters/projectstore"
	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/application/usecases"
	"github.com/tinker-works/donsy/internal/domain"
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

func TestRunDaemonPersistsEphemeralEndpointAndShutsDown(t *testing.T) {
	configBase := t.TempDir()
	t.Setenv("HOME", configBase)
	t.Setenv("XDG_CONFIG_HOME", configBase)
	installDaemonTools(t)
	originalLogger := slog.Default()
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		slog.SetDefault(originalLogger)
	})
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(configDir, rootName)

	endpoint, token, daemon := startDaemon(t, root, commandOptions{
		endpoint:    "http://127.0.0.1:0",
		endpointSet: true,
	})
	if endpoint.port == 0 {
		t.Fatal("daemon did not persist an assigned port")
	}
	if info, err := os.Stat(filepath.Join(root, tokenFileName)); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("daemon token mode = %v, %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(root, "state", "lima-retired")); err != nil {
		t.Fatalf("startup cleanup marker: %v", err)
	}
	assertDaemonRequest(t, endpoint, token, endpoint.String(), http.StatusNotImplemented)
	assertDaemonRequest(t, endpoint, token, "http://127.0.0.1:8337", http.StatusBadRequest)

	daemon.stop(t)
	lock, err := instancelock.Acquire(filepath.Join(root, "go-merge.lock"))
	if err != nil {
		t.Fatalf("acquire lock after shutdown: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}

	var restartedToken string
	endpoint, restartedToken, daemon = startDaemon(t, root, commandOptions{})
	if restartedToken != token {
		t.Fatalf("token after restart = %q, want original token", restartedToken)
	}
	assertDaemonRequest(t, endpoint, restartedToken, endpoint.String(), http.StatusNotImplemented)
	daemon.stop(t)
}

func TestRunCommandStopsOnInterrupt(t *testing.T) {
	configBase := t.TempDir()
	t.Setenv("HOME", configBase)
	t.Setenv("XDG_CONFIG_HOME", configBase)
	installDaemonTools(t)
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(configDir, rootName)
	command := exec.Command(os.Args[0], "-test.run=^TestDaemonProcess$")
	command.Env = append(os.Environ(), "DONSY_DAEMON_PROCESS=1", "DONSY_DAEMON_ENDPOINT=http://127.0.0.1:0")
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill() })
	if !waitForDaemonStart(root) {
		t.Fatalf("daemon process did not start: %s", output.String())
	}
	if err := command.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("daemon process stopped with %v: %s", err, output.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("daemon process did not stop: %s", output.String())
	}
}

func TestDaemonProcess(t *testing.T) {
	if os.Getenv("DONSY_DAEMON_PROCESS") != "1" {
		return
	}
	args := []string{"server"}
	if endpoint := os.Getenv("DONSY_DAEMON_ENDPOINT"); endpoint != "" {
		args = append(args, "--endpoint", endpoint)
	}
	if err := runCommand(args); err != nil {
		t.Fatal(err)
	}
}

type daemon struct {
	cancel context.CancelFunc
	done   <-chan error
	once   sync.Once
	err    error
}

func startDaemon(t *testing.T, root string, options commandOptions) (endpoint, string, *daemon) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runDaemon(ctx, options)
	}()
	daemon := &daemon{cancel: cancel, done: done}
	t.Cleanup(func() { daemon.stop(t) })
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		contents, err := os.ReadFile(filepath.Join(root, endpointFileName))
		if err == nil {
			selected, err := parseEndpoint(string(contents))
			if err == nil && selected.port != 0 && daemonAcceptsRequests(selected, root) {
				return selected, readDaemonToken(t, root), daemon
			}
		}
		select {
		case err := <-done:
			t.Fatalf("daemon stopped during startup: %v", err)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	daemon.stop(t)
	t.Fatal("daemon did not start")
	return endpoint{}, "", nil
}

func daemonAcceptsRequests(endpoint endpoint, root string) bool {
	token, err := os.ReadFile(filepath.Join(root, tokenFileName))
	if err != nil || len(token) == 0 {
		return false
	}
	request, err := http.NewRequest(http.MethodGet, endpoint.String()+"/api/v1/process", nil)
	if err != nil {
		return false
	}
	request.Header.Set("Authorization", "Bearer "+string(token))
	response, err := (&http.Client{Timeout: 100 * time.Millisecond}).Do(request)
	if err != nil {
		return false
	}
	defer func() { _ = response.Body.Close() }()
	return response.StatusCode == http.StatusOK
}

func waitForDaemonStart(root string) bool {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		contents, err := os.ReadFile(filepath.Join(root, endpointFileName))
		if err == nil {
			selected, err := parseEndpoint(string(contents))
			if err == nil && selected.port != 0 && daemonAcceptsRequests(selected, root) {
				return true
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func readDaemonToken(t *testing.T, root string) string {
	t.Helper()
	token, err := os.ReadFile(filepath.Join(root, tokenFileName))
	if err != nil {
		t.Fatal(err)
	}
	return string(token)
}

func assertDaemonRequest(t *testing.T, endpoint endpoint, token, origin string, wantStatus int) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, endpoint.String()+"/api/v1/reconcile", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Origin", origin)
	response, err := (&http.Client{Timeout: time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != wantStatus {
		t.Fatalf("HTTP response = %d, want %d", response.StatusCode, wantStatus)
	}
}

func (d *daemon) stop(t *testing.T) {
	t.Helper()
	d.once.Do(func() {
		d.cancel()
		select {
		case d.err = <-d.done:
		case <-time.After(5 * time.Second):
			d.err = errors.New("daemon did not stop")
		}
	})
	if d.err != nil {
		t.Fatalf("runDaemon() error = %v", d.err)
	}
}

func installDaemonTools(t *testing.T) {
	t.Helper()
	directory := t.TempDir()
	for _, name := range []string{"colima", "docker", "gh", "limactl"} {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestShutdownHostsStopsEveryRegisteredProject(t *testing.T) {
	registry := projectRegistry{projects: []domain.Project{{ID: 1}, {ID: 2}}}
	host := &projectHost{stopped: make(chan uint, 2)}
	shutdownHosts(registry, host, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for range registry.projects {
		select {
		case <-host.stopped:
		case <-time.After(time.Second):
			t.Fatal("host was not stopped")
		}
	}
}

type projectRegistry struct{ projects []domain.Project }

func (r projectRegistry) List() ([]domain.Project, error) { return r.projects, nil }
func (projectRegistry) Create(*domain.Project) error      { return nil }
func (projectRegistry) Touch(uint) error                  { return nil }
func (projectRegistry) Delete(uint) error                 { return nil }
func (projectRegistry) Close() error                      { return nil }

var _ application.ProjectRegistry = projectRegistry{}

type projectHost struct{ stopped chan uint }

func (h *projectHost) ReapExpiredContainers(context.Context, uint, time.Time, time.Time) (bool, error) {
	return false, nil
}
func (h *projectHost) StopProfile(_ context.Context, projectID uint) (bool, error) {
	h.stopped <- projectID
	return true, nil
}
func (*projectHost) DeleteProfile(context.Context, uint) error { return nil }

var _ agent_runtime.ProjectHost = (*projectHost)(nil)

type failingListener struct{ err error }

func (l failingListener) Accept() (net.Conn, error) { return nil, l.err }
func (failingListener) Close() error                { return nil }
func (failingListener) Addr() net.Addr              { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)} }
