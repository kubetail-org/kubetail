---
title: A sandboxed command reaches GitHub over plain HTTP at gh's own host name
date: 2026-11-17
scope: sidecar
status: Accepted
---

# A sandboxed command reaches GitHub over plain HTTP at gh's own host name

## Context

[The run's door](2026-11-17-a-sandboxed-command-reaches-a-cloud-platform-through-a-proxy-on-its-runs-one-relay.md)
gives each cloud platform a route, a grant and a gate, and builds none. GitHub's step
([#932](https://github.com/kstackhq/kstack/issues/932)) needs `git` and `gh` to read the user's
repositories from a sandboxed run while the sandbox holds no token: `~/.config/gh`,
`~/.git-credentials` and `~/.netrc` are denied always, and `neverEnv` refuses `GH_TOKEN` and
`GITHUB_TOKEN`. `gh` verifies TLS through the system's verifier and reads no certificate file, so
it cannot trust a per-run certificate, and it speaks plain HTTP to one host name alone,
`github.localhost`.

## Decision

- **Plain HTTP at `gh`'s host name.** The routes are `github.localhost` (Git's smart HTTP to
  `https://github.com`) and `api.github.localhost` (REST and GraphQL to `https://api.github.com`).
  The run's `gh` gets `GH_HOST=github.localhost` and a `hosts.yml` whose login is a placeholder;
  its `git` gets a `gitconfig`, named through `GIT_CONFIG_COUNT` and `include.path`, that rewrites
  every `github.com` remote, SSH included, to the route and names the door as its proxy.
- **The grant's token rides as proxy credentials**, `Basic` with the user `kubetail`. `git` sends
  them only once challenged, so GitHub's grant answers `407`.
- **`HTTP_PROXY` carries `gh` to the door**, since `gh` has no proxy setting of its own.
- **A repository's content is a read.** GitHub serves it from Git's protocol, dozens of REST routes
  and GraphQL fields, so no list names it whole. What asks is a change, a push, and a read of
  stored data or of metadata GitHub documents as holding a secret (`dataPaths`); a request whose
  answer is a credential is refused (`credentialPaths`). Both lists read a path by id as its form
  by name, since GitHub serves `/repositories/{id}` and `/organizations/{id}` too.
- **GraphQL is a change if any mutation is in the document**, whatever `operationName` selects.
- **No repository allowlist.** GraphQL names a repository inside the document, by name or node id.
- **No redirect is followed.** A link to the upstream's own origin, in `Location` or `Link`, is
  handed back as the route's; one to another host passes only on an approved data read.
- **The token comes from `gh auth token` alone** (`services/githubauth`), run outside the sandbox,
  held five minutes; `gh` runs at most once a minute.

## Alternatives considered

- **A `<name>.github.kubetail.invalid` host**, step 1's naming rule. `gh` would dial it over HTTPS.
- **A `gh` wrapper on `PATH`** that sets the proxy for `gh` alone. `PATH` is frozen and filtered
  by design, and a Kubetail-written folder on it is a new thing every run would search.
- **A repository allowlist.** It holds for Git and REST, which name the repository in the path, but
  not for GraphQL. A user who wants a narrower reach logs `gh` in with a fine-grained token.
- **A gate on reading repository content.** A prompt on every fetch would be approved unread, and
  a gate on some routes would be drawn as a boundary and not be one.
- **Following GitHub's redirects from the sidecar.** It would make logs and release assets work
  with network off, and make the proxy a path to GitHub's download hosts.
- **`git credential fill` for a user with no `gh`.** It runs the user's credential helper outside
  the sandbox.

## Consequences

- With GitHub's switch on, a sandboxed command reads what the user's token reads, private
  repositories included, and the answer reaches the model's provider.
- `HTTP_PROXY` hands the grant's token to every Go and Python program in the run. It opens
  GitHub's grant alone.
- A push is approved on its repository, refs and options; its pack is not shown.
- `credentialPaths` and `dataPaths` are kept by hand: a route GitHub adds whose answer is a
  credential or a secret is a read until it joins one.
- Workflow logs, artifacts, archives and release assets ask first and then need network, and
  `gh release download` and `gh run download` do not work.
- A redirect on GitHub's own origin comes back through the proxy as a new request, so an approved
  request GitHub redirects there asks again.

## Revisit when

`gh` trusts a certificate file, or GitHub's GraphQL schema serves stored data or a secret that
`dataPaths` names.
