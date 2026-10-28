# noonside

Monitoring for photovoltaic systems: a Go API in `backend/` plus a React SPA in `web/`, and a static Astro landing site with a waitlist in `landing/`. It replaces a Solar-Log 1200 HTML export site. It supports multiple systems, inverters and users with per-system roles, and pulls data from Solar-Log exports or accepts pushes from devices. The SPA signs in against a self-hosted GoTrue (the Auth server; client `@supabase/auth-js`) and calls the API with its access token. It is deployed to Kubernetes with the Helm chart `deploy/helm/noonside` (two images: `api` and `web`). The repository is public: nothing specific to an installation (hosts, SMTP, DSNs, buckets, operator data) goes into it; that lives in the deployment's Helm values and secrets.

## Stack

- Go in `backend/` (version in `backend/go.mod`), stdlib `net/http`, cobra/viper
- API is spec-first with [ogen](https://github.com/ogen-go/ogen): `backend/internal/platform/httpapi/api.yaml` generates `backend/internal/platform/httpapi/generated/`
- **Postgres 17:** plain Postgres via pgx/v5 with hand-written SQL (no ORM, no Timescale). Migrations live in `backend/db/migrations/` and are applied by `noonside migrate` (recorded in `noonside_migrations.schema_migrations`; a database from the Supabase CLI has its `supabase_migrations` schema renamed on the first run). Production: a CloudNativePG cluster from the chart; locally `compose.yaml`. Both are bootstrapped by `deploy/helm/noonside/files/init.sql` (roles `anon`/`authenticated`/`service_role`/`supabase_auth_admin`, schemas `auth` and `extensions`, citext, pgcrypto).
- **Auth: GoTrue** (`supabase/gotrue` image, self-hosted): accounts, sign-in, sessions, password reset and email confirmation, in the `auth` schema of the same database. It is served under `/auth/v1` of the app's origin (HTTPRoute in production, Vite proxy in dev, both strip the prefix). The Go API only verifies the access token (JWKS, fetched from `auth_internal_url`) and calls the admin API with the service_role key.
- Email: [go-mail](https://github.com/wneessen/go-mail) + `html/template`
- Errors: [Sentry](https://sentry.io) (optional) in the API (`sentry-go`, `internal/platform/errtrack`) and the SPA (`@sentry/react`, `web/src/lib/sentry.ts`): errors, traces, error replays and the "Report a problem" form. Off without a DSN.
- **Web (`web/`):** React 19 + Vite + TypeScript, React Router, TanStack Query, Tailwind v4, Radix primitives, react-hook-form + zod. The API client is generated from `api.yaml` with [Hey API](https://heyapi.dev) (`web/openapi-ts.config.ts`): fetch SDK, TanStack Query options and Zod schemas in `web/src/api/gen`.

## Commands

```sh
mise run db:up               # compose.yaml: Postgres :54322, GoTrue :9999, Mailpit :54324 (SMTP :54325)
mise run db:down             # stop it (keeps the data)
mise run db:reset            # re-create the local DB (volume), migrate, backend/db/seed.sql (wipes local data)
mise run db:migrate          # noonside migrate against the local DB
mise run db:migration:new NAME  # new backend/db/migrations/<UTC timestamp>_NAME.sql

mise run generate            # ogen (Go) + Hey API (web/src/api/gen) — run after editing api.yaml
mise run generate:check      # fail if generated code is out of date (CI)
mise run build               # backend/bin/noonside
mise run format              # go fmt
mise run test                # unit tests
mise run test:integration    # integration tests against the compose stack (needs `mise run db:up`; migrates first)
mise run lint                # golangci-lint
mise run lint:actions        # actionlint on .github/
mise run dev                 # serve with ENVIRONMENT=dev against the compose stack
mise run dev:stack           # compose stack + migrations, API with reload (air) + Vite; Ctrl-C stops both
mise run dev:seed            # apply backend/db/seed.sql (dev accounts, demo systems) without a reset

mise run web:install         # pnpm install in web/
mise run web:dev             # Vite on :5173, proxies /api and /media to :8080 and /auth/v1 to GoTrue :9999
mise run web:test            # vitest
mise run web:lint            # tsc + eslint
mise run web:build           # web/dist (the web image serves it with nginx, web/nginx)
mise run web:e2e             # Playwright: smoke test + layout checks on all viewports (running stack)
mise run web:shots           # layout checks only, screenshots in web/e2e/screenshots/<project>/
mise run docs:screens        # README screenshots (English, 1440×900 @2x + dark phone) into docs/screenshots (running stack; re-seeds first)

mise run landing:install     # pnpm install in landing/
mise run landing:dev         # Astro on :4321 (forms post to the API via Vite :5173, so run dev:stack too)
mise run landing:build       # astro check + build into landing/dist
mise run landing:screens     # German app screenshots (card crops) into landing/src/assets/screens (running stack; re-seeds first)
mise run landing:build:prod  # landing with LANDING_URL, PUBLIC_APP_URL, PUBLIC_DEMO_URL, PUBLIC_OPERATOR_*, PUBLIC_HOSTING_*, PUBLIC_MAIL_PROVIDER from landing/.env.production (gitignored)

mise run docker:build        # images noonside-api:dev (API, migrations, auth templates) and noonside-web:dev (nginx + web app)
mise run chart:lint          # helm lint + kubeconform of deploy/helm/noonside (CI)
mise run web:sourcemaps [DIR]   # upload a web build's source maps to Sentry, then delete them (SENTRY_AUTH_TOKEN, SENTRY_ORG, SENTRY_PROJECT, SENTRY_URL)

noonside user create EMAIL [--name N] [--superadmin]   # confirmed GoTrue user; password from stdin/prompt
noonside user promote EMAIL [--revoke]                 # superadmin flag (app_metadata)
noonside keygen                                        # SECRET_KEY
noonside keygen vapid                                  # Web Push key pair (vapid_public_key, VAPID_PRIVATE_KEY)
noonside import solarlog DIR --system ID               # Solar-Log export incl. full history
noonside import measurements FILE.csv [--batch N]      # minute data from a CSV of the former measurements table
noonside migrate [--wait 5m]                           # apply backend/db/migrations (migrations_dir); --wait for Postgres + GoTrue's auth.users
noonside keygen gotrue                                 # GoTrue signing key (GOTRUE_JWT_KEYS/SECRET) + service_role/anon keys
noonside email list|preview NAME [--text]|send-test NAME [--to X]
noonside email auth-templates                          # regenerate backend/db/auth-templates/*.html (GoTrue's emails)
noonside waitlist stats|export [--all]                 # landing waitlist sign-ups and pricing clicks (CSV export)
```

## Local development

- **First run:** `mise run db:reset` (stack, migrations, seed), then `mise run dev:stack`, then open http://localhost:5173.
- **Stack:** `compose.yaml` runs Postgres 17, GoTrue and Mailpit with fixed local-only keys (the service_role key is `vars.dev_secret_key` in `mise.toml`). There is no Studio; use psql on :54322. GoTrue issues tokens for `http://localhost:5173/auth/v1` (`<public_url>/auth/v1`, the Vite proxy), and `auth_internal_url` in `dev_config.yml` is GoTrue itself. GoTrue fetches its email templates from the running API (`/auth-templates/`), else uses its built-in ones.
- **Accounts:** `backend/db/seed.sql` creates them (`mise run db:reset`); `mise run dev:seed` restores them on a running database. Password `noonside-dev-pw` for all; in dev the login page lists them for one-click sign-in (`web/src/features/auth/DevAccounts.tsx`, keep it in sync with the seed):
  - `dev@noonside.local`: superadmin; sees every system, including integration-test leftovers.
  - `demo@noonside.local`: owner of "Demo PV" (3 inverters, 2 planes, 3+ years of daily data, minute data for the last 3 days, an open alert) and "Demo Balcony". Playwright signs in with it. `demo-de@noonside.local` is the same in German.
  - `admin@noonside.local` and `viewer@noonside.local`: admin and viewer of "Demo PV", for checking permissions.
  - Public share link: `/p/s0p_local-dev-demo-link` (`?kiosk=1` for kiosk mode).
- **Scope:** local only; nothing applies the seed in production. Other accounts: `noonside user create`, which fails if the email exists. Auth emails land in Mailpit (:54324).
- **Busy ports:** if another stack holds the compose ports, stop it; `NOONSIDE_{PG,AUTH,MAIL,SMTP}_PORT` and `NOONSIDE_SFTPGO_{API,SFTP,FTP}_PORT` move them. If 8080 or 5173 are taken, `dev:stack` stops and shows the process holding them, usually an older dev server that has to be restarted for backend changes. Don't start a second stack on other ports; Vite proxies to :8080.
- **Playwright MCP:** `.mcp.json` configures it `--isolated` (in-memory profile, so it never collides with another session's browser) and with service workers blocked (no stale builds). Sign in there with the seed accounts.

## Checking UI work

Check every UI change at desktop and phone size before calling it done:

1. **Run the checks.** With the stack running, `mise run web:shots` opens every page in `web/e2e/layout.spec.ts` as desktop, desktop-dark, tablet (768), phone (390) and phone-de. It fails when the page scrolls sideways, when an element sticks out of the screen outside a scroll box (also content clipped by the mobile shell), on console errors and on API 5xx.
2. **Review the screenshots.** At least phone, phone-de and desktop-dark, of the pages you touched: `web/e2e/screenshots/<project>/<page>.png`, opened with the Read tool.
3. **Try new interactions** (dialogs, menus, tooltips, drag) in the Playwright MCP, at 1280×800 and at 390×844 (`browser_resize`).
4. **Checklist:** no sideways scrolling at 390 px; long names and the longer German labels; empty and loading states; dark mode; keyboard focus; touch targets of about 40 px.
5. **Add new pages** to the page list in `layout.spec.ts`.

## Layout

| Path | Purpose |
|---|---|
| `backend/` | Go module: `main.go`, `go.mod`, `.air.toml` (reload for `dev:stack`), build output in `bin/` (gitignored). The `noonside` CLI finds `config/` from the repository root or from `backend/`; relative paths in it (`media_dir`, `migrations_dir`, `auth_templates_dir`) are relative to that directory (`config.Dir`, `app.ConfigPath`) |
| `backend/cmd/` | CLI commands (serve, migrate, user, import, keygen, email) |
| `backend/config/` | `default_config.yml` + `<ENVIRONMENT>_config.yml` (also in the image under `/config`) |
| `backend/db/` | `migrations/` (`noonside migrate`), `seed.sql` (local accounts and demo data), `auth-templates/` (GoTrue's emails, generated) |
| `deploy/` | `postgres/init.sql` (database bootstrap for compose and the chart) and the Helm chart `helm/noonside` (`api.yaml`: API Deployment with the migrate init container; `web.yaml`: nginx with the SPA; `auth.yaml`: GoTrue; `postgres.yaml`: CNPG Cluster + optional backups; `sftpgo.yaml`: optional hosted FTP; `httproute.yaml`: `/auth/v1` → GoTrue, `/api`, `/media`, `/healthz`, `/readyz`, `/openapi.yaml` → API, the rest → web; `ci/test-values.yaml` switches everything on for linting) |
| `compose.yaml` | Local stack: Postgres 17, GoTrue, Mailpit (+ Swagger UI, profile `docs`) |
| `backend/internal/app` | Composition root: `NewDeps` builds the external clients from config (`config.go`), `NewModules` wires the modules, `Build` registers the scheduler jobs (in order) and composes the API handler (`handler.go`: one embedded handler per module + the security handler) |
| `backend/internal/router` | Mounts the API under `/api/v1`, plus `/healthz` (header `Noonside-Release`), `/readyz`, `/openapi.yaml`, `/media/` and `/auth-templates/` (GoTrue fetches them in-cluster; not routed publicly); per-IP rate limits (`ratelimit.go`). The SPA is not served here: it is the web image |
| `backend/internal/archtest` | Tests the module boundaries: allowed module dependencies, `<m>api` only imported by `app`, SQL only on the module's own tables, every migration table has an owner |
| `backend/internal/platform/db` | Connection pool (queries are Sentry spans, `trace.go`), transactions in the context (`InTx`, `Q`), `ErrNotFound`/`ErrConflict`, job leases (`TryLockJob`, table `job_leases`), migrations (`migrate.go`) |
| `backend/internal/platform/httpapi` | `api.yaml` + `generated/` (ogen), error responses (`errors.go`: `BadRequest`, …, `NewError`), `Authz` (scope checks with the role from the systems module), shared converters |
| `backend/internal/platform/scheduler` | Generic job runner: `Add` jobs, `Run` (ticker, in `serve`) or `RunDue` (once); waits for jobs' background work (data source polls) |
| `backend/internal/platform/config` | Loads `backend/config/default_config.yml` + `backend/config/<ENVIRONMENT>_config.yml`; env vars override |
| `backend/internal/platform/auth` | GoTrue token `Verifier` (issuer `auth_url`, default `<public_url>/auth/v1`; JWKS from `auth_internal_url`), `Principal`, `Role`, roles → scopes (`scopes.go`), opaque tokens, `GenerateGoTrueKeys` |
| `backend/internal/platform/gotrue` | Minimal GoTrue admin client (create user, app_metadata), against the GoTrue base URL (`app.AuthURL`) |
| `backend/internal/platform/push` | Web Push client (VAPID) and the push-service host allow-list |
| `backend/internal/platform/notify` | ntfy and webhook senders for users' own targets (`notification_targets`) and `PostJSON` for system webhooks; webhooks are signed (`X-Noonside-Signature`) |
| `backend/internal/platform/netguard` | SSRF guard for server-side requests to user-supplied hosts (data sources, ntfy, webhooks): dialer refusing private/local addresses, `HTTPClient`, `CheckURL` |
| `backend/internal/platform/errtrack` | Sentry: `Init` (no DSN = off), `Handler` (slog: records at Error become events), `Middleware` (scope, transaction, panic → logged error + 500), `Operation` (ogen middleware naming transactions by operation), `Recover` (deferred in goroutines and jobs), `StartJob` (transaction per scheduler job and data source run), `HTTPClient`/`Transport` (span per outgoing request, host only, no trace headers), `SetUser`/`SetTag`; `scrub` removes credentials headers, query strings, bodies and path tokens |
| `backend/internal/platform/email` | Email rendering and sending (also renders GoTrue's email templates) |
| `backend/internal/platform/media` | Uploaded pictures: `Read` validates (JPEG/PNG/WebP by content, ≤ 2 MB, ≤ 4096 px), `NewKey` (immutable random keys), `Store` = `Dir` (`media_dir`, served by the router); `URL` = `/media/<key>` |
| `backend/internal/platform/secretbox` | AES-GCM for secrets at rest (data source passwords) |
| `backend/internal/modules/identity` | Profiles and accounts (`profiles`, reads `auth.users`), user creation, editing (name, email, ban, superadmin), deletion and password reset links through the Auth admin API (`admin.go`), profile pictures (`avatar.go`); `/me`, admin users |
| `backend/internal/modules/systems` | Systems, planes, targets, inverters, members, invitations, API keys (`AuthenticateAPIKey`), share links (`ResolveShareLink`); `MemberRole` for authorization; `OnDelete` hooks; `geocode/` (Photon client, `GET /geocode`, address validation, hourly `geocode_backfill` job) and `pvgis/` (target estimates) |
| `backend/internal/modules/telemetry` | Single write path for measurements, daily energy, status/events (`ingest.go`, `plausible.go`), the time series (`store_timeseries.go`), minute data retention and rejection pruning jobs; push ingest and events API |
| `backend/internal/modules/weather` | Open-Meteo client, forecast cache per system (`weather_forecasts`, 15 min, refreshed on request), the PV model for expected power (irradiance on the panel plane × kWp × PR, temperature-corrected) and the `expected_daily` backfill job |
| `backend/internal/modules/prices` | Day-ahead prices per bidding zone into `market_prices` (job `market_prices`, hourly): aWATTar (DE-LU, AT) or ENTSO-E with `ENTSOE_API_TOKEN`; `NegativeDuring` for feed-in at negative prices |
| `backend/internal/modules/advice` | "Best window today" for large loads from the forecast and the prices (`GET /systems/{id}/advice`, morning push, `surplus.forecast`); advisory only; no tables |
| `backend/internal/modules/datasources` | Data sources (`data_sources`), their poll job (lease per source, `data_sources.locked_until`, so a killed server cannot block it; polls stop starting new files 2 min before their deadline) and `RunOnce`; `solarlog/` (file parsers + poller over FTP/FTPS/SFTP; the local directory storage is only used by `noonside import solarlog`), `pvoutput/` (kind `pvoutput_export`: uploads 5-minute status and daily output; progress in `data_sources.state`), `hostedftp/` (hosted FTP `solarlog_hosted`: SFTPGo REST client `sftpgo.go`, `Service` with one SFTPGo user per data source: provision, reset password, quota state, purge + delete, the upload event rule, hourly `hosted_ftp_gc` job), `adapter/` (`Poller`) |
| `backend/internal/modules/notifications` | Notifications (inbox), prefs, own targets, push subscriptions, outbox dispatcher (`dispatch.go`: email, push, ntfy, webhook), renderer registry (`RegisterRenderer`; other modules register their types), `Channels`/`Deliveries`/`Muted` |
| `backend/internal/modules/alerting` | Alert engine (rules in `rules.go`, run every 5 min by the scheduler), alerts and rule overrides, morning forecast push (`forecast.go`), server-rendered DE/EN alert texts (`text.go`); the alert hook feeds the webhooks module |
| `backend/internal/modules/reports` | Yield reports (`notification_prefs.digest`: daily/weekly/monthly around 06:00 local, plus the yearly report in January), queued as `report` notifications on the user's `report_channels`; the email is its own full report (`email.YieldReport`), push/ntfy/webhook get the summary from `reports.Notification` (registered with `notifications.RegisterRenderer`); DE/EN texts in `text.go`; no tables |
| `backend/internal/modules/webhooks` | Per-system webhooks (`webhooks`): queues `alert.opened`/`alert.resolved` (alert engine hook), `daily.summary` and `surplus.forecast` (job `webhook_events`) into the `webhook_deliveries` outbox; `Sender` (job `webhooks`) posts them with the notification backoff |
| `backend/internal/modules/analytics` | Read-only composition (`analyticsapi` only, no tables): live, overview (`expected.go`, `performance.go`, `periods.go`), daily energy, dashboard, exports and the public share-link views |
| `backend/internal/modules/waitlist` | Landing page waitlist (`waitlist_entries`, `pricing_clicks`), its form endpoints and the pruning of unconfirmed sign-ups |
| `backend/internal/integration` | End-to-end API tests against the compose stack (`-tags=integration`) |
| `web/` | React SPA; see "Web app" below |
| `landing/` | Astro landing site (German, static, no client framework): pages in `src/pages` (home, `bald/<plan>`, `warteliste/*`, Impressum, Datenschutz), operator data and app/demo URLs in `src/lib/site.ts`, styles import `web/src/styles/tokens.css`. Own pnpm project. See "Landing site" below |
| `backend/Dockerfile`, `web/Dockerfile` | The two images, cross-compiled for amd64/arm64. API (context `backend/`): distroless with binary, config, `backend/db/migrations` in `/migrations`, `backend/db/auth-templates`; release = `APP_VERSION` via ldflags `errtrack.version`. Web (context `web/`): nginx-unprivileged with the build; `web/nginx/*.template` become its config at start (the CSP, cache headers, SPA fallback and `/config.js` from env). Its stage `web-dist` is the build with source maps (uploaded to Sentry, never shipped) |
| `.github/workflows/ci.yml` | CI: Go lint + unit tests, `generate:check`, integration tests against `docker compose up` (tests compile while it starts), web lint/test/build, landing build, `chart:lint`, actionlint; jobs only run when their paths changed (`changes` job), no run (and so no deploy) when only `*.md`, `docs/` or `.mcp.json` changed. Go caches are restored on every run and saved only on main (keys end in the run id); the pnpm store is cached by lock file (composite actions in `.github/actions/`). Every job has a timeout |
| `.github/workflows/deploy.yml` | After CI on main: `image` (web source maps to Sentry when `vars.SENTRY_ORG` is set, multi-arch images `ghcr.io/<owner>/noonside-api:<sha>` and `noonside-web:<sha>`) → `chart` (`0.1.<run>`, appVersion = sha, to `oci://ghcr.io/<owner>/charts`) → `smoke` (only with `vars.PUBLIC_URL`: waits for `Noonside-Release` = sha). No cluster credentials: a GitOps tool (e.g. Flux `OCIRepository`, semver `>=0.1.0`) pulls the chart |

## Conventions

- **Modules:** the backend is a modular monolith. Each module in `internal/modules/<m>` owns its tables (only its SQL touches them; it reads others through their modules), its domain types and services in the root package, its API operations in `<m>api`, and its scheduler jobs. Other modules import only a module's root package and only along the allowed dependencies in `internal/archtest` (no cycles; inversions go through ports, e.g. the alert hook, `systems.OnDelete`, `notifications.RegisterRenderer`, `identity.Memberships`). Only `internal/app` imports `<m>api` packages; `internal/platform` imports no module. Transactions span modules through the context (`db.DB.InTx`). A new table needs an owner in `archtest.owner`; `mise run test` enforces all of this.
- **API changes:** edit `api.yaml` first, run `mise run generate`, then implement the new method on the owning module's handler (`internal/modules/<m>/<m>api`); `internal/app/handler.go` embeds all of them, so a missing operation fails to compile. Error responses all use the shared `default` response. Handlers return errors (`httpapi.BadRequest`, `httpapi.Forbidden`, …, or `db.ErrNotFound`/`ErrConflict`), and `httpapi.NewError` in `backend/internal/platform/httpapi/errors.go` maps them to status codes.
- **Auth:**
  - The GoTrue access token only proves identity (`sub`, `email`). Superadmin comes from `app_metadata.solar0_superadmin`, which only the service_role key can set.
  - Do not add login/refresh/password endpoints; that is GoTrue's job.
  - Public sign-up is disabled (`GOTRUE_DISABLE_SIGNUP` in `compose.yaml` and the chart). Accounts are created by invitation accept or `noonside user create`, both through the admin API with the email pre-confirmed. GoTrue's settings (sign-up, password length, rate limits, SMTP, email subjects) live in `deploy/helm/noonside/templates/auth.yaml` and `compose.yaml`; keep both in sync.
- **Authorization:**
  - Every system-scoped handler starts with `h.authz.Authorize(ctx, systemID, auth.ScopeX)` (`httpapi.Authz`). It loads the caller's role from the systems module (`MemberRole`) on every request.
  - Non-members get 404, and members without the scope get 403.
  - Roles and scopes are defined in `backend/internal/platform/auth/scopes.go`. Only owners may grant or revoke owner, and a system must keep one owner.
  - Invitation tokens and device API keys are ours and stored only as hashes.
  - Opening an invitation link creates a pre-confirmed account for the invited address, so the token must only reach the invitee. The API returns it (`token`/`accept_url`) only if the email couldn't be sent and the caller is a superadmin. Otherwise the link goes out by email only, and an unsent invitation is revoked.
- **Database:**
  - Users live in `auth.users`. App tables reference `public.profiles`, which a trigger keeps in sync.
  - Nothing exposes the database (no PostgREST any more); the older migrations' RLS and `anon`/`authenticated` revokes stay, so `init.sql` still creates those roles. New tables don't need them.
  - The Go server and `noonside migrate` connect as the superuser `postgres` (the migrations alter `auth.users` triggers and default privileges for `postgres`); GoTrue as `supabase_auth_admin`. Connections are direct (no pooler), so session state is fine, but jobs still use leases (`job_leases`) so several replicas or a CLI run never double-run them. Migrations run in the app pod's init container (`migrate --wait`), each file in a transaction.
- **Energy:** `inverter_daily` (in the system's local calendar) is the source of truth for month/year/total views. Its `energy_wh` is always the device's value (ingest keeps the larger one); a manual correction (`manual_energy_wh`, `PUT /systems/{id}/days/{day}/energy`, scope `data:edit`) sits beside it and wins: every reader sums `COALESCE(manual_energy_wh, energy_wh)` (`telemetry.Energy`, which also reports `Corrected`), and ingest never touches it. Minute data lives in `measurement_days`, one row per inverter and **UTC** day with the samples as parallel arrays (`secs` = seconds since 00:00 UTC, sorted), to keep the table small. Only `backend/internal/modules/telemetry/store_timeseries.go` touches it: intraday curves (`date_bin`) and peaks. Ingest is idempotent, and new samples replace old ones with the same time. `systems.minute_retention_years` keeps minute data for the current year plus N full local calendar years (`System.MinuteCutoff`): older samples are not stored at ingest, Solar-Log minute files before the cutoff are skipped, and the scheduler deletes older data on every run, in batches with a time budget (`DeleteMeasurementsBefore`), never in an API request (they should stay short). Daily energy is never pruned.
- **Plausibility:** `backend/internal/modules/telemetry/plausible.go` checks every point and day on the way in (push API and pollers): points more than 10 min in the future are dropped; negative power or power above 1.3 × max(rated AC, kWp) drops that field; eday or daily energy above 12 kWh/kWp drops the field or the day (no bound without kWp). Drops are counted in `ingest_rejections` (per inverter, local day, reason; pruned after 90 days by the telemetry job `ingest_rejections_prune`), returned as `rejected`, and raise `implausible_data`.
- **Time:** always use the system's time zone (`System.Loc()`) for calendar logic. Dates are stored as `date`.
- **Solar-Log minute format:** `Pac;Pdc;Eday;Udc[;Temp]`. Eday is the third field.
- **Error reporting (Sentry):**
  - Go: every `slog` record at level `Error` is a Sentry event (`errtrack.Handler`), with the request's scope when logged with its `ctx` (`ErrorContext`). So log at `Error` only what the operator must look at; failures the user can fix (an unreachable data source, wrong credentials, a bad request) are `Warn`. Don't put emails or other personal data into log messages or attributes. New goroutines and jobs `defer errtrack.Recover(ctx, logger, "…")`. Outgoing HTTP clients use `errtrack.HTTPClient` (or `errtrack.Transport`), so their requests show up in traces. Loggers come from `slog.Default()` at construction time, never in a package-level `var` (that one predates the handler and loses the level).
  - SPA: `reportError` for errors the app catches itself; `ApiError` is never reported (5xx come from the API). `scrubUrl` keeps tokens (`/p/`, `/invitations/`, `/public/`), query strings and fragments out of events; extend it for new token routes, in Go (`errtrack.tokenPath`) too, and in `tokenPage` (pages opened with a token get no replay: the recording's page meta can't be scrubbed).
  - The SPA sends its events through the API (`tunnel`): `POST /api/v1/diag` (`errtrack.Tunnel`, mounted outside the Sentry middleware) forwards envelopes of the `SENTRY_WEB_DSN` project only, so ad blockers don't drop them and the CSP needs no Sentry host. `worker-src` allows `blob:` for the replay compression worker (`web/nginx/headers.inc.template`). Source maps are built as `hidden`, uploaded by the deploy (`web:sourcemaps`) and never served.
  - The DSNs are not secret and come from the chart's `sentry.*` values: the API gets `SENTRY_DSN`, `SENTRY_ENVIRONMENT`, the sample rates and `SENTRY_WEB_DSN` (for the tunnel); the web container gets the web DSN, environment and rates, which nginx writes into `/config.js` (`web/src/lib/runtimeConfig.ts` reads it; `VITE_SENTRY_DSN` only stands in for `vite` dev). No DSN is ever baked into an image. Release = the commit (`APP_VERSION` via ldflags, else `vcs.revision` in Go; `APP_VERSION` in the web build).
- **Secrets:** read from the environment (`DATABASE_URL` or libpq `PG*`, `AUTH_SERVICE_ROLE_KEY`, `AUTH_JWT_SECRET`, `SECRET_KEY`, `SMTP_PASSWORD`, `VAPID_PRIVATE_KEY`, optional `OPEN_METEO_API_KEY`, optional `ENTSOE_API_TOKEN`, optional `SFTPGO_ADMIN_PASSWORD`). Never log them or put them in config files or the chart's values. The chart reads them from existing secrets (`api.existingSecret`, `auth.existingSecret`, `postgres.authRoleSecret`, `postgres.backup.s3.credentials.existingSecret`, `hostedFtp.admin.existingSecret`). The keys in `compose.yaml` and `mise.toml` (`vars.dev_secret_key`) are local-only dev values.
- **Production config:** `backend/config/prod_config.yml` holds only generic production defaults (paths in the image, rate limits, scheduler). Everything specific to an installation comes from the chart's values as env vars: `host`/`publicUrl` (`PUBLIC_URL`; GoTrue's public URL `auth_url` defaults to `<public_url>/auth/v1`), `smtp.*` (for the API and GoTrue), `vapidPublicKey`, `landingUrl`, `trustedProxyHops`, `rateLimit.enabled`, `sentry.*`, `hostedFtp.*`; the chart also sets the in-cluster addresses (`DATABASE_URL`, `AUTH_INTERNAL_URL`). Never commit an installation's values (hosts, DSNs, bucket names, SMTP accounts) here.
- **Backups:** `postgres.backup` (off by default): barman-cloud plugin with WAL archiving (`walArchiving`), scheduled base backups (`schedule`, empty for none), any S3-compatible store (`s3.endpointURL`) and credentials from `s3.credentials.existingSecret`.
- **Production (Kubernetes):** one app pod (`serve`: API, web app, `/media`, scheduler on its ticker; Recreate, the media volume is ReadWriteOnce), GoTrue and a CNPG cluster in namespace `noonside`; Cilium Gateway API in front (HTTPRoute from the chart: `/auth/v1` → GoTrue with the prefix stripped, everything else → app). No request time limit beyond the client's.
- **Edge security:**
  - Per-IP rate limits live in the API (`internal/router/ratelimit.go`, 429): 600 per 5 min on `/api/*`, 60 on the public endpoints (invitations, share links, waitlist). The client is the gateway's `X-Forwarded-For` entry (`trusted_proxy_hops`, 1 in prod; on in prod only). GoTrue limits its own endpoints (`GOTRUE_RATE_LIMIT_*`).
  - The SPA's Content-Security-Policy is in `web/nginx/headers.inc.template` (the web image; `web.cspConnectSrc` adds connect-src hosts). Keep inline scripts out of `index.html` (`script-src 'self'`; `/theme.js` and `/config.js` are files). The only external host is the map tile server in `img-src` (`TILE_URL` in `web/src/lib/map.ts`); GoTrue shares the origin.
- **Alerts & notifications:**
  - The scheduler runs `alerting.Engine.Run` (every 5 min, `job_leases`), `notifications.Dispatcher.Run` (outbox `notification_deliveries`, retries with backoff) and the modules' prune jobs. Singleton jobs use `db.DB.TryLockJob`, not advisory locks.
  - An alert is open per (system, kind, subject) while its rule finds the condition and resolves after 2 evaluations without it. Rules about past days (`dayRules` in `rules.go`: underperformance, zero day, low yield, clipping, feed-in cap, implausible data) are held while the sun is down, so nothing about yesterday opens or resolves at night. Every open, resolve and 24 h reminder creates one notification per member (`ListRecipients` merges their prefs) with its email/push/ntfy/webhook deliveries (`notifications.Channels`; ntfy and webhook only when the user set that target under `/me/notification-targets`).
  - Channels are chosen per notification type: `notification_prefs.alert_channels`, `report_channels` and `forecast_channels` (API: `alerts`/`reports`/`forecast` objects in `NotificationPreference`), each a subset of `in_app, email, push, ntfy, webhook`. `notifications.Deliveries` turns them into delivery rows; `in_app` is not a delivery, it only decides whether the notification counts as unread (every notification is listed). The old boolean columns `in_app/email/push/ntfy/webhook` are unused and dropped by a later migration.
  - ntfy and webhook targets are per user; the ntfy token and webhook secret are sealed with `secretbox` and never returned (`has_…` flags). Their URLs must be https and public (`netguard`; `allow_private_data_source_hosts` also allows private hosts and http). Webhooks POST JSON `{id, type, severity, title, body, url, system_id, alert_id, kind, created_at}`; with a secret, `X-Noonside-Signature: sha256=<hex HMAC-SHA256 of the raw body>`, so a receiver recomputes it over the body as received and compares in constant time. Errors from these requests leave out the URL (webhook URLs often carry tokens).
  - `low_yield_vs_expected` gets yesterday's expectation through `Engine.WithExpected` (weather). `clipping` (needs `inverters.max_ac_power_w`) and `feed_in_cap` read 5-minute curves of the last 7 days, loaded only when one of them is enabled and cached per system for an hour.
  - To add a rule: add a `RuleDef` in `backend/internal/modules/alerting/rules.go` with defaults, its DE/EN texts in `text.go`, the web labels under `alerts.rules` in `web/src/i18n`, and its group in `web/src/features/settings/alertGroups.ts`.
  - User-facing text for alerts/notifications is rendered in Go in the recipient's `profiles.locale` (the SPA syncs it through `PATCH /me`), so email, push and the API agree.
  - Push endpoints must be on a known push service (`push.AllowedEndpoint`); the server posts to them.
- **Addresses:** a system's coordinates come from its address (`location`). The SPA's address field suggests matches from `GET /geocode` and sends the picked coordinates. When `location` changes without coordinates in the request, the API geocodes it and rejects an address it can't find (`location not found…`, 400). If the geocoder is down, saving still works and the coordinates stay. Clearing the address clears the coordinates, and explicit coordinates always win. Photon's public instance (`geocode_base_url`) is fair-use: the SPA debounces, and the backfill waits 1 s between lookups. Use a self-hosted instance for heavy or commercial use, and keep the "© OpenStreetMap contributors" attribution in the suggestions. **Maps:** Leaflet with OSM tiles, lazy-loaded (`web/src/components/data/LazyMap.tsx`): a pin picker under the address field and the dashboard's map view (fleet with more than one system). Moving the pin sets the coordinates and fills the address from `GET /geocode/reverse` (nearest numbered house, else street); the pin's coordinates are sent, so they win. The public tile server is fair-use as well (switch `TILE_URL` and the CSP for heavy use) and needs its attribution.
- **Weather:** only through `weather.Service` (`GET /systems/{id}/weather`), for systems with coordinates (usually from the address). The browser never calls Open-Meteo, so the CSP stays unchanged. **Planes:** a system's module fields live in `system_planes` (kWp, tilt, azimuth; saved with the system as `planes`, which replaces them). Open-Meteo is asked once per plane and the irradiance is combined weighted by kWp (`weather.combine`), so everything downstream sees one curve; no planes means flat modules. The forecast is fetched lazily on request when the cached row is older than 15 min or its request key (`Request.Key`: model version, coordinates, time zone, planes) changed; a failed fetch falls back to the stale row. No background job. **Expected energy:** the scheduler job `expected_daily` (hourly, `Service.Backfill`) stores weather-based kWh per kWp for every past local day in `expected_daily`, from the first data or commissioning day to yesterday. It uses the historical forecast API from 2022 and ERA5 before, and recomputes all days when `expected_daily_keys.req_key` changes (coordinates, planes, or the `v2|` model version in `Request.Key`). The overview uses it per day (`expectation` in `backend/internal/modules/analytics/analyticsapi/expected.go`; today and later come from the forecast) and falls back to the monthly targets for days without it; `summary.target_source` says which applies. Day views get the expected curve in `Overview.expected`: from the forecast for today, otherwise fetched from the historical API on request (not cached). The curve is already scaled to the shown inverters' kWp; the SPA interpolates it onto the 5-minute buckets (`web/src/lib/weather.ts`). Open-Meteo values are labelled with the **end** of their interval, and a date range ends at 23:00/23:45, so history requests ask for one day more (`History`). **Open-Meteo's free API is non-commercial only:** before commercial use, set `OPEN_METEO_API_KEY` and `weather_base_url: https://customer-api.open-meteo.com` (or a self-hosted instance). Keep the attribution link (CC BY 4.0) in the UI. **Performance ratio:** `expected_daily.irradiation_kwh_m2` (kWp-weighted plane irradiation) gives PR = energy ÷ (irradiation × kWp) over complete past days with weather data (`backend/internal/modules/analytics/analyticsapi/performance.go`; a day view uses its irradiance curve, today until now), per bucket and as a yearly trend in the total view. **PVGIS:** `POST /systems/{id}/targets/estimate` asks PVGIS (`pvgis_base_url`) per plane in the request and saves nothing. **Morning forecast:** the `forecast_push` job (`alerting.Forecaster`) sends the day's expected yield between 07:00 and 10:00 system time to members with `forecast_push` on, over their `forecast_channels`, once a day per system (`job_leases` row `forecast_push:<id>`).
- **Pictures:** profile pictures (`profiles.avatar_key`) and system pictures (`systems.image_key`, scope `system:write`) are uploaded as `application/octet-stream` (`PUT /me/avatar`, `/admin/users/{id}/avatar`, `/systems/{id}/image`). The SPA scales and re-encodes them first (`web/src/lib/image.ts`, which also drops EXIF/GPS); the server validates (`media.Read`) and stores them as received under a new random key, then deletes the old object (also when the user or system is deleted; failures are only logged). They are served same-origin under `/media/<key>` with an immutable cache header, so the CSP needs no change: the router serves them from `media_dir` (production: the chart's volume at `/data/media`; dev: `backend/tmp/media`, Vite proxies `/media`). Anyone with the URL can load a picture (share links show the system's). Not set: `Me.features.uploads` is false and the SPA hides the controls.
- **User management:** superadmins edit accounts on `/admin/users/:id` (`ProfileForm` is shared with the own profile). Changing the email or name also sets `user_metadata.name`, because the `sync_profile` trigger copies it into `profiles.name` on every email/metadata change; GoTrue answers a taken email with a 500, so `UpdateUser` checks `profiles` first (409). Disabling bans the user in GoTrue (`ban_duration`); deleting refuses while they are a system's only owner (`Memberships.SoleOwnerships`). Superadmins can't revoke, disable or delete themselves.
- **Share links:** `share_links` (token hash only, scope `share:manage`) give read-only access under `/public/{token}` (system, live, overview) without an account; revoked or expired links are 404. They leave out members, events, alerts, the address and status/error texts, and set revenue to 0 unless `show_financials`. The SPA route is `/p/:token` (`?kiosk=1` for large screens). The API's `public` rate limit covers them together with the invitation endpoints, so keep the kiosk's polling low.
- **Data sources:** `solarlog_remote` refuses private/local addresses unless `allow_private_data_source_hosts` is set (on in dev, e.g. for a LAN FTP server in a self-hosted Docker setup). The same switch applies to ntfy and webhook targets and to system webhooks. `pvoutput_export` pushes instead of pulling (PVOutput allows 60 requests an hour and data up to 14 days old): each run uploads the output of finished days (3 h after midnight) and at most 3 batches of 30 statuses; the API key is sealed like passwords.
- **Hosted FTP (`solarlog_hosted`):** for devices that can only push over FTP (Solar-Log), on the installation's own SFTPGo (chart `hostedFtp.*`, compose service `sftpgo`). On when `hosted_ftp_api_url`, `SFTPGO_ADMIN_PASSWORD` and `SECRET_KEY` are set; `Me.features.hosted_ftp` tells the SPA.
  - Each source owns an SFTPGo user (`pv` + 8 random characters, description `data-source=<id>`, home `<hosted_ftp_home_base>/<system>/<source>`, only FTP and SSH, `quota_size` = `hosted_ftp_quota_mb`, uploads ≤ 20 MB). The device uploads over FTP/FTPS (`hosted_ftp_public_host`/`_port`); the API reads over SFTP in the cluster (`hosted_ftp_sftp_addr`) with the same credentials, sealed in `config` like `solarlog_remote`, with the host key pinned at creation (or `hosted_ftp_host_key`). SFTPGo's PUT replaces a user, so a password change reads the user and writes it back.
  - **Imported on upload, never polled:** an SFTPGo event action + rule `noonside-upload` (applied at `serve` start and by every `hosted_ftp_gc` run) POSTs each upload to `POST /api/v1/hooks/hosted-ftp` (`hosted_ftp_hook_url`; security scheme `hostedFtpHookAuth`, token = hex HMAC-SHA256 of `hosted-ftp-hook` keyed with `SECRET_KEY`, compared in constant time; exempt from the per-IP rate limit). The handler sets `data_sources.upload_at`; the scheduler runs a hosted source once `upload_at` is newer than `imported_upload_at` and 30 s quiet (one run per upload session), and a successful run records the `upload_at` it started with. Failed runs retry with the backoff. `poll_interval_s` is ignored (hidden in the SPA).
  - The handler inserts the row (disabled) before creating the user, so GC never deletes a user that is being created; on failure it deletes the row. The password is returned once (`upload.password`, create and `reset-password`), never listed. One per system (partial unique index) and `hosted_ftp_max_accounts` in total (0 = no limit).
  - Deleting the source or its system purges the files over SFTP and deletes the user (SFTPGo keeps the empty home directory); failures are left to `hosted_ftp_gc`, which deletes users whose source is gone.
  - Every run does housekeeping (`solarlog.Poller.WithHousekeeping`): imported `minYYMMDD.js` older than `hosted_ftp_retention_days`, non-export files after a day and files over 20 MB (`storage.MaxFileSize`, also the read limit for every source) are deleted. SFTPGo refuses uploads over the quota, so usage never reaches it: from 90 % the account counts as full (`locked`) and the run fails (`ErrQuota`, `data_source_failing`). Usage lives in `data_sources.state`.
  - Sources from the former Hetzner Storage Box fail with "created on the old server; delete it and create a new one".
- **Integrations and control (advisory, signal-based):** noonside never switches devices; it gives advice and signals that Home Assistant, an EV charger or other automations act on.
  - **Settings tabs, by data direction** (issue #43, `TABS` in `web/src/features/settings/SettingsPage.tsx`): *Data sources* holds what brings data in (loggers, `push`: keys with `data:ingest` and the curl example, `import`: CSV daily energy); *Integrations* holds what sends it out or reads it (`pvoutput`: data sources of kind `pvoutput_export`, `webhooks`, `keys`: keys with `data:read` and the Home Assistant/Grafana examples). `dataSourceKinds.ts` tags each kind `direction: 'in' | 'out'`. Keys are one table: a key with both scopes is listed in both sections. A failing PVOutput upload links to `/settings/pvoutput` (`source_kind` in the alert details, `notifications.Link`).
  - **Read API:** API keys carry `data:ingest`, `data:read` or both (`auth.KeyScopes`). `data:read` keys (`X-API-Key`) may call live, overview, daily-energy, weather, prices and advice; those operations list `apiKeyAuth` in `api.yaml`. Handlers there must not assume a user principal.
  - **System webhooks** (scope `webhook:manage`, admins): URL, sealed secret, events, locale (texts), enabled. Deliveries dedupe on (webhook, event, key: alert id or date). The body is `WebhookPayload` `{id, event, created_at, system_id, system_name, url, data}` with `X-Noonside-Event`, `X-Noonside-Delivery` and the signature; `daily.summary` goes out once from 06:00, `surplus.forecast` once between 07:00 and 10:00 (leases `webhook:<event>:<system>`). The test button posts synchronously and is not logged. Finished deliveries are pruned after 30 days.
  - **Day-ahead prices:** `systems.price_zone` (DE-LU, AT) selects them; fetched only by the scheduler (today and tomorrow until complete, then one 31-day chunk of history per run back to 2024), never in a request. The day view gets `prices`; every period gets `negative_price_kwh`/`negative_price_hours` from minute data (`EnergyDuring`). With `unpaid_at_negative_prices` (§ 51 EEG, Solarspitzengesetz) revenue leaves that energy out (`unpaid_revenue`), in reports too. Without meters all production counts as fed in. aWATTar is fair use (100 requests a day); for commercial use prefer ENTSO-E. Keep the source attribution (`prices_source`).
  - **Advice:** the window (default 3 h) with the most expected PV energy and the cheapest one for the rest of today, plus negative-price intervals. The morning forecast push adds the PV window.
  - **Curtailment:** devices may push `power_limit_pct` (e.g. § 14a EnWG); it is part of the inverter event, so a change starts a new one, and a limit alone keeps the current status. Without it, the `feed_in_cap` rule detects plateaus.
- **Former name:** the app was called solar0. `app_metadata.solar0_superadmin` keeps that name on purpose (renaming it would drop every superadmin), and `web/public/theme.js` moves old `solar0.*` localStorage keys to `noonside.*`.
- **CI:** jobs call the `mise` tasks, so keep them working non-interactively; tool versions come only from `mise.toml`. Commit regenerated code, or `generate:check` fails.
- **Tests:** use testify. Add integration tests for anything that touches SQL or authorization; create users with `env.createUser` and sign in through GoTrue with `env.login`. Tests share the local database, so use unique emails (`uniq`) and don't assert on global counts.
- **Docs:** keep `README.md` and this file in sync with the code in the same commit (commands, config keys, chart values, paths that moved). Roadmaps and business plans are not kept in this repository.
- **Git:** commit directly to `main`; there are no PR previews. Every push to `main` publishes images and a chart after CI.

## Landing site

- **Purpose:** demand check before launch: value proposition, screenshots, live demo (share link in kiosk mode on the app host, never an iframe), pricing "fake door" and a waitlist with double opt-in.
- **No inline code:** wherever it is hosted, serve it with the CSP `default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'none'; frame-ancestors 'none'; base-uri 'self'; form-action 'self' <public_url>; object-src 'none'` (it is not hosted at the moment; `landing_url` is empty). Astro is set to emit no inline styles; the only script is `public/scripts/token.js`. Don't use `style=""` attributes or `<script>` without `src` (JSON-LD excepted).
- **Waitlist API:** plain HTML forms post cross-origin to `POST /api/v1/waitlist`, `/waitlist/confirm`, `/waitlist/unsubscribe` and `/waitlist/interest` (form-encoded, `security: []`); every answer is a 303 to a page under `landing_url` (only there, so no open redirect), also for invalid input. Without `landing_url` they are 404. Handlers in `backend/internal/modules/waitlist/waitlistapi`, tables `waitlist_entries` and `pricing_clicks` (no IPs), email `waitlist_confirm` (German).
- **Double opt-in:** the confirm token is stored as a hash and valid 7 days; links carry it in the URL fragment (`#t=…`) and the page needs a button press, so mail scanners can't confirm. At most one email per address an hour and `waitlist_mails_per_hour` in total. Unconfirmed entries are deleted after 7 days (waitlist job `waitlist_prune`). The unsubscribe token is random and stored as is (it only removes the entry); emails send `List-Unsubscribe` + `List-Unsubscribe-Post` (one-click via `?token=`). Changing the consent text means bumping `waitlistConsentVersion`.
- **Fake door:** the pricing buttons POST to `/waitlist/interest`, which counts per plan and UTC day and redirects to `/bald/<plan>/`. `noonside waitlist stats` prints sign-ups and clicks for the "Ergebnisse" table.
- **Operator data** (Impressum, Datenschutz: `PUBLIC_OPERATOR_*`, `PUBLIC_HOSTING_*`, `PUBLIC_MAIL_PROVIDER*`) comes from env at build time (`landing/.env.production`, never committed); `src/lib/site.ts` only has neutral placeholders. Public builds (`LANDING_URL` https) fail while one of them or `PUBLIC_APP_URL`/`PUBLIC_DEMO_URL` is unset. The API's `public` rate limit covers `/api/v1/waitlist`.
- **Screenshots:** `web/e2e/landing-shots.spec.ts` crops single cards (`.card` by heading) at 1100 px, plus the phone notifications; time-dependent or missing data (advice, a hosted FTP source, members, the inbox) comes from `page.route` fixtures. The lock screen in the hero is CSS (`LockScreen.astro`). Competitors stay unnamed on the page (§ 6 UWG).
- **Checking:** `mise run landing:build`, then the flow in the Playwright MCP at 1280×800 and 390×844, light and dark (the email lands in Mailpit).

## Web app

- **PWA / push:** the service worker is `src/sw.ts` (vite-plugin-pwa `injectManifest`, own `tsconfig.sw.json`; still emitted as `sw.js`). It shows push notifications and sets the app badge; `src/lib/push.ts` subscribes (from a tap; on iOS only in the home screen app) and re-registers the subscription on start.
- **Layout:** `src/api` (generated `gen/`, `config.ts` with the client's runtime config, `client.ts` with the token, the error interceptor and `ApiError`, `queries.ts`, `types.ts` re-exporting schema types), `src/auth` (GoTrue session via `@supabase/auth-js` on the same origin (`client.ts`, storage key `noonside.auth`), `RequireAuth`, `scopes.ts` mirroring `backend/internal/platform/auth/scopes.go`), `src/components/{ui,layout,data}` (design-system components), `src/features/<area>` (pages), `src/lib` (formatting, periods, CSV).
- **API calls:** use the generated TanStack Query options from `@/api/gen/@tanstack/react-query.gen`, named by `operationId`: `useQuery(getSystemOptions({ path: { systemId } }))`, `useMutation(updateSystemMutation())` then `mutateAsync({ path, body })`; outside React the SDK functions in `@/api/gen/sdk.gen` (`(await getPushConfig()).data`). Query keys are `[{ _id: operationId, path, query, … }]`: after a mutation call `useInvalidate()` with the affected operationIds (`invalidate('listInverters', 'getDashboard')`), and filter the cache with `queryId(q)` from `@/api/queries`. Every error response is thrown as `ApiError` (status + message). Zod schemas for the API types are in `@/api/gen/zod.gen` (`zRole`, `zWebhookEvent`, …); `vite.config.ts` marks `z` pure so unused ones are tree-shaken. `api.yaml` is OpenAPI 3.1, which Hey API reads strictly: nullable is `type: [string, "null"]` (ogen rejects a numeric `exclusiveMinimum`, so use `minimum`).
- **Design:** tokens live in `src/styles/tokens.css` (light + dark); `--accent` is the brand orange `#c2410c` in both themes (fills: buttons, switches, focus), `--accent-text` the orange for text and icons (lighter `#ea580c` in dark mode, where `#c2410c` is too dark to read), `--brand-ink` the sign-in panel, `--brand-sun` the logo tile (`LogoMark` in `src/components/ui/Logo.tsx`). The orange is also written out in `index.html` (splash), `public/noonside-logo.svg`, `public/favicon.svg`, `pwa-assets.config.ts` and the email templates; after changing the logo, copy it to `favicon.svg` and run `mise run web:icons`, component classes in `src/styles/app.css` (ported from the Open Design prototype), exposed to Tailwind via `@theme inline`. Build UI from the components in `src/components` before adding new classes.
- **Time:** format instants with the system's time zone (`fmtTime(ts, sys.timezone)` etc.); plain `YYYY-MM-DD` dates are never shifted.
- **Permissions:** hide what the caller can't do with `can(me, systemId, scope)`; the server still enforces it.
- **Routes** must match links the backend sends: invitations go to `/invitations/:token`, password recovery to `/reset-password` (allow-listed in `GOTRUE_URI_ALLOW_LIST`). The service worker must not answer `/auth/` navigations (GoTrue's email links), see the denylist in `src/sw.ts`.
- **Forms:** react-hook-form + zod; map server 400/409 messages to fields with `applyServerError`.

## Adding a new email

Emails live in `backend/internal/platform/email`. Each email is a Go struct plus two templates.

1. **Create the type** in `backend/internal/platform/email/<name>.go`. The struct is the template data, available as `.Data`, and methods on it can be called from templates:

   ```go
   type PasswordReset struct {
       UserName  string
       ResetURL  string
       ExpiresAt time.Time
   }

   func (PasswordReset) Name() string      { return "password_reset" } // selects the template files
   func (PasswordReset) Subject() string   { return "Reset your password" }
   func (PasswordReset) Preheader() string { return "Use this link to choose a new password." }

   func init() {
       Register(func() Email { // sample data for previews and tests
           return PasswordReset{UserName: "Alex", ResetURL: "http://localhost:5173/reset/x", ExpiresAt: time.Now().Add(time.Hour)}
       })
   }
   ```

2. **Add `templates/<name>.html`.** It must define `content`, and may override `footer`. Build it from the partials in `templates/_partials.html` and keep styles inline:

   ```html
   {{define "content"}}
   {{template "heading" "Reset your password"}}
   {{template "paragraph" (printf "Hi %s, someone asked to reset your password." .Data.UserName)}}
   {{template "facts" (rows "Valid until" (datetime .Data.ExpiresAt))}}
   {{template "button" (map "URL" .Data.ResetURL "Label" "Choose new password")}}
   {{template "fallback-link" .Data.ResetURL}}
   {{end}}
   ```

   The template helpers `map`, `rows`, `date`, `datetime`, `title` and `repeat` are defined in `render.go`. `.App.Name` and `.App.URL` are available everywhere.

3. **Add `templates/<name>.txt`,** the plain-text version. It also defines `content`, and optionally `footer`.

4. **Send it** from a handler through the `Mailer` interface:

   ```go
   err := h.mailer.Send(ctx, user.Email, email.PasswordReset{...})
   ```

   Only call this when `h.mailer.Enabled()` is true. If the email is not essential to the request, log a failure instead of failing the request (see `sendInvitationEmail` in `backend/internal/modules/systems/systemsapi/members.go`).

5. **Check it:**
   - `go test ./internal/platform/email/` (in `backend/`) renders every registered sample and fails if a template has no `Register` call, or the reverse.
   - `noonside email preview <name> > /tmp/x.html` renders it with sample data.
   - `ENVIRONMENT=dev noonside email send-test <name>` sends it to Mailpit from `compose.yaml` (http://127.0.0.1:54324).
