package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/tinker-works/donsy/internal/adapters/agentlog"
	"github.com/tinker-works/donsy/internal/adapters/clock"
	"github.com/tinker-works/donsy/internal/adapters/colima"
	"github.com/tinker-works/donsy/internal/adapters/filestore"
	"github.com/tinker-works/donsy/internal/adapters/github"
	"github.com/tinker-works/donsy/internal/adapters/opencode"
	"github.com/tinker-works/donsy/internal/adapters/projectstore"
	"github.com/tinker-works/donsy/internal/adapters/workspace"
	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/application/usecases"
	"github.com/tinker-works/donsy/internal/httpapi"
)

const description = "donsy is the go-merge daemon and host"

const (
	shutdownGrace     = 45 * time.Second
	shutdownHostGrace = 3 * time.Minute
)

type commandOptions struct {
	endpoint    string
	endpointSet bool
	token       string
	tokenSet    bool
}

func main() {
	if err := runCommand(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func printDescription(w io.Writer) error {
	_, err := fmt.Fprintln(w, description)
	return err
}

func runCommand(args []string) error {
	if len(args) > 0 && args[0] == "server" {
		args = args[1:]
	} else if len(args) > 0 {
		return fmt.Errorf("unknown command %q; use server", args[0])
	}
	flags := flag.NewFlagSet("donsy server", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	options := commandOptions{}
	flags.StringVar(&options.endpoint, "endpoint", "", "loopback HTTP endpoint")
	flags.StringVar(&options.token, "token", "", "daemon HTTP token")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	flags.Visit(func(flag *flag.Flag) {
		switch flag.Name {
		case "endpoint":
			options.endpointSet = true
		case "token":
			options.tokenSet = true
		}
	})
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runDaemon(ctx, options)
}

func runDaemon(ctx context.Context, options commandOptions) error {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	if options.endpointSet {
		if _, err := configuredEndpoint("", options.endpoint, true); err != nil {
			return err
		}
	}
	if options.tokenSet && options.token == "" {
		return fmt.Errorf("--token must not be empty")
	}
	root, lock, err := prepareRoot(configDir)
	if err != nil {
		return err
	}
	defer func() {
		if err := lock.Release(); err != nil {
			log.Print(err)
		}
	}()

	selectedEndpoint, err := configuredEndpoint(root, options.endpoint, options.endpointSet)
	if err != nil {
		return err
	}
	logFile, err := openLogFile(filepath.Join(root, "log", "donsy.log"))
	if err != nil {
		return err
	}
	defer func() { _ = logFile.Close() }()
	log.SetOutput(logFile)
	logger := slog.New(slog.NewTextHandler(logFile, nil))
	slog.SetDefault(logger)

	githubClient := github.NewClient()
	var githubAPI application.GitHubClient
	var currentUser string
	if err := githubClient.CheckAuth(ctx); err != nil {
		logger.Warn("GitHub integration is unavailable", "error", err)
	} else if currentUser, err = githubClient.CurrentUser(ctx); err != nil {
		logger.Warn("read GitHub identity", "error", err)
	} else {
		githubAPI = githubClient
	}
	registry, err := projectstore.Open(filepath.Join(root, "state.db"))
	if err != nil {
		return err
	}
	defer func() {
		if err := registry.Close(); err != nil {
			logger.Error("close state registry", "error", err)
		}
	}()
	if err := colima.CheckTooling(); err != nil {
		return err
	}
	colima.RetireLima(ctx, root)

	realClock := clock.Real{}
	agentLogDir := filepath.Join(root, "log", "agents")
	sandboxes := colima.NewClient(
		agentLogDir, root,
		filepath.Join(root, "containers"),
		filepath.Join(root, "build"),
	)
	credentials, err := opencode.NewCredentials(root)
	if err != nil {
		return err
	}
	agentWorkspace := workspace.NewAgentWorkspace(filepath.Join(root, "workspace"))
	useCases := usecases.NewUseCases(
		registry,
		projectstore.NewFactory(filepath.Join(root, "stores", "projects")),
		realClock,
		githubAPI,
		&usecases.EpicAgentDependencies{
			Output:         agentlog.NewReader(agentLogDir),
			Sandboxes:      sandboxes,
			Host:           sandboxes,
			Inspector:      sandboxes,
			Runtime:        sandboxes,
			Disk:           sandboxes,
			Builder:        opencode.Builder{},
			Credentials:    credentials,
			Repositories:   agentWorkspace,
			Differ:         agentWorkspace,
			IssueTreeStore: filestore.NewIssueTreeStore(filepath.Join(root, "workspace")),
			Code:           workspace.NewCodeWorkspace(filepath.Join(root, "workdir", "code")),
		},
	)
	useCases.CurrentUser = currentUser
	token, err := configuredToken(root, options.token, options.tokenSet)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", selectedEndpoint.listenAddress())
	if err != nil {
		return fmt.Errorf("listen on HTTP API address %s: %w", selectedEndpoint.listenAddress(), err)
	}
	defer func() { _ = listener.Close() }()
	selectedEndpoint, err = selectedEndpoint.withListener(listener)
	if err != nil {
		return err
	}
	if err := persistEndpoint(root, selectedEndpoint); err != nil {
		return err
	}
	api, err := httpapi.NewWithDaemonLog(useCases, logger, logFile.Name(), token)
	if err != nil {
		return err
	}
	if err := api.ConfigureEndpoint(selectedEndpoint.String()); err != nil {
		return err
	}
	return serve(ctx, api, listener, registry, sandboxes, useCases, realClock, logger)
}

func serve(
	ctx context.Context,
	api *httpapi.Server,
	listener net.Listener,
	registry application.Registry,
	sandboxes *colima.Client,
	useCases *usecases.UseCases,
	realClock application.Clock,
	logger *slog.Logger,
) error {
	apiContext, stopAPI := context.WithCancel(context.Background())
	apiServer := api.HTTPServer(apiContext)
	workerContext, stopWorker := context.WithCancel(context.Background())
	worker := usecases.NewEpicWorker(
		registry,
		useCases.ListEpics,
		useCases.ReconcileSandboxes,
		useCases.RunEpicAgent,
		realClock,
		5*time.Second,
		useCases.IssueLoop(),
		useCases.PurgeFinishedWork,
	)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		worker.Run(workerContext, func(err error) {
			logger.Error("epic worker failed", "error", err)
		})
	}()
	serverDone := make(chan struct{})
	serverErr := make(chan error, 1)
	go func() {
		defer close(serverDone)
		if err := apiServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP API stopped unexpectedly", "error", err)
			serverErr <- fmt.Errorf("serve HTTP API: %w", err)
			return
		}
		serverErr <- nil
	}()

	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-serverErr:
	}
	stopAPI()
	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := apiServer.Shutdown(shutdownContext); err != nil {
		logger.Error("shut down HTTP API", "error", err)
		_ = apiServer.Close()
	}
	cancel()
	<-serverDone
	stopWorker()
	select {
	case <-workerDone:
		shutdownHosts(registry, sandboxes, logger)
	case <-time.After(shutdownGrace):
		logger.Warn("epic worker did not stop in time; the next launch will reconcile")
	}
	return serveErr
}

func shutdownHosts(projects application.ProjectRegistry, host agent_runtime.ProjectHost, logger *slog.Logger) {
	registered, err := projects.List()
	if err != nil {
		logger.Error("list projects for host shutdown", "error", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownHostGrace)
	defer cancel()
	var wait sync.WaitGroup
	var failures []error
	var failuresMu sync.Mutex
	for _, project := range registered {
		wait.Add(1)
		go func(projectID uint) {
			defer wait.Done()
			if _, err := host.StopProfile(ctx, projectID); err != nil {
				failuresMu.Lock()
				failures = append(failures, fmt.Errorf("stop host of project %d: %w", projectID, err))
				failuresMu.Unlock()
			}
		}(project.ID)
	}
	wait.Wait()
	if err := errors.Join(failures...); err != nil {
		logger.Error("stop project hosts", "error", err)
	}
}

func openLogFile(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}
