package progpow

import "testing"

// Reference vectors from github.com/ifdefelse/ProgPOW/blob/master/test-vectors.md.
// These validate the ProgPoW primitives bit-for-bit against the canonical C++
// reference. The self-referential tests in testbench/ cannot detect drift
// from the reference — only these vectors can.

func TestFNV1AReferenceVectors(t *testing.T) {
	cases := []struct {
		h, d, want uint32
	}{
		{0x811c9dc5, 0xddd0a47b, 0xd37ee61a},
		{0xd37ee61a, 0xee304846, 0xdedc7ad4},
		{0xdedc7ad4, 0x00000000, 0xa9155bbc},
	}
	for _, c := range cases {
		got := fnv1a(c.h, c.d)
		if got != c.want {
			t.Errorf("fnv1a(%#x, %#x) = %#x, want %#x", c.h, c.d, got, c.want)
		}
	}
}

func TestKISS99ReferenceOutputs(t *testing.T) {
	// Initial state from test-vectors.md: the classic Marsaglia KISS99 seeds.
	rng := kiss99State{z: 362436069, w: 521288629, jsr: 123456789, jcong: 380116160}
	want := []uint32{769445856, 742012328, 2121196314, 2805620942}
	for i, w := range want {
		got := rng.next()
		if got != w {
			t.Errorf("iter %d: got %d, want %d", i+1, got, w)
		}
	}
}

func TestMathOpReferenceVectors(t *testing.T) {
	// (a, b, r) → expected. r's low bits route to the op via r%11.
	cases := []struct {
		name       string
		a, b, r, want uint32
	}{
		{"add", 0x8626bb1f, 0xbbdfbc4e, 0x883e5b49, 0x4206776d},
		{"mul", 0x3f4bdfac, 0xd79e414f, 0x36b71236, 0x4c5cb214},
		{"mulhi", 0x6d175b7e, 0xc4e89d4c, 0x944ecabb, 0x53e9023f},
		{"min", 0x2eddd94c, 0x7e70cb54, 0x3f472a85, 0x2eddd94c},
		{"rotl32", 0x8a81e396, 0x3f4bdfac, 0xcec46e67, 0x1e3968a8},
		{"rotr32", 0x8a81e396, 0x7e70cb54, 0xdbe71ff7, 0x1e3968a8},
		{"and", 0xa7352f36, 0xa0eb7045, 0x59e7b9d8, 0xa0212004},
		{"or", 0xc89805af, 0x64291e2f, 0x1bdc84a9, 0xecb91faf},
		{"xor", 0x760726d3, 0x79fc6a48, 0xc675cac5, 0x0ffb4c9b},
		{"clz", 0x75551d43, 0x3383ba34, 0x2863ad31, 0x00000003},
		{"popcount", 0xea260841, 0xe92c44b7, 0xf83ffe7d, 0x0000001b},
	}
	for _, c := range cases {
		got := mathOp(c.a, c.b, c.r)
		if got != c.want {
			t.Errorf("%s: mathOp(%#x, %#x, %#x) = %#x, want %#x",
				c.name, c.a, c.b, c.r, got, c.want)
		}
	}
}

// TestSeedRNGProducesDistinctState guards the 2026-04-21 bug where the Go
// seedRNG passed fnvOffsetBasis by value to all four fnv1a calls, producing
// z == jsr and w == jcong. The C++ reference threads a mutable fnv_hash
// through successive calls, so all four KISS99 state words must be distinct.
func TestSeedRNGProducesDistinctState(t *testing.T) {
	got := seedRNG(0x0102030405060708)
	if got.z == got.jsr {
		t.Errorf("seedRNG: z == jsr (%#x) — fnv_hash not being threaded", got.z)
	}
	if got.w == got.jcong {
		t.Errorf("seedRNG: w == jcong (%#x) — fnv_hash not being threaded", got.w)
	}
	if got.z == 0 || got.w == 0 || got.jsr == 0 || got.jcong == 0 {
		t.Errorf("seedRNG: state has zero word: %+v", got)
	}
}

// TestSeedRNGMatchesReference computes the expected KISS99 initial state for
// prog_seed = 0 per the ifdefelse/ProgPOW getKern formula:
//   fnv_hash = 0x811c9dc5, threaded through four fnv1a calls with seed0=seed1=0.
// This pins the exact state sequence. If this test fails, seedRNG diverges
// from the reference and every hash the chain produces is incompatible.
func TestSeedRNGMatchesReference(t *testing.T) {
	// Compute expected by running the exact C++ pattern with Go's fnv1a.
	// fnv1a(0x811c9dc5, 0) = (0x811c9dc5 ^ 0) * 0x1000193 = 0x811c9dc5 * 0x1000193
	// Let the test compute the reference values directly so it stays a pure
	// specification check rather than a hand-transcribed constant.
	fnvHash := uint32(0x811c9dc5)
	wantZ := fnv1a(fnvHash, 0)
	wantW := fnv1a(wantZ, 0)
	wantJsr := fnv1a(wantW, 0)
	wantJcong := fnv1a(wantJsr, 0)

	got := seedRNG(0)
	if got.z != wantZ || got.w != wantW || got.jsr != wantJsr || got.jcong != wantJcong {
		t.Errorf("seedRNG(0): got {z:%#x w:%#x jsr:%#x jcong:%#x}, want {z:%#x w:%#x jsr:%#x jcong:%#x}",
			got.z, got.w, got.jsr, got.jcong, wantZ, wantW, wantJsr, wantJcong)
	}
}

// TestFillMixMatchesReference pins fillMix against the EIP-1057 fill_mix:
//
//   uint32_t fnv_hash = FNV_OFFSET_BASIS;
//   st.z     = fnv1a(fnv_hash, seed);
//   st.w     = fnv1a(fnv_hash, seed >> 32);
//   st.jsr   = fnv1a(fnv_hash, lane_id);
//   st.jcong = fnv1a(fnv_hash, lane_id);
//   for (int i = 0; i < PROGPOW_REGS; i++) mix[i] = kiss99(st);
//
// fnv_hash is mutated by each fnv1a call (pass-by-reference in C++). If Go
// passes fnvOffsetBasis by value to all four calls, jsr==z and jcong==w and
// every per-lane register is wrong. The test recomputes the expected KISS99
// seeds by threading fnv_hash, generates REGS words, and checks them all.
func TestFillMixMatchesReference(t *testing.T) {
	var hashSeed uint64 = 0x0102030405060708
	var laneID uint32 = 3

	fh := fnvOffsetBasis
	fh = fnv1a(fh, uint32(hashSeed))
	z := fh
	fh = fnv1a(fh, uint32(hashSeed>>32))
	w := fh
	fh = fnv1a(fh, laneID)
	jsr := fh
	fh = fnv1a(fh, laneID)
	jcong := fh
	ref := kiss99State{z: z, w: w, jsr: jsr, jcong: jcong}
	var want [progpowRegs]uint32
	for i := range want {
		want[i] = ref.next()
	}

	got := fillMix(hashSeed, laneID)
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("fillMix mix[%d] = %#x, want %#x", i, got[i], want[i])
		}
	}
}
