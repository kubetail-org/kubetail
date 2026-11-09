# dashboard-ui — React single-page app

React 19 + React Router 7 + Apollo Client 4 (GraphQL) + Jotai + Vite + Tailwind v4. UI primitives from `@kubetail/ui`. Package manager is **pnpm**, pinned in `package.json`; never `npm` or `npx`. Path alias: **`@/*` → `src/*`**. The React compiler is off. The root [`CLAUDE.md`](../CLAUDE.md) holds what crosses areas: the request path, the GraphQL contracts, the writing standards and the testing conventions.

**The Go server does not serve `index.html`.** In production the Dashboard backend renders `modules/dashboard/templates/index.tmpl` and takes the script, style and font tags from `.vite/manifest.json`, so `build.manifest: true` in `vite.config.ts` is load-bearing. A change to `index.html`'s `<head>`, title or inline theme script is repeated in `index.tmpl`. `pnpm build` alone changes nothing a Go binary serves: `make build-dashboard-ui` copies `dist` to `modules/dashboard/website/`.

## Layout

| Path | Holds |
| --- | --- |
| `src/main.tsx` | Entry point and the provider stack: `SessionProvider` → `ApolloProvider` (`dashboardClient`) → `PreferencesProvider` → `UpdateNotificationProvider` → `RouterProvider`, with `ThemeEffect` and `KubeConfigEffect` mounted inside `PreferencesProvider`, beside `UpdateNotificationProvider`. |
| `src/routes.tsx` | Every route, as JSX `<Route>` elements: `/` (home), `/console`, `/auth/login`, `/auth/logout`, under `pages/_root.tsx`. No lazy loading, no loaders. |
| `src/apollo-client.ts` | Both Apollo clients, the link chain, the cache type policies. |
| `src/app-config.ts` | `appConfig`, the runtime config the server injects. |
| `src/pages/home/` | The workload list. `state.ts` holds its atoms; `workload-data-provider.tsx` and `log-metadata-provider.tsx` fetch into them. |
| `src/pages/console/` | The log console: `index.tsx` (page, builds the log client), `main.tsx` (rows and columns), `header.tsx`, `sidebar.tsx`, `selection.ts`, `download.tsx`, `log-server-client.ts`, `state.ts`. |
| `src/pages/auth/` | The token login form and logout. |
| `src/components/widgets/` | Reusable widgets. `log-viewer/` is the headless virtualized viewer. |
| `src/components/layouts/`, `utils/` | `AppLayout`, `ModalLayout`; `AuthRequired`, `LoadingPage`. |
| `src/lib/` | App-wide modules: `auth.tsx`, `hooks.ts`, `kubeconfig.tsx`, `preferences.tsx`, `theme.tsx`, `server-status.ts`, `update-notifications.tsx`, `util.ts`. |
| `src/lib/graphql/{dashboard,cluster-api}/` | `ops.ts`, `fragments.ts` and the generated `__generated__/`. |
| `src/test-utils.tsx`, `vitest.setup.ts`, `src/mocks/` | Test support. See *Tests*. |

Files under `components/` are PascalCase. Files under `pages/`, `lib/` and `log-viewer/` are kebab-case with named exports, except a page's route component, which is its file's default export. New files outside `components/` follow that.

## Runtime config, environment and base path

- **`appConfig` (`src/app-config.ts`) is `window.runtimeConfig`**, which the server writes into the page (`modules/dashboard/pkg/app/website.go`): `authMode`, `basePath`, `cliVersion`, `environment`, `clusterAPIEnabled`, `columns`. `pnpm dev` has no server-rendered page, so the app runs on the defaults in that file (`desktop`, base path `/`) whatever the backend's config says; a `VITE_<KEY>` variable overrides one.
- **`appConfig.environment` is `desktop` or `cluster`** (root guide). In `desktop`, `KubeConfigEffect` holds the one `KUBE_CONFIG_WATCH` subscription and preferences are stored on the server. In `cluster` the subscription is skipped, the kube context is `''`, and preferences live in localStorage alone.
- **Several modules read `appConfig` when they load** (`lib/kubeconfig.tsx`, `lib/preferences.tsx`, `pages/console/state.ts`, `apollo-client.ts`). `withAppConfig` in a test cannot change what they already read.
- **Build every URL to the server with `getBasename()` and `joinPaths`** (`src/lib/util.ts`), as `getSession` does. The app can be served under a base path or behind the kube-apiserver's service proxy, where the basename is everything up to `/proxy/`. Router links get the basename from the router; a raw `<a href>` or `fetch` adds it by hand.
- **`pnpm dev` proxies `/api/`, `/graphql`, `/cluster-api-proxy/` and `/healthz`** to `localhost:4000` (`vite.config.ts`, `VITE_DASHBOARD_PROXY_PORT` in `.env`). A new backend route prefix needs an entry there.

## GraphQL

**There are two Apollo clients** (`src/apollo-client.ts`). `dashboardClient` is the provider's default and talks to the Dashboard backend. `getClusterAPIClient(kubeContext)` returns one client per kube context for the Cluster API, reached at `clusterAPIProxyPath` (`<basename>/cluster-api-proxy`, plus `/<kubeContext>` in `desktop`). The Cluster API client is chosen in two places only: the console (`pages/console/index.tsx`) and `pages/home/log-metadata-provider.tsx`.

**`useIsClusterAPIEnabled(kubeContext)` decides which one**, and returns `undefined` while it does not know yet (`src/lib/hooks.ts`). The console mounts no viewer until it answers.

**Queries and mutations go over HTTP, subscriptions over WebSocket** (`graphql-ws`). The HTTP chain is error link → retry link → CSRF link → HTTP link; subscriptions skip all three.

- **The CSRF token is a module variable in `src/lib/auth.tsx`, never in storage.** It comes from the `X-CSRF-Token` response header of `GET /api/auth/session`. It goes out as the `X-CSRF-Token` header on HTTP, as `csrfToken` in the WebSocket's `connectionParams`, and as a hidden `csrfToken` field in the download form. On the Apollo HTTP link a 403 resets it and retries. A new way of calling the server carries it too.
- **Both links retry forever.** The WebSocket's `retryWait` waits for the tab to be visible and online and for `healthz` to answer.
- **GraphQL errors on HTTP raise a toast**, one per field path, and a network error being retried shows one per operation. Nothing global handles a subscription's errors: the code that subscribes does.

**Operations live in `src/lib/graphql/<server>/ops.ts`, fragments in `fragments.ts`, and nowhere else.** Codegen globs those directories only, so a document defined in a component gets no types.

- Write them with the generated `gql()` function from `./__generated__/gql`, called on a template string: ``gql(`…`)``. Not Apollo's `gql` tag.
- A constant is `SCREAMING_SNAKE`. One that belongs to a feature is prefixed by it (`HOME_`, `CONSOLE_`, `SOURCE_PICKER_`) and suffixed by its kind (`_FETCH`, `_GET`, `_FIND`, `_WATCH`, `_FOLLOW`); the operation name is its PascalCase.
- Fragment masking is off: read a fragment's fields directly.
- **`pnpm graphql-codegen` regenerates `__generated__/`** from the two schemas under `../modules/`. Run it after changing `ops.ts`, `fragments.ts` or either schema, and commit the result. **Never edit `__generated__/`.**
- **The console's log operations are written once, in `dashboard/ops.ts`, and sent to either server** (root guide, *The GraphQL contracts*). Codegen checks them against the Dashboard's schema alone.

**Custom scalars arrive as `any` and are converted by the cache.** `dateField` and `bigIntField` in `apollo-client.ts` turn named fields into `Date` and `bigint` in a `read` function. A new time or int64 field needs a type policy there, or it is a string.

**Kubernetes lists are a query plus a watch, through the shared hooks** in `src/lib/hooks.ts`: `useListQueryWithSubscription` (pages through `continue`, then watches from the list's `resourceVersion`), `useGetQueryWithSubscription`, `useCounterQueryWithSubscription`. Use them; don't pair `useQuery` and `useSubscription` by hand. A paginated list field needs `k8sPagination()` in `DashboardCustomCache`.

## State

- **URL search params hold what a link should reproduce.** The console's `kubeContext`, `source`, `grep`, `mode`, `cursor` and its filters are search params, written with `setSearchParams`. Changing `source` is the exception: it reloads the page.
- **Jotai holds page state and derived data**: a page's atoms in a `state.ts` beside it, app-wide ones in `lib/`. There is no Jotai `Provider`: atoms are module-global and outlive a route change. An atom's name ends in `Atom`, a family's in `AtomFamily`.
- **A fetcher component writes an atom, views read the atom.** `WorkloadDataFetcher`, `LogMetadataProvider`, `SourcesFetcher` and `KubeConfigEffect` render nothing: they run the Apollo hooks and set atoms.
- **User settings go through `usePreferences`** (`src/lib/preferences.tsx`) and its wrappers `useTheme`, `useTimezone`, `useTimestampFormat`. The localStorage key `kubetail:preferences` is also read by the inline script in `index.html` and `index.tmpl` before React loads; keep its shape in step with both.
- **Shared accessors, not raw calls**: `useSession` for the session, `useKubeConfig` for the kubeconfig, `useDashboardServerStatus` and its siblings for health.
- **A page checks the session by wrapping itself in `AuthRequired`.**

## The log viewer

`src/components/widgets/log-viewer/` is headless: outside its test doubles it imports only React, `@tanstack/react-virtual` and `cn`, and exports through `index.ts`.

- **`types.ts` is the contract.** A `Client` has `fetchSince`, `fetchUntil`, `fetchAfter`, `fetchBefore` (each a page of records plus `nextCursor`, `null` when there is no more that way) and `subscribe`. The console's implementation is `LogServerClient` (`pages/console/log-server-client.ts`), whose cursor is a record's timestamp.
- **A new `client` object resets the viewer.** `LogViewer` remounts its inner component when the prop's identity changes, so `pages/console/index.tsx` memoizes the client on the kube context, sources, filters and grep, and keeps those stable with deep-compared values.
- **Records sit in a `DoubleTailedArray`** (`double-tailed-array.ts`): constant-time append and prepend with stable integer keys, so prepending older lines moves no existing key.
- **Scroll corrections run before paint.** `useBeforePaint` queues callbacks for a layout effect; loading older lines adjusts `scrollTop` by the height it added.
- **Followed records are appended once per animation frame.**
- **`RecordRow` (`pages/console/main.tsx`) is `memo` with a hand-written comparator.** A new prop is added to the comparator, or the row does not re-render for it.

## Security invariants

The whole picture is the root guide's *Security invariants*. What an ordinary change here can break:

- **Log text and Kubernetes names are attacker-controlled.** Outside tests, `src/` has no `dangerouslySetInnerHTML`, `innerHTML` or `eval`. A log message reaches the DOM in one place, `<AnsiHtml text={record.message} />` from `fancy-ansi` (`pages/console/main.tsx`); every other column is a React text child. Copy and width measurement use `stripAnsi`.
- **A name that becomes part of a CSS identifier goes through `cssEncode`** (`src/lib/util.ts`).
- **The Kubernetes token a user types at login is posted once and never stored client-side.** The session is the server's cookie.
- **Every state-changing request carries the CSRF token** (*GraphQL*).

## Styling

- Tailwind utilities inline in `className`; `src/index.css` is the Tailwind config (there is no `tailwind.config.*`). Merge classes with **`cn(...)`** from `@/lib/util` and nothing else.
- **Primitives come from `@kubetail/ui/elements/<name>`**, by deep import. Don't import `@base-ui/react` directly.
- Colour with the semantic tokens (`bg-background`, `text-muted-foreground`, `border-sidebar-border`), not raw palette classes. Dark mode is the `dark` class on `<html>`, set by `ThemeEffect`.
- Icons are `lucide-react`; the Kubernetes workload glyphs are local SVGs imported as components with `?react` (`src/lib/workload.ts`).

## Tests

Vitest + `@testing-library/react` (jsdom), co-located as `foo.test.ts[x]`. Globals are on: `describe`, `it`, `expect` and `vi` need no import. The repo-wide rules are the root guide's *Testing conventions*.

- **Run `pnpm test run`**, or `pnpm test run <file>` for one file. `pnpm test` alone is watch mode and never exits.
- **`renderElement(element, mocks?)`** from `@/test-utils` wraps a component in a fresh Jotai store, Apollo's `MockedProvider` with the real cache, and a `MemoryRouter`. `mocks` are `MockedResponse[]` built from the real documents in `ops.ts`; results carry `__typename`; `delay: Infinity` holds a request pending.
- **`withAppConfig(overrides, fn)`** runs `fn` with `appConfig` patched and restores it.
- **`vitest.setup.ts` mocks for every test**: `useSession` (default: signed out, so `AuthRequired` redirects), the CSRF token functions, and `useIsClusterAPIEnabled` (default: `true`). A test of a signed-in page sets `(useSession as Mock).mockReturnValue({ session: { user: 'test' } })`.
- **`vi.resetAllMocks()` runs after every test**, so a `mockReturnValue` or `mockImplementation` belongs in `beforeEach`: set at module scope it lasts one test.
- **`requestAnimationFrame` is a zero-delay timer in tests.** Code that schedules a frame is driven with fake timers (`await act(() => vi.runAllTimersAsync())`) or awaited with `waitFor`.
- **Log viewer doubles**: `createMockClient` and `createMockLogViewerHandle` in `log-viewer/mock.ts`.
- **References**: `log-viewer/double-tailed-array.test.ts` (a pure unit), `log-viewer/log-viewer.test.tsx` (hooks with fake timers), `lib/update-notifications.test.tsx` (GraphQL mocks, localStorage and `withAppConfig`), `apollo-client.test.ts` (cache policies).

## Lint / build

- **`pnpm lint`** is ESLint (`eslint.config.ts`: the airbnb-extended presets) with Prettier run as an ESLint rule (single quotes, 120 columns). **`pnpm lint --fix` is the formatter**; run it after every change. A warning fails the run, and so does an unused disable comment. Run it from `dashboard-ui/`.
- **`pnpm build`** is `tsc -b && vite build`: the only type-check, and it covers test files. TypeScript is `strict` with `noUnusedLocals` and `noUnusedParameters`.
- **Imports are grouped, a blank line between groups**: third-party packages, `@kubetail/*`, `@/…`, then relative `./…`; alphabetical by path within a group. The lint does not check this.
- **`react-hooks/exhaustive-deps` is off.** Nothing flags a missing dependency, so write the array as though it were on.
- **A disable comment names its rule and is rare.** Prefer fixing the code.
- **A new dependency must be at least ten days old** (`minimumReleaseAge` in `pnpm-workspace.yaml`); `@kubetail/ui` is exempt.

When you change the app's structure, a convention or a command here, update this file in the same change.
