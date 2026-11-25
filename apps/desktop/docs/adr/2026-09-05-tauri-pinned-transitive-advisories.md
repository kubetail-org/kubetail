---
title: Track the advisories Tauri's dependency graph pins
date: 2026-09-05
scope: host
status: Accepted
---

# Track the advisories Tauri's dependency graph pins

## Context

`cargo audit` ran green while seventeen advisories stood against the locked dependencies. It
exits non-zero for *vulnerabilities*; unsound and unmaintained advisories are warnings and exit 0.
The [4 September review](../security/2026-09-04-security-review.md) found them by matching the
lockfile against RustSec by hand (R-09), which is not a check anything reruns.

All seventeen are transitive, and none is reachable from a direct dependency we chose. They fall
into three groups:

**glib 0.18.5, [RUSTSEC-2024-0429](https://rustsec.org/advisories/RUSTSEC-2024-0429.html)** —
unsoundness in `VariantStrIter`, which can dereference a null pointer. The patched line is
`>= 0.20.0`. This is the only one of the seventeen with a memory-safety claim.

**The GTK 3 stack, RUSTSEC-2024-0411 through -0420, plus proc-macro-error
(RUSTSEC-2024-0370)** — `gtk`, `gtk-sys`, `gtk3-macros`, `gdk`, `gdk-sys`, `gdkx11`, `gdkx11-sys`,
`gdkwayland-sys`, `atk` and `atk-sys` at 0.18, all unmaintained; upstream moved to gtk4.
`proc-macro-error` is build-time only, reached through `glib-macros` and `gtk3-macros`.

**The unic crates, RUSTSEC-2025-0075/0080/0081/0098/0100** — `unic-char-range`, `unic-common`,
`unic-char-property`, `unic-ucd-version` and `unic-ucd-ident` at 0.9.0, unmaintained, reached
through `urlpattern 0.3.0`, a `tauri-utils` dependency. This group is already fixed upstream and
merely unreleased: `urlpattern` 0.4.0 replaced `unic-ucd-ident` with `icu_properties`, and Tauri's
`dev` branch pins `urlpattern = "0.6"`. Published `tauri-utils` 2.9.3 — the newest, and ours —
still pins `0.3`, and 0.3 → 0.4 is semver-incompatible for a `0.x` crate, so no `cargo update`
reaches it.

**The zbus 5 crates, RUSTSEC-2026-0370/0371/0372** (added 2026-10-18) — `zvariant`,
`zbus_names` and `zvariant_derive` at 5.x, unmaintained because zbus 6.0.0 folded all three into
`zbus` itself. No defect is claimed. `tauri-plugin-opener` and `tauri-plugin-single-instance`
depend on `zbus = "^5.9"` in every release through 2.7.2 and 2.5.4 (and 3.0.0-alpha.3), so the
retired crates stay in the graph whatever we do. `src-tauri` also depends on zbus 5 directly, for
the Linux wake sources in `wake/linux.rs`; moving it to 6 alone would put two zbus majors in the
Linux build and clear nothing, so it moves with the plugins.

**chrono 0.4, [RUSTSEC-2026-0543](https://rustsec.org/advisories/RUSTSEC-2026-0543)** (added
2026-11-17) — unmaintained; no defect is claimed. Nothing in any build compiles it
(`cargo tree -i chrono --target all -e all` prints nothing), yet it stays in `Cargo.lock`:
`serde_with`, which `tauri-utils` depends on, names it through weak `chrono_0_4?/…` features in its
default `std` feature, and Cargo locks the optional dependency a weak feature names. `cargo audit`
reads the lockfile, so the notice fires anyway. The host's own uses of chrono — an unused direct
dependency and `file-rotate`, which needed it for timestamp suffixes the host never used — were
removed rather than accepted; size rotation is `log_rotate.rs`.

We pin none of the first three groups. `tao`, `wry`, `tauri`, `muda` and `libappindicator` bring in the GTK 3 line,
and `tauri-utils` brings in `urlpattern`. Every glib consumer in the lock is part of that Linux
GTK/WebKitGTK stack, so the first two groups do not compile into the macOS or Windows builds.

## Decision

Accept all seventeen, and the three zbus notices and the chrono notice with them, and make the acceptance a thing CI enforces rather than a thing a reviewer
remembers. `src-tauri/.cargo/audit.toml` lists them with a reason each, and CI runs
`cargo audit --deny unsound --deny unmaintained`, so a *new* advisory fails the build while these
stay recorded and visible.

The glib acceptance rests on a call-graph check, not on the severity being low. `array_iter_str`
is the sole entry point to `VariantStrIter`, and it has no call site in the graph: the sources of
`gtk-rs-core` 0.18.5 (glib, gio, pango, cairo-rs, gdk-pixbuf), `gtk3-rs` 0.18, `wry` 0.55.1, `tao`
0.35.3, `webkit2gtk-rs`, `javascriptcore-rs` and `libappindicator-rs` contain none, and `src-tauri`
names neither `glib` nor `Variant`. Inside glib the function appears only in its own tests and a
doc example. `soup3` 0.5.0 was not inspected — its source host was unreachable — so the check
covers every crate in the stack but that one.

## Alternatives considered

**Force glib to 0.20.** The patched line does not fit a GTK 0.18 graph; every sibling crate would
have to move with it, and the versions that do are gtk4's. There is no backport either — 0.18.5 is
the last of its line. This is upstream's migration, not a bump we can take.

**Patch `tauri-utils` to a git revision** to take the `urlpattern` bump early. It would clear five
notices by pulling an unreleased Tauri into a shipping app, which is a larger risk than five
unmaintained crates that have no known defect.

**Leave `cargo audit` bare.** It stays green, which is the property that hid these for a release
cycle. A check that cannot fail is not a check.

**`--deny warnings`.** Also denies yanked crates, a different axis with a different response —
a yank is usually actionable the same day. Denying the two advisory kinds we mean keeps the
signal legible.

**Drop the GTK dependency.** It is how Tauri renders on Linux. Not ours to remove.

## Consequences

An advisory against a crate already on the list goes unreported, including a future
*vulnerability* against one of these packages — `ignore` is per advisory ID, but a new ID against
`gtk` would be caught, while a rescoped existing one would not. That is the cost of the list, and
the reason each entry names its reason: the list is only as good as the next person's willingness
to reread it.

The glib finding is accepted as unreachable, not as harmless. If anything in the graph starts
calling `array_iter_str`, the acceptance is void and nothing automated will say so.

`docs/security-model.md` gains no Enforced row from this: the advisory jobs already had one, and
this narrows what they miss rather than adding a protection.

## Revisit when

`tauri-utils` publishes past 2.9.3 with `urlpattern >= 0.4`, which retires the five unic entries
for the cost of a `cargo update`; check on each Tauri bump. Or `serde_with` stops naming chrono, or
Cargo stops locking an optional dependency only a weak feature names, which retires the chrono
entry. Or `tauri-plugin-opener` and `tauri-plugin-single-instance` both
publish on zbus 6, which retires the three zbus entries: bump them, move our own `zbus` to 6 and
port `wake/linux.rs` in the same change. Or Tauri's Linux backend moves off
GTK 3, which retires the other eleven — not close: `wry` and `tao` still pin `gtk = "0.18"` and
`webkit2gtk = "=2.0.2"` on their `dev` branches. Also when `soup3` becomes inspectable, which closes the one gap in the glib call-graph
check; when a `cargo audit` run reports an ID not on the list, which is the mechanism working; or
at the next security review, which should re-derive the call-graph check rather than trust this
paragraph.
