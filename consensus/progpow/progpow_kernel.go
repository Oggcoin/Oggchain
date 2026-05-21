package progpow

import (
	"encoding/binary"
        "math/bits"
)

// ----------------------------------------------------------------------------
// KISS99 pseudo-random number generator (as used by ProgPoW / EIP-1057)
// ----------------------------------------------------------------------------

type kiss99State struct {
	z, w, jsr, jcong uint32
}

func (k *kiss99State) next() uint32 {
	k.z = 36969*(k.z&65535) + (k.z >> 16)
	k.w = 18000*(k.w&65535) + (k.w >> 16)
	mwc := (k.z << 16) + k.w
	// SHR3 — per ifdefelse/ProgPOW kiss99 reference (shifts 17, 13, 5):
	// st.jsr ^= (st.jsr << 17); st.jsr ^= (st.jsr >> 13); st.jsr ^= (st.jsr << 5);
	k.jsr ^= k.jsr << 17
	k.jsr ^= k.jsr >> 13
	k.jsr ^= k.jsr << 5
	k.jcong = 69069*k.jcong + 1234567
	return (mwc ^ k.jcong) + k.jsr
}

// ----------------------------------------------------------------------------
// keccak_f800 — 800-bit Keccak permutation (22 rounds, 32-bit words)
// Used by ProgPoW instead of the standard keccak-256.
// ----------------------------------------------------------------------------

var keccakf800RC = [22]uint32{
	0x00000001, 0x00008082, 0x0000808A, 0x80008000,
	0x0000808B, 0x80000001, 0x80008081, 0x00008009,
	0x0000008A, 0x00000088, 0x80008009, 0x8000000A,
	0x8000808B, 0x0000008B, 0x00008089, 0x00008003,
	0x00008002, 0x00000080, 0x0000800A, 0x8000000A,
	0x80008081, 0x00008080,
}

// piLane[i] is the destination state index for step i of the Keccak Pi+Rho transform.
var piLane = [24]int{10, 7, 11, 17, 18, 3, 5, 16, 8, 21, 24, 4, 15, 23, 19, 13, 12, 2, 20, 14, 22, 9, 6, 1}

// rhoOffset[i] = (i+1)*(i+2)/2 mod 32 — Keccak-f[800] Rho rotation amounts.
var rhoOffset = [24]int{1, 3, 6, 10, 15, 21, 28, 4, 13, 23, 2, 14, 27, 9, 24, 8, 25, 11, 30, 18, 7, 29, 20, 12}

func keccakF800(st *[25]uint32) {
	var bc [5]uint32
	for r := 0; r < 22; r++ {
		// Theta
		for i := 0; i < 5; i++ {
			bc[i] = st[i] ^ st[i+5] ^ st[i+10] ^ st[i+15] ^ st[i+20]
		}
		for i := 0; i < 5; i++ {
			t := bc[(i+4)%5] ^ bits.RotateLeft32(bc[(i+1)%5], 1)
			for j := 0; j < 25; j += 5 {
				st[j+i] ^= t
			}
		}
		// Rho + Pi
		t := st[1]
		for i := 0; i < 24; i++ {
			j := piLane[i]
			bc[0] = st[j]
			st[j] = bits.RotateLeft32(t, rhoOffset[i])
			t = bc[0]
		}
		// Chi
		for j := 0; j < 25; j += 5 {
			for i := 0; i < 5; i++ {
				bc[i] = st[j+i]
			}
			for i := 0; i < 5; i++ {
				st[j+i] ^= (^bc[(i+1)%5]) & bc[(i+2)%5]
			}
		}
		// Iota
		st[0] ^= keccakf800RC[r]
	}
}

// keccakF800Short computes the ProgPoW seed hash from a 32-byte header hash
// and a 64-bit nonce. Returns 8 uint32 words (256 bits).
//
// State layout:
//   st[0..7]  = header hash (8 × uint32 LE)
//   st[8..9]  = nonce (2 × uint32 LE)
//   st[10]    = Keccak padding 0x00000001
//   st[18]    = Keccak padding 0x80008081
func keccakF800Short(headerHash [32]byte, nonce uint64) [8]uint32 {
	var st [25]uint32
	for i := 0; i < 8; i++ {
		st[i] = binary.LittleEndian.Uint32(headerHash[i*4:])
	}
	st[8] = uint32(nonce)
	st[9] = uint32(nonce >> 32)
	st[10] = 0x00000001
	st[18] = 0x80008081

	keccakF800(&st)

	var out [8]uint32
	for i := range out {
		out[i] = st[i]
	}
	return out
}

// keccakF800Long computes the final ProgPoW hash from the header, nonce, and
// the 8-word compressed mix hash. Returns 8 uint32 words (256 bits).
//
// State layout:
//   st[0..7]  = header hash
//   st[8..9]  = nonce
//   st[10..17]= mixHash (8 uint32 — the compressed mix)
//   st[18]    = Keccak padding 0x00000001
//   st[24]    = Keccak padding 0x80008081
func keccakF800Long(headerHash [32]byte, nonce uint64, mixHash [8]uint32) [8]uint32 {
	var st [25]uint32
	for i := 0; i < 8; i++ {
		st[i] = binary.LittleEndian.Uint32(headerHash[i*4:])
	}
	st[8] = uint32(nonce)
	st[9] = uint32(nonce >> 32)
	for i, w := range mixHash {
		st[10+i] = w
	}
	st[18] = 0x00000001
	st[24] = 0x80008081

	keccakF800(&st)

	var out [8]uint32
	for i := range out {
		out[i] = st[i]
	}
	return out
}

// ----------------------------------------------------------------------------
// Math operations used in the random program (EIP-1057 §3)
// ----------------------------------------------------------------------------

// merge folds value b into a using operation selector s.
func merge(a, b, s uint32) uint32 {
	switch s % 4 {
	case 0:
		return a*33 + b
	case 1:
		return (a ^ b) * 33
	case 2:
		return bits.RotateLeft32(a, int(((b>>16)&31)+1)) ^ b
	default:
		return bits.RotateLeft32(a, -(int((b>>16)&31) + 1)) ^ b
	}
}

// mathOp applies one of 11 deterministic integer operations (selector s % 11).
func mathOp(a, b, s uint32) uint32 {
	switch s % 11 {
	case 0:
		return a + b
	case 1:
		return a * b
	case 2:
		return uint32((uint64(a) * uint64(b)) >> 32) // mulhi
	case 3:
		if a < b {
			return a
		}
		return b
	case 4:
		return bits.RotateLeft32(a, int(b))
	case 5:
		return bits.RotateLeft32(a, -int(b))
	case 6:
		return a & b
	case 7:
		return a | b
	case 8:
		return a ^ b
	case 9:
		return uint32(bits.LeadingZeros32(a) + bits.LeadingZeros32(b))
	default: // 10
		return uint32(bits.OnesCount32(a) + bits.OnesCount32(b))
	}
}

// fnv1a is the 32-bit FNV-1a hash used throughout ProgPoW.
func fnv1a(h, data uint32) uint32 {
	return (h ^ data) * 0x1000193
}

// ----------------------------------------------------------------------------
// Program generation (called once per period, not once per nonce)
// ----------------------------------------------------------------------------

type progpowProg struct {
	dstReg  [progpowCntMath]uint32
	srcReg  [progpowCntMath]uint32
	selA    [progpowCntMath]uint32
	selB    [progpowCntMath]uint32
	cacheSrc [progpowCntCache]uint32
	cacheSel [progpowCntCache]uint32
	dagSrc  uint32 // which lane drives the DAG address
}

// buildProgram generates the per-period random program from the KISS99 RNG.
func buildProgram(rng *kiss99State) progpowProg {
	var p progpowProg
	var regUsed [progpowRegs]bool

	pickReg := func() uint32 {
		r := rng.next() % progpowRegs
		if !regUsed[r] {
			regUsed[r] = true
			return r
		}
		for j := uint32(1); j < progpowRegs; j++ {
			c := (r + j) % progpowRegs
			if !regUsed[c] {
				regUsed[c] = true
				return c
			}
		}
		// All registers used — reset and pick
		for i := range regUsed {
			regUsed[i] = false
		}
		r = rng.next() % progpowRegs
		regUsed[r] = true
		return r
	}

	for i := 0; i < progpowCntMath; i++ {
		p.dstReg[i] = pickReg()
		p.srcReg[i] = pickReg()
		p.selA[i] = rng.next()
		p.selB[i] = rng.next()
	}
	for i := 0; i < progpowCntCache; i++ {
		p.cacheSrc[i] = rng.next() % progpowRegs
		p.cacheSel[i] = rng.next()
	}
	p.dagSrc = rng.next() % progpowLanes

return p
}

// seedRNG initialises a KISS99 RNG from the block-period number (progSeed).
// Matches ifdefelse/ProgPOW::getKern(uint64_t prog_seed). The C++ fnv1a takes
// its first arg by reference (uint32_t &h) and mutates it, so each call feeds
// the previous return value — Go must thread it through explicitly.

func seedRNG(progSeed uint64) kiss99State {
        seed0 := uint32(progSeed)
        seed1 := uint32(progSeed >> 32)

        fnvHash := fnvOffsetBasis

        z := fnv1a(fnvHash, seed0)
        w := fnv1a(z, seed1)
        jsr := fnv1a(w, seed0)
        jcong := fnv1a(jsr, seed1)

        return kiss99State{z: z, w: w, jsr: jsr, jcong: jcong}
}

// fillMix initialises one lane's mix[REGS] register file from the 64-bit
// hash_seed and lane_id. Matches EIP-1057 reference fill_mix:
//
//   uint32_t fnv_hash = FNV_OFFSET_BASIS;
//   st.z     = fnv1a(fnv_hash, seed);
//   st.w     = fnv1a(fnv_hash, seed >> 32);
//   st.jsr   = fnv1a(fnv_hash, lane_id);
//   st.jcong = fnv1a(fnv_hash, lane_id);
//   for (int i = 0; i < PROGPOW_REGS; i++) mix[i] = kiss99(st);
//
// The C++ fnv1a takes its first argument by reference and mutates it, so each
// call threads the previous return value. Go's fnv1a returns the new hash, so
// chaining the return values reproduces the same semantics.
func fillMix(hashSeed uint64, laneID uint32) [progpowRegs]uint32 {
	z := fnv1a(fnvOffsetBasis, uint32(hashSeed))
	w := fnv1a(z, uint32(hashSeed>>32))
	jsr := fnv1a(w, laneID)
	jcong := fnv1a(jsr, laneID)
	st := kiss99State{z: z, w: w, jsr: jsr, jcong: jcong}
	var mix [progpowRegs]uint32
	for i := range mix {
		mix[i] = st.next()
	}
	return mix
}

// ----------------------------------------------------------------------------
// progpowLoop — the core per-nonce hash loop (EIP-1057 §4)
// ----------------------------------------------------------------------------

// progpowLoop executes the ProgPoW inner loop for one nonce attempt.
//
//   seed      — 8-word seed from keccakF800Short
//   dag       — DAG as []uint32 (full Ethash dataset)
//   cache     — L1 cache as []uint32 of progpowCacheWords elements
//   dagSize   — DAG size in bytes
//   period    — blockNumber / progpowPeriod
//
// Returns the 8-word compressed mix hash (= block header MixDigest).
func progpowLoop(
	seed [8]uint32,
	dag []uint32,
	cache []uint32,
	dagSize uint64,
	period uint64,
) [8]uint32 {
	// Build the per-period random program from the period number (not the
	// keccak seed). Matches ifdefelse/ProgPOW::getKern(prog_seed).
	rng := seedRNG(period)
	prog := buildProgram(&rng)

	// DAG geometry.
	rowWords := uint64(progpowLanes * progpowDAGLoads)
	numRows := dagSize / (rowWords * 4)
	if numRows == 0 {
		numRows = 1
	}

	// Derive the 64-bit hash_seed from the first two words of the keccak
	// seed. Matches EIP-1057 reference:
	//   seed = ((uint64_t)hash_init.uint32s[1] << 32) | hash_init.uint32s[0]
	hashSeed := uint64(seed[1])<<32 | uint64(seed[0])

	// Initialise per-lane mix[REGS] via fill_mix (EIP-1057 §3). Each lane's
	// 32 registers are seeded from a KISS99 whose initial state comes from
	// (hash_seed_low, hash_seed_high, lane_id, lane_id) threaded through
	// mutable fnv_hash — see fillMix.
	var state [progpowLanes][progpowRegs]uint32
	for l := uint32(0); l < progpowLanes; l++ {
		state[l] = fillMix(hashSeed, l)
	}

	// Outer loop: progpowCntDAG iterations.
	// Structure per iteration (matches ifdefelse/ProgPOW::progPowLoop):
	//   1. DAG load: read PROGPOW_DAG_LOADS words per lane from a shared row
	//   2. Interleaved cache + math: max(CNT_CACHE, CNT_MATH) iterations
	//   3. DAG merge: fold the loaded DAG words into mix registers
	for i := uint32(0); i < progpowCntDAG; i++ {

		// 1. DAG load — compute address from prog.dagSrc lane (broadcast,
		//    matching C++ __shfl_sync(mix[0], dag_src)).
		var dagData [progpowLanes][progpowDAGLoads]uint32
		dagRow := (uint64(state[prog.dagSrc][0]) ^ uint64(i)) % numRows
		for l := uint32(0); l < progpowLanes; l++ {
			// Per-lane column within the row: (lane ^ i) % LANES
			col := (l ^ (i % progpowLanes)) % progpowLanes
			baseOffset := dagRow*rowWords + uint64(col)*progpowDAGLoads
			for d := uint32(0); d < progpowDAGLoads; d++ {
				idx := baseOffset + uint64(d)
				if idx < uint64(len(dag)) {
					dagData[l][d] = dag[idx]
				}
			}
		}

		// 2. Interleaved cache reads + math ops.
		maxOps := progpowCntMath // max(progpowCntCache, progpowCntMath) = max(11,18) = 18
		for j := 0; j < maxOps; j++ {
			// Cache read (if j < CNT_CACHE)
			if j < progpowCntCache {
				for l := uint32(0); l < progpowLanes; l++ {
					s := prog.cacheSrc[j]
					cacheIdx := (state[l][s] ^ (i*progpowLanes + l)) % progpowCacheWords
					state[l][s] = merge(state[l][s], cache[cacheIdx], prog.cacheSel[j])
				}
			}
			// Math op (if j < CNT_MATH)
			if j < progpowCntMath {
				for l := uint32(0); l < progpowLanes; l++ {
					dst := prog.dstReg[j]
					src := prog.srcReg[j]
					r := mathOp(state[l][dst], state[l][src], prog.selA[j])
					state[l][dst] = merge(state[l][dst], r, prog.selB[j])
				}
			}
		}

		// 3. DAG merge — fold loaded words into mix registers.
		for l := uint32(0); l < progpowLanes; l++ {
			for d := uint32(0); d < progpowDAGLoads; d++ {
				state[l][d] = merge(state[l][d], dagData[l][d], seed[d%8])
			}
		}
	}

	// Compress the full [LANES][REGS] register state to [8]uint32.
	//
	// Step 1: reduce each lane to one word.
	var laneDigest [progpowLanes]uint32
	for l := 0; l < progpowLanes; l++ {
		laneDigest[l] = fnvOffsetBasis
		for r := 0; r < progpowRegs; r++ {
			laneDigest[l] = fnv1a(laneDigest[l], state[l][r])
		}
	}
	// Step 2: reduce all lane digests to 8 words.
	var mixHash [8]uint32
	for i := range mixHash {
		mixHash[i] = fnvOffsetBasis
	}
	for l := 0; l < progpowLanes; l++ {
		mixHash[l%8] = fnv1a(mixHash[l%8], laneDigest[l])
	}
	return mixHash
}

// BuildL1Cache fills a progpowCacheWords-element []uint32 from the seed hash.
// This is the L1 cache that ProgPoW uses for fast random reads during mining.
func BuildL1Cache(dag []uint32) []uint32 {
    cache := make([]uint32, progpowCacheWords)
    for i := range cache {
        cache[i] = dag[i]
    }
    return cache
}
