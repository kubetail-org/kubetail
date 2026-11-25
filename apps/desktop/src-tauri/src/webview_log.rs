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

//! Webview error records, re-emitted as host `tracing` events under
//! [`WEBVIEW_TARGET`], which is what routes them to `webview.log`.
//!
//! The webview's own structured text never becomes a host field: it arrives as
//! one JSON value under `reported`, the record's only page-controlled key. The
//! host's own reading of the record is unprefixed — `window` names the emitting
//! window and `source` the validated source, so lines can be filtered on.
//!
//! The host bounds what it writes, not what it receives — the request is
//! parsed before the command body runs — so every string is capped here, and
//! a per-webview [`Limiter`] bounds how fast records reach the file.

use std::collections::HashMap;
use std::sync::Mutex;
use std::time::{Duration, Instant};

use serde::Deserialize;

use crate::logging::{sanitize_text, WEBVIEW_TARGET};

/// Escaped bytes a `message` may occupy.
pub const MAX_MESSAGE_BYTES: usize = 2 * 1024;
/// Escaped bytes a `stack` may occupy.
pub const MAX_STACK_BYTES: usize = 8 * 1024;
/// Escaped bytes a `componentStack` may occupy.
pub const MAX_COMPONENT_STACK_BYTES: usize = 8 * 1024;
/// Serialized bytes a `context` may occupy; past it the whole value becomes
/// `{"truncated": true}`, since JSON cut mid-string is not JSON.
pub const MAX_CONTEXT_BYTES: usize = 4 * 1024;
/// Serialized bytes the whole `reported` value may occupy; past it nothing of
/// it survives but the note that it was cut.
pub const MAX_REPORTED_BYTES: usize = 24 * 1024;

/// One error record as the webview sends it (`src/lib/error-log.ts`).
#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct WebviewRecord {
    pub source: String,
    pub message: String,
    #[serde(default)]
    pub stack: Option<String>,
    #[serde(default)]
    pub component_stack: Option<String>,
    #[serde(default)]
    pub context: Option<serde_json::Value>,
}

/// The bus's `AppErrorSource`, plus `Unknown` for a value the host does not
/// recognize — most likely a newer frontend, so it is placed rather than
/// dropped.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub enum Source {
    Graphql,
    Subscription,
    Network,
    Render,
    Auth,
    Unknown,
}

/// Severity a record is written at. Derived from the source: the caller
/// reports errors and has no level to choose.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Severity {
    Error,
    Warn,
}

impl Source {
    fn parse(raw: &str) -> Self {
        match raw {
            "graphql" => Self::Graphql,
            "subscription" => Self::Subscription,
            "network" => Self::Network,
            "render" => Self::Render,
            "auth" => Self::Auth,
            _ => Self::Unknown,
        }
    }

    pub fn as_str(self) -> &'static str {
        match self {
            Self::Graphql => "graphql",
            Self::Subscription => "subscription",
            Self::Network => "network",
            Self::Render => "render",
            Self::Auth => "auth",
            Self::Unknown => "unknown",
        }
    }

    /// `render` and `auth` are faults; the rest are transient and retried.
    /// An unknown source is not a confirmed fault, so it reads as `warn`.
    pub fn severity(self) -> Severity {
        match self {
            Self::Render | Self::Auth => Severity::Error,
            Self::Graphql | Self::Subscription | Self::Network | Self::Unknown => Severity::Warn,
        }
    }
}

/// Source plus the first line of the message: the key repeats are suppressed by.
type Signature = (Source, String);

/// A record after validation and bounding: what the host is willing to write.
#[derive(Debug)]
pub struct Normalized {
    pub source: Source,
    pub message: String,
    /// The `reported` value, serialized by the host.
    pub reported: String,
}

impl Normalized {
    /// What the limiter charges: the bytes this record puts on the line.
    pub fn bytes(&self) -> usize {
        self.message.len() + self.reported.len()
    }

    fn signature(&self) -> Signature {
        let first_line = self.message.split("\\n").next().unwrap_or_default();
        (self.source, first_line.to_string())
    }
}

/// Validates the source, sanitizes and caps every string, and serializes what
/// the page reported. Truncation happens before serializing, never after.
pub fn normalize(record: WebviewRecord) -> Normalized {
    let source = Source::parse(&record.source);
    let message = sanitize_text(&record.message, MAX_MESSAGE_BYTES);

    let mut reported = serde_json::Map::new();
    if let Some(stack) = record.stack {
        reported.insert(
            "stack".into(),
            sanitize_text(&stack, MAX_STACK_BYTES).into(),
        );
    }
    if let Some(component_stack) = record.component_stack {
        reported.insert(
            "componentStack".into(),
            sanitize_text(&component_stack, MAX_COMPONENT_STACK_BYTES).into(),
        );
    }
    if let Some(context) = record.context {
        let within =
            serde_json::to_string(&context).is_ok_and(|json| json.len() <= MAX_CONTEXT_BYTES);
        reported.insert(
            "context".into(),
            if within {
                context
            } else {
                serde_json::json!({ "truncated": true })
            },
        );
    }

    let mut serialized = serde_json::to_string(&reported).unwrap_or_default();
    if serialized.len() > MAX_REPORTED_BYTES {
        serialized = serde_json::json!({ "truncated": true }).to_string();
    }

    Normalized {
        source,
        message,
        reported: serialized,
    }
}

/// Writes one record as a `tracing` event. `suppressed` is how many repeats of
/// its signature were withheld since the last time it was written.
pub fn emit(label: &str, record: &Normalized, suppressed: u64) {
    // `tracing` fixes the level at compile time, so dispatch per arm.
    macro_rules! emit {
        ($level:ident) => {
            if suppressed > 0 {
                tracing::$level!(
                    target: WEBVIEW_TARGET,
                    { window = label, source = record.source.as_str(), suppressed, reported = %record.reported },
                    "{}", record.message
                );
            } else {
                tracing::$level!(
                    target: WEBVIEW_TARGET,
                    { window = label, source = record.source.as_str(), reported = %record.reported },
                    "{}", record.message
                );
            }
        };
    }
    match record.source.severity() {
        Severity::Error => emit!(error),
        Severity::Warn => emit!(warn),
    }
}

/// Names records the limiter withheld from `label`, so a drop is counted
/// rather than lost.
fn summarize(label: &str, dropped: u64) {
    tracing::warn!(
        target: WEBVIEW_TARGET,
        { window = label, dropped },
        "webview records dropped"
    );
}

/// Marks a page load, so two crashes across a reload do not read as one. At
/// `info`: the default filter is `info`, and a `debug` marker would be absent
/// from every log a user sends.
pub fn page_loaded(label: &str) {
    tracing::info!(target: WEBVIEW_TARGET, { window = label }, "page loaded");
}

// ---------------------------------------------------------------------------
// Rate limiting
// ---------------------------------------------------------------------------

/// The pace at which one webview's records reach the file. Production passes
/// [`LimiterConfig::DEFAULT`]; tests shrink it.
#[derive(Clone, Copy, Debug)]
pub struct LimiterConfig {
    /// Records accepted in a burst.
    pub burst: u32,
    /// Records regained per second.
    pub refill_per_sec: f64,
    /// Bytes accepted in a burst.
    pub byte_burst: usize,
    /// Bytes regained per second.
    pub byte_refill_per_sec: f64,
    /// Records of the burst only `error` severity may draw on.
    pub error_reserve: u32,
    /// Bytes of the burst only `error` severity may draw on — without it,
    /// large warns drain the bytes and the record reserve holds slots a
    /// crash record cannot pay for.
    pub error_byte_reserve: usize,
    /// How long a signature is suppressed after it is written.
    pub signature_window: Duration,
    /// Signatures remembered per webview; the least-recently-seen is evicted.
    pub max_signatures: usize,
}

impl LimiterConfig {
    pub const DEFAULT: Self = Self {
        burst: 30,
        refill_per_sec: 1.0,
        byte_burst: 256 * 1024,
        byte_refill_per_sec: 8.0 * 1024.0,
        error_reserve: 10,
        error_byte_reserve: 64 * 1024,
        signature_window: Duration::from_secs(60),
        max_signatures: 64,
    };
}

#[derive(Debug)]
struct SignatureState {
    written_at: Instant,
    seen_at: Instant,
    suppressed: u64,
}

#[derive(Debug)]
struct Bucket {
    tokens: f64,
    bytes: f64,
    refilled_at: Instant,
    /// Records withheld since the last summary: budget drops plus the repeats
    /// of signatures evicted before they could report.
    dropped: u64,
    signatures: HashMap<Signature, SignatureState>,
}

impl Bucket {
    fn full(config: &LimiterConfig, now: Instant) -> Self {
        Self {
            tokens: f64::from(config.burst),
            bytes: config.byte_burst as f64,
            refilled_at: now,
            dropped: 0,
            signatures: HashMap::new(),
        }
    }

    fn refill(&mut self, config: &LimiterConfig, now: Instant) {
        let elapsed = now
            .saturating_duration_since(self.refilled_at)
            .as_secs_f64();
        self.tokens = (self.tokens + elapsed * config.refill_per_sec).min(f64::from(config.burst));
        self.bytes =
            (self.bytes + elapsed * config.byte_refill_per_sec).min(config.byte_burst as f64);
        self.refilled_at = now;
    }

    /// Everything this bucket withheld and has not yet reported.
    fn unreported(&self) -> u64 {
        self.dropped + self.signatures.values().map(|s| s.suppressed).sum::<u64>()
    }

    /// Makes room for one more signature, folding an evicted one's repeats
    /// into `dropped` so they are still reported.
    fn evict_stalest_signature(&mut self, config: &LimiterConfig) {
        if self.signatures.len() < config.max_signatures {
            return;
        }
        let stalest = self
            .signatures
            .iter()
            .min_by_key(|(_, state)| state.seen_at)
            .map(|(key, _)| key.clone());
        if let Some(state) = stalest.and_then(|key| self.signatures.remove(&key)) {
            self.dropped += state.suppressed;
        }
    }
}

/// What the limiter decided for one record.
#[derive(Debug, PartialEq, Eq)]
pub enum Verdict {
    /// Write it. `dropped` records were withheld since the last one written
    /// and want a summary first; `suppressed` repeats of this signature ride
    /// on the record itself.
    Accept {
        dropped: u64,
        suppressed: u64,
    },
    Drop,
}

/// Per-webview token buckets and signature suppression. Time is passed in,
/// so the tests never wait on a clock.
#[derive(Debug)]
pub struct Limiter {
    config: LimiterConfig,
    buckets: HashMap<String, Bucket>,
}

impl Limiter {
    pub fn new(config: LimiterConfig) -> Self {
        Self {
            config,
            buckets: HashMap::new(),
        }
    }

    pub fn admit(&mut self, label: &str, record: &Normalized, now: Instant) -> Verdict {
        let config = self.config;
        let bucket = self
            .buckets
            .entry(label.to_string())
            .or_insert_with(|| Bucket::full(&config, now));
        bucket.refill(&config, now);

        // A repeat inside its window costs nothing and is withheld; its next
        // occurrence outside the window carries the count.
        let signature = record.signature();
        if let Some(state) = bucket.signatures.get_mut(&signature) {
            state.seen_at = now;
            if now.saturating_duration_since(state.written_at) < config.signature_window {
                state.suppressed += 1;
                return Verdict::Drop;
            }
        }

        // `warn` may not draw the reserves down; `error` may.
        let (token_floor, byte_floor) = match record.source.severity() {
            Severity::Error => (0.0, 0.0),
            Severity::Warn => (
                f64::from(config.error_reserve),
                config.error_byte_reserve as f64,
            ),
        };
        let cost = record.bytes() as f64;
        if bucket.tokens - 1.0 < token_floor || bucket.bytes - cost < byte_floor {
            bucket.dropped += 1;
            return Verdict::Drop;
        }
        bucket.tokens -= 1.0;
        bucket.bytes -= cost;

        let suppressed = match bucket.signatures.get_mut(&signature) {
            Some(state) => {
                state.written_at = now;
                std::mem::take(&mut state.suppressed)
            }
            None => {
                bucket.evict_stalest_signature(&config);
                bucket.signatures.insert(
                    signature,
                    SignatureState {
                        written_at: now,
                        seen_at: now,
                        suppressed: 0,
                    },
                );
                0
            }
        };
        let dropped = std::mem::take(&mut bucket.dropped);
        Verdict::Accept {
            dropped,
            suppressed,
        }
    }

    /// Forgets a webview, returning what it withheld and never reported.
    pub fn evict(&mut self, label: &str) -> u64 {
        self.buckets
            .remove(label)
            .map(|bucket| bucket.unreported())
            .unwrap_or_default()
    }

    /// Removes every entry whose webview is gone — a record dispatched before
    /// `Destroyed` can run after it and recreate one. Returns the unreported
    /// count of each, for a summary.
    pub fn sweep(&mut self, is_live: impl Fn(&str) -> bool) -> Vec<(String, u64)> {
        let dead: Vec<String> = self
            .buckets
            .keys()
            .filter(|label| !is_live(label))
            .cloned()
            .collect();
        dead.into_iter()
            .map(|label| {
                let unreported = self.evict(&label);
                (label, unreported)
            })
            .collect()
    }

    #[cfg(test)]
    fn has(&self, label: &str) -> bool {
        self.buckets.contains_key(label)
    }
}

/// The command's body: everything after the label is read off the `Webview`.
/// A record for a label that no longer resolves is dropped — `main` is
/// rebuilt under its own name, so a tombstone would reject the new window's.
pub fn record_from(
    limiter: &Mutex<Limiter>,
    label: &str,
    record: WebviewRecord,
    now: Instant,
    is_live: impl Fn(&str) -> bool,
) {
    if !is_live(label) {
        return;
    }
    let normalized = normalize(record);
    let (swept, verdict) = {
        let mut limiter = limiter.lock().unwrap_or_else(|p| p.into_inner());
        let swept = limiter.sweep(&is_live);
        (swept, limiter.admit(label, &normalized, now))
    };
    for (label, unreported) in swept {
        if unreported > 0 {
            summarize(&label, unreported);
        }
    }
    if let Verdict::Accept {
        dropped,
        suppressed,
    } = verdict
    {
        if dropped > 0 {
            summarize(label, dropped);
        }
        emit(label, &normalized, suppressed);
    }
}

/// The `Destroyed` arm's body: forgets the webview and reports what it
/// withheld.
pub fn destroyed(limiter: &Mutex<Limiter>, label: &str) {
    let unreported = limiter
        .lock()
        .unwrap_or_else(|p| p.into_inner())
        .evict(label);
    if unreported > 0 {
        summarize(label, unreported);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    use crate::logging::captured;

    fn record(source: &str, message: &str) -> WebviewRecord {
        WebviewRecord {
            source: source.to_string(),
            message: message.to_string(),
            stack: None,
            component_stack: None,
            context: None,
        }
    }

    /// The `reported` value off a captured line, parsed.
    fn reported_of(line: &str) -> serde_json::Value {
        let start = line.find("reported=").expect("reported present") + "reported=".len();
        let json = &line[start..];
        // The fmt layer writes the value verbatim, so the JSON runs to the
        // end of the line.
        serde_json::from_str(json.trim_end()).expect("reported parses as JSON")
    }

    #[test]
    fn a_message_with_newlines_and_escapes_is_one_line_without_escape_bytes() {
        let out = captured(|| {
            emit(
                "main",
                &normalize(record("graphql", "first\nsecond\x1b[31mred")),
                0,
            );
        });
        assert_eq!(out.lines().count(), 1, "got: {out:?}");
        assert!(!out.contains('\u{1b}'), "got: {out:?}");
        assert!(out.contains("first\\nsecond"), "got: {out:?}");
    }

    #[test]
    fn over_long_fields_are_capped_and_the_fields_still_parse() {
        let mut long = record("render", &"m".repeat(MAX_MESSAGE_BYTES * 2));
        long.stack = Some("s".repeat(MAX_STACK_BYTES * 2));
        long.component_stack = Some("c".repeat(MAX_COMPONENT_STACK_BYTES * 2));
        long.context = Some(serde_json::json!({ "blob": "x".repeat(MAX_CONTEXT_BYTES * 2) }));

        let normalized = normalize(long);
        assert!(normalized.message.len() <= MAX_MESSAGE_BYTES);

        let out = captured(|| emit("main", &normalized, 0));
        let reported = reported_of(&out);
        assert!(reported["stack"].as_str().unwrap().len() <= MAX_STACK_BYTES);
        assert!(reported["componentStack"].as_str().unwrap().len() <= MAX_COMPONENT_STACK_BYTES);
        assert_eq!(
            reported["context"],
            serde_json::json!({ "truncated": true })
        );
    }

    #[test]
    fn a_short_context_rides_through_intact() {
        let mut rec = record("subscription", "watch closed");
        rec.context = Some(serde_json::json!({ "dropped": 3 }));
        let out = captured(|| emit("main", &normalize(rec), 0));
        assert_eq!(
            reported_of(&out)["context"],
            serde_json::json!({ "dropped": 3 })
        );
    }

    /// A reader must be able to tell webview-supplied text from a host field,
    /// so all of it rides under the one key.
    #[test]
    fn page_supplied_fields_ride_under_one_key() {
        let mut rec = record("graphql", "x");
        rec.stack = Some("Error: x\n    at y".into());
        rec.component_stack = Some("    in App".into());
        rec.context = Some(serde_json::json!({ "dropped": 1 }));
        let out = captured(|| emit("main", &normalize(rec), 0));
        assert!(out.contains("reported="), "got: {out}");
        for key in [" stack=", " componentStack=", " context="] {
            assert!(!out.contains(key), "{key} escaped the namespace: {out}");
        }
    }

    #[test]
    fn a_record_carries_its_label_and_severity_follows_its_source() {
        let out = captured(|| {
            emit("window-2", &normalize(record("render", "crash")), 0);
            emit("window-3", &normalize(record("network", "dial")), 0);
        });
        let mut lines = out.lines();
        let render = lines.next().unwrap();
        let network = lines.next().unwrap();
        assert!(
            render.contains("ERROR") && render.contains("window=\"window-2\""),
            "got: {render}"
        );
        assert!(
            network.contains("WARN") && network.contains("window=\"window-3\""),
            "got: {network}"
        );
        assert!(render.contains("source=\"render\""), "got: {render}");
    }

    #[test]
    fn an_unrecognized_source_becomes_unknown_at_warn() {
        let out = captured(|| emit("main", &normalize(record("telemetry", "x")), 0));
        assert!(out.contains("WARN"), "got: {out}");
        assert!(out.contains("source=\"unknown\""), "got: {out}");
    }

    #[test]
    fn a_suppressed_count_rides_on_the_record() {
        let out = captured(|| emit("main", &normalize(record("graphql", "x")), 4));
        assert!(out.contains("suppressed=4"), "got: {out}");
    }

    // Limiter ---------------------------------------------------------------

    /// Small enough to exhaust in a loop, with a window the tests step past.
    fn config() -> LimiterConfig {
        LimiterConfig {
            burst: 5,
            refill_per_sec: 1.0,
            byte_burst: 64 * 1024,
            byte_refill_per_sec: 64.0 * 1024.0,
            error_reserve: 2,
            error_byte_reserve: 0,
            signature_window: Duration::from_secs(10),
            max_signatures: 3,
        }
    }

    fn warn_record(n: usize) -> Normalized {
        normalize(record("subscription", &format!("retry {n}")))
    }

    fn error_record(n: usize) -> Normalized {
        normalize(record("render", &format!("crash {n}")))
    }

    #[test]
    fn a_burst_past_the_bound_is_dropped_and_summarized_once_refilled() {
        let mut limiter = Limiter::new(config());
        let t0 = Instant::now();
        // Five tokens, two of them reserved: three warns land.
        for n in 0..3 {
            assert_eq!(
                limiter.admit("main", &warn_record(n), t0),
                Verdict::Accept {
                    dropped: 0,
                    suppressed: 0
                }
            );
        }
        assert_eq!(limiter.admit("main", &warn_record(3), t0), Verdict::Drop);
        assert_eq!(limiter.admit("main", &warn_record(4), t0), Verdict::Drop);

        // One second refills one token; the next accepted record names the two.
        let verdict = limiter.admit("main", &warn_record(5), t0 + Duration::from_secs(1));
        assert_eq!(
            verdict,
            Verdict::Accept {
                dropped: 2,
                suppressed: 0
            }
        );
    }

    #[test]
    fn a_warn_storm_leaves_the_error_reserve() {
        let mut limiter = Limiter::new(config());
        let t0 = Instant::now();
        for n in 0..20 {
            limiter.admit("main", &warn_record(n), t0);
        }
        assert!(matches!(
            limiter.admit("main", &error_record(0), t0),
            Verdict::Accept { .. }
        ));
        assert!(matches!(
            limiter.admit("main", &error_record(1), t0),
            Verdict::Accept { .. }
        ));
        assert_eq!(limiter.admit("main", &error_record(2), t0), Verdict::Drop);
    }

    /// The reserve holds bytes as well as slots: large warns must not drain
    /// the byte budget below what a crash record needs, or an outage's
    /// warnings hide the crash the reserve exists for.
    #[test]
    fn a_warn_storm_leaves_bytes_for_an_error() {
        let mut limiter = Limiter::new(LimiterConfig {
            burst: 10,
            byte_burst: 5000,
            error_byte_reserve: 2500,
            ..config()
        });
        let t0 = Instant::now();
        // Each warn costs ~2 KiB; without a byte floor two of them land and
        // leave less than one crash record's cost.
        let big = "x".repeat(2000);
        for n in 0..5 {
            let warn = normalize(record("subscription", &format!("w{n} {big}")));
            limiter.admit("main", &warn, t0);
        }
        assert!(matches!(
            limiter.admit(
                "main",
                &normalize(record("render", &format!("crash {big}"))),
                t0
            ),
            Verdict::Accept { .. }
        ));
    }

    #[test]
    fn a_record_larger_than_the_byte_budget_is_dropped() {
        let record = warn_record(0);
        // Sized off the record so the budget stays the smaller of the two.
        let mut limiter = Limiter::new(LimiterConfig {
            byte_burst: record.bytes() - 1,
            ..config()
        });
        assert_eq!(
            limiter.admit("main", &record, Instant::now()),
            Verdict::Drop
        );
    }

    #[test]
    fn a_repeated_signature_costs_one_slot_and_reports_after_the_window() {
        let mut limiter = Limiter::new(config());
        let t0 = Instant::now();
        let same = normalize(record("graphql", "not found"));
        assert_eq!(
            limiter.admit("main", &same, t0),
            Verdict::Accept {
                dropped: 0,
                suppressed: 0
            }
        );
        for _ in 0..4 {
            assert_eq!(
                limiter.admit("main", &same, t0 + Duration::from_secs(1)),
                Verdict::Drop
            );
        }
        // Repeats cost nothing: the other slots are still there.
        assert!(matches!(
            limiter.admit("main", &warn_record(0), t0),
            Verdict::Accept { .. }
        ));

        // Past the window the signature is written again, carrying the count.
        let later = t0 + Duration::from_secs(11);
        assert_eq!(
            limiter.admit("main", &same, later),
            Verdict::Accept {
                dropped: 0,
                suppressed: 4
            }
        );
    }

    /// The window is what releases a stream of one signature: nothing else
    /// need arrive for it to report.
    #[test]
    fn a_stream_of_one_signature_still_reports() {
        let mut limiter = Limiter::new(config());
        let t0 = Instant::now();
        let same = normalize(record("network", "dial failed"));
        limiter.admit("main", &same, t0);
        for i in 1..=9 {
            limiter.admit("main", &same, t0 + Duration::from_secs(i));
        }
        assert_eq!(
            limiter.admit("main", &same, t0 + Duration::from_secs(10)),
            Verdict::Accept {
                dropped: 0,
                suppressed: 9
            }
        );
    }

    #[test]
    fn past_the_ceiling_the_least_recently_seen_signature_is_evicted() {
        let mut limiter = Limiter::new(LimiterConfig {
            burst: 100,
            error_reserve: 0,
            ..config()
        });
        let t0 = Instant::now();
        let a = normalize(record("graphql", "a"));
        let b = normalize(record("graphql", "b"));
        let c = normalize(record("graphql", "c"));
        let d = normalize(record("graphql", "d"));
        limiter.admit("main", &b, t0);
        // A repeat of `b`, withheld; `b` is then the least recently seen.
        limiter.admit("main", &b, t0 + Duration::from_secs(1));
        limiter.admit("main", &a, t0 + Duration::from_secs(2));
        limiter.admit("main", &c, t0 + Duration::from_secs(3));

        // A fourth signature evicts `b`; its withheld repeat rides the summary.
        assert_eq!(
            limiter.admit("main", &d, t0 + Duration::from_secs(4)),
            Verdict::Accept {
                dropped: 1,
                suppressed: 0
            }
        );
        // `a` is remembered: still suppressed.
        assert_eq!(
            limiter.admit("main", &a, t0 + Duration::from_secs(5)),
            Verdict::Drop
        );
        // `b` is forgotten: it is written again inside what was its window.
        assert!(matches!(
            limiter.admit("main", &b, t0 + Duration::from_secs(5)),
            Verdict::Accept { suppressed: 0, .. }
        ));
    }

    #[test]
    fn one_webview_exhausting_its_budget_does_not_suppress_another() {
        let mut limiter = Limiter::new(config());
        let t0 = Instant::now();
        for n in 0..20 {
            limiter.admit("window-1", &warn_record(n), t0);
        }
        assert_eq!(
            limiter.admit("window-2", &warn_record(0), t0),
            Verdict::Accept {
                dropped: 0,
                suppressed: 0
            }
        );
    }

    #[test]
    fn eviction_removes_the_entry_and_reports_what_it_withheld() {
        let mut limiter = Limiter::new(config());
        let t0 = Instant::now();
        let same = normalize(record("graphql", "again"));
        limiter.admit("window-1", &same, t0);
        limiter.admit("window-1", &same, t0);
        // Burst 5 with 2 reserved and one slot taken: two land, two drop.
        for n in 0..4 {
            limiter.admit("window-1", &warn_record(n), t0);
        }

        // Two budget drops plus one suppressed repeat.
        assert_eq!(limiter.evict("window-1"), 3);
        assert!(!limiter.has("window-1"));
        assert_eq!(limiter.evict("window-1"), 0);
    }

    #[test]
    fn a_record_for_a_dead_label_is_dropped_and_leaves_no_entry() {
        let limiter = Mutex::new(Limiter::new(config()));
        let out = captured(|| {
            record_from(
                &limiter,
                "window-9",
                record("render", "late"),
                Instant::now(),
                |_| false,
            );
        });
        assert!(out.is_empty(), "got: {out}");
        assert!(!limiter.lock().unwrap().has("window-9"));
    }

    /// The lost race: the liveness check passes, the webview is destroyed
    /// before the record inserts, and the entry it leaves is swept on the
    /// next lock rather than leaking.
    #[test]
    fn an_entry_recreated_after_eviction_is_swept_on_the_next_lock() {
        let mut limiter = Limiter::new(config());
        let t0 = Instant::now();
        let is_live = |label: &str| label == "main";

        // The command checked liveness while `window-1` was alive...
        assert!(is_live("main"));
        limiter.admit("window-1", &warn_record(0), t0);
        // ...then `Destroyed` evicted it...
        limiter.evict("window-1");
        // ...and the record's insert ran after.
        limiter.admit("window-1", &warn_record(1), t0);
        assert!(limiter.has("window-1"));

        // The next lock sweeps it.
        let swept = limiter.sweep(is_live);
        assert_eq!(swept, vec![("window-1".to_string(), 0)]);
        assert!(!limiter.has("window-1"));
    }

    #[test]
    fn the_command_body_writes_a_summary_before_the_record_that_follows_drops() {
        let limiter = Mutex::new(Limiter::new(LimiterConfig {
            burst: 1,
            error_reserve: 0,
            ..config()
        }));
        let out = captured(|| {
            record_from(
                &limiter,
                "main",
                record("graphql", "one"),
                Instant::now(),
                |_| true,
            );
            record_from(
                &limiter,
                "main",
                record("graphql", "two"),
                Instant::now(),
                |_| true,
            );
        });
        assert_eq!(out.lines().count(), 1, "the second is dropped, got: {out}");
    }

    #[test]
    fn destroyed_reports_what_the_webview_withheld() {
        let limiter = Mutex::new(Limiter::new(LimiterConfig {
            burst: 1,
            error_reserve: 0,
            ..config()
        }));
        let out = captured(|| {
            record_from(
                &limiter,
                "main",
                record("graphql", "one"),
                Instant::now(),
                |_| true,
            );
            record_from(
                &limiter,
                "main",
                record("graphql", "two"),
                Instant::now(),
                |_| true,
            );
            destroyed(&limiter, "main");
        });
        assert!(out.contains("dropped=1"), "got: {out}");
    }

    #[test]
    fn a_page_load_is_marked_at_info_and_leaves_the_budget_alone() {
        let mut limiter = Limiter::new(config());
        let t0 = Instant::now();
        for n in 0..3 {
            limiter.admit("main", &warn_record(n), t0);
        }
        let out = captured(|| page_loaded("main"));
        assert!(
            out.contains("INFO") && out.contains("window=\"main\""),
            "got: {out}"
        );
        // Still out of ordinary budget: the marker charged nothing and freed nothing.
        assert_eq!(limiter.admit("main", &warn_record(3), t0), Verdict::Drop);
    }

    /// The command body summarizes both what the lock swept from a closed
    /// webview and what a refill lets a live one report.
    #[test]
    fn the_command_body_summarizes_what_was_swept_and_what_was_dropped() {
        let limiter = Mutex::new(Limiter::new(LimiterConfig {
            burst: 1,
            error_reserve: 0,
            ..config()
        }));
        let t0 = Instant::now();
        let main_only = |label: &str| label == "main";

        let out = captured(|| {
            record_from(&limiter, "window-1", record("graphql", "a"), t0, |_| true);
            record_from(&limiter, "window-1", record("graphql", "b"), t0, |_| true);
            record_from(&limiter, "main", record("graphql", "c"), t0, main_only);
            record_from(&limiter, "main", record("graphql", "d"), t0, main_only);
            let later = t0 + Duration::from_secs(1);
            record_from(&limiter, "main", record("graphql", "e"), later, main_only);
        });

        assert!(out.contains("window=\"window-1\" dropped=1"), "got: {out}");
        assert!(out.contains("window=\"main\" dropped=1"), "got: {out}");
    }

    #[test]
    fn every_source_round_trips_its_name() {
        for name in ["graphql", "subscription", "network", "render", "auth"] {
            assert_eq!(Source::parse(name).as_str(), name);
        }
        assert_eq!(Source::parse("other").as_str(), "unknown");
    }

    /// Fields that fit their own caps can still overrun the whole once JSON
    /// escapes them; the whole is then replaced, never cut.
    #[test]
    fn reported_fields_too_large_together_are_replaced() {
        let mut big = record("render", "boom");
        big.stack = Some("\\".repeat(MAX_STACK_BYTES));
        big.component_stack = Some("\\".repeat(MAX_COMPONENT_STACK_BYTES));

        let out = captured(|| emit("main", &normalize(big), 0));

        assert_eq!(reported_of(&out), serde_json::json!({ "truncated": true }));
    }
}
