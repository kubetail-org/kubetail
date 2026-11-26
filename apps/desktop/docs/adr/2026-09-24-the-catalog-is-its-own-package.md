---
title: The catalog is its own package, and lists every tool each provider is offered
date: 2026-09-24
scope: sidecar
status: Accepted
---

# The catalog is its own package, and lists every tool each provider is offered

## Context

`llm` held the vendor catalog (`provider.go`), and the catalog listed the vendor tools each model
takes by contract (`Model.NativeTools`). So the contract constants lived in `llm` too
(`contract.go`), and a new vendor tool meant a new constant there, although [every tool is in the
box](2026-09-24-every-tool-is-in-the-box.md) had already moved each tool's spelling out of `llm`.

Which tools a turn got was decided in three places: the model's contracts, `Target.Takes`, which
checked the contracts and the wire, and box order in `Box.For`, which picked the first tool of each
`ActionKind`. That ADR gave box order the security weight of choosing between a vendor's tool and
ours. A contract named the tool a second time, vendor-qualified (`anthropic_web_search_20260318`),
which *A native tool is a contract* had needed so that a
bare vendor word could not stand for a shape.

## Decision

**`catalog` is its own package, above `llm` and `tools`** (`sidecar/internal/catalog`). It holds
the vendor rows and `KeyVars` (`providers.go`), and, **for each provider, every tool its turns are
offered**, ours and the vendor's, by tool name (`catalog.go`). `New(apiKeys, fake)` lists the keyed
providers in picker order, then the fake; `Providers()` goes to `llm.New`, and `ToolsFor(target)`
to `Box.For`. A vendor row goes here from now on, never in `llm`.

**A list is policy.** It decides which code runs each kind of action for a provider, and what
leaves the machine for it. Where a vendor's tool and one of ours do the same kind of thing, the
list names one of them. `Box.For(target, names)` offers the box's tools the list names, in box
order, each native one only where `target.Takes` it, and keeps no kind rule of its own: `app`'s
tests hold every list to one tool per kind.

**`llm` names no vendor and no tool.** `New(providers...)` holds what it is given. `Model` loses
`NativeTools`, `contract.go` goes, `NativeTool` is `Name()` alone, and `Target.Takes` is whether
the wire can spell the tool. The fake stays in `llm` as its own test double, built by the caller
and handed over as `FakeProvider(f)`.

**A contract is the vendor's own identifier, in `tools`.** `tools.ContractName` is what the vendor
calls the tool's shape (`web_search_20260318`); the vendor qualifies the tool in its `Name()`
(`anthropic_web_search_20260318`). The contract is recorded on each provider-run row and matched by
nothing: a row finds its reader by name alone, since every row is written under its tool's name,
unique in the box. It is kept as provenance, the version of the vendor's tool a call ran on, which a
tool's name records only by convention.

This amends [llm is one package](2026-09-21-llm-is-one-package.md): `New` no longer wires the
providers itself. It amends *A native tool is a contract*:
the catalog no longer lists contracts, a contract is the vendor's own identifier, and the reason to
qualify the contract no longer applies, since the name carries the vendor. It amends [every tool is
in the box](2026-09-24-every-tool-is-in-the-box.md): `For` offers the provider's list and returns no
unoffered contracts, and the list, not box order, decides between a vendor's tool and ours, with the
security weight that ADR gave box order.

## Alternatives considered

- **Keep the rows in `llm` and add the lists beside them.** `llm` would import every tool package
  for its name, and `tools` already imports `llm`, so the lists could not live there without a
  cycle.
- **List tools per model.** Every model of a provider, on the one dialect it speaks, takes the same
  tools today. `ToolsFor` takes the whole target, so keying by model or dialect later changes
  `catalog` alone.
- **Keep box order as the preference within a kind.** It works while one tool of a kind is a
  vendor's, but it puts a security choice in the order of a slice in `app.go`, and a provider
  that should get our tool over a vendor's cannot say so.
- **Drop the contract, since nothing reads it.** The row would no longer say which version of the
  vendor's tool ran, and the name only says so by convention.

## Consequences

- Adding a vendor tool is a tool package, its wire interface where it has none yet, and its name on
  a list. Adding it to a list, or leaving one of ours off, is a security change.
- `TestOnlyAnthropicAndTheFakeListAVendorTool` pins that no other provider is offered a vendor's
  tool; `TestEachTargetIsOfferedItsList`, `TestEachListIsOneToolOfEachKind` and
  `TestEveryToolIsOnAList` in `app_test.go` hold the lists to the box and the wires. A provider with
  no list is offered no tools, which those tests catch.
- The lists do not depend on API keys, so every tool stays in the box and an old chat's rows read
  after its provider's key is gone.
- A provider-run row written before this change keeps `anthropic_web_search_20260318` as its
  contract, and a row named otherwise than its tool (`web_search`) no longer reads. Nothing reads
  the contract, and the app is pre-release.

## Revisit when

A request routed through one provider reaches models that take different tools, or a provider
serves two dialects.
