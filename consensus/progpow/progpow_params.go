// Package progpow implements the ProgPoW proof-of-work algorithm.
// Algorithm constants from EIP-1057: https://eips.ethereum.org/EIPS/eip-1057
package progpow

const (
	progpowPeriod     = 10         // blocks per random-program period
	progpowLanes      = 16         // parallel lanes
	progpowRegs       = 32         // register file size per lane
	progpowDAGLoads   = 4          // uint32 loads from DAG per lane per DAG-access step
	progpowCacheBytes = 16 * 1024  // L1 cache size in bytes
	progpowCacheWords = progpowCacheBytes / 4
	progpowCntDAG     = 64 // DAG accesses per loop iteration
	progpowCntCache   = 11 // cache accesses per loop iteration (per lane) — EIP-1057 final
	progpowCntMath    = 18 // math ops per loop iteration (per lane) — EIP-1057 final

	// fnvOffsetBasis is the FNV-1a 32-bit offset basis, used for mix compression.
	fnvOffsetBasis = uint32(0x811c9dc5)
)
