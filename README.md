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