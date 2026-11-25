package main

import "os"

// scrubExecEnvironment hands back the arguments and the process environment as
// they are: Windows has no exec and nothing that reads another process's launch
// environment. takeProviderKeys still clears the keys from the live copy.
func scrubExecEnvironment() ([]string, func(string) string, error) {
	return os.Args, os.Getenv, nil
}
