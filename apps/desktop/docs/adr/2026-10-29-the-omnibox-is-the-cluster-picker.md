---
title: The omnibox is the cluster picker
date: 2026-10-29
scope: frontend
status: Accepted
---

# The omnibox is the cluster picker

## Context

The omnibox is the address bar of the cluster surface, and the cluster is the address. It used to
hold two cluster controls. A small dropdown of kube-contexts switched the window's cluster but
showed no health, and its dot was a green placeholder. The Clusters dialog, opened from a server
button, showed each cluster's health and held its actions, but it could not switch. A user had
to look in one place to see whether a cluster was healthy and act in another to move to it.

Issue #505 asked how the two should relate. Questions 1, 4, 5, 6 and 7 and the switching half of
question 2 were answered. Question 2's content and grouping, and question 3, were left open.

## Decision

A press anywhere on the omnibox except the memory button opens **the cluster picker**
(`components/widgets/cluster-picker/`). It is a popover hung from the omnibox through
`OmniboxPopover`, the positioner the memory popover also uses. The picker lists the kubeconfig's
contexts, the window's first. Each row shows the Clusters dialog's own verdict, moved to
`src/lib/cluster-health.ts` so both surfaces read one function. A click on a row switches the
window's cluster. A filter appears from six contexts up. The omnibox's dot carries the window's
cluster's verdict.

The Clusters dialog keeps its actions (enable, pause, clear, remove), its detail and the orphaned
caches. Its one entry is *Manage clusters…* in the picker's footer. That footer stays while there
is no kubeconfig, so the dialog's orphaned caches stay reachable.

## Alternatives considered

- **Keep the dropdown and give it real health.** This leaves two surfaces listing the same
  clusters with two looks. The dropdown's menu items also have no room for a summary line or a
  badge.
- **Switch from the dialog.** A modal table is too heavy a step for the most frequent cluster
  action, and the dialog's rows also list orphaned caches, which have no context to switch to.
- **Move the dialog's actions and detail into the picker now.** That is #505's open questions 2
  and 3, and answering them first would have held back switching with health.
- **Make the omnibox a text field, like a browser's.** Kubeconfig contexts are a short, known
  list rather than an open address space. A filter inside the picker gives the same narrowing
  without a second editable control in the app bar.

## Consequences

Switching and health are in one place, one press away, and the verdict has one implementation.
The omnibox pill has no padding of its own, so no press on it lands between controls. Its
`data-tauri-drag-region="false"` still keeps a press from dragging the window.

The dialog is reachable only through the picker. A change to the verdict changes both surfaces
at once, which is intended. Anything that draws a cluster's health elsewhere should read
`cluster-health.ts`, not write its own.

## Revisit when

#505's questions 2 and 3 are answered. If the dialog's actions or detail move into the picker,
the dialog may go, and this ADR is superseded.
