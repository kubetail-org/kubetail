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

package cmd

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-logr/logr"
	"github.com/go-playground/validator/v10"
	"github.com/pkg/browser"
	zlog "github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	k8sruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"

	"github.com/kubetail-org/kubetail/modules/dashboard/pkg/app"
	dashcfg "github.com/kubetail-org/kubetail/modules/dashboard/pkg/config"
	sharedcfg "github.com/kubetail-org/kubetail/modules/shared/config"
	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
	"github.com/kubetail-org/kubetail/modules/shared/logging"

	"github.com/kubetail-org/kubetail/modules/cli/internal/tunnel"
	"github.com/kubetail-org/kubetail/modules/cli/pkg/config"
)

type serveOptions struct {
	port     int
	host     string
	skipOpen bool
	remote   bool
	test     bool
}

const serveHelp = `
This command starts the dashboard server and opens the UI in your default browser.
`

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start dashboard server and open web UI",
	Long:  serveHelp,
	PreRunE: func(cmd *cobra.Command, args []string) error {
		var cli config.CLI
		cli.Config, _ = cmd.Flags().GetString("config")

		if cli.Config != "" {
			return validator.New().Struct(cli)
		}

		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		cfg, opts, err := loadServerConfig(cmd)
		if err != nil {
			zlog.Fatal().Caller().Err(err).Send()
		}

		// Handle remote tunnel
		if opts.remote {
			serveRemote(cfg.KubeconfigPath, opts.port, opts.skipOpen)
			return
		}

		// listen for termination signals
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		defer close(quit)

		// set gin mode
		gin.SetMode("release")

		// Capture unhandled kubernetes client errors
		k8sruntime.ErrorHandlers = []k8sruntime.ErrorHandler{func(ctx context.Context, err error, msg string, keysAndValues ...any) {
			// Suppress for now
		}}

		// Suppress messages sent to klog for now
		klog.SetLogger(logr.Discard())

		// Capture messages sent to system logger
		log.SetOutput(k8shelpers.NewZlogWriter(zlog.Logger))

		// TODO: add more tests than config, basic setup
		if opts.test {
			fmt.Println("ok")
			return
		}

		// create app
		app, err := app.NewApp(cfg)
		if err != nil {
			zlog.Fatal().Caller().Err(err).Send()
		}

		// create server
		server := http.Server{
			Addr:         cfg.Addr,
			Handler:      app,
			IdleTimeout:  1 * time.Minute,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 10 * time.Second,
		}

		// create listener
		listener, err := net.Listen("tcp", server.Addr)
		if err != nil {
			// let system pick port
			listener, err = net.Listen("tcp", fmt.Sprintf("%s:0", opts.host))
			if err != nil {
				zlog.Fatal().Caller().Err(err).Send()
			}
		}
		defer listener.Close()

		// get actual port
		opts.port = listener.Addr().(*net.TCPAddr).Port

		serverReady := make(chan struct{})

		// run server in goroutine
		go func() {
			close(serverReady)
			err := server.Serve(listener)
			if err != nil && err != http.ErrServerClosed {
				zlog.Fatal().Err(err).Send()
			}
		}()

		<-serverReady

		zlog.Info().Msgf("Started kubetail dashboard on http://%s:%d/", opts.host, opts.port)

		// open in browser
		if !opts.skipOpen {
			err = browser.OpenURL(fmt.Sprintf("http://%s:%d/", opts.host, opts.port))
			if err != nil {
				if runtime.GOOS == "linux" {
					zlog.Warn().Msg(`Unable to open browser automatically. 
					On Linux systems, you may need to install one of the following packages:
					- xdg-utils (provides xdg-open)
					- x-www-browser
					- www-browser

					Alternatively, you can use the --skip-open flag to skip browser opening.
					Please open the dashboard URL manually.`)
				} else {
					zlog.Warn().Err(err).Msg("Unable to open browser automatically. Please open the dashboard URL manually.")
				}
			}
		}

		// wait for termination signal
		<-quit

		zlog.Info().Msg("Starting graceful shutdown...")

		// graceful shutdown with 30 second deadline
		// TODO: make timeout configurable
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		var wg sync.WaitGroup
		wg.Add(2)

		// attempt graceful shutdown
		go func() {
			defer wg.Done()
			if err := server.Shutdown(ctx); err != nil {
				zlog.Error().Err(err).Send()
			}
		}()

		// shutdown app
		// TODO: handle long-lived requests shutdown (e.g. websockets)
		go func() {
			defer wg.Done()
			if err := app.Shutdown(ctx); err != nil {
				zlog.Error().Err(err).Send()
			}
		}()

		wg.Wait()

		if ctx.Err() == nil {
			zlog.Info().Msg("Completed graceful shutdown")
		}
	},
}

func loadServerConfig(cmd *cobra.Command) (*dashcfg.Config, *serveOptions, error) {
	// Get flags
	configPath, _ := cmd.Flags().GetString("config")
	test, _ := cmd.Flags().GetBool("test")
	logLevel, _ := cmd.Flags().GetString("log-level")
	// remote, _ := cmd.Flags().GetBool("remote")
	remote := false
	inCluster, _ := cmd.Flags().GetBool(InClusterFlag)

	logging.ConfigureLogger(logging.LoggerOptions{
		Enabled: true,
		Level:   logLevel,
		Format:  "cli",
	})

	// Init viper
	v := viper.New()

	v.BindPFlag("general.kubeconfig", cmd.Flags().Lookup(KubeconfigFlag))
	v.BindPFlag("commands.serve.port", cmd.Flags().Lookup("port"))
	v.BindPFlag("commands.serve.host", cmd.Flags().Lookup("host"))
	v.BindPFlag("commands.serve.skip-open", cmd.Flags().Lookup("skip-open"))

	// Init config
	cfg, err := config.NewConfig(configPath, v)
	if err != nil {
		return nil, nil, err
	}

	dashCfg := dashcfg.DefaultConfig()

	dashCfg.KubeconfigPath = cfg.General.KubeconfigPath
	dashCfg.Addr = fmt.Sprintf("%s:%d", cfg.Commands.Serve.Host, cfg.Commands.Serve.Port)
	dashCfg.Environment = sharedcfg.EnvironmentDesktop
	dashCfg.CLIVersion = version
	dashCfg.Logging.Level = logLevel
	if inCluster {
		dashCfg.Environment = sharedcfg.EnvironmentCluster
	}
	dashCfg.Logging.AccessLog.Enabled = false

	serveOptions := &serveOptions{
		port:     cfg.Commands.Serve.Port,
		host:     cfg.Commands.Serve.Host,
		skipOpen: cfg.Commands.Serve.SkipOpen,
		remote:   remote,
		test:     test,
	}

	return dashCfg, serveOptions, nil
}

func serveRemote(kubeconfigPath string, localPort int, skipOpen bool) {
	// Initalize context that stops on SIGTERM
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop() // clean up resources

	tunnel, err := tunnel.NewTunnel(rootCtx, kubeconfigPath, "kubetail-system", "kubetail-dashboard", 80, localPort)
	if err != nil {
		zlog.Fatal().Err(err).Send()
	}

	tunnel.Start()

	// open in browser
	if !skipOpen {
		err = browser.OpenURL(fmt.Sprintf("http://localhost:%d/", localPort))
		if err != nil {
			zlog.Fatal().Err(err).Send()
		}
	}

	// wait for termination signal
	<-rootCtx.Done()

	// graceful shutdown with 30 second deadline
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := tunnel.Shutdown(ctx); err != nil {
		zlog.Error().Err(err).Send()
	}
}

func addServerCmdFlags(cmd *cobra.Command) {
	flagset := cmd.Flags()
	flagset.SortFlags = false
	flagset.IntP("port", "p", 7500, "Port number to listen on")
	flagset.String("host", "localhost", "Host address to bind to")
	flagset.StringP("log-level", "l", "info", "Log level (debug, info, warn, error, disabled)")
	flagset.Bool("skip-open", false, "Skip opening the browser")
	// flagset.Bool("remote", false, "Open tunnel to remote dashboard")
	flagset.Bool("test", false, "Run internal tests and exit")
}

func init() {
	rootCmd.AddCommand(serveCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// serveCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// serveCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	addServerCmdFlags(serveCmd)
}
