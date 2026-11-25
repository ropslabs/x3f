# x3f

**The patch exists. Is it applied?**

x3f tells you what is running where, in which version, who owns it, and what needs attention next: known vulnerabilities, end-of-life dates and, for blockchain nodes, upcoming hard forks.

> **Status:** early stage (v0.x). The Ethereum node check below works today; inventory, security advisories and alerts are planned. Flags and output may still change between 0.x releases.
>
> *An n-day is a known vulnerability that already has a fix. Many attacks don't need a zero-day, just a system where the existing patch was never applied. x3f finds those systems.*

---

## Quick start

x3f checks Ethereum nodes: network, client versions, sync status, whether the clients are behind their latest stable release, and whether they are ready for the next hard fork. It is a single binary that only reads from your nodes.

### Install

Download the archive for your system from the [releases page](https://github.com/ropslabs/x3f/releases) and check it against `SHA256SUMS`:

```sh
VERSION=0.1.0
curl -LO https://github.com/ropslabs/x3f/releases/download/v$VERSION/x3f_${VERSION}_linux_amd64.tar.gz
curl -LO https://github.com/ropslabs/x3f/releases/download/v$VERSION/SHA256SUMS
sha256sum -c --ignore-missing SHA256SUMS
tar -xzf x3f_${VERSION}_linux_amd64.tar.gz
sudo install x3f_${VERSION}_linux_amd64/x3f /usr/local/bin/
x3f -version
```

Archives exist for Linux and macOS (`tar.gz`) and Windows (`zip`), each for `amd64` and `arm64`. With the [GitHub CLI](https://cli.github.com) you can also check that an archive was built by this repository's release workflow: `gh attestation verify x3f_${VERSION}_linux_amd64.tar.gz --repo ropslabs/x3f`.

The macOS binaries are not signed. If macOS refuses to start x3f, remove the quarantine flag: `xattr -d com.apple.quarantine x3f`.

With Go 1.27.4 or newer you can install from source instead:

```sh
go install github.com/ropslabs/x3f/cmd/x3f@latest
```

### Configure

Copy [`x3f.example.yaml`](x3f.example.yaml) to `x3f.yaml` and list your nodes:

```yaml
nodes:
  - name: mainnet-1
    execution_url: http://localhost:8545   # execution client JSON-RPC (HTTP)
    beacon_url: http://localhost:5052      # consensus client beacon API
    network: mainnet                       # optional: expected network
```

x3f needs HTTP access to these methods, all read-only:

- execution client: `web3_clientVersion`, `eth_syncing`, `eth_chainId`, `eth_config`
- consensus client: `GET /eth/v1/node/version`, `/eth/v1/node/syncing`, `/eth/v1/config/deposit_contract`, `/eth/v1/config/spec`

Point x3f directly at each node, not at a proxy or load balancer (see [below](#fork-readiness)). Run it where the node APIs are reachable anyway, for example on the node host or in the same private network; do not open the APIs to the internet for x3f.

### Run

```sh
x3f -config x3f.yaml
```

x3f prints a table, the findings and a summary, and exits with `0` (OK), `1` (warning), `2` (critical) or `3` (x3f could not run), so scripts and monitoring systems can act on the result (see [Exit codes](#exit-codes)). To check every 15 minutes with cron and keep the last result:

```
*/15 * * * * /usr/local/bin/x3f -config /etc/x3f/x3f.yaml > /var/lib/x3f/last.txt 2>&1
```

x3f asks GitHub for the latest releases and caches the answers for an hour. Without a token GitHub allows 60 requests per hour; with many different clients or frequent runs, set `GITHUB_TOKEN` (any token works, it needs no permissions).

### Supported

| | |
|---|---|
| Execution clients | Geth, Reth, Nethermind, Erigon, Besu, Ethrex |
| Consensus clients | Lighthouse, Prysm, Teku, Nimbus, Lodestar, Grandine |
| Networks | mainnet, Sepolia, Hoodi |

The fork schedule is built into x3f. To get new fork dates, update x3f; x3f adds a note when its fork data is older than 30 days. Until a new release is out, you can pass your own schedule with `-forks` (same format as [`internal/forks/forks.yaml`](internal/forks/forks.yaml)).

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

## Usage

> What works today: network detection, client versions, sync status, comparison with the latest stable GitHub release and fork readiness (asked from the node itself, with the announced client releases as fallback). Security advisories and alerts are not built yet.

Example output:

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
| `-version` | | print the version and exit |

### Fork readiness

The fork schedule is built into the binary ([`internal/forks/forks.yaml`](internal/forks/forks.yaml)): dates and client releases per network, taken only from official announcements, each with its source. x3f notes when the built-in data is older than 30 days.

For fork readiness x3f asks the node itself: the execution client via `eth_config` ([EIP-7910](https://eips.ethereum.org/EIPS/eip-7910)), the consensus client via `GET /eth/v1/config/spec` (fork epochs and `BLOB_SCHEDULE`). The answer is compared with the fork schedule. If a client does not answer (older releases, proxies that block the method), x3f falls back to comparing the client version with the announced releases and adds a note. If the execution and consensus client are on different networks, the fork check is skipped.

Point x3f directly at each node, not at a proxy or load balancer: behind one, every request may be answered by a different node, so version, sync status and fork schedule would not describe the same machine. Versions that look like a proxy (newer than every known release) are reported in yellow as a safety net.

### Environment

`GITHUB_TOKEN` (optional) raises the GitHub API rate limit from 60 to 5000 requests per hour; it is never stored or printed. `NO_COLOR` disables colors.

### Exit codes

The exit codes follow the [Nagios plugin convention](https://nagios-plugins.org/doc/guidelines.html):

| Code | Meaning |
|---|---|
| `0` | OK: everything is green |
| `1` | WARNING: at least one yellow finding |
| `2` | CRITICAL: at least one red finding |
| `3` | UNKNOWN: x3f could not run (invalid flags or config) |

The output itself is not yet in the Nagios plugin format: Nagios shows the first output line as the check result, and for x3f that is currently the table header. A dedicated Nagios output format is planned for a later release.

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

[Apache License 2.0](LICENSE)