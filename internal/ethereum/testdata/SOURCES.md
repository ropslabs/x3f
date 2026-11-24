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
