# Sources for test fixtures

Every fixture in this directory is either taken **literally** from official
documentation / specification / source code, or **derived** from the format
the client's source code builds at runtime. Derived fixtures are marked as
such, together with every value that had to be filled in. Nothing here is
invented. Replace derived fixtures with recorded responses from real nodes
once a testnet node is available.

All links are permalinks to the release tag's commit, checked on 2026-11-24.

## Response envelopes

| Endpoint | Envelope | Source |
|---|---|---|
| `web3_clientVersion` (JSON-RPC) | `{"jsonrpc":"2.0","id":1,"result":"<string>"}` | [JSON-RPC 2.0 specification, section 5](https://www.jsonrpc.org/specification#response_object) |
| `GET /eth/v1/node/version` | `{"data":{"version":"<string>"}}` | [beacon-APIs v4.0.0, apis/node/version.yaml](https://github.com/ethereum/beacon-APIs/blob/88fc9402c6a1821b6e9ae4e3b8ae0a22f1141203/apis/node/version.yaml) |

## Execution clients (`execution/`)

### geth.json — derived

`Geth/v1.17.10-stable-1df30b18/linux-amd64/go1.27.3`

- Format: `Name[/Identity]/v<Version>/<GOOS>-<GOARCH>/<Go version>`, built in
  [node/config.go L317-L332](https://github.com/ethereum/go-ethereum/blob/1df30b18d9ed35ee9d28ffd4648442c2204554ad/node/config.go#L317-L332),
  returned by `web3_clientVersion` via
  [node/api.go L330-L333](https://github.com/ethereum/go-ethereum/blob/1df30b18d9ed35ee9d28ffd4648442c2204554ad/node/api.go#L330-L333).
- `<Version>` = `1.17.10-stable` + `-<first 8 chars of commit>`
  ([internal/version/version.go L46-L55](https://github.com/ethereum/go-ethereum/blob/1df30b18d9ed35ee9d28ffd4648442c2204554ad/internal/version/version.go#L46-L55),
  [version/version.go L20-L23](https://github.com/ethereum/go-ethereum/blob/1df30b18d9ed35ee9d28ffd4648442c2204554ad/version/version.go#L20-L23)).
  Unstable builds additionally append `-<git date>`.
- Filled in: commit `1df30b18` = commit of tag v1.17.10; Go version `go1.27.3`
  from [build/checksums.txt L10](https://github.com/ethereum/go-ethereum/blob/1df30b18d9ed35ee9d28ffd4648442c2204554ad/build/checksums.txt#L10).

### reth.json — derived (format has a literal example in source)

`reth/v2.11.0-b9abbcc1/x86_64-unknown-linux-gnu`

- Format `reth/v<version>-<short sha>/<target triple>` with the literal example
  `reth/v0.1.0-alpha.1-428a6dc2f/aarch64-apple-darwin`:
  [crates/node/core/build.rs L91-L99](https://github.com/paradigmxyz/reth/blob/b9abbcc1b982755baa9021e6874e2e58936ca65f/crates/node/core/build.rs#L91-L99),
  [crates/node/core/src/version.rs L55-L66](https://github.com/paradigmxyz/reth/blob/b9abbcc1b982755baa9021e6874e2e58936ca65f/crates/node/core/src/version.rs#L55-L66).
- Used as default `--identity`
  ([args/network.rs L276](https://github.com/paradigmxyz/reth/blob/b9abbcc1b982755baa9021e6874e2e58936ca65f/crates/node/core/src/args/network.rs#L276)),
  returned by `web3_clientVersion` via
  [crates/rpc/rpc/src/web3.rs L28-L32](https://github.com/paradigmxyz/reth/blob/b9abbcc1b982755baa9021e6874e2e58936ca65f/crates/rpc/rpc/src/web3.rs#L28-L32).
  Note: `--identity` can replace the whole string.
- Filled in: commit `b9abbcc1` = tag v2.11.0; target triple of the Linux x86_64 release build.
- **Deviates from live output:** real Reth nodes report a **7-character** commit
  (`reth/v2.11.0-b9abbcc/...`, see `execution/live/reth-publicnode-sepolia.json`),
  not 8 as derived from `build.rs`. Kept as a derived example; the parser
  accepts both.

### nethermind.json — derived

`Nethermind/v2.2.2+be201120/linux-x64/dotnet10.0.14`

- Format `{name}/{version}{versionPostfix}/{os}/{runtime}`:
  [ProductInfo.cs L28-L52, L114](https://github.com/NethermindEth/nethermind/blob/be201120899ea9cdaab1444b89878c56d1a5a4ad/src/Nethermind/Nethermind.Core/ProductInfo.cs#L28-L114);
  `web3_clientVersion` returns `PublicClientId`
  ([Web3RpcModule.cs L18](https://github.com/NethermindEth/nethermind/blob/be201120899ea9cdaab1444b89878c56d1a5a4ad/src/Nethermind/Nethermind.JsonRpc/Modules/Web3/Web3RpcModule.cs#L18)).
  The commit follows a `+` and is cut to 8 characters.
- Filled in: version `2.2.2`
  ([Directory.Build.props L13](https://github.com/NethermindEth/nethermind/blob/be201120899ea9cdaab1444b89878c56d1a5a4ad/src/Nethermind/Directory.Build.props#L13)),
  commit `be201120` = tag 2.2.2, runtime `10.0.14` from the
  [Dockerfile L29](https://github.com/NethermindEth/nethermind/blob/be201120899ea9cdaab1444b89878c56d1a5a4ad/Dockerfile#L29) base image.

### erigon.json — derived

`erigon/3.8.2-8a3f3b2c/linux-amd64/go1.27.4`

- Format `erigon/<version>-<commit[:8]>/<GOOS>-<GOARCH>/<Go version>`, **without** a `v` prefix:
  [node/privateapi/ethbackend.go L294-L296](https://github.com/erigontech/erigon/blob/8a3f3b2c7a34429f101e9a9d05b861120c8e4d77/node/privateapi/ethbackend.go#L294-L296),
  [common/path.go L30-L32](https://github.com/erigontech/erigon/blob/8a3f3b2c7a34429f101e9a9d05b861120c8e4d77/common/path.go#L30-L32),
  [db/version/app.go L33-L62](https://github.com/erigontech/erigon/blob/8a3f3b2c7a34429f101e9a9d05b861120c8e4d77/db/version/app.go#L33-L62).
- Filled in: commit `8a3f3b2c` = tag v3.8.2. Go **minor** version 1.27 from the
  [release workflow L11](https://github.com/erigontech/erigon/blob/8a3f3b2c7a34429f101e9a9d05b861120c8e4d77/.github/workflows/release.yml#L11)
  (`golang:1.27-bookworm`); the patch level `.4` is **not** pinned by any source and is a placeholder.

### besu.json — literal

`besu/v23.1.1-dev-ac23d311/osx-x86_64/graalvm-java-17`

- Literal example in the Javadoc of `BesuVersionUtils.version()`:
  [BesuVersionUtils.java L103](https://github.com/besu-eth/besu/blob/09b9e94dd585f2dc7174adcdeeac96590c0becd0/util/src/main/java/org/hyperledger/besu/util/BesuVersionUtils.java#L103).
- `web3_clientVersion` uses `nodeName(identity)`
  ([RunnerBuilder.java L1152](https://github.com/besu-eth/besu/blob/09b9e94dd585f2dc7174adcdeeac96590c0becd0/app/src/main/java/org/hyperledger/besu/RunnerBuilder.java#L1152)),
  which inserts an optional identity segment after `besu/`
  ([BesuVersionUtils.java L116-L118](https://github.com/besu-eth/besu/blob/09b9e94dd585f2dc7174adcdeeac96590c0becd0/util/src/main/java/org/hyperledger/besu/util/BesuVersionUtils.java#L116-L118)).

### ethrex.json — derived

`ethrex/v31.0.0-HEAD-2f0bfb201d4a576ebc6a7396c8e8b1c1763ac50c/x86_64-unknown-linux-gnu/rustc-v1.93.0`

- Format `{name}/v{version}-{channel}-{commit}/{os_arch}/rustc-v{rustc}`:
  [crates/networking/rpc/rpc.rs L369-L372](https://github.com/lambdaclass/ethrex/blob/2f0bfb201d4a576ebc6a7396c8e8b1c1763ac50c/crates/networking/rpc/rpc.rs#L369-L372),
  returned by `web3_clientVersion`
  ([rpc.rs L1975](https://github.com/lambdaclass/ethrex/blob/2f0bfb201d4a576ebc6a7396c8e8b1c1763ac50c/crates/networking/rpc/rpc.rs#L1975));
  fields from
  [cmd/ethrex/utils.rs L219-L228](https://github.com/lambdaclass/ethrex/blob/2f0bfb201d4a576ebc6a7396c8e8b1c1763ac50c/cmd/ethrex/utils.rs#L219-L228).
  The commit is the **full** SHA.
- Channel ([utils.rs L206-L216](https://github.com/lambdaclass/ethrex/blob/2f0bfb201d4a576ebc6a7396c8e8b1c1763ac50c/cmd/ethrex/utils.rs#L206-L216),
  [.github/actions/build-docker/image-meta.sh](https://github.com/lambdaclass/ethrex/blob/2f0bfb201d4a576ebc6a7396c8e8b1c1763ac50c/.github/actions/build-docker/image-meta.sh)):
  `stable` for the promoted release image, `rc.N` for release candidate
  tag builds, `release` for a final tag image build, otherwise the git
  branch. The release binaries are built with plain `cargo build` on a tag
  checkout ([tag_release.yaml L125-L128](https://github.com/lambdaclass/ethrex/blob/2f0bfb201d4a576ebc6a7396c8e8b1c1763ac50c/.github/workflows/tag_release.yaml#L125-L128));
  on a detached HEAD, vergen-git2 9.1.0 (version from Ethrex's
  `Cargo.lock`) sets the branch to `HEAD`
  (`add_branch_name` in `src/git2/mod.rs` of the
  [vergen-git2 9.1.0 crate](https://crates.io/crates/vergen-git2/9.1.0)).
  So `HEAD`, `stable` and `release` count as releases; any other channel
  except `rc.N` is a branch, i.e. a dev build.
- Filled in: version `31.0.0` and commit `2f0bfb20…` of tag v31.0.0, target
  `x86_64-unknown-linux-gnu` (`ubuntu-22.04` x86_64 build in tag_release.yaml),
  rustc `1.93.0` from `rust-toolchain.toml` at the tag.
- Known limitation: a branch name containing `/` (e.g. `release/v32.0.0`)
  splits the version segment (see `TestParseClientVersionKnownLimitations`).

### error-method-not-found.json — derived

`{"code":-32601,"message":"the method web3_clientVersion does not exist/is not available"}`

- Geth's method-not-found error:
  [rpc/errors.go L78-L82](https://github.com/ethereum/go-ethereum/blob/1df30b18d9ed35ee9d28ffd4648442c2204554ad/rpc/errors.go#L78-L82);
  error object layout from the [JSON-RPC 2.0 specification, section 5.1](https://www.jsonrpc.org/specification#error_object).

## Consensus clients (`consensus/`)

### spec-example.json — literal

`Lighthouse/v0.1.5 (Linux x86_64)` — example value of the `Version` schema in
[beacon-APIs v4.0.0, types/primitive.yaml L15-L18](https://github.com/ethereum/beacon-APIs/blob/88fc9402c6a1821b6e9ae4e3b8ae0a22f1141203/types/primitive.yaml#L15-L18).
Old format, but it is the official example.

### lighthouse.json — derived (format has a literal example in source)

`Lighthouse/v8.3.3-57c2abc/x86_64-linux`

- Literal example `Lighthouse/v8.0.0-67da032/x86_64-linux` in
  [common/lighthouse_version/src/lib.rs L22-L23](https://github.com/sigp/lighthouse/blob/57c2abc13078bf949e4d19df6178a644b064c32c/common/lighthouse_version/src/lib.rs#L22-L23);
  the same file's test regex allows pre-releases like `-rc.1` / `-beta.1`.
  Used by the endpoint in
  [beacon_node/http_api/src/lib.rs L2365-L2366](https://github.com/sigp/lighthouse/blob/57c2abc13078bf949e4d19df6178a644b064c32c/beacon_node/http_api/src/lib.rs#L2365-L2366).
- Filled in: version 8.3.3, commit `57c2abc` (7 chars, as in the example) = tag v8.3.3.

### prysm.json — derived

`Prysm/v7.3.2-844d8b8 (linux amd64)`

- Format `Prysm/<tag>-<commit[:7]> (<GOOS> <GOARCH>)`:
  [beacon-chain/rpc/eth/node/handlers.go L108-L115](https://github.com/OffchainLabs/prysm/blob/844d8b8df585ac7a55be9f7788c65d00174d845e/beacon-chain/rpc/eth/node/handlers.go#L108-L115).
- Filled in: tag v7.3.2, commit `844d8b8` = tag v7.3.2.

### teku.json — derived

`teku/v26.10.3/linux-x86_64/-eclipseadoptium-openjdk64bitservervm-java-25`

- Format `teku/v<version>/<os>-<arch>/<vm>-java-<spec version>`:
  [VersionProvider.java L30, L50, L209](https://github.com/Consensys/teku/blob/9591436143efd2b0e457ea2b6614eee678fca770/infrastructure/version/src/main/java/tech/pegasys/teku/infrastructure/version/VersionProvider.java#L30).
  Unknown JVM vendors are rendered as `-<vendor>-<vm name>` (lowercase, alphanumerics only).
- Filled in: version 26.10.3; Java 25 from the
  [docker/jdk25/Dockerfile](https://github.com/Consensys/teku/blob/9591436143efd2b0e457ea2b6614eee678fca770/docker/jdk25/Dockerfile)
  (`eclipse-temurin:25`). The vendor/VM names `Eclipse Adoptium` / `OpenJDK 64-Bit Server VM`
  are the standard Temurin system properties, not taken from the Teku repository.

### nimbus.json — derived

`Nimbus/v26.10.2-0abc0d-stateofus`

- Format `Nimbus/v<version>-<git revision[0..5]>-<versionBlob>`:
  [beacon_chain/version.nim L15-L30](https://github.com/status-im/nimbus-eth2/blob/0abc0dd0fc0750a8875c49089d074f7d7197b62c/beacon_chain/version.nim#L15-L30),
  served by
  [beacon_chain/rpc/rest_node_api.nim L154-L155, L257-L258](https://github.com/status-im/nimbus-eth2/blob/0abc0dd0fc0750a8875c49089d074f7d7197b62c/beacon_chain/rpc/rest_node_api.nim#L154-L258).
- Filled in: revision `0abc0d` (6 chars) = tag v26.10.2. `stateofus` is a fixed codename, not a pre-release marker.

### lodestar.json — derived (format has literal examples in source)

`Lodestar/v1.51.0/7923280`

- Endpoint returns `Lodestar/${version}`:
  [packages/beacon-node/src/api/impl/node/index.ts L59-L63](https://github.com/ChainSafe/lodestar/blob/7923280c2e664e2fb81344921aaf41cb751957c7/packages/beacon-node/src/api/impl/node/index.ts#L59-L63).
- Literal version examples `v0.36.0/80c248bb` (stable) and `v0.36.0-dev.80c248bb/80c248bb` (dev):
  [packages/cli/src/util/version.ts L21-L22](https://github.com/ChainSafe/lodestar/blob/7923280c2e664e2fb81344921aaf41cb751957c7/packages/cli/src/util/version.ts#L21-L22).
  The commit is in its own `/` segment.
- Filled in: version 1.51.0, commit `7923280` (7 chars, per the comment in the same file) = tag v1.51.0.

### grandine.json — derived

`Grandine/2.0.6-70a5c7ea/x86_64-linux`

- Format `Grandine/<version>-<commit[:8]>/<arch>-<os>`, **without** a `v` prefix:
  [grandine_version/src/lib.rs L5-L31](https://github.com/grandinetech/grandine/blob/70a5c7ea6fbf07c22ecea09b7cdd71c3af93cbb0/grandine_version/src/lib.rs#L5-L31),
  set as default in
  [runtime/src/defaults.rs L25](https://github.com/grandinetech/grandine/blob/70a5c7ea6fbf07c22ecea09b7cdd71c3af93cbb0/runtime/src/defaults.rs#L25),
  served by
  [http_api/src/standard.rs L2613-L2619](https://github.com/grandinetech/grandine/blob/70a5c7ea6fbf07c22ecea09b7cdd71c3af93cbb0/http_api/src/standard.rs#L2613-L2619).
- Filled in: version 2.0.6, commit `70a5c7ea` = tag 2.0.6.

## Sync status

No client documentation checked (Lighthouse book, Nimbus guide) contains a
literal response for these endpoints, so apart from the spec example all sync
fixtures are derived. To avoid inventing numbers, block numbers reuse the
execution-apis example values (`0x0`, `0x1518`, `0x9567a3`) and slots reuse the
beacon-APIs `Uint64` example value (`"1"`).

### execution/syncing-false.json — derived

`"result": false` — "Should always return false if not syncing" in the
`SyncingStatus` schema,
[execution-apis src/schemas/client.yaml L1-L19](https://github.com/ethereum/execution-apis/blob/1ca0818569467cff85b7f41372f22e728d5daa14/src/schemas/client.yaml#L1-L19);
Geth returns it once sync is done
([internal/ethapi/api.go L164-L167](https://github.com/ethereum/go-ethereum/blob/1df30b18d9ed35ee9d28ffd4648442c2204554ad/internal/ethapi/api.go#L164-L167)).

### execution/syncing-spec-example.json — literal

Result object `{"startingBlock":"0x0","currentBlock":"0x1518","highestBlock":"0x9567a3"}`
from the `eth_syncing` example in
[execution-apis src/eth/client.yaml L14-L29](https://github.com/ethereum/execution-apis/blob/1ca0818569467cff85b7f41372f22e728d5daa14/src/eth/client.yaml#L14-L29).

### execution/syncing-geth.json — derived

Field set exactly as built by Geth's `Syncing()`
([internal/ethapi/api.go L169-L192](https://github.com/ethereum/go-ethereum/blob/1df30b18d9ed35ee9d28ffd4648442c2204554ad/internal/ethapi/api.go#L169-L192)).
Filled in: block numbers from the spec example; all Geth-specific counters are `0x0` placeholders.

### execution/syncing-geth-indexing.json — derived

Same field set. Geth still reports syncing when `currentBlock >= highestBlock`
but an index is incomplete (`SyncProgress.Done`,
[interfaces.go L153-L158](https://github.com/ethereum/go-ethereum/blob/1df30b18d9ed35ee9d28ffd4648442c2204554ad/interfaces.go#L153-L158)).
Filled in: `currentBlock` = `highestBlock` = `0x9567a3`; `txIndexRemainingBlocks` and
`txIndexFinishedBlocks` = `0x1518` (placeholder, reusing an example value); everything else `0x0`.

### consensus/syncing-spec-example.json — derived

`{"data":{"head_slot":"1","sync_distance":"1","is_syncing":true,"is_optimistic":true,"el_offline":true}}`

- Structure and required fields from
  [beacon-APIs v4.0.0 apis/node/syncing.yaml](https://github.com/ethereum/beacon-APIs/blob/88fc9402c6a1821b6e9ae4e3b8ae0a22f1141203/apis/node/syncing.yaml);
  `Uint64` is a decimal string with example `"1"`
  ([types/primitive.yaml L31-L33](https://github.com/ethereum/beacon-APIs/blob/88fc9402c6a1821b6e9ae4e3b8ae0a22f1141203/types/primitive.yaml#L31-L33)).
- Filled in: all booleans `true` to exercise every flag.

### consensus/syncing-synced.json — derived

Same structure; `sync_distance` `"0"` ("0 if synced" per the spec), all booleans `false`.

### consensus/syncing-nimbus-pre-bellatrix.json — derived

Nimbus omits `is_optimistic` before the Bellatrix fork epoch and `el_offline`
before Capella
([beacon_chain/rpc/rest_node_api.nim L261-L290](https://github.com/status-im/nimbus-eth2/blob/0abc0dd0fc0750a8875c49089d074f7d7197b62c/beacon_chain/rpc/rest_node_api.nim#L261-L290)),
so both are missing here. Filled in: synced values as above.

## Live recordings (`execution/live/`, `consensus/live/`)

Recorded live on 2026-11-24 from public endpoints that need no API key, by
sending the same requests x3f sends (`web3_clientVersion`, `eth_syncing`,
`GET /eth/v1/node/version`, `GET /eth/v1/node/syncing`) with curl. Only the
response body is stored, unchanged (including the provider's field order and
whitespace); no request or response headers.

| File | Endpoint | Notes |
|---|---|---|
| `execution/live/geth-publicnode-mainnet.json` | `https://ethereum-rpc.publicnode.com` | Same format as derived `geth.json`, built with Go 1.25.14 |
| `execution/live/reth-publicnode-sepolia.json` | `https://ethereum-sepolia-rpc.publicnode.com` | **7-character commit**, derived `reth.json` assumed 8 |
| `execution/live/reth-flashbots-mainnet.json` | `https://rpc.flashbots.net` | Load-balanced; an earlier request returned `reth/v2.10.0-f5192ac/...`. `result` before `id` |
| `execution/live/proxy-drpc-mainnet.json` | `https://eth.drpc.org` | **`Geth/v10.0.0/drpc`**: the proxy pretends to be Geth 10.0.0, no real client version |
| `execution/live/proxy-mevblocker-mainnet.json` | `https://rpc.mevblocker.io` | `MEVblocker`, no version; JSON with spaces after `:` |
| `execution/live/proxy-cloudflare-mainnet.json` | `https://cloudflare-eth.com` | `cloudflare-ethereum`, no version |
| `execution/live/error-drpc-sepolia-free-plan.json` | `https://sepolia.drpc.org` | JSON-RPC error code 35 (non-standard) |
| `execution/live/syncing-false-flashbots-mainnet.json` | `https://rpc.flashbots.net` | `eth_syncing` = `false` |
| `consensus/live/lighthouse-publicnode-mainnet.json` | `https://ethereum-beacon-api.publicnode.com` | Identical to derived `lighthouse.json` |
| `consensus/live/nimbus-unstable-mainnet.json` | `http://unstable.mainnet.beacon-api.nimbus.team` | Unstable branch build with the same version number as the release, only the commit differs |
| `consensus/live/lodestar-chainsafe-mainnet.json` | `https://lodestar-mainnet.chainsafe.io` | Identical to derived `lodestar.json` |
| `consensus/live/syncing-lighthouse-publicnode-mainnet.json` | `https://ethereum-beacon-api.publicnode.com` | Fields in alphabetical order, `is_optimistic: true` |
| `consensus/live/syncing-nimbus-unstable-mainnet.json` | `http://unstable.mainnet.beacon-api.nimbus.team` | `is_optimistic: true`, `el_offline: true` |

Also observed, not stored as a file: `https://eth.llamarpc.com` answered with
HTTP 525 and the plain-text body `error code: 525` (Cloudflare origin TLS
error); covered by a synthetic test case.

## Network detection

### execution/chainid-spec-example.json — literal

`"result": "0x1"` from the `eth_chainId` example in
[execution-apis src/eth/client.yaml L1-L13](https://github.com/ethereum/execution-apis/blob/1ca0818569467cff85b7f41372f22e728d5daa14/src/eth/client.yaml#L1-L13).

### consensus/deposit-contract-spec-example.json — literal

`{"data":{"chain_id":"1","address":"0x00000000219ab540356cBB839Cbe05303d7705Fa"}}`, the
`example` values of
[beacon-APIs v4.0.0 apis/config/deposit_contract.yaml](https://github.com/ethereum/beacon-APIs/blob/88fc9402c6a1821b6e9ae4e3b8ae0a22f1141203/apis/config/deposit_contract.yaml).

### Chain IDs

| Network | Chain ID | Execution side | Consensus side |
|---|---|---|---|
| mainnet | 1 | [go-ethereum params/chainspecs/mainnet.json L3](https://github.com/ethereum/go-ethereum/blob/1df30b18d9ed35ee9d28ffd4648442c2204554ad/params/chainspecs/mainnet.json#L3) | [Lighthouse mainnet/config.yaml L152](https://github.com/sigp/lighthouse/blob/57c2abc13078bf949e4d19df6178a644b064c32c/common/eth2_network_config/built_in_network_configs/mainnet/config.yaml#L152) (`DEPOSIT_CHAIN_ID`) |
| sepolia | 11155111 (`0xaa36a7`) | [go-ethereum params/chainspecs/sepolia.json L3](https://github.com/ethereum/go-ethereum/blob/1df30b18d9ed35ee9d28ffd4648442c2204554ad/params/chainspecs/sepolia.json#L3) | [Lighthouse sepolia/config.yaml L111](https://github.com/sigp/lighthouse/blob/57c2abc13078bf949e4d19df6178a644b064c32c/common/eth2_network_config/built_in_network_configs/sepolia/config.yaml#L111) |
| hoodi | 560048 (`0x88bb0`) | [go-ethereum params/chainspecs/hoodi.json L3](https://github.com/ethereum/go-ethereum/blob/1df30b18d9ed35ee9d28ffd4648442c2204554ad/params/chainspecs/hoodi.json#L3) | [Lighthouse hoodi/config.yaml L111](https://github.com/sigp/lighthouse/blob/57c2abc13078bf949e4d19df6178a644b064c32c/common/eth2_network_config/built_in_network_configs/hoodi/config.yaml#L111) |

Holesky is not supported: "deprecated as of September 2025" per
[ethereum.org/developers/docs/networks](https://ethereum.org/en/developers/docs/networks/).

### Live recordings (2026-11-24)

| File | Endpoint | Notes |
|---|---|---|
| `execution/live/chainid-publicnode-mainnet.json` | `https://ethereum-rpc.publicnode.com` | `0x1` |
| `execution/live/chainid-publicnode-sepolia.json` | `https://ethereum-sepolia-rpc.publicnode.com` | `0xaa36a7` |
| `execution/live/chainid-publicnode-hoodi.json` | `https://ethereum-hoodi-rpc.publicnode.com` | `0x88bb0` |
| `consensus/live/deposit-contract-publicnode-mainnet.json` | `https://ethereum-beacon-api.publicnode.com` | `"1"`, address in lower case |
| `consensus/live/deposit-contract-publicnode-sepolia.json` | `https://ethereum-sepolia-beacon-api.publicnode.com` | `"11155111"` |
| `consensus/live/deposit-contract-lodestar-hoodi.json` | `https://lodestar-hoodi.chainsafe.io` | `"560048"`; **same deposit contract address as mainnet**, so only `chain_id` identifies the network |

## Fork configuration

### execution/eth-config-eip-example.json, execution/eth-config-eip-no-future.json — literal

The two complete responses from
[EIP-7910 L225-L377](https://github.com/ethereum/EIPs/blob/0c82d532192eca83ab5ce12b2a0d3e019c803066/EIPS/eip-7910.md?plain=1#L225-L377)
(Hoodi with Prague scheduled, and "Without Future Fork Scheduled"), copied
unchanged from the code blocks. The EIP defines `activationTime` as a JSON
number in Unix seconds and `next`/`last` as `null` if no future fork is
configured.

### consensus/config-spec-example.json — literal

The `example` of `data` in
[beacon-APIs v4.0.0 apis/config/spec.yaml](https://github.com/ethereum/beacon-APIs/blob/88fc9402c6a1821b6e9ae4e3b8ae0a22f1141203/apis/config/spec.yaml),
converted from YAML to JSON. It has no `*_FORK_EPOCH` keys, but a
`BLOB_SCHEDULE` with entries at the Deneb and Electra epochs: such entries
belong to hard forks, not to BPO forks. The same file says the response
includes "constants for all hard forks known by the beacon node", so a
missing `<NAME>_FORK_EPOCH` key means the client does not know the fork.

### Live recordings (2026-11-24)

Same method as above: the request x3f sends (`eth_config`,
`GET /eth/v1/config/spec`) with curl, only the response body stored.

| File | Endpoint | Notes |
|---|---|---|
| `execution/live/eth-config-publicnode-mainnet.json` | `https://ethereum-rpc.publicnode.com` | `current.activationTime` 1767747671 (BPO2), `next`/`last` `null` |
| `execution/live/eth-config-publicnode-sepolia.json` | `https://ethereum-sepolia-rpc.publicnode.com` | 1791294816 (Glamsterdam); Reth 2.11.0, `blobSchedule` keys in alphabetical order |
| `execution/live/eth-config-publicnode-hoodi.json` | `https://ethereum-hoodi-rpc.publicnode.com` | 1762955544 (BPO2): Geth 1.17.10 without the postponed Glamsterdam schedule |
| `execution/live/eth-config-mevblocker-mainnet.json` | `https://rpc.mevblocker.io` | JSON-RPC error -32601: proxy does not offer `eth_config` |
| `consensus/live/spec-publicnode-mainnet.json` | `https://ethereum-beacon-api.publicnode.com` | Lighthouse 8.3.3; `GLOAS` and `HEZE` far future (18446744073709551615) |
| `consensus/live/spec-publicnode-sepolia.json` | `https://ethereum-sepolia-beacon-api.publicnode.com` | Lighthouse 8.3.3; `GLOAS_FORK_EPOCH` 353024 |
| `consensus/live/spec-lodestar-hoodi.json` | `https://lodestar-hoodi.chainsafe.io` | Lodestar 1.51.0; `GLOAS` far future (postponed) |
| `consensus/live/spec-nimbus-unstable-mainnet.json` | `http://unstable.mainnet.beacon-api.nimbus.team` | Nimbus 26.10.2; **no `HEZE_FORK_EPOCH` key** (fork not known yet) |

All values are quoted integers, as the beacon-APIs spec requires. The
`BLOB_SCHEDULE` entries of all four recordings are the BPO forks only.
