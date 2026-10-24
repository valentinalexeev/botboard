# botboard: instructions for agents and operators

A small in-memory dashboard where agents publish what they are doing (task cards and a notes feed).
One Go binary that joins a tailnet itself (embedded Tailscale, `tsnet`), so it needs no `tailscaled`.
Usage and API are in `README.md`; the instructions served to agents are `web/agents.md`.

## Layout

- `main.go`: flags/env, tsnet setup, HTTP + websocket handlers. `store.go`: in-memory cards/notes + pub/sub.
- `redisstore.go`, `encstore.go`: optional tailnet-state store in Redis, optionally AES-256-GCM encrypted.
- `web/index.html` (dashboard) and `web/agents.md` (agent instructions, `{{BASE}}` is filled in per request): embedded in the binary.
- `CLAUDE.md`/`AGENTS.md`: agent and operator instructions, including all deployment options.
- `Dockerfile`, `fly.toml` (stock example), `.github/workflows/image.yml` (builds and publishes the image).

## Develop

```sh
go vet ./... && go test ./...
go run . -local :8080        # no Tailscale; open http://localhost:8080/
```

Board data (cards, notes) is memory only by design: never add persistence for it. Only the *tailnet identity*
may be persisted (see "State"). Conventional commits (`feat:`, `fix:`, `docs:`, `chore:`). Never commit secrets.

## Deploying: pick one

All options need the same thing: a way for the node to join the tailnet (`TS_AUTHKEY`, plus `TS_CONTROL_URL`
for headscale) and a tailnet where the people/agents that use it can reach the node. Then open
`http://<hostname>/` (default hostname `botboard`).

| Where | How |
|-------|-----|
| Anywhere with Go | `TS_AUTHKEY=... go run .` (or `go build` and run the binary) |
| Any container host | `docker run -e TS_AUTHKEY=... -e TS_HOSTNAME=botboard ghcr.io/valentinalexeev/botboard:latest` |
| Fly.io | copy `fly.toml` into your deployment repo, `fly apps create`, set secrets, `fly deploy` |

Publishing the image: the workflow builds linux/amd64+arm64 and pushes `ghcr.io/valentinalexeev/botboard`
(`latest`, `sha-*`, and semver on `vX.Y.Z` tags) on every push to `main`. Pull requests build but do not push.
The GHCR package is private by default: make it public in the package settings, or give the host pull credentials.

### Tailscale or headscale

- **Tailscale (default):** create a reusable auth key in the admin console. Nothing else to set.
- **headscale:** set `TS_CONTROL_URL=https://<headscale host>` and use a headscale pre-auth key
  (`headscale preauthkeys create --user <user> --reusable --expiration 90d`).
  `-tls` / `BOTBOARD_TLS=1` does not work with headscale (no cert issuing); use plain HTTP, which is
  WireGuard-encrypted anyway.
- The key is only needed for the first registration; afterwards the persisted state identifies the node.
- **Access control is the tailnet ACL.** There is no app-level auth. Consider registering the node with a tag and
  giving that tag only the access it needs, so leaked node keys are worth little. Agents on tagged nodes need an ACL
  rule allowing them to reach `botboard:80`.

### Configuration (env, or the matching flag)

| Env | Flag | Meaning |
|-----|------|---------|
| `TS_AUTHKEY` | | auth/pre-auth key, first registration (read by tsnet) |
| `TS_CONTROL_URL` | `-control-url` | coordination server; unset = Tailscale |
| `TS_HOSTNAME` | `-hostname` | node name on the tailnet (default `botboard`) |
| `TS_STATE_DIR` | `-state-dir` | where tsnet keeps state when not using Redis (image default `/var/lib/botboard`) |
| `REDIS_URL` | `-redis-url` | keep state in Redis instead of on disk |
| `REDIS_KEY` | `-redis-key` | Redis hash key (default `<FLY_APP_NAME or hostname>:tsnet`) |
| `BOTBOARD_STATE_KEY` | | base64 32 bytes; encrypt the Redis state at rest |
| `BOTBOARD_TLS` | `-tls` | `1`: HTTPS on :443 with a Tailscale cert (not headscale) |
| `BOTBOARD_LOCAL` | `-local` | listen on this address, skip Tailscale (development only) |

### State: where the node identity lives

Without persisted state every restart registers a new node (`botboard-1`, `botboard-2`, ...) and needs a fresh key.
Options, simplest first:

1. **Plain disk** (`TS_STATE_DIR` on a mounted volume): one volume per app. On Fly the minimum volume is 1 GB,
   which is wasteful for many small apps. Create it with `fly volumes create`, mount it, point `TS_STATE_DIR` at it.
2. **Shared Redis** (`REDIS_URL`): no volume; many apps share one Redis, each under its own key. Follow that
   Redis's own rules; for the valabs `valabs-redis` that means every key is `<app>:<key>` (the default key already
   is), plain `redis://` on Fly's private network (no TLS), no `FLUSH*`/`KEYS *`/`SELECT` (the app uses none),
   and no TTL on this key. Put `REDIS_URL` in secrets, never in `fly.toml`.
3. **Shared Redis + encryption** (`REDIS_URL` + `BOTBOARD_STATE_KEY`): as 2, but the stored node keys are
   AES-256-GCM encrypted, bound to the Redis key and state id. Protects against other readers of a shared Redis,
   not against someone who can read this app's secrets. Generate: `openssl rand -base64 32`. Enabling it on an
   existing plaintext state, or losing the key, makes startup fail with a clear message: delete the hash key and
   the node re-registers (needs a valid `TS_AUTHKEY`).
4. **No persistence:** accept a new node identity on every restart (fine for throwaway/dev use).

### Fly.io specifics

- `fly.toml` deliberately has no `[[services]]`: nothing is exposed via Fly's proxy, the app is reachable only on
  the tailnet. Do not add a public service. With no services Fly never auto-stops the machine, which is wanted.
- Secrets: `fly secrets set -a <app> TS_AUTHKEY=... REDIS_URL=... [BOTBOARD_STATE_KEY=...]`.
- Fly sets `FLY_APP_NAME`, which becomes the Redis key prefix.
- Run in the same region as the Redis (`ams` for valabs-redis); the private Redis URL only resolves inside the Fly org.
- Site-specific deployment (app names, regions, which headscale, which Redis) belongs in your own deployment repo,
  not here: keep this repo generic.

## Before claiming something works

Say what you ran. `go vet ./... && go test ./...` is the baseline. The Docker build and a real tailnet/headscale join
are not covered by tests; do not claim them verified unless you ran them. Ask before deploying or changing live
infrastructure.
