# Module: Frontend

## Purpose

The frontend is a React 19 + Vite 8 single-page application that talks to
the Go server exclusively through the `/api/*` JSON envelope
(`{success, data, error:{code,message}, meta?}`). It provides eight tabs —
dashboard, conflicts, tree diff, AI suggestions (with the full
preview → approve → apply → revert patch lifecycle), commit graph,
run history, AI context, and (scaffolded) compare — with all state held
in component-local `useState` and data loaded via `fetch` in `useEffect`.

## Files

### `frontend/package.json`
Scripts `dev`/`build`/`lint`/`preview`; dependencies `react ^19.2.8`,
`react-dom ^19.2.8`, `react-router-dom ^7.18.2`; dev deps Vite `^8.2.0`,
`@vitejs/plugin-react ^6.0.4`, ESLint 10 flat config with
`eslint-plugin-react-hooks` + `eslint-plugin-react-refresh`.

### `frontend/vite.config.js`
Dev server on port **5173** with a proxy: `/api` →
`http://localhost:8080` (the Go `serve` default), `changeOrigin`,
`secure: false`. Production builds are served by the Go server from
`frontend/dist`.

### `frontend/index.html`
Root document: `#root`, `/src/main.jsx` module entry, **CDN** Bootstrap
5.3.8 (CSS + JS bundle, SRI-pinned) and Font Awesome 7.3.0 (SRI-pinned)
— icons/layout rely on these; favicon `/favicon.svg`, title "frontend".

### `frontend/src/main.jsx`
`StrictMode` → `createRoot(...).render(<App />)`, imports `index.css`.

### `frontend/src/App.jsx`
- `BrowserRouter` shell: `Navbar` (with `toggleSidebar`) + collapsible
  `Sidebar` (`sidebarOpen` boolean state) + route table.
- Routes: `/` → Dashboard, `/suggestions`, `/conflicts`, `/treediff`,
  `/commitgraph`, `/history`, `/context` → AIContext, `/compare` →
  Compare.
- **Known issue:** `/compare` imports `./tabs/compare`, which is a
  0-byte file (see below).

### `frontend/src/components/navbar.jsx`
On mount fetches `GET /api/repository` → displays repo name and
`currentBranch` as chips next to the "MergeSolver" brand; clicking the
brand calls `toggleSidebar`. Uses `API_BASE =
import.meta.env.VITE_API_BASE || ''` (pattern shared by every tab).

### `frontend/src/components/sidebar.jsx`
Pure navigation: `useNavigate` entries — Dashboard `/`, Conflicts,
TreeDiff, Suggestions, Commit Graph, **Compare `/compare`**, AI Context
`/context`, History. (Font Awesome icons per entry.)

### `frontend/src/tabs/dashboard.jsx`
Fetches `/api/repository` **and** `/api/analysis` on mount (loading/
error states; `payload.success` checked, `error.message` surfaced).
Renders a welcome section, repository card with the branch flow
(current → incoming), a "Conflicted files" count card, and one
`analysis-card` per file showing smart-diff counts
(`smartDiff.collisions/our_changes/their_changes`) with an inline
collision preview (`base_content || our_content || their_content`).

### `frontend/src/tabs/conflicts.jsx`
Fetches `/api/analysis`, auto-selects the first file. Two-pane layout:
a file list (collision count badges) and a detail pane with summary
counts and three `renderChanges` sections (Collisions / Our changes /
Their changes) rendering Base/Ours/Theirs `code-block`s per item. Three
actions navigate with `?file=` query params:
`/treediff?file=…`, `/suggestions?file=…`, `/context?file=…`.

### `frontend/src/tabs/treediff.jsx`
Reads `?file=` from `useLocation().search`; fetches
`/api/analysis?file=…` or all analyses (accepts array or single object).
Renders per file: an "AST Difference Tree" (file root → collision nodes
with Base/Ours/Theirs branches) and a 3-column
`ast-comparison-grid` (`renderASTSection` for `baseAst`, `ourAst`,
`theirAst` listing functions and variables with line numbers).

### `frontend/src/tabs/suggestions.jsx` (largest tab, ~637 lines)
The patch-lifecycle workbench.
- **Data**: fetches `/api/suggestions[?file=]` (abortable), then
  `/api/resolutions[?file=]` into a `resolutionsMap` keyed by id;
  `meta` (provider · model, run id, generated/reused/belowThreshold/
  failed counts, `failures[]`, `retryable`) drives chips and the
  "Retry failures" banner.
- **Status vocabulary**: `statusLabel` covers
  proposed/previewed/approved/applied/reverted/stale/failed/
  validation_failed/complete/below_threshold/**manual_review**;
  `validationLabel` covers passed/failed/running/timed_out/cancelled/
  not_run.
- **Error UX**: `PROVIDER_ERROR_CODES` set (PROVIDER_UNAVAILABLE,
  MODEL_MISSING, API_KEY_MISSING, AI_CONFIG_INVALID, BASE_URL_INVALID,
  PROVIDER_UNSUPPORTED) renders a dedicated "AI provider unavailable"
  state; `code === 'TIMEOUT'` a dedicated timeout state; others a generic
  error + Retry.
- **Handlers** (all POST JSON `{resolutionId, repositoryRoot, file,
  suggestionRevision}`; revert sends `postApplyHash`):
  `handlePreview` → `/api/resolutions/{id}/preview` (opens read-only
  unified diff, colored `+`/`-`/`@@` lines), `handleApprove` →
  `/approve`, `handleApply` → `/apply` (stores `postApplyHash` +
  `validationStatus`), `handleRevert` → `/revert` (inline confirm step
  warning that rollback is refused if the file changed after apply),
  `handleRefreshAnalysis` → `POST /api/analysis/refresh` then reload.
- **Card logic**: per-resolution `actionState` ({inFlight, error,
  diffOpen, confirmRevert}); `isApproved`/`isApplied`/`isReverted`/
  `isStale`/`isManualReview` derived from resolution + suggestion
  status; **Apply is disabled until explicit approval**; stale files show
  a warning banner + Refresh Analysis; manual-review banners disable
  automatic apply; failed items show `errorCode: errorMessage` and are
  excluded from the action toolbar.

### `frontend/src/tabs/commitgraph.jsx`
Fetches `/api/graph` (Cytoscape-style `{nodes:[{data:{id, label, kind,
status, base_code, our_code, their_code}}], edges:[{data:{source,
target, type}}]}`). Builds `childMap` + `nodeById`, finds roots
(`data.status === 'file'`, falling back to all nodes), and recursively
renders a tree with Base/Ours/Theirs code leaves, plus an edge list
(`source → target  type`) and node/edge counts.

### `frontend/src/tabs/history.jsx`
Abortable fetch of `/api/history` with a `reloadKey`-driven retry.
Derives run status from `cancelled` / `timedOut` / `completedAt`
(Cancelled / Timed out / In progress / Completed) and renders cards with
repository, provider · model, run id, timestamps, files analyzed,
collisions, successful/failed/below-threshold suggestions, and
`errorSummaries` list.

### `frontend/src/tabs/aicontext.jsx`
Fetches `/api/prompt`; `?file=` preselects (key = `ctx.files[0]`).
Left list shows per-file token estimates (`estimatedTokens`); detail pane
shows repository summary, the **`repositoryReadme` section** ("Included
as pre-context so the resolver understands the project"), files in
scope, imports, function cards (kind/name/line/scope/signature +
content), stats chips, and a `<details>` "Raw assembled context (what
the provider receives)" rendering `selected.context`.

### `frontend/src/tabs/compare.jsx` + `compare.css` — implemented
The Compare tab posts `{repositoryRoot, baseRef, oursRef, theirsRef}` to
`POST /api/compare` and renders the aggregate summary, per-file
status/explanation/recommendation, and the AST structural collisions
(base/ours/theirs code). Typed API failures (`COMPARE_NO_MERGE_BASE`,
`COMPARE_REF_INVALID`) surface as error panels. Covered by
`compare.test.jsx`.

### Styling (grouped — `src/index.css`, `src/App.css`, `components/*.css`,
`tabs/*.css`)
Dark theme (`bg-dark` navbar, `#1a1a`-family backgrounds), CSS-grid
card layouts per tab (`analysis-grid`, `conflict-grid`,
`aicontext-grid`, `history-grid`), status-colored badges
(`suggestion-status-*`, `validation-status-*`, `history-status-*`),
custom diff coloring (`diff-line-add/-del/-hunk`), and layout shims for
the Bootstrap/Font Awesome CDN classes. Each tab imports its own CSS
file; `App.css` owns `.app-layout`/`.page-area`.

## Cross-Module Flow

- Dev: Vite (5173) proxies `/api` → Go server (8080); prod: Go server
  serves `frontend/dist` with SPA fallback.
- Read path: `serve` → `runstate` → `/api/repository|graph|analysis|
  prompt|history|suggestions|resolutions` → tabs.
- Write path: only `suggestions.jsx` →
  `/api/resolutions/{id}/preview|approve|apply|revert` +
  `/api/analysis/refresh` → `internal/api` → mutation stack.
- Every tab honors the `{success, data, error}` envelope and surfaces
  `error.message`; AI-specific `error.code`s map to tailored states.

## Tests

- **Vitest + Testing Library** (`npm test`) with jsdom; setup in
  `src/test/setup.js`, config in `vite.config.js`. Suites:
  `compare.test.jsx` (tab ⇄ API contract: wrapped inputs, POST body keys,
  status label, typed errors) and `dashboard.test.jsx` (name-free welcome,
  emoji-free render). Quality gates: `npm run lint`, `npm run build`,
  `npm test`.

## Fixed defects (historical notes)

- **commitgraph recursion:** `renderNode` recursed through the CALLS
  child map with no visited set, so mutual recursion crashed with
  "Maximum call stack size exceeded". Nodes are now rendered once with a
  shared `rendered` set; repeats degrade to a dashed `↻ cycle` marker and
  duplicate edges are deduped per source.
- **Suggestions region chip:** `Region #` rendered even for an unmapped
  region (`regionId === ''`); now gated on a truthy `regionId`.
- **Fetch lifecycle:** all eight tabs plus the navbar use an
  `AbortController` with `AbortError` filtering, so route changes no
  longer leak requests or setState after unmount.
- **Navigation:** sidebar items are `NavLink`s (keyboard accessible,
  active-route highlighting), the navbar brand is a real `<button>` with
  `aria-label="Toggle sidebar"`, and `href="#"` anchors are gone.
- **Spacing/tokens:** page padding standardized (28px, dashboard 32/28),
  empty states 24px, one `--navbar-height` var shared by `App.css` and
  every tab, and `src/index.css` is the global token source
  (`--brand`, `--bg`, `--card-border`, `--navbar-height`).
