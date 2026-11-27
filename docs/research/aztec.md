Aztec module – research notes

Status: 2026-11-27. Technical background for the Aztec module of x3f. Live findings from read-only checks against public endpoints are added as they are verified.

Networks
Alpha (mainnet, L1 = Ethereum mainnet, chain ID 1) and testnet (L1 = Sepolia, chain ID 11155111), both on 6.0.0 ("Tierra").
Public RPC endpoints for testing: https://aztec-mainnet.drpc.org, https://aztec-testnet.drpc.org.
Official network list: AztecProtocol/networks, file network_config.json (L1 chain ID and registry address per network).
Releases
One version for node, Aztec.nr and aztec.js, released from the monorepo AztecProtocol/aztec-packages.
Channels: stable (e.g. 6.0.0), release candidates (x.y.z-rc.N), nightlies (x.y.z-nightly.<date>). Other non-stable tags exist (-devnet, -spartan, v0.0.1-dev).
Stable releases roughly monthly, usually mid-month, no fixed calendar. Release notes on GitHub; the git history is the source of truth.
Mandatory network-wide upgrades are common: the network is decentralized, so a fix only takes effect once operators upgrade. Several critical upgrades happened within one year (Alpha v4, V5, V6 "Tierra").
Node API (JSON-RPC 2.0)
Public port 8080, admin port 8880 (requires an API key, must not be exposed).
Method prefix: current docs use aztec_* / aztecAdmin_*, older versions node_* / nodeAdmin_*. Live check: both prefixes answer on 6.0.0 with identical results.
Purpose	Method
Version	getNodeVersion, getNodeInfo (nodeVersion, l1ChainId, rollupVersion, L1 contract addresses incl. rollup, registry, governance)
Readiness	isReady
Sync	getChainTips (proposed, checkpointed, proven, finalized), getWorldStateSyncStatus, getSyncedL2SlotNumber, getSyncedL1Timestamp
Network	getChainId, getVersion, getL1ContractAddresses
P2P	getPeers (only with P2P enabled)
Validator	getValidatorsStats, getValidatorStats, getValidatorStatsBatch
Admin	aztecAdmin_getConfig, aztecAdmin_getSlashOffenses (admin port only, not used by x3f)

Health check according to the operator docs: proposed keeps increasing; proven is not null and approaches proposed; isReady is true; chain ID and rollup address are as expected.

L1 contracts
Registry: getCanonicalRollup(), numberOfVersions(), getVersion(i), getRollup(version). A node whose rollup address differs from the canonical rollup is no longer on the current version.
Governance: proposalCount(), getProposalState(id) (Pending, Active, Queued, Executable, Rejected, Executed, Droppable, Dropped, Expired), getProposal(id) with creation time and deadlines. Mainnet timings: 3 days voting delay, 7 days voting, 2 days execution delay.
Source for contract functions: aztec-packages at tag v6.0.0, l1-contracts/src/governance/.
Node roles
Full node: keeps state in sync, serves RPC, relays transactions; no stake.
Sequencer: orders transactions, builds blocks, votes on governance; mainnet stake at least 200,000 AZTEC; hardware roughly 8 cores, 16 GiB RAM, 1 TB NVMe.
Prover: produces block proofs; hardware-heavy, specialized operators.
Many operators run dozens of sequencer identities, which makes "what runs where, in which version" a real problem.
Every Aztec node depends on Ethereum L1 endpoints, so x3f's Ethereum checks apply to its L1 connection.
Live results (2026-11-27, read-only)
Node JSON-RPC against aztec-mainnet.drpc.org and aztec-testnet.drpc.org (aztec.drpc.org answers like the mainnet host; aztec-sepolia.drpc.org returns 404):
getNodeInfo: identical answers for node_ and aztec_. Mainnet: nodeVersion 6.0.0, l1ChainId 1, rollupVersion 2070860440, rollupAddress 0xe37a2663cc9c119ba41572c40fab0bbb23a5402f, registryAddress 0x35b22e09ee0390539439e24f06da43d83f90e298. Testnet: nodeVersion 6.0.0, l1ChainId 11155111, rollupVersion 679261766, rollupAddress 0x1f435a7f2da358caf887a470faeee7d5d5f4efba, registryAddress 0xa0bfb1b494fb49041e5c6e8c2c1be09cd171c6ba. Both registry addresses match AztecProtocol/networks network_config.json (commit 7182c14b).
getChainTips: same keys for both prefixes; proposed {number, hash}; checkpointed, proven and finalized each with block {number, hash} and checkpoint {number, hash}. Mainnet example: proposed 64687, checkpointed 64683, proven 64634, finalized 64495.
isReady: true. getBlockNumber answered 64687 (node_) and 64688 (aztec_) one call later, so answers through drpc are not necessarily from the same backend.
getL2Tips is not available through drpc (mainnet: "not available on free plan"; testnet: -32601).
L1 via eth_call (public Ethereum mainnet and Sepolia RPC endpoints of publicnode.com), function names from aztec-packages v6.0.0 (commit 0219f11d81), l1-contracts/src/governance/:
Registry mainnet: numberOfVersions() = 4 (versions 0, 2934756905, 4248422647, 2070860440); getCanonicalRollup() = 0xe37a…402f, the rollup the drpc node reports; Rollup.getVersion() = 2070860440 = rollupVersion of the node. getGovernance() matches governanceAddress from getNodeInfo.
Registry testnet: numberOfVersions() = 9; canonical rollup 0x1f43…efba = the drpc node's rollup; Rollup.getVersion() = 679261766.
Governance mainnet: proposalCount() = 7, all Executed. Testnet: 9 proposals, proposal 0 Rejected, 1–8 Executed. No upgrade pending on either network.
Releases: latest stable on GitHub is v6.1.0 (2026-11-19); the drpc nodes run 6.0.0 (stable since 2026-10-22).
Security advisories of aztec-packages: 3 published (GHSA-73xc-j27q-wg75 medium, GHSA-2hc8-mq24-76v2 high, GHSA-778c-2h5c-8r96 low). They are per npm package (@aztec/bb.js, @aztec-foundation/bb.js, barretenberg-rs; GHSA-778c-2h5c-8r96 lists "@aztec/archiver @aztec/stdlib" as one package name); ranges include nightly, devnet and spartan versions, some entries have no patched version, and one range is free text ("current Ignition mainnet release (v4 tag)").

Open points
Exact response schemas against a self-hosted node (ports, namespaces).
Which rollup contract function returns the latest checkpoint, to measure how far a node lags behind L1.
Official source for which node version belongs to which rollup version.
Whether a governance proposal registers a new rollup can be seen from its payload (IPayload.getActions()).
Node metrics (Prometheus/OpenTelemetry) for later integration.
Sources
docs.aztec.network: networks, node API reference, operator guide "start a node"
Aztec Operator Feedback Survey (Aztec forum, June 2026)
AztecProtocol/aztec-packages, tag v6.0.0, l1-contracts/src/governance/
AztecProtocol/networks, network_config.json
Live checks: aztec-mainnet.drpc.org, aztec-testnet.drpc.org; L1 reads via ethereum-rpc.publicnode.com and ethereum-sepolia-rpc.publicnode.com; GitHub API (releases, security advisories of AztecProtocol/aztec-packages)
