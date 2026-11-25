# Sources for forks.yaml

Checked on 2026-11-24; Ethrex rows and new announcements checked on
2026-11-25. Only official sources are used for the data; the
eth-clients repositories serve as a cross-check (see below).

## Announcements

| Network | Fork | Epoch | Timestamp | Source |
|---|---|---|---|---|
| mainnet | Fusaka (Osaka/Fulu) | 411392 | 1764798551¹ | [Fusaka Mainnet Announcement](https://blog.ethereum.org/2025/11/06/fusaka-mainnet-announcement) |
| mainnet | BPO1 | 412672 | 1765290071 | same |
| mainnet | BPO2 | 419072 | 1767747671 | same |
| sepolia | Fusaka | 272640² | 1760427360 | [Fusaka Testnet Announcement](https://blog.ethereum.org/2025/09/26/fusaka-testnet-announcement) |
| sepolia | BPO1 | 274176 | 1761017184 | same |
| sepolia | BPO2 | 275712 | 1761607008 | same |
| sepolia | Glamsterdam (Amsterdam/Gloas) | 353024 | 1791294816 | [Glamsterdam Testnet Announcement](https://blog.ethereum.org/2026/09/17/glamsterdam-testnet-announcement) |
| hoodi | Fusaka | 50688² | 1761677592 | [Fusaka Testnet Announcement](https://blog.ethereum.org/2025/09/26/fusaka-testnet-announcement) |
| hoodi | BPO1 | 52480 | 1762365720 | same |
| hoodi | BPO2 | 54016 | 1762955544 | same |
| hoodi | Glamsterdam, **postponed** | 132352 | 1793036568 | [Hoodi Announcement](https://blog.ethereum.org/2026/10/16/glamsterdam-hoodi-announcement), [Postponed](https://blog.ethereum.org/2026/10/22/glamsterdam-hoodi-delay) |

¹ The mainnet announcement states slot 13,164,544 and the time, not the
timestamp: 1606824023 + 13164544 × 12 = 1764798551.
² The testnet announcement states the Fusaka slot: Sepolia 8,724,480 / 32 =
272640, Hoodi 1,622,016 / 32 = 50688.

Glamsterdam has no mainnet date: "Hoodi and mainnet activation dates have not
yet been decided" (2026-09-17), and the postponement post (2026-10-22) "does
not set a Glamsterdam mainnet activation date". On 2026-11-06 the EF blog
reported that Hoodi will not fork until the issues with EIP-8037/EIP-8038
are resolved.

## Client releases

- `min_versions` are the release tables of the announcement of each fork. A
  BPO uses the releases of the hard fork it belongs to (same announcement).
- Hoodi Glamsterdam: `scheduled_in` is the table of the Hoodi announcement
  (2026-10-16), `removed_in` the table of the postponement post
  (2026-10-22), introduced there as "releases incorporating the postponed
  Hoodi activation". The announcement does not say whether releases before
  `scheduled_in` contained the schedule too.
- Ethrex appears in the 2026 tables only: Sepolia Glamsterdam 29.0.1, Hoodi
  Glamsterdam `scheduled_in` 30.0.0 and `removed_in` v30.0.1 (written with
  "v" in the post). The download links point to `lambdaclass/ethrex`. The
  Fusaka announcements (2025) list no Ethrex release.
- On 2026-11-25 the EF blog had no announcement newer than 2026-11-06, and
  the eth-clients `metadata/` directories had no commit newer than the
  snapshot.

The release tables were read from the original page text, not from a
summary: an automated summary of the postponement post misreported the
tables as unconfirmed.

## Genesis times

Used by the loader to check `timestamp = genesis_time + epoch × 384`.
`GET /eth/v1/beacon/genesis` live on 2026-11-24:

| Network | genesis_time | Endpoint |
|---|---|---|
| mainnet | 1606824023 | `https://ethereum-beacon-api.publicnode.com` |
| sepolia | 1655733600 | `https://ethereum-sepolia-beacon-api.publicnode.com` |
| hoodi | 1742213400 | `https://lodestar-hoodi.chainsafe.io` |

All eleven epoch/timestamp pairs above pass this check.

## Cross-check: eth-clients (testdata/eth-clients)

Snapshot of the network configs, used by `TestCrossCheckEthClients`:

| Repository | Commit | Files |
|---|---|---|
| [eth-clients/mainnet](https://github.com/eth-clients/mainnet/tree/8cad4805e9cb5aa5e8e74d30e3f5029a7f133372/metadata) | `8cad4805` (2026-09-21) | `metadata/config.yaml`, `config` section of `metadata/genesis.json` |
| [eth-clients/sepolia](https://github.com/eth-clients/sepolia/tree/7b1b9d9549787b6bbf538f943304c4cf84f7e39e/metadata) | `7b1b9d95` (2026-10-13) | same |
| [eth-clients/hoodi](https://github.com/eth-clients/hoodi/tree/2a7efb3c530cce7c71702cc98d0f965df2411c11/metadata) | `2a7efb3c` (2026-10-22, "unschedule Hoodi Glamsterdam activation") | same |

The genesis files are trimmed to their `config` section (the mainnet file is
860 KB, almost all of it the initial allocation); `config.yaml` is unchanged.

Result on 2026-11-24: **no deviations**. Every fork time and epoch above
matches `osakaTime`/`bpo1Time`/`bpo2Time`/`amsterdamTime`, `*_FORK_EPOCH` and
`BLOB_SCHEDULE`; Hoodi no longer schedules Glamsterdam; there are no
additional forks in eth-clients.

If the test fails after a snapshot update, compare with the official sources
and report the deviation; do not copy values from eth-clients.
