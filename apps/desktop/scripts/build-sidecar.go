//go:build ignore

// Builds the Go sidecar and places it in src-tauri/binaries/ with the
// Tauri-required `<basename>-<target-triple>` filename so externalBin can pick
// it up. Run from the repo root:
//
//	go run scripts/build-sidecar.go              # release build, untagged
//	go run scripts/build-sidecar.go -tags debug  # `make build-sidecar-dev`
//
// It is Go rather than shell because `tauri dev` runs it through cmd.exe on
// Windows, where there is no POSIX shell and `bash` is WSL's.
//
// KUBETAIL_HOST_TRIPLE names the Rust host triple, so CI can skip installing
// Rust; SIDECAR_VERSION, when set, is stamped into internal/lib/version.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const basename = "kubetail-sidecar"

func main() {
	// Empty by default, so the release path builds untagged and its endpoints
	// can only come from the host's arguments.
	tags := flag.String("tags", "", "Go build tags")
	flag.Parse()

	if err := build(*tags); err != nil {
		fmt.Fprintln(os.Stderr, "build-sidecar:", err)
		os.Exit(1)
	}
}

func build(tags string) error {
	if _, err := os.Stat(filepath.Join("sidecar", "go.mod")); err != nil {
		return fmt.Errorf("run from the repo root: %w", err)
	}

	triple, err := hostTriple()
	if err != nil {
		return err
	}
	ext := ""
	if strings.Contains(triple, "windows") {
		ext = ".exe"
	}

	outDir, err := filepath.Abs(filepath.Join("src-tauri", "binaries"))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	out := filepath.Join(outDir, basename+"-"+triple+ext)

	ldflags := "-s -w"
	if v := os.Getenv("SIDECAR_VERSION"); v != "" {
		ldflags += " -X github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/version.Version=" + v
	}

	fmt.Println("→ building", out)
	cmd := exec.Command("go", "build", "-trimpath", "-tags", tags, "-ldflags", ldflags, "-o", out, ".")
	cmd.Dir = "sidecar"
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	fmt.Println("✓", out)
	return nil
}

// Tauri names the binary by the *Rust* host triple, not the Go one.
func hostTriple() (string, error) {
	if t := os.Getenv("KUBETAIL_HOST_TRIPLE"); t != "" {
		return t, nil
	}
	// From src-tauri/ so rustup applies rust-toolchain.toml's pin rather
	// than its default toolchain, which may not be set.
	cmd := exec.Command("rustc", "-vV")
	cmd.Dir = "src-tauri"
	b, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("rustc -vV: %w", err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if t, ok := strings.CutPrefix(line, "host: "); ok {
			return strings.TrimSpace(t), nil
		}
	}
	return "", fmt.Errorf("rustc -vV printed no host line")
}
