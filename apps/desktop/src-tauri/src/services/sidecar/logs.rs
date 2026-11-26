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

//! What the host does with the sidecar's pipes.
//!
//! The sidecar writes its own `sidecar.log`, so the pipes are not the log any
//! more. What arrives here is re-emitted as an event of the host's own, at the
//! level the record names: the host then stamps, levels, filters and renders
//! both processes the same way, and a terminal orders them by one clock rather
//! than racing two.
//!
//! Where the event goes is the file layer's decision, not this module's
//! (`logging::is_host_log`): a debug build's pipes carry the whole log that
//! `sidecar.log` already holds, and only a release build's panics reach
//! `main.log`.

use crate::logging::{sanitize_text, SIDECAR_TARGET};

/// Escaped bytes one sidecar line may occupy.
const MAX_MESSAGE_BYTES: usize = 16 * 1024;

/// One record as the sidecar's logger wrote it.
struct Record {
    level: String,
    /// The record's message with its own fields folded in as `key=value`.
    /// `tracing` takes only static field names, so they cannot be re-emitted as
    /// fields; the values keep their JSON form, which quotes strings the way
    /// `tracing` does anyway.
    message: String,
}

/// Parses one record the sidecar's logger wrote.
///
/// `None` for anything else, which is not an error: a Go panic and the
/// sidecar's own report that it could not open its file reach the pipes
/// precisely because its logger was not the thing that wrote them.
fn parse_record(text: &str) -> Option<Record> {
    let serde_json::Value::Object(mut obj) = serde_json::from_str(text).ok()? else {
        return None;
    };
    // Required, then dropped: the host stamps the line it writes, so both
    // processes reach a terminal on one clock.
    take_string(&mut obj, "timestamp")?;
    let level = take_string(&mut obj, "level")?;
    let mut message = take_string(&mut obj, "message")?;

    for (key, value) in &obj {
        message.push(' ');
        message.push_str(key);
        message.push('=');
        message.push_str(&value.to_string());
    }
    Some(Record { level, message })
}

fn take_string(obj: &mut serde_json::Map<String, serde_json::Value>, key: &str) -> Option<String> {
    match obj.remove(key)? {
        serde_json::Value::String(s) => Some(s),
        _ => None,
    }
}

/// Re-emits one line from the sidecar's stdout or stderr. Blank lines are
/// dropped: they are the pipe's framing, not records.
pub(super) fn forward_sidecar_line(raw: &[u8]) {
    let text = String::from_utf8_lossy(raw);
    let text = text.trim_end();
    if text.is_empty() {
        return;
    }

    match parse_record(text) {
        Some(record) => emit(&record.level, &record.message),
        // Not a record, so nothing says how serious it is. A Go panic and a
        // logger that could not open its file are both worth a warning.
        None => emit("WARN", text),
    }
}

/// Emits one forwarded line at the level the sidecar named.
fn emit(level: &str, message: &str) {
    let message = sanitize_text(message, MAX_MESSAGE_BYTES);

    // `tracing` fixes the level at compile time, so dispatch per arm.
    macro_rules! emit {
        ($level:ident) => {
            tracing::$level!(target: SIDECAR_TARGET, "{message}")
        };
    }
    match level {
        "TRACE" => emit!(trace),
        "DEBUG" => emit!(debug),
        "INFO" => emit!(info),
        "ERROR" => emit!(error),
        // WARN, and anything else the sidecar's logger comes to write.
        _ => emit!(warn),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::logging::captured;

    const STARTING: &str = r#"{"timestamp":"2026-09-08T10:28:29.677488Z","level":"INFO","message":"sidecar starting","pid":95679,"socket":"/tmp/x.sock"}"#;

    /// The point of parsing: a sidecar record reaches a developer's terminal as
    /// an event of the host's own, carrying the level the sidecar chose.
    #[test]
    fn a_record_becomes_an_event_at_the_level_it_names() {
        let out = captured(|| {
            forward_sidecar_line(STARTING.as_bytes());
            forward_sidecar_line(
                br#"{"timestamp":"2026-09-08T10:28:30.000000Z","level":"ERROR","message":"boom"}"#,
            );
        });

        let mut lines = out.lines();
        let info = lines.next().expect("a line");
        assert!(info.contains("INFO"), "got: {info}");
        assert!(
            info.contains(r#"sidecar starting pid=95679 socket="/tmp/x.sock""#),
            "the record's fields did not reach the message: {info}"
        );
        assert!(
            lines.next().expect("a line").contains("ERROR"),
            "got: {out}"
        );
    }

    /// The host stamps what it writes, so a terminal orders both processes by
    /// one clock — and a record must not bring a second timestamp along.
    #[test]
    fn a_record_brings_no_timestamp_of_its_own() {
        let out = captured(|| forward_sidecar_line(STARTING.as_bytes()));
        assert!(!out.contains("2026-09-08T10:28:29"), "got: {out}");
    }

    /// A Go panic is not JSON, and it is the one sidecar line a user cannot get
    /// any other way — so anything that is not a record has to pass through
    /// rather than be dropped as unparseable.
    #[test]
    fn a_line_that_is_not_a_record_is_not_parsed_as_one() {
        assert!(parse_record("panic: runtime error").is_none());
        assert!(parse_record("[1,2,3]").is_none());
        assert!(
            parse_record(r#"{"level":"INFO","message":"no timestamp"}"#).is_none(),
            "a partial record is not a record"
        );
        assert!(
            parse_record(r#"{"timestamp":"t","level":3,"message":"m"}"#).is_none(),
            "a level must be a string"
        );
    }

    /// Nothing says how serious such a line is, and neither of the two that
    /// reach us is routine.
    #[test]
    fn a_line_that_is_not_a_record_is_forwarded_at_warn() {
        let out = captured(|| forward_sidecar_line(b"panic: runtime error"));
        assert!(out.contains("WARN"), "got: {out}");
        assert!(out.contains("panic: runtime error"), "got: {out}");
    }

    /// The message is cluster-controlled text that arrives JSON-decoded, so its
    /// control characters are real again by the time the line is built.
    #[test]
    fn a_forwarded_message_stays_on_one_line() {
        let out = captured(|| {
            forward_sidecar_line(
                br#"{"timestamp":"2026-09-08T10:28:29.677488Z","level":"INFO","message":"first\nsecond\u001b[31m"}"#,
            );
        });

        assert_eq!(out.lines().count(), 1, "a newline forged a line: {out:?}");
        assert!(!out.contains('\u{1b}'), "an escape byte survived: {out:?}");
        assert!(out.contains("first\\nsecond"), "got: {out:?}");
    }

    #[test]
    fn blank_lines_are_dropped() {
        // An empty message would otherwise become an empty log line.
        let out = captured(|| {
            forward_sidecar_line(b"");
            forward_sidecar_line(b"   \n");
        });
        assert!(out.is_empty(), "got: {out:?}");
    }

    #[test]
    fn invalid_utf8_does_not_panic() {
        // 0xFF is never valid UTF-8; lossy decoding must carry it through.
        let out = captured(|| forward_sidecar_line(&[0xff, b'o', b'k']));
        assert!(out.contains("ok"), "got: {out}");
    }
}
