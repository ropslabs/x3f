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
`lambdaclass_ethrex.json` was recorded the same way on 2026-11-25 (all 68
releases; `v32.0.0-rc.2` and `v32.0.0-rc.1` are flagged pre-releases, the
latest stable release is `v31.0.0`).

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

## github/advisories/*.json — recorded

Recorded from the GitHub REST API on 2026-11-25 with

```
GET https://api.github.com/repos/{owner}/{repo}/security-advisories?state=published&per_page=100
```

("List repository security advisories"), without authentication, for all 12
client repositories (same file names as above). Every list fit into one page.

**Trimmed:** each advisory keeps only `ghsa_id`, `cve_id`, `html_url`,
`summary`, `severity`, `published_at`, `withdrawn_at` and, per entry of
`vulnerabilities`, `package`, `vulnerable_version_range` and
`patched_versions`. Descriptions, CVSS data, credits and author objects were
removed. Order and number of advisories are unchanged. The files contain **no
HTTP headers and no tokens**.

| Repository | Advisories |
|---|---|
| besu-eth/besu | 44 |
| ethereum/go-ethereum | 17 |
| sigp/lighthouse | 8 |
| ChainSafe/lodestar | 3 |
| Consensys-Incorporated/teku | 1 |
| erigontech/erigon | 1 |
| paradigmxyz/reth, NethermindEth/nethermind, lambdaclass/ethrex, OffchainLabs/prysm, status-im/nimbus-eth2, grandinetech/grandine | 0 (`[]`) |
| AztecProtocol/aztec-packages | 3 (recorded 2026-11-27, same request and trimming) |

The Aztec advisories name npm and Rust packages (`@aztec/bb.js`,
`@aztec-foundation/bb.js`, `barretenberg-rs`, and `@aztec/archiver
@aztec/stdlib` as one name) with ranges that contain nightly, devnet and
spartan versions, e.g. `>= 6.0.0-nightly.20260721, < 7.0.0-0` with an
empty `patched_versions` (GHSA-73xc-j27q-wg75). GHSA-778c-2h5c-8r96 has a
free-text range, `current Ignition mainnet release (v4 tag)`, patched
`> v5.0.0-rc.1`; x3f cannot decide it.

Range spellings in this data that the parser relies on (all real):

- comparators with and without space and `v`: `< 1.13.15`, `<0.35.0`,
  `<= 26.10.2`, `>= v1.9.4, < 1.9.20`, `< v7.0.0-beta.5`
- conjunction with `and` and a double space: `>=v3.1.0 and  <v3.4.0` (Erigon)
- explicit version lists: `21.10.0,21.10.1`,
  `22.4.0-RC2,22.4.0,…,22.7.0-RC4` (Besu)
- a lower bound only, the fix in `patched_versions`: `>= 24.5.2` / `26.8.1`
  (Besu GHSA-p4h2-gvh4-pv6j)
- `all` with an empty `patched_versions` (Besu GHSA-v5gq-r843-8f73)
- patched spellings: `26.8.1`, `>= 1.16.9`, `>=8.3.0`, `>= v1.9.18`,
  `v7.0.0-beta.5`, `v3.4.0`
- severities: `critical`, `high`, `medium`, `low`; `cve_id` often `null`

The pagination tests split `besu-eth_besu.json` into pages and add a `Link`
header in the format the GitHub docs show ("Using pagination in the REST
API"); GitHub did not paginate any of these lists itself.
