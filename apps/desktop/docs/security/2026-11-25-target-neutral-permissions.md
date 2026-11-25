# Security record — one permissions engine for every target, 25 November 2026

**Subject:** the permissions engine decides every sandboxed command's request, to a cluster or a
cloud platform, over one `permissions.Action`: an access level, a boundary per principal, the
account's mode and the user's rules. Modes and rules are rows of `permission_modes` and
`permission_rules`, keyed by target and account (migration `0006`). The decision is
[ADR: decide every target's request by access level, a boundary, a mode and rules over one action](../adr/2026-11-25-permissions-are-access-levels-a-boundary-modes-and-rules-over-one-action.md).
The living model is [security-model.md](../security-model.md).

## What it changes

- **A rule and a mode now allow a cloud platform's request, and an answer may grant one.** Before,
  each cloud change asked, Once or Deny. Now `auto` or an `allow` rule runs a change unasked, and
  a Chat or Always answer writes a rule for the account and level, within what the platform's
  `ordinary` table names. A change off the table asks each time, as before, and nothing allows
  it.
- **A cloud data read runs unasked under `auto`**, as a Secret read does on a cluster: an S3
  object's contents, a key vault's secret, a repository's content behind `dataPaths`. Under `ask`
  and `read-only` it asks.
- **A change to who may do what is its own level, `access-control`.** On a cluster an `allow` rule
  inside a namespace reaches its Roles and RoleBindings, `kube-system`'s included: a user who
  allows `access-control` in `kube-system` lets a command grant itself what any service account
  there holds. Across a cluster (ClusterRoles, webhooks, a CSR approval) it asks each time. On a
  cloud platform it asks each time, but for an Azure role assignment in a resource group.
- **A write to a Namespace object asks each time, a create included.** A namespace's rule
  covers what is in the namespace, never the Namespace object, and no rule names one object.
- **The monitor and the console are rows of the boundary**, not flags on a session: they read
  and nothing else, and never ask. A narrowed monitor stays a monitor. A subagent is never wider
  than its chat, and no `allow` names a subagent.
- **An operation no table names asks each time.** Kubernetes names the built-in resources and
  subresources (`builtin`, pinned against client-go's scheme); a custom resource is named with
  its `status` and `scale` alone. Each cloud platform names its `ordinary` changes.
- **An account the classifier cannot trust asks each time**: AWS credentials with no account
  id, a GitHub route by id, a Google project by number or `-`, a request whose body names another
  account, subscription or project, a body that is not JSON.
- **A dry run the API server honors is forwarded unrecorded**, as a read is.

## What holds

- **The boundary is Kubetail's, and no mode or rule lifts it.** `reach` is never allowed
  (`TestReachIsNeverAllowed`, each proxy's `TestEachCredentialRefusalIsReach`, `TestEachRefusalIsReach`);
  a destructive change always asks; every principal and mode has a row for every level
  (`TestEveryPrincipalHasEveryLevel`, `TestEveryModeHasEveryLevel`).
- **The verdict is the spec's ordered table** over every principal, level, mode, limit and rule
  set, whatever the rules' order (`TestTheVerdictMatchesTheOrderedTable`,
  `TestAVerdictIsOrderIndependent`, `TestASubagentIsNeverWiderThanItsParent`).
- **Nothing is dropped before the decision**: the action recorded is the one decided and the one
  the user saw (each proxy's `TestNoProxyDropsAFieldBeforeTheDecision`, `TestDecideKeepsEveryField`).
- **A stored rule is one Kubetail can apply.** `settings.CheckRule` refuses on write what no rule
  may hold, and a row it refuses reads as a refusal of every write and data read
  (`TestTheGoCheckRefusesWhatNoRuleMayHold`, `TestARowGoCannotReadDenies`).
- **A rule never reaches across a segment.** A scope's `*` matches one segment; an `allow` names a
  literal account (`TestAScopePatternNeverCrossesASegment`,
  `TestATrailingDoubleStarCoversTheAccountAndBelow`).
- **A cluster's modes and rules go with it**, by a trigger that compares the scope with `substr`,
  never `LIKE` (`TestMigration0006AddsTheSettingsTables`).

## What `ordinary` rests on

Each row lets the user's mode or rule run a change unasked. The rows were picked as changes that
can be undone and grant no access, but two of them reach further than their own object:

- **A tag** (EC2, Elastic Load Balancing, Resource Manager's `tags`, a Google instance's labels)
  can change an attribute-based policy's answer: an IAM or Azure condition that reads a tag
  grants or withholds access by it.
- **A comment** on an issue or a pull request can trigger automation that reads it: a workflow
  on `issue_comment`, a bot that acts on a command in a comment.

A user who sets `auto` on an account, or allows writes there, accepts these.

## Residual risk

- **`ordinary` and `builtin` are kept by hand.** A vendor's new operation is unnamed, and asks,
  until a review adds it; one a row names that changes meaning upstream keeps its row. Pinning
  each cloud table against its vendor's published model is its own follow-up.
- **A namespace's `access-control` rule reaches that namespace's service accounts**, so the grant is
  as wide as what they hold.
- **A rule Kubetail cannot read denies every write and data read until it is removed.** Settings
  and the chat's list draw it as a deny of every write, under the row's id, so removing it there
  removes the row.
- **AWS credentials that carry no account id** (static keys) never reach a mode or a rule: each
  change asks.
