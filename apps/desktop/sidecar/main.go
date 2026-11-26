// Copyright 2024 The Kubetail Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Sidecar entry point. Started by the Tauri host, listens on a Unix
// domain socket (no TCP port), and prints the socket path to stdout as
// `READY unix:<path>` so the host can dial it.
//
// This file is lifecycle only: parse flags, bind the listener, build the App
// (the composition root lives in internal/app), serve, and drive graceful
// shutdown. Shutdown signals (any one is sufficient):
//   - SIGINT / SIGTERM
//   - stdin EOF (the Tauri host closes its end when it exits, which is
//     the most reliable cross-platform "parent gone" indicator)
//
// AF_UNIX is supported on macOS, Linux, and Windows 10 build 17063+,
// so a single UDS path works for all our targets.
//
// Started as `kubetail-sidecar sandbox-init …` or `sandbox-shell …`, the binary
// is a sandboxed run's forwarder or shell launcher instead (internal/run/sandbox),
// and none of the above runs.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/app"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/app/layout"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/ipc"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/logging"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/version"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
)

func main() {
	// A sandboxed run's forwarder and shell launcher are this binary too, and
	// take none of the sidecar's flags.
	if code, ok := sandbox.Main(os.Args); ok {
		os.Exit(code)
	}
	// Before a child or a goroutine of ours: the provider keys leave the strings
	// the kernel keeps from the launch, which another process of this user can
	// read. A bad pipe stops the start; nothing logs yet, so stderr says why.
	args, getenv, err := scrubExecEnvironment()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	// Before anything can create a file.
	userUmask := setOwnerOnlyUmask()

	// The log flags decide where records go, so the command line is read before
	// the logger exists. Nothing logs before it: configFromArgs doesn't, and the
	// flag package prints a parse error to stderr itself.
	cfg, err := configFromArgs(args[1:], layout.KnownFolders(getenv))
	if errors.Is(err, flag.ErrHelp) {
		os.Exit(0)
	}
	if err != nil {
		// configFromArgs has said why on stderr; the host shows a FATAL
		// reason on its start screen.
		fmt.Fprintf(os.Stdout, "FATAL %s\n", err)
		os.Exit(2)
	}
	// Before the log, the first file opened: it opens in the resolved log
	// folder.
	if err := resolveFolders(&cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintf(os.Stdout, "FATAL %s\n", err)
		os.Exit(2)
	}
	cfg.App.UserUmask = userUmask
	// Read, so cleared from the live copy too: nothing this process starts
	// inherits a provider key. A no-op after the re-exec.
	takeProviderKeys()

	logger, closeLog, logErr := openLog(cfg, logging.ParseLevel(os.Getenv("KUBETAIL_LOG_LEVEL")))
	slog.SetDefault(logger)
	if logErr != nil {
		// Open handed back the stderr-only logger, so this reaches a terminal.
		slog.Warn("no log file; logging to stderr only", "path", cfg.LogFile, "err", logErr)
	}

	// The shutdown signals, from here: the login shell app.New runs is under
	// them too, so a quit during it cancels it — Resolve then kills and reaps
	// the shell's session, where the default disposition would end the process
	// and leave the shell running.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	// Here, not in run, which tests call: a test's app never spawns a
	// developer's real login shell.
	cfg.App.RunLoginShell = true
	cfg.App.Getenv = os.Getenv

	code := run(ctx, cfg, os.Stdin, os.Stdout)
	stop()
	// os.Exit runs no deferred call, so the file is closed by hand.
	_ = closeLog()
	os.Exit(code)
}

// openLog makes the log folder owner-only, and the Kubetail folder above it, as
// the host does, then opens the log there. A folder it cannot make leaves the
// log on stderr, with the error, as a file Open cannot open does.
func openLog(cfg config, level slog.Level) (*slog.Logger, func() error, error) {
	if err := layout.MakeOwnerOnly(cfg.App.Folders.Layout, filepath.Dir(cfg.LogFile)); err != nil {
		logger, closeLog, _ := logging.Open("", true, level)
		return logger, closeLog, fmt.Errorf("log directory: %w", err)
	}
	return logging.Open(cfg.LogFile, cfg.LogStderr, level)
}

// ownSocket is the sidecar's socket as SweepStale lists it: in the resolved
// runtime folder when it sits in the runtime folder. The host names the
// socket as it names that folder, so on macOS under /var where the folder
// swept is under /private/var.
func ownSocket(cfg config) string {
	dir := filepath.Dir(cfg.Socket)
	if realpath.Same(dir, cfg.App.Folders.Runtime) {
		return filepath.Join(cfg.App.Folders.Runtime, filepath.Base(cfg.Socket))
	}
	return cfg.Socket
}

// run is main without the process: it takes the shutdown context, the parsed
// command line and the two streams the host talks over, and returns the exit
// code instead of calling os.Exit. Everything main does after logging setup
// lives here so the boot and shutdown sequence is reachable from a test.
func run(ctx context.Context, cfg config, stdin io.Reader, stdout io.Writer) int {
	ln, err := ipc.Listen(cfg.Socket)
	if err != nil {
		slog.Error("listen", "socket", cfg.Socket, "err", err)
		return 1
	}
	// Shutdown closes it on the served path; this covers every return before it.
	// A named pipe's name stays taken until its listener closes.
	defer ln.Close()
	ln = ipc.Authenticated(ln, ipc.Policy{HostPID: cfg.HostPID})
	// Named pipes vanish with their listener; only the UDS file needs cleanup.
	removeSocket := func() { _ = os.Remove(cfg.Socket) }
	defer removeSocket()
	// A sidecar the host killed never ran its removal, so its socket is still
	// here; this one's is bound by now, so it is told apart by name.
	ipc.SweepStale(cfg.App.Folders.Runtime, ownSocket(cfg))

	slog.Info("sidecar starting",
		"version", version.Version,
		"socket", cfg.Socket,
		"pid", os.Getpid(),
		"host_pid", cfg.HostPID,
		"data_dir", cfg.App.Folders.Data,
		"oauth_issuer", cfg.App.OAuthIssuerURL,
	)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Watch stdin for EOF as a parent-died signal, from before app.New: a quit
	// during the login shell it runs cancels the shell.
	go func() {
		_, _ = io.Copy(io.Discard, stdin)
		cancel()
	}()

	application, err := app.New(ctx, cfg.App)
	// A quit during startup: a clean exit, not a READY followed by a start that
	// fails on the ended context. New's error is still logged: it may be more
	// than the cancellation.
	if ctx.Err() != nil {
		slog.Info("sidecar shutting down", "reason", "quit before start")
		if err != nil {
			slog.Warn("app init", "err", err)
		} else {
			_ = application.Close()
		}
		return 0
	}
	if err != nil {
		slog.Error("app init", "err", err)
		// Whoever spawned the sidecar reads the reason off stdout a line at a time,
		// so the text is folded onto one.
		reason := strings.Join(strings.Fields(safe.Safe(err)), " ")
		fmt.Fprintf(stdout, "FATAL %s\n", reason)
		return 1
	}

	const maxRequestBytes = 64 * 1024 * 1024
	srv := &http.Server{
		Handler:           http.MaxBytesHandler(application, maxRequestBytes),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		Protocols:         app.Protocols(),
	}
	// Shutdown runs it once the listeners are closed, so long-lived streams end
	// and its wait for in-flight requests can complete. The sequence calls it
	// again once Shutdown returns, since Shutdown does not wait for its hooks.
	srv.RegisterOnShutdown(application.CancelStreams)

	// stdout carries the host protocol and nothing else: `READY ` once the app is
	// composed, `FATAL <reason>` when it is not. The host matches the prefix;
	// scheme+path are informational (the host picked the path and passed it via --socket).
	fmt.Fprintf(stdout, "READY %s:%s\n", ipc.Scheme, cfg.Socket)

	stop, err := application.Start(ctx)
	if err != nil {
		// StartAll unwinds what it started but never closes.
		_ = application.Close()
		// A quit during the start fails it on the ended context: still a clean exit,
		// with the error logged, since it may be more than the cancellation.
		if ctx.Err() != nil {
			slog.Info("sidecar shutting down", "reason", "quit during start")
			slog.Warn("app start", "err", err)
			return 0
		}
		slog.Error("app start", "err", err)
		return 1
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	reason := "signal"
	select {
	case <-ctx.Done():
	case err := <-errCh: // coverage:ignore Serve returns before Shutdown only when its listener fails, which nothing here makes it do
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("serve", "err", err)
			return 1
		}
		reason = "serve returned"
	}

	slog.Info("sidecar shutting down", "reason", reason)
	// The watchdog exits past this function's defers, so it removes the socket itself.
	return newShutdown(srv, application, stop, removeSocket, os.Exit).run()
}
