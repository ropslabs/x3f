# x3f

**The patch exists. Is it applied?**

x3f tells you what is running where, in which version, who owns it, and what needs attention next: known vulnerabilities, end-of-life dates and, for blockchain nodes, upcoming hard forks.

> **Status:** early stage, not ready for production use. Everything below describes the plan for the first versions.
>
> *An n-day is a known vulnerability that already has a fix. Many attacks don't need a zero-day, just a system where the existing patch was never applied. x3f finds those systems.*

---

## Why

Most teams cannot answer these questions quickly:

- Which hosts and containers are running right now, and in which versions?
- Which of them are affected by the CVE published this morning?
- Which ones are past end of life, or about to be?
- Who is responsible for fixing it?

The problem is getting worse. AI-assisted research is finding vulnerabilities faster than ever, so finding them is no longer the bottleneck. Fixing them is. And you can only fix what you know is running.

Blockchain node operators have the same problem plus hard deadlines. If a node is not on the right client version before a hard fork, it ends up on the wrong chain, goes offline and loses rewards.

## What x3f does

### Version 1: see and warn

- **Inventory:** discovers hosts, containers and their software versions automatically.
- **Ownership:** records who is responsible for each service.
- **Matching:** compares versions against releases, security advisories (CVEs) and end-of-life dates.
- **Prioritization:** shows what is urgent, for example vulnerabilities that are actively exploited, instead of dumping a list of every CVE.
- **Alerts:** notifies you when something needs attention (Telegram first, more channels later).

### Web3 module: Ethereum nodes first

- Reads client versions directly from the node APIs (execution client, consensus client, MEV-Boost).
- Compares them with the latest releases and security advisories.
- Counts down to the next hard fork: "Your consensus client is too old for the fork in 6 days."

Built with solo stakers and small validator teams in mind, people who run nodes without an ops team behind them.

## Usage (preview)

> What works today: network detection, client versions, sync status, comparison with the latest stable GitHub release and fork readiness (asked from the node itself, with the announced client releases as fallback). Security advisories and alerts are not built yet.

Build the binary (requires Go) and describe your nodes in `x3f.yaml` (see [`x3f.example.yaml`](x3f.example.yaml)):

```sh
go build -o x3f ./cmd/x3f
./x3f -config x3f.yaml
```

```
STATUS  NODE       NETWORK  EXECUTION                     EL SYNC  CONSENSUS                   CL SYNC                         FORK
OK      mainnet-1  mainnet  Geth 1.17.10 (stable)         synced   Lighthouse 8.3.3            synced                          none scheduled
CRIT    hoodi-1    hoodi    Besu 23.1.1 (dev) -> 26.11.2  synced   Nimbus 26.10.2 (stateofus)  synced, optimistic, EL offline  EL below Fusaka minimum

Findings:
  CRIT  hoodi-1 execution: Besu dev build of 23.1.1 ("dev"): stable 23.1.1 released 1358 days ago; latest 26.11.2 (grace period 30 days)
  WARN  hoodi-1 consensus sync: optimistic: head not yet verified by the execution client
  CRIT  hoodi-1 consensus sync: execution client offline (reported by the consensus client)
  NOTE  hoodi-1 fork: fork check of the execution client by version only: eth_config: JSON-RPC error -32601: The method eth_config does not exist/is not available
  CRIT  hoodi-1 fork: Besu 23.1.1 is older than the minimum release for Fusaka (25.10.0): it cannot follow hoodi

Status: CRIT (1 OK, 0 WARN, 1 CRIT)
```

(Real output from 2026-11-25: mock nodes replaying the test fixtures, release data from the live GitHub API.)

Endpoint URLs are never printed, only node names, because provider URLs often contain API keys.

### Status rules

| | Green | Yellow | Red |
|---|---|---|---|
| **Version** | latest stable release (or newer, but not newer than every known release) | behind, but within the grace period; pre-release, dev build or unrecognized suffix; newer than every known release (proxy or custom build; x3f re-checks GitHub first); client not recognized; latest release unknown | behind for longer than the grace period (default 30 days, counted from the first newer stable release); client unreachable |
| **Network** | both clients on the same network (and the configured one, if `network:` is set) | network could not be detected | execution and consensus client on different networks; node on another network than configured |
| **Fork** (node answer) | the node schedules exactly the forks in the fork schedule | a fork the schedule does not list, more than 14 days ahead (the schedule may be outdated); not ready, more than 14 days before the fork, or no ready release known | not on the latest active fork; still schedules a postponed fork at its former time; an unknown fork within 14 days or already passed; not ready within 14 days although a ready release exists |
| **Fork** (version fallback) | ready for the next fork, or none scheduled | not ready, more than 14 days before the fork; no ready client release announced yet ("waiting for client release") | older than the minimum release of an active fork; release still contains a postponed fork's former schedule; not ready within 14 days although a ready release exists |
| **Sync** | synced | indexing; optimistic; up to 32 blocks/slots behind | execution client offline; more than 32 blocks/slots behind |

If GitHub is unreachable, cached release data younger than 7 days is used with a note only; older or missing data turns the status yellow.

### Flags

| Flag | Default | Meaning |
|---|---|---|
| `-config` | `x3f.yaml` | path to the config file |
| `-timeout` | `10s` | timeout per request |
| `-grace-days` | `30` | days a client may lag behind the latest stable release before it turns red |
| `-cache-dir` | `~/.cache/x3f` | where GitHub release data is cached (empty disables the cache) |
| `-cache-ttl` | `1h` | how long cached data is used without asking GitHub |
| `-forks` | built in | fork schedule file to use instead of the built-in one |
| `-fork-critical-days` | `14` | days before a fork from which a client that is not ready turns red |

The fork schedule is built into the binary ([`internal/forks/forks.yaml`](internal/forks/forks.yaml)): dates and client releases per network, taken only from official announcements, each with its source. x3f notes when the built-in data is older than 30 days.

For fork readiness x3f asks the node itself: the execution client via `eth_config` ([EIP-7910](https://eips.ethereum.org/EIPS/eip-7910)), the consensus client via `GET /eth/v1/config/spec` (fork epochs and `BLOB_SCHEDULE`). The answer is compared with the fork schedule. If a client does not answer (older releases, proxies that block the method), x3f falls back to comparing the client version with the announced releases and adds a note. If the execution and consensus client are on different networks, the fork check is skipped.

Point x3f directly at each node, not at a proxy or load balancer: behind one, every request may be answered by a different node, so version, sync status and fork schedule would not describe the same machine. Versions that look like a proxy (newer than every known release) are reported in yellow as a safety net.

Environment: `GITHUB_TOKEN` (optional) raises the GitHub API rate limit from 60 to 5000 requests per hour; it is never stored or printed. `NO_COLOR` disables colors.

### Exit codes

x3f follows the [Nagios plugin convention](https://nagios-plugins.org/doc/guidelines.html), so it can be used as a monitoring check directly:

| Code | Meaning |
|---|---|
| `0` | OK: everything is green |
| `1` | WARNING: at least one yellow finding |
| `2` | CRITICAL: at least one red finding |
| `3` | UNKNOWN: x3f could not run (invalid flags or config) |

## Design principles

- **Plug in, don't replace.** x3f works alongside the tools you already run: metrics for Prometheus, dashboards for Grafana, tickets in GitHub.
- **Read-only by default.** Version 1 only looks. It never changes anything on your systems.
- **Small and focused.** Do one thing really well before adding the next.
- **Open source.** Free to use, inspect and self-host.

## Roadmap

x3f grows in stages. Each one has to earn enough trust for the next.

| Stage | What x3f does |
|---|---|
| 1. Warn | Shows what is outdated, vulnerable or approaching a deadline |
| 2. Suggest | Opens a pull request or ticket with the exact change, a changelog summary and a risk estimate |
| 3. One click | Runs the update with pre-checks, a health check afterwards and automatic rollback |
| 4. Automatic for low risk | Applies safe updates (e.g. patch releases of stateless containers) on its own, one instance first |
| 5. Autonomous | Only where you explicitly allow it |

Later stages may use anonymous, opt-in data about how updates went for other users ("this update ran cleanly for 40 operators") to estimate how safe an update is. This will always be opt-in and never include identifying information.

## Contributing

The project is just getting started. Ideas, issues and feedback are very welcome, especially from people who run their own infrastructure or nodes and know the pain.

## License

To be decided (an OSI-approved open source license).