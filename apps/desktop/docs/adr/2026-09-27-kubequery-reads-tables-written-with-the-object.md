---
title: KubeQuery reads tables written with the object
date: 2026-09-27
scope: sidecar
status: Accepted
---

# KubeQuery reads tables written with the object

## Context

The common resource questions — what a node has requested, which containers run without limits,
why a container last exited — live inside Pod bodies: `spec.containers`,
`status.containerStatuses`, and quantities like `250m` and `1Gi`. KubeQuery's first views read
only what the store already kept as rows, plus the body itself through `body()`.

The first design for these questions was a view over a Go function, `containers_of(body)`, that
parsed each Pod's body at query time and returned its containers as JSON for `json_each` to
expand. A function on the query driver can be called by any statement, on any text the statement
builds up to the 16 MiB length limit, and the query's deadline cannot interrupt a Go callback. So
every such function has to be cheap whatever it is handed, and a body parser is not: reviewing the
design found a 16 MiB list of empty containers decoding to about 1.9 GB, one status joined to 1,024
containers of one name copying its strings into each (about 15 GB), and escapes that grow the
output past its input. Each case needed its own bound, and even bounded, a call could allocate
about 300 MiB. It also parsed every body on every scan.

## Decision

**What a view would compute from bodies, the store writes when it writes the object**, as it
already writes `labels` and `owner_refs`. A Pod's containers are rows of the `containers` table
(`kubestore/containers.go`), read by `projectObject` from the sanitized body and written by
`insertObjectRow` in the object's own transaction, deleted with it through `cascadeTables` and a
kind's clear. KubeQuery's `containers` view is a plain `SELECT` of that table.

**No function a statement can call parses a body.** The query driver registers two: `body()`,
which only decompresses a stored blob, at most 16 MiB; and `quantity()`, which parses one
Kubernetes quantity and refuses text past 64 bytes or an exponent past ±1,000 before the parse
(`kubestore/quantity.go`), since `ParseQuantity`'s cost grows with the exponent. The writer reads a
container's requests and limits through the same parse. The planned resolution of selectors and
references follows this rule.

## Alternatives considered

- **A view over a body-parsing function, with bounds.** It needed a bound per shape an attacker
  could build — list length, repeated names, output size, escaping — each found by review and
  none by the type system, and still left a call allocating a multiple of its input. It also
  parsed every Pod body on every scan of the view.
- **Parse at query time, but only from stored bodies.** A statement cannot be kept from calling a
  registered function on its own text, so this is the same function with the same exposure.

## Consequences

- A statement's reach into Go code stays two small functions whose cost is fixed by their input
  bounds, so the deadline keeps meaning what the security model says it means.
- Every Pod write pays for its containers: a DELETE and one INSERT of its rows. Measured on a
  relist page of 500 Pods of about 6 KB each (`BenchmarkWriteAPodPage`), the page takes 164 ms with
  the rows and 118 ms without, about 38% more. The INSERT materializes `json_each`'s value: read
  eighteen times, each element was rendered and parsed again on every read, and the rows cost
  about two thirds more.
- A new side table must join `cascadeTables` and `ClearKind`'s list, or its rows outlive their
  object. The tests that pin `containers` show the shape.
- Rows reflect the stored body at its last write. A container resized in place reads its spec
  until the resize lands.

## Revisit when

The write cost of the side tables dominates a relist on large clusters, or a question needs a body
field often enough that reading it through `body()` is the bottleneck.
