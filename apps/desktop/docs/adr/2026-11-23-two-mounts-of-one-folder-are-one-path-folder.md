---
title: Treat two mounts of one folder as one PATH folder
date: 2026-11-23
scope: sidecar
status: Accepted
amended_by:
  - [A sandboxed command's PATH is one setting the user writes](2026-11-24-the-sandbox-path-is-one-setting-the-user-writes.md)
---

# Treat two mounts of one folder as one PATH folder

## Context

The frozen `PATH` compares folders as files: `filterPath` drops an entry that is a folder already
kept. It compares device and inode (`os.SameFile`), as the file tools and `realpath.Under` do.

On Linux, two bind mounts of one directory share a device and an inode and can still differ: a
file or a folder can be mounted beneath one and not the other. So the filter can drop a second
entry that finds a program the first does not.

## Decision

A folder's identity is its device and inode everywhere, and two mounts of one directory are one
`PATH` folder. The first entry in the shell's order is kept.

A stored decision is another matter: `targetLookup` carries one only to a fresh folder whose name
differs from the stored one in case alone, so a decision made for one mount does not hold for the
other. That entry reads as moved and waits for the user. The exception is two mounts whose names
differ in case alone, which are one folder to the comparison: a decision for one holds for the
other, as the duplicate rule holds.

Making the mounts takes root, and root can already replace any program in an adopted folder. What
is left is a tool that is not found, on a machine that lists both mounts in `PATH` and mounts a
program beneath only the second.

## Alternatives considered

**Compare the mount as well (`statx`'s `STATX_MNT_ID`).** It tells the two apart, but it needs
Linux 5.8 with a fallback before it, a comparison of Kubetail's own in place of `os.SameFile` on
Linux alone, and tests that need the privilege to mount. `fileguard` and `realpath.Under` would
keep the device and inode, so "the same folder" would mean two things.

## Consequences

One meaning of "the same folder" across the sidecar, from the standard library.

On Linux a `PATH` entry that is a second mount of a kept folder is dropped as a duplicate, and what
is mounted only beneath it is not searched.

## Revisit when

A comparison of mounts is needed elsewhere, so every comparison can move at once.
