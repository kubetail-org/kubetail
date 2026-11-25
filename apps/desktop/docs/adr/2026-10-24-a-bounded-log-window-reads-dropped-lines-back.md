---
title: A bounded log window reads its dropped lines back from a moment
date: 2026-10-24
scope: cross-cutting
status: Accepted
---

# A bounded log window reads its dropped lines back from a moment

## Context

The log viewer's window kept every line it was shown, so a long follow grew without bound. It
did not need to keep a line it could read again: every line the sidecar served is answered by
its log cache, or by the kubelet while it still holds it. A bound needs a way back to what it
drops.

A page cursor is positioned only at a page's ends, and a follow's lines carry none. A drop inside
a page, or anywhere in a follow's lines, leaves no cursor that reads on from the cut, and `seen`
cannot rewind one. Lines are held in arrival order, so a follow's streams interleave, and a
container's clock can run behind another's.

Reading back also exposed a gap in the sidecar. A workload resolves only the pods its mirror
walk reaches now, so a pod replaced since the view landed was skipped although the cache still
held its lines (#392). A pod's file rows carried no owner to find it by.

## Decision

**`maxLines` bounds the window** (`window.ts`, `core.tsx`, default 5000). Past it, a page or
batch ends with a drop at one edge, never of a line on screen or the paging margin past it.

**A dropped edge reads back from a moment.** Each edge keeps a *seam*, the farthest stamp among
the lines dropped there, folded into the seam already set rather than set over it. The next page
that way is a one-sided first page at that moment: `Until` before, `Since` after.

**Both seam pages read per container.** `Since` sends each container's newest held line as
`seen`, as every page after does. `Until` sends each container's oldest held line, and the
sidecar accepts `seen` on a first page from `Last`: each container reads strictly before its
entry, and every `prev` carries the entries on, moved back as their containers are read.

**A workload finds its gone pods in the file, by ownership the file records.** Each filing of a
pod's streams adds its controller chain to `pod_owners` in the log file, walked up the mirror's
controller references. A page or follow reads the owned pods the mirror no longer holds, each
only where a read can deliver a line of it, from that stream's own start to the read's far edge.
A chain grows with the mirror and loses a pair only with its pod's last stream.

## Alternatives considered

**Read back from a page cursor.** No cursor sits at a drop that falls inside a page or a
follow, and `seen` cannot move a cursor back.

**Set the seam over on each drop.** Lines arrive out of order, so a later drop's farthest stamp
can lie short of an earlier one's. Setting it over would read back from too near a moment and
skip lines the earlier drop took.

**Read the `Until` from the moment alone.** A container whose clock runs behind can hold a line
stamped before the seam yet newer than its held run, and two lines of one container can share a
nanosecond. Only a per-container cursor keeps both out. The entries ride every page back because
a `prev` lands on another container's line, which is a time bound for a lagging one.

**Have the window name the containers it has shown.** A seam page would carry the names of
containers it holds no line of, and the sidecar would read their gone pods. The answer would
depend on one window's memory, the cursor would grow with every pod the view showed, and a pod
replaced before the landing would stay unread.

**Read a gone pod a cursor's entry names.** This was #392's first fix. An entry checked against
the scope by namespace alone read another workload's gone pod in that namespace, and checked by
ownership it reads nothing the chain does not already find. Entries now position streams and
add none.

**Call a pod gone when the resolve hands out no stream of it.** A previous view of a pod that
never restarted resolves nothing, and a workload's chain still names a pod it orphaned. Neither
is gone. The mirror is asked instead, as the janitor's sweep asks it.

## Consequences

The window's memory is bounded, and a read back costs one page. A line both the cache and the
kubelet let go comes back as the existing `gap` on the line past it.

A dropped key comes back on another line, since keys stay consecutive. Anything holding a key
across a drop must let it go: the selection prunes to `firstKey`..`firstKey + count`.

A workload's landing, not only a scroll back, now shows the pods it replaced within the file's
retention, marked previous. The reach bound is one-sided, so a page back takes every gone pod
older than its start. Paging back over 300 gone pods costs about 12 ms a page, and gone pods do
not count toward `MaxStreams`.

A `Missing` workload's gone pods have no owner to find them by, and go unread. A pod that left
the mirror before any page read it whole is found by the owners it recorded, its ReplicaSet and
not its Deployment. An orphaning takes no pair away.

## Revisit when

A workload's gone pods grow past what a page can walk in tens of milliseconds, or a view needs
pods that left the mirror before any page filed their chain.
