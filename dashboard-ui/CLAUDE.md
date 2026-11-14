# dashboard-ui — React single-page app

React 19 + React Router 8 + Apollo Client 4 (GraphQL) + Jotai + Vite + Tailwind v4. UI primitives from `@kubetail/ui`. Package manager is **pnpm**; never `npm` or `npx`. Path alias: **`@/*` → `src/*`**. The React compiler is off.

**The Go server does not serve `index.html`.** In production the Dashboard backend renders `modules/dashboard/templates/index.tmpl` and takes the script, style and font tags from `.vite/manifest.json`, so `build.manifest: true` in `vite.config.ts` is load-bearing. A change to `index.html`'s `<head>`, title or inline theme script is repeated in `index.tmpl`. → [ADR: embed the UI by a copy step and render from the Vite manifest](../docs/adr/2026-11-09-embed-the-ui-by-a-copy-step-and-render-from-the-vite-manifest.md)

## Checks

```sh
pnpm format         # oxfmt: formats and sorts imports
pnpm lint           # oxlint (type-aware), then `oxfmt --check`. A warning fails the run.
pnpm build          # tsc -b && vite build. `tsc -b` (`make vet-ts`) is the only type-check, and it covers test files
pnpm test run       # `pnpm test` alone is watch mode and never exits
```

## Layout

- **`src/routes.tsx` holds every route; `src/main.tsx` the provider stack.** Pages are in `src/pages/<page>/` (`home`, `console`, `auth`), reusable widgets in `src/components/widgets/`, app-wide modules in `src/lib/`.
- **Files under `components/` are PascalCase.** Files under `pages/`, `lib/` and `log-viewer/` are kebab-case with named exports, except a page's route component, which is its file's default export. New files outside `components/` follow that.

## Runtime config, environment and base path

- **`appConfig` (`src/app-config.ts`) is `window.runtimeConfig`**, which the server writes into the page (`modules/dashboard/pkg/app/website.go`). `pnpm dev` has no server-rendered page, so the app runs on the defaults in that file (`desktop`, base path `/`) whatever the backend's config says; a `VITE_` variable listed in `ENV_KEYS` overrides one, parsed to the default's type (a boolean is `true` or `false`, a list is comma-separated).
- **`appConfig.environment` is `desktop` or `cluster`.** In `desktop`, `KubeConfigEffect` holds the one `KUBE_CONFIG_WATCH` subscription and preferences are stored on the server. In `cluster` the subscription is skipped, the kube context is `''`, and preferences live in localStorage alone.
- **Several modules read `appConfig` when they load** (`lib/kubeconfig.tsx`, `lib/preferences.tsx`, `pages/console/state.ts`, `apollo-client.ts`). `withAppConfig` in a test cannot change what they already read.
- **Build every URL to the server with `getBasename()` and `joinPaths`** (`src/lib/util.ts`), as `getSession` does. The app can be served under a base path or behind the kube-apiserver's service proxy. Router links get the basename from the router; a raw `<a href>` or `fetch` adds it by hand.
- **`pnpm dev` proxies `/api/`, `/graphql`, `/cluster-api-proxy/` and `/healthz`** to `localhost:4000` (`vite.config.ts`). A new backend route prefix needs an entry there.

## GraphQL

- **There are two Apollo clients** (`src/apollo-client.ts`). `dashboardClient` is the provider's default. `getClusterAPIClient(kubeContext)` returns one client per kube context for the Cluster API, and is chosen in two places only: the console (`pages/console/index.tsx`) and `pages/home/log-metadata-provider.tsx`.
- **`useIsClusterAPIEnabled(kubeContext)` decides which one**, and returns `undefined` while it does not know yet. The console mounts no viewer until it answers.
- **Queries and mutations go over HTTP, subscriptions over WebSocket** (`graphql-ws`). The HTTP chain is error link → retry link → CSRF link → HTTP link; subscriptions skip all three, so nothing global handles a subscription's errors: the code that subscribes does.
- **The CSRF token is a module variable in `src/lib/auth.tsx`, never in storage.** It comes from the `X-CSRF-Token` response header of `GET /api/auth/session`. It goes out as the `X-CSRF-Token` header on HTTP, as `csrfToken` in the WebSocket's `connectionParams`, and as a hidden `csrfToken` field in the download form. A new way of calling the server carries it too.
- **Operations live in `src/lib/graphql/<server>/ops.ts`, fragments in `fragments.ts`, and nowhere else.** Codegen globs those directories only, so a document defined in a component gets no types.
- **Write them with the generated `gql()` function** from `./__generated__/gql`, called on a template string: ``gql(`…`)``. Not Apollo's `gql` tag. Fragment masking is off: read a fragment's fields directly.
- **A constant is `SCREAMING_SNAKE`.** One that belongs to a feature is prefixed by it (`HOME_`, `CONSOLE_`, `SOURCE_PICKER_`) and suffixed by its kind (`_FETCH`, `_GET`, `_FIND`, `_WATCH`, `_FOLLOW`); the operation name is its PascalCase.
- **`pnpm graphql-codegen` regenerates `__generated__/`** from the two schemas under `../modules/`. Run it after changing `ops.ts`, `fragments.ts` or either schema, and commit the result. **Never edit `__generated__/`.** The two presets in `codegen.ts` override the same four defaults; keep them in step. → [ADR: override the codegen client preset's v6 defaults](../docs/adr/2026-11-09-override-the-codegen-client-presets-v6-defaults.md)
- **The console's log operations are written once, in `dashboard/ops.ts`, and sent to either server.** Codegen checks them against the Dashboard's schema alone.
- **Custom scalars arrive as `any` and are converted by the cache.** `dateField` and `bigIntField` in `apollo-client.ts` turn named fields into `Date` and `bigint`. A new time or int64 field needs a type policy there, or it is a string. Read a scalar with `as` to the type it has at run time.
- **Kubernetes lists are a query plus a watch, through the shared hooks** in `src/lib/hooks.ts`: `useListQueryWithSubscription`, `useCounterQueryWithSubscription`. Don't pair `useQuery` and `useSubscription` by hand. A paginated list field needs `k8sPagination()` in `DashboardCustomCache`.

## State

- **URL search params hold what a link should reproduce**: the console's `kubeContext`, `source`, `grep`, `mode`, `cursor` and its filters, written with `setSearchParams`. Changing `source` is the exception: it reloads the page.
- **Jotai holds page state and derived data**: a page's atoms in a `state.ts` beside it, app-wide ones in `lib/`. There is no Jotai `Provider`: atoms are module-global and outlive a route change. An atom's name ends in `Atom`, a family's in `AtomFamily`.
- **A fetcher component writes an atom, views read the atom.** `WorkloadDataFetcher`, `LogMetadataProvider`, `SourcesFetcher` and `KubeConfigEffect` render nothing: they run the Apollo hooks and set atoms.
- **User settings go through `usePreferences`** (`src/lib/preferences.tsx`) and its wrappers `useTheme`, `useTimezone`, `useTimestampFormat`. The localStorage key `kubetail:preferences` is also read by the inline script in `index.html` and `index.tmpl` before React loads; keep its shape in step with both.
- **Shared accessors, not raw calls**: `useSession` for the session, `useKubeConfig` for the kubeconfig, `useDashboardServerStatus` and its siblings for health.
- **A page checks the session by wrapping itself in `AuthRequired`.**

## The log viewer

`src/components/widgets/log-viewer/` is headless: it imports only React, `@tanstack/react-virtual` and `cn`, and exports through `index.ts`.

- **`types.ts` is the contract.** The console's `Client` is `LogServerClient` (`pages/console/log-server-client.ts`), whose cursor is a record's timestamp.
- **A new `client` object resets the viewer.** `LogViewer` remounts its inner component when the prop's identity changes, so `pages/console/index.tsx` memoizes the client on the kube context, sources, filters and grep, and keeps those stable with deep-compared values.
- **Row keys are stable**: records sit in a `DoubleTailedArray`, so prepending older lines moves no existing key.
- **`RecordRow` (`pages/console/main.tsx`) is `memo` with a hand-written comparator.** A new prop is added to the comparator, or the row does not re-render for it.

## Security invariants

What an ordinary change here can break, beside the root guide's list:

- **Log text and Kubernetes names are attacker-controlled.** Outside tests, `src/` has no `dangerouslySetInnerHTML`, `innerHTML` or `eval`. A log message reaches the DOM in one place, `<AnsiHtml text={record.message} />` from `fancy-ansi` (`pages/console/main.tsx`); every other column is a React text child. Copy and width measurement use `stripAnsi`.
- **A name that becomes part of a CSS identifier goes through `cssEncode`** (`src/lib/util.ts`).
- **The Kubernetes token a user types at login is posted once and never stored client-side.** The session is the server's cookie.
- **Every state-changing request carries the CSRF token** (*GraphQL*).

## Styling

- Tailwind utilities inline in `className`; `src/index.css` is the Tailwind config. Merge classes with **`cn(...)`** from `@/lib/util` and nothing else.
- **Primitives come from `@kubetail/ui/elements/<name>`**, by deep import. Don't import `@base-ui/react` directly.
- Colour with the semantic tokens (`bg-background`, `text-muted-foreground`), not raw palette classes. Dark mode is the `dark` class on `<html>`.
- Icons are `lucide-react`; the Kubernetes workload glyphs are local SVGs imported as components with `?react`.

## Tests

Vitest + `@testing-library/react` (jsdom), co-located as `foo.test.ts[x]`. Globals are on: `describe`, `it`, `expect` and `vi` need no import.

- **`renderElement(element, mocks?)`** from `@/test-utils` wraps a component in a fresh Jotai store, Apollo's `MockedProvider` with the real cache, and a `MemoryRouter`. `mocks` are `MockLink.MockedResponse[]` built from the real documents in `ops.ts`; results carry `__typename`; `delay: Infinity` holds a request pending.
- **`withAppConfig(overrides, fn)`** runs `fn` with `appConfig` patched and restores it.
- **`vitest.setup.ts` mocks for every test**: `useSession` (default: signed out, so `AuthRequired` redirects), the CSRF token functions, and `useIsClusterAPIEnabled` (default: `true`). A test of a signed-in page sets `(useSession as Mock).mockReturnValue({ session: { user: 'test' } })`.
- **`vi.resetAllMocks()` runs after every test**, so a `mockReturnValue` or `mockImplementation` belongs in `beforeEach`: set at module scope it lasts one test.
- **To wait, use `await waitFor(...)` or fake timers** (`vi.useFakeTimers()` with `vi.advanceTimersByTimeAsync`). `requestAnimationFrame` is a zero-delay timer in tests, so code that schedules a frame is driven the same way.
- **Log viewer doubles**: `createMockClient` and `createMockLogViewerHandle` in `src/mocks/log-viewer.ts`. `FakeClient` (`log-viewer/fake-client.ts`) streams generated records for trying the viewer by hand; the barrel does not export it.

## Lint conventions

`.oxlintrc.json` is the lint: oxlint's `correctness`, `suspicious` and `perf` categories plus a chosen list of rules. A rule turned off or given options says why in a comment. → [ADR: lint the dashboard UI with oxlint's categories and a chosen rule list](../docs/adr/2026-11-09-lint-the-dashboard-ui-with-oxlints-categories-and-a-chosen-rule-list.md)

- **Imports are grouped, a blank line between groups**: third-party packages, `@kubetail/*`, `@/…`, relative `./…`, then stylesheets. `pnpm format` sorts them (`sortImports` in `.oxfmtrc.json`).
- **No `any` outside tests** (`no-explicit-any` and the `no-unsafe-*` rules). Read JSON and other outside data as `unknown` and check it, as `parseSession` in `lib/auth.tsx` does. Tests turn the `no-unsafe-*` rules off, since a mock's calls are `any`. → [ADR: forbid any in the dashboard UI outside tests](../docs/adr/2026-11-09-forbid-any-in-the-dashboard-ui-outside-tests.md)
- **A disable comment is `oxlint-disable`, names its rule, gives a reason after `--`, and is rare**; an unused one fails the lint. `eslint-disable` comments are ignored.
- **TypeScript is `strict` with `noUnusedLocals` and `noUnusedParameters`.**
- **A new dependency must be at least ten days old** (`minimumReleaseAge` in `pnpm-workspace.yaml`); `@kubetail/ui` is exempt.
- **A dependency must declare every package it imports.** The sandbox installs with pnpm's global virtual store, where an undeclared import fails with `Cannot find package` while the host and CI pass. Take a release that declares it, or add a `packageExtensions` entry. Types are covered: `tsconfig.app.json` maps `react` to `@types/react`. → [ADR: keep the sandbox's build outputs off the host mount](../docs/adr/2026-11-09-keep-the-sandboxs-build-outputs-off-the-host-mount.md)
