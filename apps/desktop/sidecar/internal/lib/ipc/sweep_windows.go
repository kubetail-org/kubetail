//go:build windows

package ipc

// SweepStale has nothing to sweep: a named pipe is gone with its listener.
func SweepStale(string, string) {}
