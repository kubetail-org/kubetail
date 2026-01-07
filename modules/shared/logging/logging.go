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

package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
)

// LoggerOptions controls global logger configuration.
type LoggerOptions struct {
	Enabled bool
	Level   string
	Format  string
}

var configureLoggerOnce sync.Once

func ConfigureLogger(opts LoggerOptions) {
	// ensure this will only be called once
	configureLoggerOnce.Do(func() {
		if !opts.Enabled {
			zlog.Logger = zerolog.Nop()
			log.SetOutput(io.Discard)
			return
		}

		// global settings
		zerolog.TimestampFunc = func() time.Time {
			return time.Now().UTC()
		}
		zerolog.TimeFieldFormat = time.RFC3339Nano
		zerolog.DurationFieldUnit = time.Millisecond

		// set log level
		level, err := zerolog.ParseLevel(opts.Level)
		if err != nil {
			panic(err)
		}
		zerolog.SetGlobalLevel(level)

		// configure output format
		switch opts.Format {
		case "pretty":
			zlog.Logger = zlog.Logger.Output(zerolog.ConsoleWriter{
				Out:        os.Stdout,
				TimeFormat: time.RFC3339Nano,
			})
		case "cli":
			zlog.Logger = zlog.Logger.Output(zerolog.ConsoleWriter{
				Out:     os.Stderr,
				NoColor: false,
				FormatTimestamp: func(i interface{}) string {
					return ""
				},
				FormatLevel: func(i interface{}) string {
					if i == nil {
						return ""
					}
					switch i.(string) {
					case "fatal", "error":
						return "\033[31mError:\033[0m "
					case "warn":
						return "\033[33mWarn:\033[0m "
					default:
						return ""
					}
				},
				FormatCaller: func(i interface{}) string {
					return ""
				},
				FormatMessage: func(i interface{}) string {
					if i == nil {
						return ""
					}
					return fmt.Sprintf("%s", i)
				},
				FormatFieldName: func(i interface{}) string {
					return ""
				},
				FormatFieldValue: func(i interface{}) string {
					return ""
				},
				FormatErrFieldName: func(i interface{}) string {
					return ""
				},
				FormatErrFieldValue: func(i interface{}) string {
					if i == nil {
						return ""
					}
					return fmt.Sprintf("%s", i)
				},
			})
		}
	})
}
