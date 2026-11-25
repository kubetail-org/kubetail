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

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/catalog"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/app"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/app/layout"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/ipc"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors/aws"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors/chatgpt"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors/github"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors/google"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/connectors/openrouter"
)

// config is everything the command line decides: the app's own configuration
// plus the two values main needs to bind the listener.
type config struct {
	App app.Config
	// Folders is Kubetail's folders as the command line names them, which
	// resolveFolders resolves into App.Folders.
	Folders app.Named
	Socket  string
	// Zero leaves the endpoint open to any process of this user, which is what
	// a standalone dev run wants; the host always passes its own.
	HostPID int
	// LogFile is logFileName in the resolved layout's log folder, set by
	// resolveFolders. LogStderr copies every record to stderr too.
	LogFile   string
	LogStderr bool
}

// logFileName is the sidecar's log, beside the host's main.log.
const logFileName = "sidecar.log"

// devKeychainService is the keyring service a dev profile defaults to, the
// host's KEYCHAIN_SERVICE in a debug build.
const devKeychainService = "Kubetail-dev"

// configFromArgs parses the sidecar's command line. Endpoints are arguments,
// never environment: the sidecar inherits the host's environment, and anything
// that can set a variable there could otherwise redirect sign-in. Only a build
// tagged `debug` lets applyEnvOverrides put the environment back in play. The
// model-provider keys are the stated exception, read here by readProviders.
// getenv is the environment those reads see — os.Getenv from main, and a test's
// own map, so a test never reads a key out of the shell running it.
//
// The sidecar places every folder of Kubetail's itself, from the roots getenv
// names (layout.RootsFromEnv), as the host places its own from the same
// environment; the host passes only the runtime directory, which a squatted
// name can move. It reads no disk: resolveFolders follows the folders' links.
//
// On a bad flag or root the problem is already on stderr; the caller only
// needs to exit.
func configFromArgs(args []string, getenv func(string) string) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("kubetail-sidecar", flag.ContinueOnError)
	fs.StringVar(&cfg.Socket, "socket", ipc.DefaultSocketPath(), "path to the IPC endpoint (Unix domain socket on Unix, named pipe on Windows) to listen on")
	fs.IntVar(&cfg.HostPID, "host-pid", 0, "pid of the host process; the only process allowed to connect (0 allows any process of this user)")
	fs.StringVar(&cfg.App.KubeconfigPath, "kubeconfig", "", "explicit kubeconfig path; empty uses the clientcmd default-loading rules ($KUBECONFIG / ~/.kube/config)")
	fs.StringVar(&cfg.Folders.Runtime, "runtime-dir", "", "directory for what lives for a session: the shell snapshot, a sandboxed run's files (required)")
	// The OAuth client is public (PKCE/loopback, no secret), so baking the
	// production defaults into the binary leaks nothing.
	fs.StringVar(&cfg.App.OAuthIssuerURL, "oauth-issuer", "https://oauth.kubetail.com", "OAuth issuer URL")
	fs.StringVar(&cfg.App.OAuthClientID, "oauth-client-id", "kubetail-desktop", "OAuth client id")
	// GitHub's device flow runs under a public client too; empty refuses its sign-in.
	fs.StringVar(&cfg.App.GitHubClientID, "github-client-id", "", "GitHub OAuth App client id")
	// Google's installed-app client carries a secret that is not confidential;
	// an empty id refuses its sign-in.
	fs.StringVar(&cfg.App.GoogleClientID, "google-client-id", "", "Google OAuth client id")
	fs.StringVar(&cfg.App.GoogleClientSecret, "google-client-secret", "", "Google OAuth client secret")
	// The host passes its install's name, so a dev run and an installed
	// release never share one keychain entry.
	fs.StringVar(&cfg.App.KeychainService, "keychain-service", "", "keyring service name; empty is the Kubetail default, or Kubetail-dev with --dev")
	fs.BoolVar(&cfg.LogStderr, "log-stderr", false, "also write every record to stderr")
	// The host passes its own build's profile, so its folders and the
	// sidecar's take one leaf whatever either was built with.
	dev := fs.Bool("dev", debugBuild, "use a dev build's folders (the `-dev` leaf); defaults to how this binary was built")

	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	cfg.App.HostPID = cfg.HostPID
	roots, err := layout.RootsFromEnv(layout.Current(), getenv)
	if err != nil {
		fmt.Fprintln(fs.Output(), err)
		return config{}, err
	}
	cfg.Folders.Platform = layout.Current()
	cfg.Folders.Layout = layout.Of(cfg.Folders.Platform, *dev, roots)
	cfg.Folders.Profile = roots.Home
	// A dev profile's sign-ins stay apart from an installed release's, as
	// its folders do; the host passes this name itself.
	if *dev && cfg.App.KeychainService == "" {
		cfg.App.KeychainService = devKeychainService
	}
	cfg.Folders.Data, cfg.Folders.Cache = cfg.Folders.Layout.Data, cfg.Folders.Layout.Cache
	readProviders(&cfg, getenv)
	cfg.App.AddFake = debugBuild
	applyEnvOverrides(&cfg, getenv)
	if err := refuseDotDot(cfg); err != nil {
		fmt.Fprintln(fs.Output(), err)
		return config{}, err
	}
	return cfg, nil
}

// resolveFolders follows the links of every folder cfg names, checks them,
// and makes the sidecar's own (app.ResolveFolders). The log file is then in
// the resolved log folder.
func resolveFolders(cfg *config) error {
	folders, err := app.ResolveFolders(cfg.Folders)
	if err != nil {
		return err
	}
	cfg.App.Folders = folders
	cfg.LogFile = filepath.Join(folders.Layout.Logs, logFileName)
	return nil
}

// refuseDotDot refuses a directory the sidecar writes in that holds `..` on
// Unix: the runtime directory, and the data and cache directories a debug
// build's overrides can move. The fences take a `..` out by name, while the
// filesystem reads one after a link as the link target's parent, so files
// written there could sit outside the fences. The roots the layout hangs off
// are refused likewise (layout.RootsFromEnv). Windows reads `..` by name
// itself.
func refuseDotDot(cfg config) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	for _, d := range []struct{ name, path string }{
		{"--runtime-dir", cfg.Folders.Runtime},
		{"the data directory", cfg.Folders.Data},
		{"the cache directory", cfg.Folders.Cache},
	} {
		if slices.Contains(strings.Split(d.path, "/"), "..") {
			return fmt.Errorf("%s is %q, which holds `..`", d.name, d.path)
		}
	}
	return nil
}

// readProviders takes the model-provider keys from the environment in every build —
// an exception to *the environment reaches the config only in a debug build*, and
// stated as one: a key selects an account, never an endpoint, so it cannot
// redirect where anything is sent. A key that is set is kept under its provider's
// id; one that is not is left out, for app.New to read from the login shell. Each
// key is registered with the redactor here, before the logger exists, so no log
// line can carry it.
func readProviders(cfg *config, getenv func(string) string) {
	for id, keyVar := range catalog.KeyVars() {
		key := getenv(keyVar)
		if key == "" {
			continue
		}
		safe.AddSecret(key)
		if cfg.App.LLMKeys == nil {
			cfg.App.LLMKeys = map[string]string{}
		}
		cfg.App.LLMKeys[id] = key
	}
}

// takeProviderKeys removes every key variable in the table from the process
// environment, the config having read them. The sidecar spawns kubeconfig
// credential plugins and a child inherits the environment; a key left in it would
// be handed to every plugin the kubeconfig names. Only the key variables are
// cleared: the rest of the user's environment is theirs.
func takeProviderKeys() {
	for _, keyVar := range catalog.KeyVars() {
		os.Unsetenv(keyVar)
	}
}

// applyEnvOverrides lets a standalone dev run — no host to pass arguments —
// point the sidecar somewhere else. It is a no-op unless the binary was built
// with `-tags debug` (`make build-sidecar-dev`); the body stays compiled in every
// build so vet and the tests always see it.
func applyEnvOverrides(cfg *config, getenv func(string) string) {
	if !debugBuild {
		return
	}
	set := func(dst *string, name string) {
		if v := getenv(name); v != "" {
			*dst = v
		}
	}
	set(&cfg.App.OAuthIssuerURL, "KUBETAIL_OAUTH_ISSUER")
	set(&cfg.App.OAuthClientID, "KUBETAIL_OAUTH_CLIENT_ID")
	set(&cfg.App.GitHubClientID, github.ClientIDVar)
	set(&cfg.App.GoogleClientID, google.ClientIDVar)
	set(&cfg.App.GoogleClientSecret, google.ClientSecretVar)
	set(&cfg.Folders.Data, "KUBETAIL_DATA_DIR")
	set(&cfg.Folders.Cache, "KUBETAIL_CACHE_DIR")
	set(&cfg.Folders.Runtime, "KUBETAIL_RUNTIME_DIR")
	set(&cfg.App.KeychainService, "KUBETAIL_KEYCHAIN_SERVICE")
	set(&cfg.App.Faults, "KUBETAIL_FAULTS")
	// setEach is set for a map: each variable that is set moves its id's entry.
	setEach := func(dst *map[string]string, vars map[string]string) {
		for id, name := range vars {
			if v := getenv(name); v != "" {
				if *dst == nil {
					*dst = map[string]string{}
				}
				(*dst)[id] = v
			}
		}
	}
	setEach(&cfg.App.LLMBaseURLs, catalog.BaseURLVars())
	setEach(&cfg.App.ConnectorAuthURLs, map[string]string{
		chatgpt.ID:    chatgpt.IssuerURLVar,
		openrouter.ID: openrouter.AuthURLVar,
		aws.ID:        aws.SigninURLVar,
		github.ID:     github.AuthURLVar,
	})
}
