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
