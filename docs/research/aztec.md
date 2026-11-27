Aztec module – research notes

Status: 2026-11-27. Technical background for the Aztec module of x3f. Live findings from read-only checks against public endpoints are added as they are verified.

Networks
Alpha (mainnet, L1 = Ethereum mainnet, chain ID 1) and testnet (L1 = Sepolia, chain ID 11155111), both on 6.0.0 ("Tierra").
Public RPC endpoints for testing: https://aztec-mainnet.drpc.org, https://aztec-testnet.drpc.org.
Official network list: AztecProtocol/networks, file network_config.json (L1 chain ID and registry address per network).
Releases
One version for node, Aztec.nr and aztec.js. Up to v5 everything was released from the monorepo AztecProtocol/aztec-packages. Since v6 the node source is aztec-labs-eng/aztec-node, with its own GitHub releases (Docker image azteclabs/aztec); aztec-packages keeps Barretenberg, bb.js and the L1 contracts (see "Live results: open points" below).
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

Live results: open points (2026-11-27, read-only)
Rollup checkpoint (source: aztec-packages v6.0.0, l1-contracts/src/core/interfaces/IRollup.sol and core/libraries/compressed-data/Tips.sol): Rollup.getTips() returns ChainTips {pending, proven}, both checkpoint numbers; getPendingCheckpointNumber() and getProvenCheckpointNumber() return the same values; getCheckpoint(n) returns a CheckpointLog with slotNumber; getSlotAt(timestamp), getSlotDuration() (72 s) and getEpochDuration() (32 slots) come from IValidatorSelection.sol. Live, mainnet: L1 pending 36642 / proven 36613 = the drpc node's checkpointed.checkpoint.number / proven.checkpoint.number; the slot of the pending checkpoint equals getSlotAt(latest L1 block timestamp). Testnet: L1 42884 / 42868 = node. The proposed tip of getChainTips is a block number (65493 on mainnet), not a checkpoint number; several blocks form one checkpoint.
Node version and rollup version: the official source is the networks page of the docs (AztecProtocol/aztec-packages, docs/docs/networks.md, rows "Version" and "Rollup Version" per network; from 2026-03 to 2026-10 the page lived in aztec-labs-eng/aztec-node, its git history covers every change). Current: Alpha 6.0.0 / 2070860440, Testnet 6.0.0 / 679261766, matching the live nodes; at the v6.0.0 submodule commit: Alpha 5.2.0 / 4248422647, Testnet 6.0.0-rc.1 / 2914217885. The page lists only what a network runs now, not a table of all versions, and it still says 6.0.0 after the node release 6.1.0. Within a release line the rollup stays the same: the aztec-node v6.1.0 notes say "no protocol change, same rollup"; a new major (v6 "Tierra") is a new rollup deployed from genesis (operator changelog v6.md). AztecProtocol/networks network_config.json has no rollup version (keys: bootnodes, snapshots, registryAddress, l1ChainId, feeAssetHandlerAddress, file store URLs).
Governance payloads (source: governance/interfaces/IGovernance.sol, IPayload.sol, IRegistry.sol): getProposal(id) returns a static Proposal struct; the payload address is word 8 (after the 7-word ProposalConfiguration and cachedState), creation word 10. IPayload.getActions() returns Action[] {target, data}; getURI() a free-text description. A rollup upgrade is an action with target = registry and selector 0x59264f00 = addRollup(address); its argument is the new rollup, whose getVersion() gives the new rollup version. Live, mainnet: 7 proposals, addRollup in #2 (2026-03-13, version 2934756905), #4 (2026-07-02, 4248422647) and #5 (2026-10-14, "V6UpgradePayload", 2070860440 = canonical); getActions() of #1 reverts (custom error 0xe7a31ae1), so decoding must tolerate failures. Testnet: 9 proposals, addRollup in all of them, #8 (2026-10-22, "V6UpgradePayload") = canonical 679261766. The docs networks page has a row "Register New Rollup Version Payload" (N/A on both networks today).
Self-hosted node, ports and namespaces (source: aztec-labs-eng/aztec-node at 1b6de149f4, yarn-project/aztec/src/cli/aztec_start_options.ts, aztec_start_action.ts, register_node_rpc_handlers.ts): public JSON-RPC on --port / AZTEC_PORT, default 8080; admin JSON-RPC on --admin-port / AZTEC_ADMIN_PORT, default 8880, with an API key unless disabled. Namespaces on the public port: aztec_* (since v5), node_* as an alias of the same handlers (legacy, to be removed, ticket A-1169), p2p_*, and aztecDebug_* / nodeDebug_* in debug mode. Errors come as HTTP 400 (http200OnError false). Every response carries x-aztec-* headers (l1ChainId, l1RollupAddress, rollupVersion, l2ProtocolContractsHash, l2CircuitsVkTreeRoot, packageVersion); the server never rejects a request because of them. Live through drpc: the headers arrive (packageVersion 6.0.0, rollupVersion as above). Not verified against a self-hosted node: running the official image was not possible in this check.
Release source: aztec-labs-eng/aztec-node has GitHub releases v6.0.0-rc.1 (pre-release), v6.0.0 (2026-10-22) and v6.1.0 (2026-11-24, a security release: denial of service in JSON-RPC input handling, "all Aztec nodes should upgrade as soon as possible", no published GHSA) and no security advisories; its tags include v7.0.0-nightly.<date>. aztec-packages v6.1.0 (2026-11-19) is a Barretenberg/L1 release; its notes say node operators should read the aztec-node release notes instead.
Open points
Confirm the above against a self-hosted node (ports, namespaces, headers).
Node metrics (Prometheus/OpenTelemetry) for later integration.
Sources
docs.aztec.network: networks, node API reference, operator guide "start a node"
Aztec Operator Feedback Survey (Aztec forum, June 2026)
AztecProtocol/aztec-packages, tag v6.0.0, l1-contracts/src/governance/
AztecProtocol/networks, network_config.json
AztecProtocol/aztec-packages docs/docs/networks.md (branch next) and its history; aztec-labs-eng/aztec-node docs/docs-operate/operators/reference/changelog/v6.md, GitHub releases of aztec-labs-eng/aztec-node and aztec-packages v6.1.0
Live checks: aztec-mainnet.drpc.org, aztec-testnet.drpc.org; L1 reads via ethereum-rpc.publicnode.com and ethereum-sepolia-rpc.publicnode.com; GitHub API (releases, security advisories of AztecProtocol/aztec-packages)
