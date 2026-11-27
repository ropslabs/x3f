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

The canonical rollups and their versions (2070860440, 679261766) are those
the dRPC nodes report in getNodeInfo. Function names and signatures are from
aztec-packages v6.0.0 (commit 0219f11d81),
`l1-contracts/src/governance/interfaces/IRegistry.sol` and
`l1-contracts/src/core/interfaces/IRollup.sol` (via `IHaveVersion`).

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
