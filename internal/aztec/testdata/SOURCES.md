# Sources for test fixtures

## live/*.json — recorded

Recorded on 2026-11-27 from the public dRPC endpoints, without
authentication, with

```
POST https://aztec-mainnet.drpc.org   {"jsonrpc":"2.0","id":1,"method":"<method>","params":[]}
POST https://aztec-testnet.drpc.org
```

| File | Method | Endpoint |
|---|---|---|
| `aztec_getNodeInfo-drpc-mainnet.json` | `aztec_getNodeInfo` | aztec-mainnet.drpc.org |
| `node_getNodeInfo-drpc-mainnet.json` | `node_getNodeInfo` | aztec-mainnet.drpc.org |
| `aztec_getNodeInfo-drpc-testnet.json` | `aztec_getNodeInfo` | aztec-testnet.drpc.org |
| `aztec_getChainTips-drpc-{mainnet,testnet}.json` | `aztec_getChainTips` | recorded at 13:41 UTC, right after the L1 snapshot below |

The response bodies are unchanged (they contain public data only: version,
chain IDs, contract addresses, the node's ENR). No HTTP headers were stored.
Both namespaces answered identically on 6.0.0. The registry addresses match
`network_config.json` of AztecProtocol/networks (commit 7182c14b).

## live/l1/*.json — recorded

Answers of an Ethereum node to the L1 reads of x3f, recorded on 2026-11-27
at 12:51 UTC from the public publicnode.com endpoints (Ethereum mainnet and
Sepolia), without authentication, with `{"jsonrpc":"2.0","id":1,...}`
requests. The response bodies are unchanged; no HTTP headers were stored.

| File | Request |
|---|---|
| `web3_sha3-getCanonicalRollup-publicnode-mainnet.json` | `web3_sha3` of the bytes of `getCanonicalRollup()` |
| `web3_sha3-getVersion-publicnode-mainnet.json` | `web3_sha3` of the bytes of `getVersion()` |
| `eth_chainId-publicnode-{mainnet,sepolia}.json` | `eth_chainId` |
| `eth_call-getCanonicalRollup-publicnode-{mainnet,sepolia}.json` | `eth_call` of `getCanonicalRollup()` on the registry from the recorded getNodeInfo answers (`0x35b2…e298`, `0xa0bf…c6ba`), block `latest` |
| `eth_call-getVersion-publicnode-{mainnet,sepolia}.json` | `eth_call` of `getVersion()` on the canonical rollup of that answer (`0xe37a…402f`, `0x1f43…efba`) |
| `eth_call-revert-publicnode-mainnet.json` | `eth_call` of `getVersion()` on the mainnet registry, which has only `getVersion(uint256)`: reverts |
| `eth_call-no-code-publicnode-mainnet.json` | `eth_call` of `getVersion()` on the zero address, which has no code: `"0x"` |

### Sync snapshot (13:41 UTC)

For the sync check, one snapshot per network was recorded the way x3f
reads it: L1 first, then the node's `aztec_getChainTips` (above).

| File | Request |
|---|---|
| `web3_sha3-{getTips,getCheckpoint,getSlotAt,getSlotDuration,getEpochDuration}-publicnode-mainnet.json` | `web3_sha3` of the signatures `getTips()`, `getCheckpoint(uint256)`, `getSlotAt(uint256)`, `getSlotDuration()`, `getEpochDuration()` |
| `web3_sha3-Rollup__UnavailableTempCheckpointLog-publicnode-mainnet.json` | `web3_sha3` of the error signature `Rollup__UnavailableTempCheckpointLog(uint256,uint256,uint256)` |
| `eth_getBlockByNumber-latest-publicnode-{mainnet,sepolia}.json` | `eth_getBlockByNumber` with `["latest", false]` |
| `eth_call-getTips-publicnode-*.json` | `getTips()` on the canonical rollup: mainnet pending 36762, proven 36734; Sepolia 42994, 42991 |
| `eth_call-getSlotAt-publicnode-*.json` | `getSlotAt(timestamp of that block)`: slot 57983, 43314 |
| `eth_call-getSlotDuration-publicnode-*.json`, `eth_call-getEpochDuration-publicnode-*.json` | 72 seconds, 32 slots on both |
| `eth_call-getCheckpoint-{pending,proven}-publicnode-*.json` | `getCheckpoint(n)` of the pending and the proven checkpoint; `slotNumber` is word 6 of the 12-word `CheckpointLog` |
| `eth_call-getCheckpoint-unavailable-publicnode-*.json` | `getCheckpoint(pending - 65)`: reverts with `Rollup__UnavailableTempCheckpointLog`, since the rollup keeps only epochDuration × (proof submission epochs + 1) + 1 = 65 checkpoints (`core/libraries/rollup/STFLib.sol`) |

Both nodes were in sync with L1 at that moment (checkpointed = pending,
proven = proven).

The canonical rollups and their versions (2070860440, 679261766) are those
the dRPC nodes report in getNodeInfo. Function names and signatures are from
aztec-packages v6.0.0 (commit 0219f11d81),
`l1-contracts/src/governance/interfaces/IRegistry.sol`,
`l1-contracts/src/core/interfaces/IRollup.sol` (with `IHaveVersion`),
`core/interfaces/IValidatorSelection.sol` and `core/libraries/Errors.sol`.

## networks/network_config.json — recorded, trimmed

`network_config.json` of
[AztecProtocol/networks](https://github.com/AztecProtocol/networks/blob/7182c14bbb32f5af995e06edcec0690633acae4a/network_config.json)
at commit 7182c14b (main on 2026-11-27; the file last changed on
2026-05-28). The Aztec node loads this file for `--network`
(`yarn-project/cli/src/config/network_config.ts` in aztec-labs-eng/aztec-node
at commit 1b6de149f4). **Trimmed:** each network keeps only `l1ChainId` and
`registryAddress`, the fields x3f uses; bootnodes, snapshot and file store
URLs were removed. The docs page `docs/docs/networks.md` of aztec-packages
lists the same registry addresses.

## method-not-found.json — derived

The answer of an Aztec node to a JSON-RPC method it does not serve, built
from the node source in aztec-labs-eng/aztec-node at commit 1b6de149f4 (the
`labs` submodule of AztecProtocol/aztec-packages v6.0.0):

- [`safe_json_rpc_server.ts` line 282](https://github.com/aztec-labs-eng/aztec-node/blob/1b6de149f4/yarn-project/foundation/src/json-rpc/server/safe_json_rpc_server.ts#L282):
  `{ jsonrpc, id, error: { code: -32601, message: `Method not found: ${method}` } }`
- [`aztec_start_action.ts` line 98](https://github.com/aztec-labs-eng/aztec-node/blob/1b6de149f4/yarn-project/aztec/src/cli/aztec_start_action.ts#L98):
  the node starts its server with `http200OnError: false`, so the error is
  sent with **HTTP 400** (see `getRouter` in the same server file).

Namespaces: [`register_node_rpc_handlers.ts`](https://github.com/aztec-labs-eng/aztec-node/blob/1b6de149f4/yarn-project/aztec-node/src/aztec-node/register_node_rpc_handlers.ts)
registers `aztec_*` (since v5) and the legacy `node_*` (before v5, kept for
compatibility, to be removed in a future release).
