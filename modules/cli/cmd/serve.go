// Copyright 2024-2025 Andres Morey
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
	"crypto/rand"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-logr/logr"
	"github.com/pkg/browser"
	zlog "github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	k8sruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"

	"github.com/kubetail-org/kubetail/modules/dashboard/pkg/app"
	"github.com/kubetail-org/kubetail/modules/shared/config"
	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"

	"github.com/kubetail-org/kubetail/modules/cli/internal/tunnel"
)

const serveHelp = `
This command starts the dashboard server and opens the UI in your default browser.
`

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start dashboard server and open web UI",
	Long:  serveHelp,
	Run: func(cmd *cobra.Command, args []string) {
		// Get flags
		port, _ := cmd.Flags().GetInt("port")
		host, _ := cmd.Flags().GetString("host")
		skipOpen, _ := cmd.Flags().GetBool("skip-open")
		// remote, _ := cmd.Flags().GetBool("remote")
		remote := false
		test, _ := cmd.Flags().GetBool("test")

		// Get the kubeconfig path (if set)
		kubeconfigPath, _ := cmd.Flags().GetString(KubeconfigFlag)

		// Init viper
		v := viper.New()
		v.BindPFlag("dashboard.logging.level", cmd.Flags().Lookup("log-level"))
		v.Set("dashboard.addr", fmt.Sprintf("%s:%d", host, port))

		// init config
		cfg, err := config.NewConfig(v, "")
		if err != nil {
			zlog.Fatal().Caller().Err(err).Send()
		}
		cfg.KubeconfigPath = kubeconfigPath
		cfg.Dashboard.Environment = config.EnvironmentDesktop
		cfg.Dashboard.Logging.AccessLog.Enabled = false

		// Handle remote tunnel
		if remote {
			serveRemote(cfg.KubeconfigPath, port, skipOpen)
			return
		}

		secret, err := generateRandomString(32)
		if err != nil {
			zlog.Fatal().Caller().Err(err).Send()
		}
		cfg.Dashboard.CSRF.Secret = secret

		// set gin mode
		if !test {
			gin.SetMode("release")
		}

		// configure logger
		config.ConfigureLogger(config.LoggerOptions{
			Enabled: true,
			Level:   cfg.Dashboard.Logging.Level,
			Format:  "pretty",
		})

		// Capture unhandled kubernetes client errors
		k8sruntime.ErrorHandlers = []k8sruntime.ErrorHandler{func(ctx context.Context, err error, msg string, keysAndValues ...any) {
			// Suppress for now
		}}

		// Suppress messages sent to klog for now
		klog.SetLogger(logr.Discard())

		// Capture messages sent to system logger
		log.SetOutput(k8shelpers.NewZlogWriter(zlog.Logger))

		// TODO: add more tests than config, basic setup
		if test {
			cmd.Println("ok")
			return
		}

		// listen for termination signals
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		defer close(quit)

		serve(cfg, host, skipOpen, quit)
	},
}

func serve(cfg *config.Config, host string, skipOpen bool, quit <-chan os.Signal) {
	// create app
	app, err := app.NewApp(cfg)
	if err != nil {
		zlog.Fatal().Caller().Err(err).Send()
	}

	// create server
	server := http.Server{
		Addr:         cfg.Dashboard.Addr,
		Handler:      app,
		IdleTimeout:  1 * time.Minute,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// create listener
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		// let system pick port
		listener, err = net.Listen("tcp", fmt.Sprintf("%s:0", host))
		if err != nil {
			zlog.Fatal().Caller().Err(err).Send()
		}
	}
	defer listener.Close()

	// get actual port
	port := listener.Addr().(*net.TCPAddr).Port

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

	zlog.Info().Msgf("Started kubetail dashboard on http://%s:%d/", host, port)

	// open in browser
	if !skipOpen {
		err = browser.OpenURL(fmt.Sprintf("http://%s:%d/", host, port))
		if err != nil {
			zlog.Fatal().Err(err).Send()
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

func generateRandomString(n int) (string, error) {
	// Create a byte slice of length n
	bytes := make([]byte, n)

	// Read random bytes into the slice
	_, err := rand.Read(bytes)
	if err != nil {
		return "", err
	}
	// Convert the byte slice to a hexadecimal string
	return string(bytes), nil
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
	flagset := serveCmd.Flags()
	flagset.SortFlags = false
	flagset.IntP("port", "p", 7500, "Port number to listen on")
	flagset.String("host", "localhost", "Host address to bind to")
	flagset.StringP("log-level", "l", "info", "Log level (debug, info, warn, error, disabled)")
	flagset.Bool("skip-open", false, "Skip opening the browser")
	// flagset.Bool("remote", false, "Open tunnel to remote dashboard")
	flagset.Bool("test", false, "Run internal tests and exit")
}
