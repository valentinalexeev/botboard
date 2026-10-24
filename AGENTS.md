# AGENTS.md

Instructions for agents working in this repo live in **[CLAUDE.md](CLAUDE.md)**: layout, how to develop, and the
deployment options (binary, container image, Fly.io; Tailscale or headscale; state on disk, Redis, or encrypted Redis).
Non-negotiables: board data stays in memory; no secrets in git; keep this repo generic (site-specific deployment
lives in the deployer's own repo); ask before deploying.
