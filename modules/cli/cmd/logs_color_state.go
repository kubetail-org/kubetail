package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kubetail-org/kubetail/modules/shared/logs"
)

const (
	dotColorStateSchemaVersion = 1
	dotColorStateTTL           = 30 * 24 * time.Hour
	dotColorStateLockTimeout   = 2 * time.Second
	dotColorStateLockRetry     = 20 * time.Millisecond
)

var dotANSIColors = []string{
	"31m", // red
	"32m", // green
	"33m", // yellow
	"34m", // blue
	"35m", // magenta
	"36m", // cyan
	"91m", // bright red
	"92m", // bright green
	"93m", // bright yellow
	"94m", // bright blue
	"95m", // bright magenta
	"96m", // bright cyan
	"37m", // white
	"90m", // gray
	"97m", // bright white
}

type dotColorStateEntry struct {
	ColorIndex int       `json:"colorIndex"`
	LastSeenAt time.Time `json:"lastSeenAt"`
}

type dotColorStateFile struct {
	SchemaVersion int                           `json:"schemaVersion"`
	NextColor     int                           `json:"nextColor"`
	Entries       map[string]dotColorStateEntry `json:"entries"`
}

type dotColorAssigner struct {
	statePath string
	lockPath  string
	nextColor int
	entries   map[string]dotColorStateEntry
	seenInRun map[string]struct{}
	now       func() time.Time
}

func newDotColorAssigner() (*dotColorAssigner, error) {
	statePath, err := defaultDotColorStatePath()
	if err != nil {
		return nil, err
	}

	return newDotColorAssignerAtPath(statePath)
}

func newDotColorAssignerAtPath(statePath string) (*dotColorAssigner, error) {
	if statePath == "" {
		return nil, fmt.Errorf("state path is empty")
	}

	a := &dotColorAssigner{
		statePath: statePath,
		lockPath:  statePath + ".lock",
		entries:   map[string]dotColorStateEntry{},
		seenInRun: map[string]struct{}{},
		now:       time.Now,
	}

	state, err := a.readStateFile()
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		return a, nil
	}

	a.nextColor = positiveModulo(state.NextColor, len(dotANSIColors))
	for k, v := range state.Entries {
		if v.ColorIndex < 0 || v.ColorIndex >= len(dotANSIColors) {
			continue
		}
		a.entries[k] = v
	}

	return a, nil
}

func defaultDotColorStatePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}

	return filepath.Join(home, ".kubetail", "state", "log-dot-colors.v1.json"), nil
}

func (a *dotColorAssigner) Dot(source logs.LogSource) string {
	key := dotColorSourceKey(source)
	if key == "" {
		return ansiDot(dotColorFromHash(source.ContainerID))
	}

	entry, ok := a.entries[key]
	if !ok {
		entry = dotColorStateEntry{
			ColorIndex: a.nextColor,
		}
		a.nextColor = (a.nextColor + 1) % len(dotANSIColors)
	}
	entry.LastSeenAt = a.now().UTC()

	a.entries[key] = entry
	a.seenInRun[key] = struct{}{}

	return ansiDot(entry.ColorIndex)
}

func (a *dotColorAssigner) Close() error {
	if len(a.seenInRun) == 0 {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(a.statePath), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	unlock, err := a.acquireLock(dotColorStateLockTimeout)
	if err != nil {
		return err
	}
	defer unlock()

	state, err := a.readStateFile()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if state == nil {
		state = &dotColorStateFile{
			SchemaVersion: dotColorStateSchemaVersion,
			Entries:       map[string]dotColorStateEntry{},
		}
	}

	if state.SchemaVersion != dotColorStateSchemaVersion {
		state = &dotColorStateFile{
			SchemaVersion: dotColorStateSchemaVersion,
			Entries:       map[string]dotColorStateEntry{},
		}
	}

	state.NextColor = positiveModulo(state.NextColor, len(dotANSIColors))

	keys := make([]string, 0, len(a.seenInRun))
	for key := range a.seenInRun {
		keys = append(keys, key)
	}

	for _, key := range keys {
		local := a.entries[key]
		if existing, ok := state.Entries[key]; ok {
			existing.LastSeenAt = maxTime(existing.LastSeenAt, local.LastSeenAt)
			state.Entries[key] = existing
			continue
		}

		state.Entries[key] = dotColorStateEntry{
			ColorIndex: local.ColorIndex,
			LastSeenAt: local.LastSeenAt,
		}
		state.NextColor = (state.NextColor + 1) % len(dotANSIColors)
	}

	now := a.now().UTC()
	for key, entry := range state.Entries {
		if now.Sub(entry.LastSeenAt) > dotColorStateTTL {
			delete(state.Entries, key)
		}
	}

	state.SchemaVersion = dotColorStateSchemaVersion

	if err := writeJSONAtomic(a.statePath, state); err != nil {
		return err
	}

	return nil
}

func (a *dotColorAssigner) readStateFile() (*dotColorStateFile, error) {
	b, err := os.ReadFile(a.statePath)
	if err != nil {
		return nil, err
	}

	var state dotColorStateFile
	if err := json.Unmarshal(b, &state); err != nil {
		return nil, fmt.Errorf("parse dot color state: %w", err)
	}

	if state.Entries == nil {
		state.Entries = map[string]dotColorStateEntry{}
	}

	return &state, nil
}

func (a *dotColorAssigner) acquireLock(timeout time.Duration) (func(), error) {
	deadline := time.Now().Add(timeout)

	for {
		err := os.Mkdir(a.lockPath, 0o700)
		if err == nil {
			return func() {
				_ = os.Remove(a.lockPath)
			}, nil
		}

		if !os.IsExist(err) {
			return nil, fmt.Errorf("acquire lock: %w", err)
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("acquire lock: timeout")
		}

		time.Sleep(dotColorStateLockRetry)
	}
}

func dotColorSourceKey(source logs.LogSource) string {
	if source.ContainerID != "" {
		return "container-id:" + source.ContainerID
	}

	parts := []string{source.Namespace, source.PodName, source.ContainerName}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}

	key := strings.Join(parts, "/")
	if key == "//" {
		return ""
	}

	return "source:" + key
}

func ansiDot(colorIndex int) string {
	colorIndex = positiveModulo(colorIndex, len(dotANSIColors))
	return fmt.Sprintf("\033[%s%s\033[0m", dotANSIColors[colorIndex], "\u25CF")
}

func dotColorFromHash(containerID string) int {
	hash := 5381
	for _, val := range containerID {
		hash = int(val) + ((hash << 5) + hash)
	}

	return positiveModulo(hash, len(dotANSIColors))
}

func positiveModulo(n, mod int) int {
	if mod <= 0 {
		return 0
	}

	n = n % mod
	if n < 0 {
		return n + mod
	}

	return n
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func writeJSONAtomic(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	b = append(b, '\n')

	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, "log-dot-colors-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(b); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write temp state: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("sync temp state: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp state: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp state: %w", err)
	}

	return nil
}
