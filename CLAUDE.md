# CLAUDE.md

Guidance for Claude when working in this repository.

## Project

**x3f** answers: *what is running where, in which version, who owns it, and what needs attention next?* That means known vulnerabilities, end-of-life dates and, for blockchain nodes, upcoming hard forks. Tagline: "The patch exists. Is it applied?" See `README.md` for the full vision and roadmap.

Solo open-source project, built in spare time. The maintainer is a DevOps engineer.

## Current scope: version 1 = Ethereum node module

Goal: for each configured node, report whether its clients are up to date, whether a known security advisory affects them, and whether they are ready for the next hard fork.

1. **Config** (`x3f.yaml`): list of nodes with name, execution client JSON-RPC URL and beacon API URL.
2. **Collect versions**
   - Execution client (Geth, Reth, Nethermind, Erigon, Besu): JSON-RPC `web3_clientVersion`
   - Consensus client (Lighthouse, Prysm, Teku, Nimbus, Lodestar, Grandine): `GET /eth/v1/node/version`
   - Sync status: `eth_syncing` and `GET /eth/v1/node/syncing`
3. **Compare**
   - Latest version: GitHub releases of each client repo
   - Vulnerabilities: GitHub security advisories of the same repos
   - Fork readiness: a curated `forks.yaml` (next fork, date, minimum version per client). There is no official machine-readable source for this, so the curated file is part of the product.
4. **Output**: CLI table with green / yellow / red status, plus a Telegram alert when something turns red.

**Out of scope for v1:** MEV-Boost (no version API), other chains, web UI, database, applying updates, the company/server module.

## Tech decisions

- **Language: Go** (current stable). Prefer the standard library; add dependencies only when clearly worth it, and say why.
- Single static binary.
- Secrets (GitHub token, Telegram bot token) come from environment variables, never from the config file and never in logs.
- All HTTP calls have timeouts and context cancellation.
- GitHub API is rate-limited (60 requests/hour without a token): cache responses on disk and support an optional token.

## Layout

```
cmd/x3f/              CLI entry point
internal/config/      load and validate x3f.yaml
internal/ethereum/    query clients for version and sync status, parse version strings
internal/releases/    GitHub releases (with disk cache) and security advisories, version comparison
internal/status/      green / yellow / red rules and findings
internal/forks/       load and evaluate forks.yaml
internal/notify/      Telegram
internal/*/testdata/  per-package test fixtures (e.g. recorded API responses), with their source noted
```

## Principles

- **Read-only.** x3f never changes anything on the systems it inspects.
- **Plug in, don't replace.** Integrate with existing tools (Prometheus, Grafana, GitHub) instead of duplicating them.
- **Small and focused.** One building block at a time, working end to end before the next.

## Testing

- No real network access in unit tests. Use `net/http/httptest` mock servers that replay real client responses from the package's `testdata/` directory. Each fixture notes its source (official docs or client source code); never invent responses.
- Table-driven tests, especially for version-string parsing (each client formats its version differently).
- Development starts against mock servers; a real testnet node comes later.
- Run `gofmt`, `go vet` and `go test ./...` before considering a step done.

## Conventions

- Code, comments, commit messages and docs in English.
- The maintainer writes in German; reply in German.
- Before larger changes, briefly explain the plan and wait for a go-ahead.
- Keep changes small and reviewable. Explain non-obvious Go idioms, since the maintainer is learning as he goes.

## Build order

1. Project skeleton: `go.mod`, `cmd/x3f`, config loading, `.gitignore`
2. Read versions from execution and consensus clients (with mock servers and tests), print a table
3. Compare against GitHub releases (with caching)
4. `forks.yaml` and fork-readiness check
5. GitHub security advisories
6. Telegram alerts