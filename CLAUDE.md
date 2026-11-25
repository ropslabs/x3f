# CLAUDE.md

Guidance for Claude when working in this repository.

## Project

**x3f** answers: *what is running where, in which version, who owns it, and what needs attention next?* That means known vulnerabilities, end-of-life dates and, for blockchain nodes, upcoming hard forks. Tagline: "The patch exists. Is it applied?" See `README.md` for the full vision and roadmap.

Solo open-source project, built in spare time. The maintainer is a DevOps engineer.

## Current scope: version 1 = Ethereum node module

Goal: for each configured node, report whether its clients are up to date, whether a known security advisory affects them, and whether they are ready for the next hard fork.

1. **Config** (`x3f.yaml`): list of nodes with name, execution client JSON-RPC URL and beacon API URL.
2. **Collect versions**
   - Execution client (Geth, Reth, Nethermind, Erigon, Besu, Ethrex): JSON-RPC `web3_clientVersion`
   - Consensus client (Lighthouse, Prysm, Teku, Nimbus, Lodestar, Grandine): `GET /eth/v1/node/version`
   - Sync status: `eth_syncing` and `GET /eth/v1/node/syncing`
3. **Compare**
   - Latest version: GitHub releases of each client repo
   - Vulnerabilities: GitHub security advisories of the same repos
   - Fork readiness: a curated `forks.yaml` (next fork, date, minimum version per client). There is no official machine-readable source for this, so the curated file is part of the product.
4. **Output**: CLI table with green / yellow / red status, plus Telegram notifications:
   - only on status changes per node (worse and back to good), using a state file; a red node that gets a second problem (a check gets worse) is reported too;
   - `min_level` warn (default) or crit; with crit, leaving CRIT is still reported (all clear);
   - a node that stays red gets at most one reminder per `remind_every` (default 24h, 0 disables);
   - a failed notification is a global yellow finding (exit code at least 1) and is retried on the next run.
5. **Metrics** (`-metrics-file`): Prometheus text format for the node_exporter textfile collector, written atomically (temp file without `.prom` in the same directory, then rename). Status 0/1/2 per node and per check (3 = unknown for a check that could not run), versions as info metrics with value 1, `x3f_last_run_timestamp_seconds` and `x3f_last_run_success` (GitHub data, notification, state file). No free text in labels. On a hard error (exit 3) the file is left alone.

**Out of scope for v1:** MEV-Boost (no version API), other chains, web UI, database, applying updates, the company/server module.

## Tech decisions

- **Language: Go** (current stable). Prefer the standard library; add dependencies only when clearly worth it, and say why.
- Single static binary.
- Secrets (GitHub token, Telegram bot token `X3F_TELEGRAM_BOT_TOKEN`) come from environment variables, never from the config file and never in logs. The bot token is part of the Telegram request URL: never print request URLs.
- Telegram messages use `parse_mode` HTML with all dynamic text escaped; no MarkdownV2. The API address is not configurable (only tests replace it).
- All HTTP calls have timeouts and context cancellation.
- GitHub API is rate-limited (60 requests/hour without a token): cache responses on disk and support an optional token.

## Layout

```
cmd/x3f/              CLI entry point
internal/config/      load and validate x3f.yaml
internal/network/     known networks (mainnet, sepolia, hoodi) and chain IDs
internal/ethereum/    query clients (version, sync status, chain ID, fork config), parse version strings
internal/releases/    GitHub releases (with disk cache) and security advisories, version comparison
internal/status/      green / yellow / red rules and findings
internal/forks/       load and evaluate forks.yaml
internal/notify/      decide what to notify (state file) and send it to Telegram
internal/metrics/     Prometheus text format, atomic write for the node_exporter textfile collector
internal/*/testdata/  per-package test fixtures (e.g. recorded API responses), with their source noted
scripts/              release build (build-release.sh)
.github/workflows/    CI (gofmt, vet, test -race, build) and release
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

## Releases

- Pushing a tag `vX.Y.Z` runs `.github/workflows/release.yml`: checks, `scripts/build-release.sh` (6 targets, archives, `SHA256SUMS`), GitHub artifact attestations, GitHub release. Tags with a hyphen (`v0.2.0-rc.1`) become pre-releases.
- Release notes: `docs/releases/vX.Y.Z.md` (also used for its release candidates), written for x3f users, with upgrade notes and known limitations; GitHub's generated notes follow. Bump `VERSION=` in the README install section only right before the final tag, not for a release candidate.
- Before tagging: CI green on main, `forks.yaml` checked against the official sources (see below).
- Actions are pinned to commit SHAs; update them together with the version comment.

## Updating forks.yaml

- Data only from official sources (EF blog, ethereum/pm, specs); read the original page text, not a summary. Add the source to the fork entry and to `internal/forks/SOURCES.md`.
- The loader checks `timestamp = genesis_time + epoch × 384`; a failure is almost always a typo.
- Refresh the eth-clients snapshot in `internal/forks/testdata/eth-clients` and run `TestCrossCheckEthClients`. Report deviations to the maintainer; never copy values from eth-clients.
- Bump `updated:`.

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
6. Telegram notifications on status changes
7. Prometheus metrics (e.g. a file for the node_exporter textfile collector)

Steps 1–7 are done (1–4 released as v0.1.0); version 1 is complete. Later, not scheduled yet: Nagios output format (with the company module; the first audience, solo stakers, uses Prometheus, Grafana and Telegram) and the forkId check.

While working on steps 5–7: where it costs no extra effort, do not hard-wire logic to the execution/consensus pair, so the component model below stays a small refactor.

## Roadmap after version 1

Recorded for later; not part of v1, do not implement yet.

1. **Damping against flapping**, directly after version 1: report a status change only after it persisted for two runs, at least for "unreachable", so a single timeout does not cause two messages.
2. **General component model.** A component has a type, a version source, an upstream repository and a health check. Execution and consensus clients become the first types: compiled-in implementations of one shared interface, not Go's `plugin` package.
3. **Local web UI** on top of that model: `x3f serve`, embedded in the binary, listening on localhost by default, read-only.
4. **New component types:** validator client, MEV-Boost, later L2 nodes and indexers.