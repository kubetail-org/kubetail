# **rgkl - RipGrep for Kubernetes Logs**

Grep-like tool for Kubernetes log files, written in Rust.

---

## 📘 Introduction

`rgkl` is a fast, **structured**, and **scriptable** grep tool designed specifically for **Kubernetes logs**, particularly those produced by **CRI** and **Docker** runtimes.

It supports **time-bounded searches**, **regex matching**, and **live log streaming** — all with efficient, zero-allocation processing under the hood.

In the context of **Kubetail**, rgkl is used by the **Kubetail Cluster Agent** ([modules/cluster-agent](modules/cluster-agent)) to fulfill log search and streaming requests from users.

---
## 🚀 Quickstart

Run `rgkl` in forward streaming mode to search for `ERROR` messages between timestamps `2024-10-01T05:40:50Z` and `2024-10-01T05:41:00Z`, in `pod.log`:
```
cargo run -- stream-forward ./pod.log \
    --start-time "2024-10-01T05:40:50Z" \
    --stop-time "2024-10-01T05:41:00Z" \
    --grep "ERROR"
```
Or follow logs live (like `tail -f`):
```
cargo run -- stream-forward ./pod.log \
    --grep "panic" --follow-from end
```

For reverse log inspection (useful when looking for most recent occurrence):
```
cargo run -- stream-backward ./pod.log --grep "timeout"
```

## 🧰 Command-Line Interface

```bash
rgkl [SUBCOMMAND] [OPTIONS]
```

### Subcommands:

#### `stream-forward`

Reads and optionally follows a log file forward in time.

* `--start-time <ISO8601>`: Start reading from the first entry after this timestamp.
* `--stop-time <ISO8601>`: Stop reading at the last entry before this timestamp.
* `--grep/-g <REGEX>`: Regex filter for matching lines.
* `--follow-from <noop|default|end>`:

  * `noop`: No follow; exit after processing the range.
  * `default`: Follow from `start-time`, or beginning if not set.
  * `end`: Follow new lines from the end of the file (like `tail -f`).

#### `stream-backward`

Searches the file in reverse (most recent lines first), useful for finding recent errors quickly.

* Same options as `stream-forward`, except no `follow-from`.

#### `z`

Minimalist regex matcher using `ripgrep` internals. Does not care about format or time range.

* `--query/-q <REGEX>`: Regex to search for.
* `file`: Path to the log file.

---

## 🔧 Development

### Run the CLI

Build and run the CLI with any subcommand and options:

```bash
cargo run -- <subcommand> [options]
```

Example:

```bash
cargo run -- stream-forward ./pod.log --grep "panic" --follow-from end
```

### Run Tests

Run all unit and integration tests:

```bash
cargo test
```

Test coverage includes:

* Time filtering (`start-time`, `stop-time`)
* Regex matching
* Live file watching and follow mode
* Output validation (newline-delimited JSON structure)

### Build Release Binary

Compile an optimized binary for distribution:

```bash
cargo build --release
```

The binary will be located at `target/release/rgkl`.

## 📦 Dependencies

This project depends on the following external tools:
- protoc – the Protocol Buffers compiler. This is to share a common `LogRecords` data type across different components of Kubetail.

---

## 📚 Usage Examples

### View logs for a 10-second window

```bash
rgkl stream-forward /var/log/pods/my-pod.log \
  --start-time "2024-10-01T05:40:50Z" \
  --stop-time "2024-10-01T05:41:00Z"
```

### Search for "panic" and follow new logs in real time

```bash
rgkl stream-forward /var/log/pods/my-pod.log \
  --grep "panic" \
  --follow-from end
```

### Inspect recent errors in reverse (tail-first)

```bash
rgkl stream-backward /var/log/pods/my-pod.log \
  --grep "ERROR"
```

### Raw regex search (no time or format awareness)

```bash
rgkl z /var/log/pods/my-pod.log --query "timeout exceeded"
```