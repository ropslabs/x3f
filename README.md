# x3f

**The patch exists. Is it applied?**

x3f tells you what is running where, in which version, who owns it, and what needs attention next: known vulnerabilities, end-of-life dates and, for blockchain nodes, upcoming hard forks.

> **Status:** early stage (v0.x). The Ethereum node check below works today, including known security advisories of the clients, Telegram notifications on status changes and Prometheus metrics; inventory is planned. Flags and output may still change between 0.x releases.
>
> **The Ethereum module is paused.** Development continues with a module for [Aztec](https://aztec.network) nodes; its first part, version and security advisories, is in the main branch (see [Aztec nodes](#aztec-nodes)). The Ethereum checks keep working, but the built-in fork data is not maintained after 2026-11-27; see [Fork readiness](#fork-readiness) for the known gap on Hoodi.
>
> *An n-day is a known vulnerability that already has a fix. Many attacks don't need a zero-day, just a system where the existing patch was never applied. x3f finds those systems.*

---

## Quick start

x3f checks Ethereum nodes: network, client versions, sync status, whether the clients are behind their latest stable release, whether a published security advisory affects them, and whether they are ready for the next hard fork. For Aztec nodes it checks the version and the security advisories so far (see [Aztec nodes](#aztec-nodes)). It can send a Telegram message when a node's status changes and write Prometheus metrics. It is a single binary that only reads from your nodes.

### Install

Download the archive for your system from the [releases page](https://github.com/ropslabs/x3f/releases) and check it against `SHA256SUMS`:

```sh
VERSION=0.2.0
curl -LO https://github.com/ropslabs/x3f/releases/download/v$VERSION/x3f_${VERSION}_linux_amd64.tar.gz
curl -LO https://github.com/ropslabs/x3f/releases/download/v$VERSION/SHA256SUMS
sha256sum -c --ignore-missing SHA256SUMS
tar -xzf x3f_${VERSION}_linux_amd64.tar.gz
sudo install x3f_${VERSION}_linux_amd64/x3f /usr/local/bin/
x3f -version
```

Archives exist for Linux and macOS (`tar.gz`) and Windows (`zip`), each for `amd64` and `arm64`. With the [GitHub CLI](https://cli.github.com) you can also check that an archive was built by this repository's release workflow: `gh attestation verify x3f_${VERSION}_linux_amd64.tar.gz --repo ropslabs/x3f`. The GitHub CLI needs to be logged in for this (`gh auth login`), even though the repository is public.

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

x3f asks GitHub for the latest releases and security advisories and caches the answers for an hour: two requests per client and hour, however many nodes run that client. Without a token GitHub allows 60 requests per hour; with many different clients or frequent runs, set `GITHUB_TOKEN` (any token works, it needs no permissions).

### Supported

| | |
|---|---|
| Execution clients | Geth, Reth, Nethermind, Erigon, Besu, Ethrex |
| Consensus clients | Lighthouse, Prysm, Teku, Nimbus, Lodestar, Grandine |
| Networks | mainnet, Sepolia, Hoodi |
| Aztec | node version (releases of [aztec-node](https://github.com/aztec-labs-eng/aztec-node)) and advisories of [aztec-packages](https://github.com/AztecProtocol/aztec-packages) (see [Aztec nodes](#aztec-nodes)) |

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

> What works today: network detection, client versions, sync status, comparison with the latest stable GitHub release, security advisories of the client repositories, fork readiness (asked from the node itself, with the announced client releases as fallback), Telegram notifications on status changes and Prometheus metrics for the node_exporter textfile collector.

Example output:

```
STATUS  NODE       NETWORK  EXECUTION                     EL SYNC  CONSENSUS                   CL SYNC  FORK                     ADVISORIES
OK      mainnet-1  mainnet  Geth 1.17.10 (stable)         synced   Lighthouse 8.3.3            synced   none scheduled           none known
CRIT    mainnet-2  mainnet  Besu 23.1.1 (dev) -> 26.11.2  synced   Nimbus 26.10.2 (stateofus)  synced   EL below Fusaka minimum  EL 38 (high)

Findings:
  CRIT  mainnet-2 execution: Besu dev build of 23.1.1 ("dev"): stable 23.1.1 released 1358 days ago; latest 26.11.2 (grace period 30 days)
  NOTE  mainnet-2 fork: fork check of the execution client by version only: eth_config: JSON-RPC error -32601: The method eth_config does not exist/is not available
  CRIT  mainnet-2 fork: Besu 23.1.1 is older than the minimum release for Fusaka (25.11.0): it cannot follow mainnet
  CRIT  mainnet-2 execution advisories: Besu 23.1.1 is affected by 38 published advisories (13 high, 19 medium, 6 low), fixed in 26.11.0 or later: GHSA-4h4f-925g-8h9p (high: "EIP-7702 and sender signature recovery skips the secp256k1 curve-orde…"), GHSA-m886-r6ch-pjvr (high: "QBFT/IBFT2 message decode performs unbounded eager signature recovery…"), GHSA-g5j7-4r2x-rx9j (high: "A permissionless non-validator peer can halt QBFT finality through pr…") and 35 more; https://github.com/besu-eth/besu/security/advisories
  NOTE  mainnet-2 execution advisories: cannot tell whether Besu 23.1.1 is affected by GHSA-v5gq-r843-8f73 (medium: "SEC-50 RLPx AES CTR keystream reusage #1315. (Case for private networ…"), check it yourself (affected versions "all", patched "")

Status: CRIT (1 OK, 0 WARN, 1 CRIT)
```

(Real output from 2026-11-25: mock nodes replaying the test fixtures, release and advisory data from the live GitHub API.)

Endpoint URLs are never printed, only node names, because provider URLs often contain API keys.

### Status rules

| | Green | Yellow | Red |
|---|---|---|---|
| **Version** | latest stable release (or newer, but not newer than every known release) | behind, but within the grace period; pre-release, dev build or unrecognized suffix; newer than every known release (proxy or custom build; x3f re-checks GitHub first); client not recognized; latest release unknown | behind for longer than the grace period (default 30 days, counted from the first newer stable release); client unreachable |
| **Network** | both clients on the same network (and the configured one, if `network:` is set) | network could not be detected | execution and consensus client on different networks; node on another network than configured |
| **Fork** (node answer) | the node schedules exactly the forks in the fork schedule | a fork the schedule does not list, more than 14 days ahead (the schedule may be outdated); not ready, more than 14 days before the fork, or no ready release known | not on the latest active fork; still schedules a postponed fork at its former time; an unknown fork within 14 days or already passed; not ready within 14 days although a ready release exists |
| **Advisories** | no published advisory affects the running version ("none known"); a medium or low advisory without a released fix is only a note | affected by a medium or low advisory (or one without severity) whose fix is released; affected by a critical or high advisory (or one without severity) whose fix is not released yet; advisory data unknown | affected by a critical or high advisory whose fix is released, with no grace period |
| **Fork** (version fallback) | ready for the next fork, or none scheduled | not ready, more than 14 days before the fork; no ready client release announced yet ("waiting for client release") | older than the minimum release of an active fork; release still contains a postponed fork's former schedule; not ready within 14 days although a ready release exists |
| **Sync** | synced | indexing; optimistic; up to 32 blocks/slots behind | execution client offline; more than 32 blocks/slots behind |

If GitHub is unreachable, cached release and advisory data younger than 7 days is used with a note only; older or missing data turns the status yellow.

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
| `-state-file` | `~/.local/state/x3f/state.json` | where x3f remembers what it last notified about (`$XDG_STATE_HOME` is respected) |
| `-no-notify` | | send no notifications and leave the state file alone, for manual runs |
| `-notify-test` | | send a test message to the configured Telegram chat and exit |
| `-metrics-file` | | write Prometheus metrics to this file (see [below](#prometheus-metrics)) |
| `-version` | | print the version and exit |

### Fork readiness

The fork schedule is built into the binary ([`internal/forks/forks.yaml`](internal/forks/forks.yaml)): dates and client releases per network, taken only from official announcements, each with its source. x3f notes when the built-in data is older than 30 days.

For fork readiness x3f asks the node itself: the execution client via `eth_config` ([EIP-7910](https://eips.ethereum.org/EIPS/eip-7910)), the consensus client via `GET /eth/v1/config/spec` (fork epochs and `BLOB_SCHEDULE`). The answer is compared with the fork schedule. If a client does not answer (older releases, proxies that block the method), x3f falls back to comparing the client version with the announced releases and adds a note. If the execution and consensus client are on different networks, the fork check is skipped.

Point x3f directly at each node, not at a proxy or load balancer: behind one, every request may be answered by a different node, so version, sync status and fork schedule would not describe the same machine. Versions that look like a proxy (newer than every known release) are reported in yellow as a safety net.

**The fork data is not maintained after 2026-11-27.** It was last checked against the official sources on that day and will not follow later announcements. Known gap: [ACDC #192](https://github.com/ethereum/pm/issues/2305) rescheduled **Hoodi Glamsterdam** on 2026-11-26 to epoch 143616 (2026-12-15 19:12 UTC), but the built-in data still lists it as postponed. A Hoodi node that schedules the new date is reported as "schedules unknown fork": yellow, red from 2026-12-01 19:12 UTC (14 days before the fork). To check Hoodi nodes correctly, copy [`internal/forks/forks.yaml`](internal/forks/forks.yaml), replace its last entry (Hoodi Glamsterdam with `status: postponed`) with the following and pass the copy with `-forks`:

```yaml
      - name: Glamsterdam
        execution: Amsterdam
        consensus: Gloas
        epoch: 143616
        timestamp: 1797361944
        sources:
          - https://github.com/ethereum/pm/issues/2305
```

The entry lists no client releases, so a node that does not schedule the fork yet stays yellow ("waiting for client release") instead of turning red.

### Security advisories

x3f reads the published [security advisories](https://docs.github.com/en/code-security/concepts/vulnerability-reporting-and-management/repository-security-advisories) of each client's GitHub repository and checks the running version against the affected versions. A finding names how many advisories affect the node, the release that fixes all of them, and the most severe three with their GHSA ID, CVE and title, so you can judge whether they apply to your setup (for example, many Besu advisories concern QBFT, IBFT or Clique, consensus protocols of private networks). Advisories whose affected versions x3f cannot determine are listed as a note to check yourself.

Without a released fix there is no update to install. A critical or high advisory without a fix stays yellow, since you may need a mitigation from the advisory; a medium or low one is only a note, so a node does not stay yellow with nothing you can do. It still counts in the ADVISORIES column and in the metrics.

"none known" does not mean "no known vulnerabilities": x3f only sees what a client publishes as a GitHub advisory. As of November 2026, Reth, Nethermind, Ethrex, Prysm, Nimbus and Grandine had none published there; check their release notes and security channels as well.

#### Ignoring an advisory

If an advisory does not apply to your setup, list it in `x3f.yaml`, for all nodes or for one:

```yaml
ignore_advisories:
  - id: GHSA-v5gq-r843-8f73
    reason: "private networks only; we run mainnet"   # required
    expires: 2027-03-01                                # optional
nodes:
  - name: mainnet-1
    # ...
    ignore_advisories:                                 # adds to the global list
      - id: GHSA-m886-r6ch-pjvr
        reason: "QBFT/IBFT2 only; we run mainnet"
```

An ignored advisory no longer counts for the status and the ADVISORIES column, but it is still listed as a note with its reason, so nothing disappears silently. From the `expires` date (00:00 UTC) the entry no longer applies: the advisory counts again and a note says so. A node entry replaces a global entry with the same ID. An ID that is not among the advisories of the clients it applies to is reported as a note, to catch typos. For a global entry these are the clients your nodes run, so the note may also mean the entry is for a client none of them runs right now. This check only runs when x3f knows every client and its advisories.

### Aztec nodes

An Aztec node is a node with `type: aztec`. It needs its JSON-RPC endpoint and its L1, an Ethereum node:

```yaml
nodes:
  - name: eth-mainnet-1
    execution_url: http://localhost:8545
    beacon_url: http://localhost:5052
  - name: aztec-1
    type: aztec
    url: http://localhost:8080      # the node's JSON-RPC endpoint
    l1: eth-mainnet-1               # the Ethereum node above
    network: mainnet                # optional: expected network (mainnet, testnet)
  - name: aztec-2
    type: aztec
    url: http://aztec-2:8080
    l1_url: https://eth.example.com/v3/<key>   # or: an L1 provider endpoint
```

Set exactly one of `l1` and `l1_url`. `l1` names an Ethereum node of the same config; x3f checks that node as usual, reads the Aztec contracts through its `execution_url` and shows a problem of that node in the Aztec row as well, so a broken L1 does not go unnoticed there (a Telegram message then names both nodes). `l1_url` is for an L1 provider: x3f only reads the Aztec contracts through it and does not check it as an Ethereum node, so a provider's load balancer does not turn the Aztec node yellow. Like all URLs, `url` and `l1_url` are never printed. The fields of one node type are not allowed on the other (for example `execution_url` on an Aztec node).

x3f calls `aztec_getNodeInfo` on the node (a node older than v5, which does not know it, is asked with `node_getNodeInfo`), and `eth_chainId` and `eth_call` on the L1, all read-only. Aztec nodes get a table of their own:

```
STATUS  NODE     NETWORK  VERSION               ROLLUP                  SYNC    ADVISORIES
WARN    aztec-1  mainnet  Aztec 6.0.0 -> 6.1.0  2070860440 (canonical)  synced  1 (medium)

Findings:
  WARN  aztec-1 version: Aztec 6.0.0 outdated: latest 6.1.0; first newer release 6.1.0 published 3 days ago (grace period 30 days)
  NOTE  aztec-1 version: release notes of 6.1.0 mention "security", read them: https://github.com/aztec-labs-eng/aztec-node/releases/tag/v6.1.0
  NOTE  aztec-1 advisories: Aztec 6.0.0 is affected by 1 published advisory without a released fix (1 medium): no fix released yet, check the advisory for mitigations: GHSA-73xc-j27q-wg75 (medium: "UltraHonk native verifier throws or aborts on a recursive pairing acc…"; packages @aztec/bb.js, @aztec-foundation/bb.js, barretenberg-rs); https://github.com/AztecProtocol/aztec-packages/security/advisories
  NOTE  aztec-1 advisories: cannot tell whether Aztec 6.0.0 is affected by GHSA-778c-2h5c-8r96 (low: "Checkpoint with over 72 blocks pushed via the escape hatch and proven…"; package @aztec/archiver @aztec/stdlib), check it yourself (affected versions "current Ignition mainnet release (v4 ta…", patched "> v5.0.0-rc.1")

Status: WARN (0 OK, 1 WARN, 0 CRIT)
```

(Real output from 2026-11-27 against the public Aztec mainnet endpoint of drpc.)

| | Green | Yellow | Red |
|---|---|---|---|
| **Network** | a known network (mainnet or testnet, by the node's L1 chain and registry), and the configured one if `network:` is set | an unknown network | another network than configured |
| **Rollup** | the node follows the canonical rollup, the one its registry on L1 names as current | not checked, with the reason: the L1 is unreachable, a read failed, or the L1 is on another chain than the node's L1 (the check is reported as unknown) | the node follows another rollup, for example after a network upgrade it did not follow |
| **Sync** | the newest checkpoint the node has seen is at most 2 slots (2.4 min) behind the newest checkpoint on L1 | more than 2 slots behind | more than 8 slots (9.6 min) behind, or so far behind that L1 no longer stores the node's checkpoint (the rollup keeps the newest 65) |
| **Proven** | the node knows the newest proof on L1 | its proven checkpoint is up to one epoch (32 slots, 38.4 min) behind L1's: one proof not seen yet | more than one epoch behind: proofs missed |
| **L1** (only with `l1`) | the Ethereum node named in `l1` is OK | that Ethereum node is WARN or CRIT; its own findings are in the Ethereum table | never: a problem of the L1 node is red in its own row |

x3f reads the registry through its own L1 access (`l1` or `l1_url`); which L1 endpoint the node itself uses, x3f cannot see. If that access is on another chain than the node's L1, x3f does not compare: it would read another network's registry.

**Sync** compares like with like: the newest checkpoint the node has seen on L1 (`checkpointed` in `getChainTips`) with the rollup's newest checkpoint on L1 (`pending`), and the node's `proven` checkpoint with L1's. The usual gap between proven and pending does not count. Lags are measured in slots between the two checkpoints, with slot and epoch duration read from L1 (72 seconds and 32 slots on mainnet and testnet). x3f reads L1 first and then the node, so a checkpoint that appears in between can only make the node look ahead. A node that follows another rollup than the canonical one is not compared.

Why these values: on mainnet consecutive checkpoints are up to 2 slots apart, so a node one checkpoint behind can look 2 slots behind. Red after 8 slots leaves time before the inactivity slashing of sequencers (testnet: 90 % of the attestations of one epoch missed, about 35 minutes; mainnet: 70 % in 2 consecutive epochs), if x3f runs often enough (see below).

**A stalled network** is not a problem of the node: if L1 has had no new checkpoint for more than 8 slots, x3f adds one note per network, for example `NOTE  testnet: no new checkpoint on L1 for 21.6 min (last: checkpoint 42991 at slot 43290); Aztec nodes cannot be more in sync than that`, and a warning after more than one epoch. The nodes on it stay green as long as they are as far as L1. The metric `x3f_aztec_checkpoint_age_seconds` shows the same per network.

#### How often to run x3f for Aztec nodes

Run x3f **every 5 minutes** for Aztec nodes, not hourly as in the [metrics example](#prometheus-metrics) for Ethereum nodes. A node that falls behind is red after 9.6 minutes; with a 5-minute interval x3f notices that within about 15 minutes, well before testnet's inactivity slashing after about 35 minutes. An hourly run could notice it only after that limit.

```
*/5 * * * * /usr/local/bin/x3f -config /etc/x3f/x3f.yaml -metrics-file /var/lib/node_exporter/textfile/x3f.prom > /var/lib/x3f/last.txt 2>&1
```

Running this often costs GitHub nothing extra: releases and advisories come from the cache, which x3f refreshes at most once per `-cache-ttl` (default one hour), so it still asks GitHub twice per repository and hour; no token is needed. Each run sends about ten read-only requests per Aztec node to its L1 endpoint; check that a provider's rate limit allows that.

With Prometheus, adapt the rule that alerts when x3f stops running to this interval, and alert on a stalled network if you want to:

```yaml
groups:
  - name: x3f-aztec
    rules:
      - alert: X3fNotRunning
        # three times the 5-minute interval
        expr: time() - x3f_last_run_timestamp_seconds > 900 or absent(x3f_last_run_timestamp_seconds)
        annotations:
          summary: "x3f has not finished a run for 15 minutes"
      - alert: AztecNetworkStalled
        # more than one epoch (32 slots of 72 seconds) without a checkpoint
        expr: x3f_aztec_checkpoint_age_seconds > 2304
        annotations:
          summary: "Aztec {{ $labels.network }}: no new checkpoint on L1 for more than an epoch"
```

The [status rules](#status-rules) for version and advisories are the same as for Ethereum clients:

- **Releases** come from [`aztec-labs-eng/aztec-node`](https://github.com/aztec-labs-eng/aztec-node/releases), where the node is released since v6. The aztec-packages monorepo publishes releases with the same numbers for Barretenberg, bb.js and the L1 contracts, sometimes days earlier; they are not node releases. Only a plain version such as `6.1.0` is a stable release. `rc.N` is a pre-release; `nightly`, `dev` and `commit` builds are dev builds; any other suffix (for example `devnet` or `private`) is unrecognized. All of them are yellow on a node and never count as the latest release, even where GitHub does not mark them as pre-releases.
- **Advisories** come from [`AztecProtocol/aztec-packages`](https://github.com/AztecProtocol/aztec-packages/security/advisories); aztec-node publishes none of its own. They concern packages such as Barretenberg and bb.js, which the node ships. aztec-packages releases many packages under one version, and an advisory counts if any of its packages is affected. The finding names the affected packages, so you can judge whether the node uses them; if not, add the advisory to [`ignore_advisories`](#ignoring-an-advisory). Whether a fix is released is checked against the node releases.
- **Security mentions:** if the release notes of a newer release the node does not run contain the word "security", a note names those releases with links, for example `release notes of 6.1.0 mention "security", read them: https://github.com/aztec-labs-eng/aztec-node/releases/tag/v6.1.0`. The status does not change: the word alone does not say how urgent a release is.

Known limitations:

- **No upgrade readiness yet.** Whether a pending governance proposal will move the network to a new rollup is not checked yet.
- **Security releases without an advisory are not recognized as urgent.** aztec-node v6.1.0 is a security release, but no GitHub advisory was published for it, so it counts like any other release: yellow within the grace period. x3f adds a note when the release notes of a missing newer release contain the word "security", but the word alone does not change the status.
- **Node releases before v6 are not in aztec-node.** For a node older than 6.0.0, "first newer release" is 6.0.0, not the real successor (such as 5.2.1). The status is still right: v6 is a new network, and such a node is far beyond the grace period.

### Telegram notifications

x3f can send a Telegram message when a node's status changes. Create a bot with [@BotFather](https://core.telegram.org/bots/features#botfather), start a chat with it or add it to a group or channel, and add the chat to `x3f.yaml`:

```yaml
telegram:
  chat_id: "-1001234567890"   # numeric chat ID, or "@channelname"
  min_level: warn             # optional: warn (default) or crit
  remind_every: 24h           # optional: reminder while a node stays red, 0 = never
```

The bot token never goes into the config: x3f reads it from `X3F_TELEGRAM_BOT_TOKEN` and never stores or prints it. To find the chat ID, send the bot a message and call the Bot API method [`getUpdates`](https://core.telegram.org/bots/api#getupdates). Check the setup with:

```sh
X3F_TELEGRAM_BOT_TOKEN=... x3f -config x3f.yaml -notify-test
```

When x3f sends a message:

- **A node's status changes**, worse or back to good: OK → WARN, WARN → CRIT, CRIT → OK and so on. With `min_level: crit` only changes to and from red are reported; OK ↔ WARN is then silent.
- **A red node gets a second problem:** one of its checks gets worse (to at least `min_level`), for example a new advisory while it is already red because of the fork check.
- **A node stays red:** a reminder once per `remind_every` (default 24 hours).

All changes of one run go into one message, with the node's WARN and CRIT findings:

```
🔴 mainnet-2: CRIT (was OK)
• CRIT execution: Besu dev build of 23.1.1 ("dev"): stable 23.1.1 released …
• CRIT execution advisories: Besu 23.1.1 is affected by 38 published advisories …
```

x3f compares status levels, not finding texts, so a finding whose wording changes on every run ("8 days old") does not cause a message each time. What was last reported is kept in the state file (`-state-file`). The first run reports every node that is not OK.

If a message cannot be sent (Telegram unreachable, wrong token or chat), the nodes' status does not change, but the run gets a yellow finding and exits with at least `1`, so a broken alert path does not go unnoticed. The message is sent again on the next run, with the time the problem started.

For manual runs next to a cron job, use `-no-notify`: it sends nothing and leaves the state file alone, so the cron job still reports every change.

x3f does not yet dampen flapping: a node that is unreachable for a single run causes two messages (red, then back to good). Waiting for a second run before reporting is planned.

The Telegram API address (`https://api.telegram.org`) is built in and cannot be configured, because the bot token is part of every request URL. The tests replace it internally with a mock server; that is a test setting only, not an option.

### Prometheus metrics

With `-metrics-file`, x3f writes its results as Prometheus metrics for the [textfile collector](https://github.com/prometheus/node_exporter#textfile-collector) of the node_exporter. Point it into the directory node_exporter reads (`--collector.textfile.directory`); the directory must exist and the file name must end in `.prom`:

```
0 * * * * /usr/local/bin/x3f -config /etc/x3f/x3f.yaml -metrics-file /var/lib/node_exporter/textfile/x3f.prom > /var/lib/x3f/last.txt 2>&1
```

The file is written atomically (a temporary file in the same directory, then renamed), so node_exporter never reads half a file. It is readable by everyone (`0644`), because node_exporter usually runs as another user.

| Metric | Labels | Value |
|---|---|---|
| `x3f_node_status` | `node` | 0 OK, 1 WARN, 2 CRIT |
| `x3f_check_status` | `node`, `check` | 0 OK, 1 WARN, 2 CRIT, 3 unknown (the check could not run), per check: `network`, `execution`, `execution_sync`, `consensus`, `consensus_sync`, `fork`, `execution_advisories`, `consensus_advisories`, `ignore_advisories`; for Aztec nodes `network`, `version`, `rollup`, `sync`, `proven`, `l1` (only with `l1`), `advisories`, `ignore_advisories` |
| `x3f_client_info` | `node`, `component`, `client`, `version`, `build` | 1; `component` is `execution` or `consensus` (Ethereum) or `node` (Aztec, `client="aztec"`); `build` is `release`, `prerelease`, `dev` or `unknown` |
| `x3f_client_latest_info` | `client`, `version` | 1, the highest stable release known from GitHub |
| `x3f_advisories` | `node`, `component`, `severity` | number of published advisories that affect the client and are not ignored; `severity` is `critical`, `high`, `medium`, `low` or `none` |
| `x3f_next_fork_timestamp_seconds` | `network`, `fork` | activation time of the next scheduled fork; missing if none is scheduled |
| `x3f_aztec_checkpoint_age_seconds` | `network` | time since the newest checkpoint of an Aztec network on L1, at the newest L1 block x3f read; missing if no Aztec node's sync could be checked |
| `x3f_build_info` | `version`, `commit`, `goversion` | 1 |
| `x3f_last_run_timestamp_seconds` | | when the last run ended |
| `x3f_last_run_success` | | 1 if x3f could do its own work: GitHub data available (fresh or cached for less than 7 days), notification delivered, state file read and written; 0 otherwise. Problems of the nodes do not count here. |

Labels only carry values from small, known sets, never findings, titles or error messages; versions are plain numbers such as `1.17.10`. The findings themselves stay in x3f's output and in the Telegram message. A check that could not run, such as the sync status of an unreachable client, reports 3 (unknown), as in the Nagios plugin convention; `x3f_node_status` stays 0, 1 or 2.

If x3f cannot run at all (invalid flags or config), it leaves the file alone: the node metrics stay, and `x3f_last_run_timestamp_seconds` gets old. A file that cannot be written is a yellow finding (exit code at least `1`).

Example alerting rules for Prometheus (they work as data-source-managed rules in Grafana as well):

```yaml
groups:
  - name: x3f
    rules:
      - alert: X3fNodeCritical
        expr: x3f_node_status == 2
        for: 30m   # Prometheus waits before firing, so a single bad run does not alert
        annotations:
          summary: "x3f: {{ $labels.node }} is CRIT"
          description: "Run x3f on {{ $labels.instance }} to see the findings."
      - alert: X3fNodeWarning
        expr: x3f_node_status == 1
        for: 6h
        annotations:
          summary: "x3f: {{ $labels.node }} is WARN"
      - alert: X3fNotRunning
        # about twice the cron interval (hourly in the example above; for
        # Aztec nodes see "How often to run x3f for Aztec nodes")
        expr: time() - x3f_last_run_timestamp_seconds > 7200 or absent(x3f_last_run_timestamp_seconds)
        annotations:
          summary: "x3f has not finished a run for 2 hours"
      - alert: X3fRunDegraded
        expr: x3f_last_run_success == 0
        for: 1h
        annotations:
          summary: "x3f runs, but GitHub data, the notification or the state file failed"
      - alert: X3fTextfileError
        expr: node_textfile_scrape_error == 1
        annotations:
          summary: "node_exporter cannot read a textfile (x3f.prom?)"
```

### Environment

`GITHUB_TOKEN` (optional) raises the GitHub API rate limit from 60 to 5000 requests per hour; it is never stored or printed. `X3F_TELEGRAM_BOT_TOKEN` is the Telegram bot token, required if the config has a `telegram` section (see [above](#telegram-notifications)); it is never stored or printed. `NO_COLOR` disables colors.

x3f uses the standard proxy variables `HTTPS_PROXY`, `HTTP_PROXY` and `NO_PROXY` for GitHub, Telegram and the nodes; requests to `localhost` and loopback addresses never go through a proxy. With `https://` the proxy only sees host and port. With an `http://` node URL and `HTTP_PROXY` set, however, the proxy sees the full URL, including an API key in it: add the node hosts to `NO_PROXY` or use `https://`.

### Exit codes

The exit codes follow the [Nagios plugin convention](https://nagios-plugins.org/doc/guidelines.html):

| Code | Meaning |
|---|---|
| `0` | OK: everything is green |
| `1` | WARNING: at least one yellow finding (also: a notification or the metrics file could not be written) |
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