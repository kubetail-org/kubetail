---
title: Embed the UI by a copy step and render from the Vite manifest
date: 2026-11-09
scope: cross-cutting
status: Accepted
---

# Embed the UI by a copy step and render from the Vite manifest

## Context

The decision was made in two steps. In the first commit (`43df48a9`, February 2024) the Go server served the built UI from a `website/` directory beside the binary: `index.html` for `/` and every unknown route, and `/assets` as static files. The Docker image copied the Vite `dist` into that directory.

In March 2024, #28 (`540b0cfa`) added the `base-path` option, so Kubetail could run under a sub-path behind a reverse proxy. Its description lists the changes that carried it: the server renders the page from a template at run time, takes the asset files from the manifest Vite writes at build time, and passes config to the app as a `runtimeConfig` object written into the page. `build.manifest: true` went into `vite.config.ts` in the same PR. Vite writes asset URLs into `index.html` at build time, and the base path is a runtime setting, so the built `index.html` could not be served as it was.

In October 2024, #120 (`41485e8d`) added the CLI with the Dashboard running inside it. It added `embed.go` with `//go:embed templates/*` and `//go:embed website/*`, the Makefile's `build-dashboard-ui` target that copies `dist` into the Go module, and the `.gitignore` rules that keep only `website/.gitkeep` in git. The PR gives no reason for embedding beyond its title, "Adds CLI with embedded server". `//go:embed` accepts no `..` path element and no symlink, so the files have to sit inside the Dashboard module's own directory.

## Decision

`modules/dashboard/embed.go` embeds `templates/`, `static/` and `website/`. `website/` holds a copy of `dashboard-ui/dist`, made before any Go build that ships the UI: `make build-dashboard-ui`, the `COPY --from=frontend-builder` in `build/package/Dockerfile.dashboard`, the `mv` in `release-cli.yml` before `go mod vendor`, and the `kubetail-dashboard-compile` resource in the `Tiltfile`. Only `website/.gitkeep` is committed, so the embed pattern matches in a fresh checkout. The CLI gets the UI by importing `dashboard/pkg/app`.

The Go server never serves `index.html`. `websiteHandlers.EndpointHandler` (`modules/dashboard/pkg/app/website.go`) reads `.vite/manifest.json` once, marshals `runtimeConfig` (`authMode`, `basePath`, `cliVersion`, `environment`, `clusterAPIEnabled`, `columns`), and renders `templates/index.tmpl` for `GET` and `HEAD` on `/` and every unmatched route. The template emits the entry script, its `modulepreload` imports, its stylesheets and a preload for each `.woff2` font from the manifest, each path joined to `BasePath`. `InitStaticHandlers` serves the favicons and `/assets` from the embedded files, with a one-year immutable `Cache-Control` on `/assets`.

## Alternatives considered

**Serve the built `index.html` as a static file.** This was the server's behaviour until #28. It lost because the asset URLs in it are fixed at build time (`vite.config.ts` sets no `base`), and it gives the server no place to write `runtimeConfig`.

**Serve the UI from a directory on disk.** Also the behaviour until #120. A Dashboard binary that runs on a user's machine from `kubetail serve` then needs a second artifact beside it. #120 records no other reason for the change.

**Embed `dashboard-ui/dist` in place.** Not recorded as considered. `//go:embed` cannot reach outside the module's directory or follow a symlink, so the copy is the way in.

## Consequences

One Go binary carries the UI, and one built UI serves any base path and any config. The page's config is per process; the assets are fixed when the Go binary is built.

`pnpm build` alone changes nothing a Go binary serves. The copy is written out four times (Makefile, Dockerfile, release workflow, Tiltfile), and a fifth build path has to add it. A Go build without the copy compiles: the endpoint answers 404 `website not found`.

The page's `<head>` exists twice, in `dashboard-ui/index.html` for `pnpm dev` and in `index.tmpl` for every Go build, and nothing checks that they agree. They already differ: `index.html` links `/favicon.svg` and `index.tmpl` links `favicon.ico`. The localStorage key `kubetail:preferences` is read by the inline theme script in both.

`build.manifest: true` is load-bearing; without it every page is a 404. `pnpm dev` has no rendered page, so the app runs on the defaults in `src/app-config.ts`.

## Revisit when

The base path and the app's config no longer vary per process. The built `index.html` could then be served as it is, and the template dropped.
