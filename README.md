# x3f

**The patch exists. Is it applied?**

x3f tells you what is running where, in which version, who owns it, and what needs attention next: known vulnerabilities, end-of-life dates and, for blockchain nodes, upcoming hard forks.

> **Status:** early stage (v0.x). The Ethereum node check below works today, including known security advisories of the clients, Telegram notifications on status changes and Prometheus metrics; inventory is planned. Flags and output may still change between 0.x releases.
>
> *An n-day is a known vulnerability that already has a fix. Many attacks don't need a zero-day, just a system where the existing patch was never applied. x3f finds those systems.*

---

## Quick start

x3f checks Ethereum nodes: network, client versions, sync status, whether the clients are behind their latest stable release, whether a published security advisory affects them, and whether they are ready for the next hard fork. It can send a Telegram message when a node's status changes and write Prometheus metrics. It is a single binary that only reads from your nodes.

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
| **Advisories** | no published advisory affects the running version ("none known") | affected by a medium or low advisory (or one without severity); affected by an advisory whose fix is not released yet; advisory data unknown | affected by a critical or high advisory whose fix is released, with no grace period |
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

### Security advisories

x3f reads the published [security advisories](https://docs.github.com/en/code-security/concepts/vulnerability-reporting-and-management/repository-security-advisories) of each client's GitHub repository and checks the running version against the affected versions. A finding names how many advisories affect the node, the release that fixes all of them, and the most severe three with their GHSA ID, CVE and title, so you can judge whether they apply to your setup (for example, many Besu advisories concern QBFT, IBFT or Clique, consensus protocols of private networks). Advisories whose affected versions x3f cannot determine are listed as a note to check yourself.

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
| `x3f_check_status` | `node`, `check` | 0 OK, 1 WARN, 2 CRIT, 3 unknown (the check could not run), per check: `network`, `execution`, `execution_sync`, `consensus`, `consensus_sync`, `fork`, `execution_advisories`, `consensus_advisories`, `ignore_advisories` |
| `x3f_client_info` | `node`, `component`, `client`, `version`, `build` | 1; `build` is `release`, `prerelease`, `dev` or `unknown` |
| `x3f_client_latest_info` | `client`, `version` | 1, the highest stable release known from GitHub |
| `x3f_advisories` | `node`, `component`, `severity` | number of published advisories that affect the client and are not ignored; `severity` is `critical`, `high`, `medium`, `low` or `none` |
| `x3f_next_fork_timestamp_seconds` | `network`, `fork` | activation time of the next scheduled fork; missing if none is scheduled |
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
        # about twice the cron interval (hourly in the example above)
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