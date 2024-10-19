// Copyright 2024 Andres Morey
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
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pkg/browser"
	zlog "github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/kubetail-org/kubetail/backend/common/config"
	"github.com/kubetail-org/kubetail/backend/server/pkg/ginapp"

	"github.com/kubetail-org/kubetail/backend/cli/internal/tunnel"
)

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Open dashboard",
	Long:  `Start server and open the dashboard UI`,
	Run: func(cmd *cobra.Command, args []string) {
		// Get flags
		port, _ := cmd.Flags().GetInt("port")
		host, _ := cmd.Flags().GetString("host")
		skipOpen, _ := cmd.Flags().GetBool("skip-open")
		//remote, _ := cmd.Flags().GetBool("remote")
		remote := false
		test, _ := cmd.Flags().GetBool("test")

		// Handle remote tunnel
		if remote {
			serveRemote(port, skipOpen)
			return
		}

		// listen for termination signals
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		defer close(quit)

		// Init viper
		v := viper.New()
		v.BindPFlag("server.logging.level", cmd.Flags().Lookup("log-level"))
		v.Set("server.addr", fmt.Sprintf("%s:%d", host, port))

		// init config
		cfg, err := config.NewConfig(v, "")
		if err != nil {
			zlog.Fatal().Caller().Err(err).Send()
		}

		cfg.AuthMode = config.AuthModeLocal
		cfg.Server.Logging.AccessLog.Enabled = false

		secret, err := generateRandomString(32)
		if err != nil {
			zlog.Fatal().Caller().Err(err).Send()
		}
		cfg.Server.CSRF.Secret = secret

		// set gin mode
		gin.SetMode("release")

		// configure logger
		config.ConfigureLogger(config.LoggerOptions{
			Enabled: true,
			Level:   cfg.Server.Logging.Level,
			Format:  "pretty",
		})

		// create app
		app, err := ginapp.NewGinApp(cfg)
		if err != nil {
			zlog.Fatal().Caller().Err(err).Send()
		}

		// create server
		server := http.Server{
			Addr:         cfg.Server.Addr,
			Handler:      app,
			IdleTimeout:  1 * time.Minute,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 10 * time.Second,
		}

		// create listener
		listener, err := net.Listen("tcp", server.Addr)
		if err != nil {
			log.Fatalf("Failed to start listener: %v", err)
		}

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
		if !skipOpen && !test {
			err = browser.OpenURL(fmt.Sprintf("http://%s:%d/", host, port))
			if err != nil {
				zlog.Fatal().Err(err).Send()
			}
		}

		// wait for termination signal
		if !test {
			<-quit
		}

		zlog.Info().Msg("Starting graceful shutdown...")

		// graceful shutdown with 30 second deadline
		// TODO: make timeout configurable
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// attempt graceful shutdown
		done := make(chan struct{})
		go func() {
			if err := server.Shutdown(ctx); err != nil {
				zlog.Error().Err(err).Send()
			}
			close(done)
		}()

		// shutdown app
		// TODO: handle long-lived requests shutdown (e.g. websockets)
		app.Shutdown()

		select {
		case <-done:
			zlog.Info().Msg("Completed graceful shutdown")
		case <-ctx.Done():
			zlog.Info().Msg("Exceeded deadline, exiting now")
		}
	},
}

func serveRemote(localPort int, skipOpen bool) {
	// listen for termination signals
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer close(quit)

	tunnel, err := tunnel.NewTunnel("kubetail-system", "kubetail-server", 7500, localPort)
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
	<-quit

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
	//flagset.Bool("remote", false, "Open tunnel to remote dashboard")
	flagset.Bool("test", false, "Run internal tests and exit")
}
