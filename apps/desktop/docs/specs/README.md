# Specs

A spec describes **what we are about to build**, in enough detail to implement it and no more.
It is a plan with a shelf life.

The other two doc kinds keep their jobs: `CLAUDE.md` says what is true now, `docs/adr/` says why a
design was chosen. A spec says what comes next.

## Where a spec lives

**A spec never reaches `main`.** It lives on a pull request, then on a `wip/<topic>` branch of
this repository, and is deleted in the PR that lands its code:

1. **Propose.** Add the spec on a branch of your fork, open a PR into `main` titled
   `📜 Spec: <what it builds>`, and link the issue it answers. The spec is
   reviewed there, line by line. Use the spec PR template:
   `gh pr create --template spec.md`, or add `?template=spec.md` to the compare URL.
2. **Accept.** A maintainer creates `wip/<topic>` from `main`, retargets the spec PR to it and
   merges it. They then open a draft PR from `wip/<topic>` into `main`, which shows the whole
   change as it grows.
3. **Build.** Each implementation PR targets `wip/<topic>`, and updates the spec as the work
   moves. Keep `wip/<topic>` current with `main` as the work goes.
4. **Land.** When the work is finished, the `wip/<topic>` PR folds what is now true into the
   relevant `CLAUDE.md`, writes an ADR if a decision needs its reasons recorded, deletes the spec,
   and merges into `main`.

How `wip/<topic>` keeps up with `main`, and whether it lands as a squash, a rebase or a merge
commit, is the maintainers' call for each piece of work. A rebase rewrites the branch under any
open PR, so warn their authors first.

The open PRs titled `📜 Spec:` are the proposals; the open PRs from `wip/*` are the work in progress.

Whether something is worth building is settled before its spec, in an issue, or in a Discussion
for an open-ended proposal. A security spec's gap keeps its line in [`TODO.md`](../TODO.md#security),
linking the PR, and its row in the [security model](../security-model.md); the spec's own
*When it lands* section says where the row moves.

## Naming

A spec is named for its shape. No dates: a spec is edited as the work moves, not appended to.

| Shape | File | Prose |
| --- | --- | --- |
| One spec | `docs/specs/<topic>.md` | — |
| Steps built one after another | `docs/specs/<topic>/<n>-<slug>.md` | step 3 |
| Waves of steps built in parallel | `docs/specs/<topic>/<n><a>-<slug>.md` | step 3B |

**A sequence** — the second and third shapes — has a `docs/specs/<topic>/README.md` naming its
steps, and shares one `wip/<topic>`: the README and each step's spec merge into it as they are
accepted, and it lands once the last step is built. In the third shape, `<n>` is the wave: every
step in one wave can be built at the same time, and a step needs only steps of earlier waves. The
letter orders a wave's steps for reading, not for building. Use it only when a sequence has a wave
of more than one step.

Branches on a fork are the contributor's to name. We suggest the spec's own name: `<topic>`,
`<topic>-<n>` or `<topic>-<n><a>`.

## Working a numbered spec

The numbered specs share these rules, so each states only what is its own.

Implement a spec after its stated prerequisites have landed. The numbered spec is the unit of
integration and retirement; its tasks are smaller assignments that run in the listed order.
Assign one task at a time with the spec, the applicable `CLAUDE.md`, and the current code.
All tasks start Planned; record their status and implementation commit/PR in the spec as the work
moves.
Each handoff reports changed files, acceptance cases, verification results and remaining failures.

Keep dependent cutover work on `wip/<topic>` until its integration gate passes. Supporting
work should compile and pass focused checks; do not merge an incomplete schema/service cutover.
Paths in task lists are relative to `sidecar/internal/` unless explicitly rooted elsewhere.

When complete, update current-state documentation and ADRs as the spec's *When it lands* lists,
then delete **that spec only** and remove its row from the sequence's README in the same change. Keep later spec numbers
stable: their prerequisites are delivered code and current-state docs, never a retired spec file.
A spec that later specs of its sequence cite stays, marked built, until the last of them is
complete, and is deleted with it.

### Verification commands

Before any build/test/install in the Linux sandbox run `bash scripts/sandbox-dev-setup.sh`, per
root `CLAUDE.md`. All commands below run from the repository root unless explicitly prefixed.

- **Go checks:** `make test-go`, `make lint-go`, `make vet-go`. During development use focused
  `cd sidecar && go test ./internal/<affected-package>`; broaden before handing off the task.
- **Wire checks:** `cd sidecar && go tool gqlgen generate`, then from root
  `pnpm codegen`, `pnpm build`, `make test-ts`, `make lint-ts`. Never hand-edit generated files.
- **Full checks at a spec's integration gate:** `make test`, `make lint`, `make vet`, and green
  `codecov/patch` and `codecov/project` on the PR. Include codegen/build checks when wire changed.

## Index

Proposed: the open spec PRs, [`is:pr is:open "📜 Spec:" in:title`](https://github.com/kubetail-org/kubetail/pulls?q=is%3Apr+is%3Aopen+%22%F0%9F%93%9C+Spec%3A%22+in%3Atitle).
In progress: the open PRs from a `wip/<topic>` branch into `main`.
