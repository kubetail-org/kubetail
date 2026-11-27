# Specs

A spec describes **what we are about to build**, in enough detail to build it and no more. It is a plan with a shelf life.

The other two kinds of doc keep their jobs: a `CLAUDE.md` says what is true now, and [`docs/adr/`](../adr/README.md) says why a design was chosen. A spec says what comes next.

## When a change needs one

Whether something is worth building is settled first, in an issue. An issue whose work is too large or too open to plan in its own PR gets the `needs-spec` label. Its spec answers it, and the label comes off when the spec is accepted.

## What a spec holds

The goal and the issue it answers, the design, the tasks in build order, the acceptance cases, and a *When it lands* section naming the guides and ADRs the landing PR updates. Leave out what the code will say for itself.

**A spec that adds or changes a desktop read names its contract**: the family, the kind, and whether it is a query or a subscription. It also names the read on each surface it crosses. Choose the contract with [*Choosing a contract*](../../apps/desktop/docs/read-contracts/README.md#choosing-a-contract) in the desktop's read-contract guide.

## Where a spec lives

**A spec never reaches `main`.** It lives on a pull request, then on a `wip/<topic>` branch of this repository, and the PR that lands its code deletes it.

1. **Propose.** Add the spec on a branch of your fork and open a PR into `main` from the spec template: `gh pr create --template spec.md`, or add `?template=spec.md` to the compare URL. Title it `📜 Spec: <what it builds>`, label it `spec`, and reference the issue it answers (`Ref #123`). It is reviewed there, line by line.
2. **Accept.** A maintainer creates `wip/<topic>` from `main`, retargets the spec PR to it and merges it. They then open a draft PR from `wip/<topic>` into `main`, which shows the whole change as it grows.
3. **Build.** Each implementation PR targets `wip/<topic>` and updates the spec as the work moves. Keep `wip/<topic>` current with `main`.
4. **Land.** When the work is done, the `wip/<topic>` PR folds what is now true into the `CLAUDE.md` guides, writes an ADR for each decision whose reasons need recording, deletes the spec, closes the issue, and merges into `main`.

How `wip/<topic>` keeps up with `main`, and whether it lands as a squash, a rebase or a merge commit, is the maintainers' call for each piece of work. A rebase rewrites the branch under every open PR into it, so warn their authors first.

CI runs on a PR into `wip/<topic>` as on one into `main`. A push to `wip/**` runs `coverage.yml`, so the coverage gate has a report for the branch's tip to compare with.

## Naming

A spec is named for its shape, in lowercase. No dates: a spec is edited as the work moves, not appended to.

| Shape | File | Prose |
| --- | --- | --- |
| One spec | `docs/specs/<topic>.md` | — |
| Steps built one after another | `docs/specs/<topic>/<n>-<slug>.md` | step 3 |
| Waves of steps built in parallel | `docs/specs/<topic>/<n><a>-<slug>.md` | step 3b |

**A sequence**, the second and third shapes, has a `docs/specs/<topic>/README.md` naming its steps, and shares one `wip/<topic>`. The README and each step's spec merge into it as they are accepted, and it lands once the last step is built. In the third shape, `<n>` is the wave: every step in a wave can be built at the same time, and a step needs only steps of earlier waves. The letter orders a wave's steps for reading, not for building. Use letters only when a wave has more than one step.

A branch on a fork is its contributor's to name. The spec's own name works: `<topic>`, `<topic>-<n>` or `<topic>-<n><a>`.

`make lint-md` skips the specs, which may link to files their work has yet to add.

## Working a numbered spec

These rules hold for every step of a sequence, so each spec states only what is its own.

- **Build a step after its prerequisites have landed on `wip/<topic>`.** A step is the unit of integration and of deletion. Its tasks are smaller assignments, built in the order listed.
- **Hand over one task at a time**, with the spec, the `CLAUDE.md` guides of the areas it touches, and the current code. Each task starts Planned; record its status and its PR in the spec as the work moves.
- **Each handoff reports** the files changed, the acceptance cases, what the checks returned and what still fails.
- **A task ends on green checks.** Run the checks the root `CLAUDE.md` asks for (`make check-changed`) and those of each area's guide. A dependent cutover stays on `wip/<topic>` until its whole step passes: never merge half of one into `main`.
- **When a step is done**, update the guides and ADRs its *When it lands* section lists, then delete that step's spec alone and remove its row from the sequence's README, in the same change. Later steps keep their numbers: their prerequisites are landed code and current guides, never a deleted spec.

## Index

Proposed: the open PRs labelled [`spec`](https://github.com/kubetail-org/kubetail/pulls?q=is%3Apr+is%3Aopen+label%3Aspec).
In progress: the open PRs from a `wip/<topic>` branch into `main`.
Waiting for a spec: the open issues labelled [`needs-spec`](https://github.com/kubetail-org/kubetail/issues?q=is%3Aissue+is%3Aopen+label%3Aneeds-spec).
