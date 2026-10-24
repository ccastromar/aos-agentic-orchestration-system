# AOS UI

Web console of AOS, the *operate* vertex of the Chestertown trilogy: start predefined
workflows from the chat, follow their runs (DAG, timeline, human approvals) and browse the
history. Governed runs link to their decision in Prudence and their evidence in Ernest.

React 19 + Vite. The shell (top bar, module switcher, tabs, theme) and the design tokens come
from `@chestertown/design`, the shared design system, so AOS matches Prudence and Ernest in
light and dark mode. It is installed from a pinned tarball in `vendor/`, released from the
`chestertown-design` repository; to upgrade, run that repository's `scripts/release-local.sh`
and commit `vendor/`, `package.json` and the lock file.

## Run locally

```bash
npm ci
npm run dev
```

The dev server proxies the API paths (`/ask`, `/task`, `/api`, …) to the AOS backend at
`http://localhost:8080` (what `go run ./cmd/aos` and `run.sh` start); set `AOS_BACKEND_URL` to point elsewhere. `npm run build` writes the
production bundle to `dist/`, which the `Dockerfile` serves with nginx.

## Configuration

Build-time variables (also `ARG`s in the `Dockerfile`):

| Variable | Purpose |
|---|---|
| `VITE_OIDC_ISSUER` | Keycloak realm URL; enables SSO (OIDC + PKCE) when set |
| `VITE_OIDC_CLIENT_ID` | OIDC client, default `aos-spa` |
| `VITE_AOS_URL`, `VITE_PRUDENCE_URL`, `VITE_ERNEST_URL` | App switcher and deep links between the three consoles |
| `VITE_ENABLE_DEV_LOGIN` | `true` enables the development login (dev server only) |

## Authentication

The production UI uses the shared Chestertown SSO (OIDC/PKCE). The legacy username-and-role
modal is deliberately hidden by default: it talks to AOS' development-only authenticator, which
does not validate the password. To exercise it locally, run Vite with
`VITE_ENABLE_DEV_LOGIN=true`; it is not included in production builds. Only in that mode does
an anonymous session send the simulated role (`X-Mock-Role`) chosen in the chat panel.

## Checks

```bash
npm run lint
npm run test:e2e   # Playwright, against a running dev server and backend
```

## License

Part of AOS: Apache License, Version 2.0. See [LICENSE](../LICENSE) and [NOTICE](../NOTICE) at the repository root.
