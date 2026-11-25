package main

import (
	"io"
	"os"
)

func openTerminal() (io.ReadCloser, error) { return os.Open("CONIN$") }
