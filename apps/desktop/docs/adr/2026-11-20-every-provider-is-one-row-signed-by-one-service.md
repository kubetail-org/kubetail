---
title: Every provider is one row, signed by one service
date: 2026-11-20
scope: cross-cutting
status: Accepted
---

# Every provider is one row, signed by one service

## Context

[A provider's credential follows one order](2026-11-20-a-providers-credential-follows-one-order.md)
put every keyed provider under one service, `services/providercredentials`. ChatGPT and Vertex AI
stayed apart. `agent/catalog` kept a second list for them, `catalog.Config.Signers` signed them with
their connectors directly, and each connector ran its own catalog sync beside the service's. The
Providers pane repeated the split, with a hard-coded Vertex AI row and a ChatGPT row drawn only
while the catalog served it.

The two lists were not two kinds of provider. Under the one order, a ChatGPT or Vertex AI provider
is one whose order holds its connector alone.

## Decision

**`agent/catalog` has one table.** A row is an `llm.Provider`, an optional key variable and an
optional models reader (`providers.go`'s `row`). `rowsAt` is the one place a debug base is applied,
and leaves out a row with no base URL: Vertex AI with no project found. `New`, `Providers` (the
service's view) and `Models` (the one catalog reader) read it, so the gauge lists exactly what the
catalog holds.

**`services/providercredentials` signs and syncs every row.** A provider with no key variable takes
no key: no keyring entry, no environment key, `Save` and `Delete` refused with `ErrNoKey`. Its
connector is its only source. `providerCredentialsWatch` sends every provider, with a null
`keyVariable` for one that takes no key, and the pane draws every row it sends.

**A provider with no key syncs its catalog per session.** `WatchCredential` is `0` with no
credential, `1` for a provider that takes a key, whichever credential signs, and the connector's
session's generation for one that takes none. A ChatGPT reconnect as another account is a new value
even when the watch conflates the `0` between, so `catalogsync` reads the new account's list. A
keyed provider's catalog is the same under every credential, so a change of source publishes
nothing and its models never leave the picker. This replaces the per-connector `SyncCatalog` of [a
signed-in provider's catalog follows its
connection](2026-11-18-a-signed-in-providers-catalog-follows-its-connection.md); its rules for
when a catalog is read and emptied stand.

**Vertex AI's catalog is written**: the `gemini` row's models under `google/` ids, read without a
request, like every keyed row's.

## Alternatives considered

- **Keep two lists and add the signed-in rows to the gauge by hand.** The gauge and the catalog
  would each decide which providers exist, and could disagree whenever a project is or is not found.
- **Keep `WatchHasCredential` at `0`/`1`.** Two ChatGPT sessions in a row could arrive as one steady
  `1`, and the first account's models would stay listed.
- **Send the session's generation for every provider its connector signs.** One rule, but
  OpenRouter's models would leave the picker for a frame each time its connector connects or
  disconnects over a key, for a catalog that is the same either way.

## Consequences

- The ChatGPT plan row is always in the pane, not only while connected. With no Google Cloud
  project there is no Vertex AI row.
- A connector of a provider not in the table is ignored, so `app` passes the Google connector
  whether or not a project was found.
