# Sources for test fixtures

## github/*.json — recorded

Recorded from the GitHub REST API on 2026-11-24 with

```
GET https://api.github.com/repos/{owner}/{repo}/releases?per_page=100
```

without authentication. File names are `{owner}_{repo}.json` with the
**current** repository path (see `repos` in `releases.go`); three requests
were answered via redirect from the old path:

| Requested | Served from |
|---|---|
| `hyperledger/besu` | `besu-eth/besu` |
| `prysmaticlabs/prysm` | `OffchainLabs/prysm` |
| `Consensys/teku` | `Consensys-Incorporated/teku` |

**Trimmed:** each release keeps only the fields x3f reads (`tag_name`,
`prerelease`, `draft`, `published_at`). Everything else (author objects, URLs,
asset lists, changelog bodies) was removed. The files contain **no HTTP
headers and no tokens**; the requests were made without a token anyway.
Order and number of releases are unchanged (100 per repository; Grandine has
only 28 releases in total).

Cases in this data that the tests rely on:

- rc tags **not** flagged as pre-release: `reth v1.0.0-rc.2`, `besu 22.7.0-RC1`,
  `lighthouse v4.0.2-rc.0`
- clean tags **flagged** as pre-release: `besu 24.5.0`, `nethermind 1.37.0`,
  `grandine 0.3.0`
- tags that are not versions: `nimbus nightly`, `altair-beta`,
  `nethermind zkvm-guests-…`, `bootnode-1.0.0`
- suffix spellings: `-rc.0`, `-rc1`, `-RC1`, `.rc0`, `-alpha7`, `-BETA`,
  `-hotfix`, `-mekong`, `-exp`, `-tree.1`
- newer pre-releases than the latest stable release:
  `lighthouse v8.4.0-rc.0`, `nethermind 2.3.0-rc`, `grandine 3.0.0-rc.4`
