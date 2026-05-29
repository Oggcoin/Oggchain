// Package progpow implements the ProgPoW proof-of-work consensus engine.
// ProgPoW is a drop-in replacement for Ethash, specified in EIP-1057.
// It uses the same DAG structure and difficulty formula but replaces the inner
// hash loop with a randomly-generated GPU-friendly program each period.
package progpow

import (
	crand "crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/TeamEGEM/go-egem/common"
	"github.com/TeamEGEM/go-egem/consensus"
	"github.com/TeamEGEM/go-egem/consensus/ethash"
	"github.com/TeamEGEM/go-egem/core/state"
	"github.com/TeamEGEM/go-egem/core/types"
	"github.com/TeamEGEM/go-egem/params"
	"github.com/TeamEGEM/go-egem/rpc"
)

var maxUint256 = new(big.Int).Exp(big.NewInt(2), big.NewInt(256), big.NewInt(0))

// Config holds ProgPoW engine configuration (mirrors ethash.Config).
type Config struct {
	CacheDir       string
	CachesInMem    int
	CachesOnDisk   int
	DatasetDir     string
	DatasetsInMem  int
	DatasetsOnDisk int
	PowMode        Mode
}

// Mode selects the mining behaviour.
type Mode uint

const (
	ModeNormal   Mode = iota
	ModeShared
	ModeTest
	ModeFake
	ModeFullFake
)

// ProgPoW is the consensus engine implementing ProgPoW proof-of-work.
// It satisfies both consensus.Engine and consensus.PoW.
type ProgPoW struct {
	config  Config
	rand    *rand.Rand
	threads int
	update  chan struct{}
	hashrate int64 // atomic, hashes attempted
	lock    sync.Mutex

	// Cached Ethash dataset (ProgPoW shares the Ethash DAG per EIP-1057).
	// dagAnchor retains a reference to the ethash dataset struct so its GC
	// finalizer (which unmaps the mmap'd memory) does not run while dagSlice
	// is still in use by hashimoto goroutines.
	dagEpoch  uint64
	dagAnchor interface{}
	dagSlice  []uint32
	dagLock   sync.Mutex
}

// New creates a ProgPoW engine with the given config.
func New(config Config) *ProgPoW {
	p := &ProgPoW{
		config: config,
		update: make(chan struct{}),
	}
	if p.config.CachesInMem <= 0 {
		p.config.CachesInMem = 2
	}
	if p.config.DatasetsInMem <= 0 {
		p.config.DatasetsInMem = 1
	}
	return p
}

// NewFaker returns a ProgPoW engine that accepts all blocks instantly.
func NewFaker() *ProgPoW { return &ProgPoW{config: Config{PowMode: ModeFake}, update: make(chan struct{})} }

// NewFullFaker returns a ProgPoW engine that skips all consensus checks.
func NewFullFaker() *ProgPoW {
	return &ProgPoW{config: Config{PowMode: ModeFullFake}, update: make(chan struct{})}
}

// NewTester returns a ProgPoW engine with a tiny DAG for unit tests.
func NewTester() *ProgPoW {
	return New(Config{PowMode: ModeTest, CachesInMem: 1, DatasetsInMem: 1})
}

// SetThreads sets the number of mining threads. 0 = use all CPUs. -1 = disable.
func (p *ProgPoW) SetThreads(threads int) {
	p.lock.Lock()
	p.threads = threads
	p.lock.Unlock()
	select {
	case p.update <- struct{}{}:
	default:
	}
}

// ----------------------------------------------------------------------------
// consensus.Engine interface
// ----------------------------------------------------------------------------

func (p *ProgPoW) Author(header *types.Header) (common.Address, error) {
	return header.Coinbase, nil
}

func (p *ProgPoW) VerifyHeader(chain consensus.ChainReader, header *types.Header, seal bool) error {
	if p.config.PowMode == ModeFullFake {
		return nil
	}
	number := header.Number.Uint64()
	parent := chain.GetHeader(header.ParentHash, number-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	return p.verifyHeader(chain, header, parent, seal)
}

func (p *ProgPoW) VerifyHeaders(chain consensus.ChainReader, headers []*types.Header, seals []bool) (chan<- struct{}, <-chan error) {
	abort := make(chan struct{})
	results := make(chan error, len(headers))
	go func() {
		for i, header := range headers {
			var parent *types.Header
			if i == 0 {
				parent = chain.GetHeader(headers[0].ParentHash, headers[0].Number.Uint64()-1)
			} else {
				parent = headers[i-1]
			}
			var err error
			if p.config.PowMode == ModeFullFake {
				err = nil
			} else if parent == nil {
				err = consensus.ErrUnknownAncestor
			} else {
				err = p.verifyHeader(chain, header, parent, seals[i])
			}
			select {
			case <-abort:
				return
			case results <- err:
			}
		}
	}()
	return abort, results
}

func (p *ProgPoW) VerifyUncles(chain consensus.ChainReader, block *types.Block) error {
	if p.config.PowMode == ModeFullFake {
		return nil
	}
	if len(block.Uncles()) > 2 {
		return errors.New("too many uncles")
	}
	seen := make(map[common.Hash]struct{})
	for _, uncle := range block.Uncles() {
		if _, ok := seen[uncle.Hash()]; ok {
			return errors.New("duplicate uncle")
		}
		seen[uncle.Hash()] = struct{}{}
	}
	return nil
}

func (p *ProgPoW) VerifySeal(chain consensus.ChainReader, header *types.Header) error {
	return p.verifySeal(chain, header)
}

func (p *ProgPoW) Prepare(chain consensus.ChainReader, header *types.Header) error {
	parent := chain.GetHeader(header.ParentHash, header.Number.Uint64()-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	header.Difficulty = p.CalcDifficulty(chain, header.Time.Uint64(), parent)
	return nil
}

func (p *ProgPoW) Finalize(chain consensus.ChainReader, header *types.Header, state *state.StateDB, txs []*types.Transaction, uncles []*types.Header, receipts []*types.Receipt) (*types.Block, error) {
	// OGG: use our 4-way split reward function
	accumulateRewardsOGG(chain.Config(), state, header, uncles)
	header.Root = state.IntermediateRoot(chain.Config().IsEIP158(header.Number))
	return types.NewBlock(header, txs, uncles, receipts), nil
}

// CalcDifficulty satisfies consensus.Engine. Delegates to the package-level function.
func (p *ProgPoW) CalcDifficulty(chain consensus.ChainReader, time uint64, parent *types.Header) *big.Int {
	return CalcDifficulty(chain.Config(), time, parent)
}

// OGG ProgPoW Difficulty Algorithm
// Target:    ~13s per block
// Up:        +8.3% per fast block  (< 13s) - slow climb, prevents spikes
// Down:      -33.3% per slow block (>= 13s) - fast drop, quick recovery
// Emergency: >5 min gap + diff above 1M threshold -> instant -50%
//            Below threshold: normal -33% takes over, no death spiral

var progpowEmergencyThreshold = big.NewInt(1000001)

func CalcDifficulty(config *params.ChainConfig, time uint64, parent *types.Header) *big.Int {
	diff        := new(big.Int)
	bigTime      := new(big.Int).SetUint64(time)
	bigParentTime := new(big.Int).Set(parent.Time)
	elapsed      := new(big.Int).Sub(bigTime, bigParentTime)

	// Emergency: block gap > 5 minutes AND difficulty high enough
	if elapsed.Cmp(big.NewInt(300)) > 0 &&
		parent.Difficulty.Cmp(progpowEmergencyThreshold) > 0 {
		diff.Rsh(parent.Difficulty, 1)
		if diff.Cmp(params.MinimumDifficulty) < 0 {
			diff.Set(params.MinimumDifficulty)
		}
		return diff
	}

	adjustUp   := new(big.Int).Div(parent.Difficulty, big.NewInt(12)) // +8.3%
	adjustDown := new(big.Int).Div(parent.Difficulty, big.NewInt(3))  // -33.3%

	if elapsed.Cmp(big.NewInt(13)) < 0 {
		diff.Add(parent.Difficulty, adjustUp)
	} else {
		diff.Sub(parent.Difficulty, adjustDown)
	}

	if diff.Cmp(params.MinimumDifficulty) < 0 {
		diff.Set(params.MinimumDifficulty)
	}
	return diff
}

// Seal implements consensus.Engine — searches for a valid nonce.
func (p *ProgPoW) Seal(chain consensus.ChainReader, block *types.Block, stop <-chan struct{}) (*types.Block, error) {
	if p.config.PowMode == ModeFake || p.config.PowMode == ModeFullFake {
		header := block.Header()
		header.Nonce, header.MixDigest = types.BlockNonce{}, common.Hash{}
		return block.WithSeal(header), nil
	}

	abort := make(chan struct{})
	found := make(chan *types.Block)

	p.lock.Lock()
	threads := p.threads
	if p.rand == nil {
		seed, err := crand.Int(crand.Reader, big.NewInt(math.MaxInt64))
		if err != nil {
			p.lock.Unlock()
			return nil, err
		}
		p.rand = rand.New(rand.NewSource(seed.Int64()))
	}
	p.lock.Unlock()

	if threads == 0 {
		threads = runtime.NumCPU()
	}
	if threads < 0 {
		threads = 0
	}

	var wg sync.WaitGroup
	for i := 0; i < threads; i++ {
		wg.Add(1)
		go func(id int, startNonce uint64) {
			defer wg.Done()
			p.mine(block, startNonce, abort, found)
		}(i, uint64(p.rand.Int63()))
	}

	var result *types.Block
	select {
	case <-stop:
		close(abort)
	case result = <-found:
		close(abort)
	case <-p.update:
		close(abort)
		wg.Wait()
		return p.Seal(chain, block, stop)
	}
	wg.Wait()
	return result, nil
}

// Hashrate satisfies consensus.PoW.
func (p *ProgPoW) Hashrate() float64 {
	return float64(atomic.LoadInt64(&p.hashrate))
}

// SeedHash implements consensus.SeedHasher. For ProgPoW, the per-nonce seed is
// derived inside the kernel from keccakF800Short(headerHash, nonce), so there
// is no meaningful epoch-based seed. Return the Ethash seed for compatibility
// with mining pools that expect a seed hash field in GetWork responses.
func (p *ProgPoW) SeedHash(blockNumber uint64) []byte {
	return ethash.SeedHash(blockNumber)
}

// APIs returns the RPC APIs exposed by the ProgPoW engine.
func (p *ProgPoW) APIs(chain consensus.ChainReader) []rpc.API {
	return []rpc.API{{
		Namespace: "eth",
		Version:   "1.0",
		Service:   &API{p},
		Public:    true,
	}}
}

// ----------------------------------------------------------------------------
// Internal helpers
// ----------------------------------------------------------------------------

func (p *ProgPoW) verifyHeader(chain consensus.ChainReader, header, parent *types.Header, seal bool) error {
	// Timestamp must increase.
	if header.Time.Cmp(parent.Time) <= 0 {
		return errors.New("timestamp equals parent's")
	}
	// Difficulty must match.
	expected := p.CalcDifficulty(chain, header.Time.Uint64(), parent)
	if expected.Cmp(header.Difficulty) != 0 {
		return fmt.Errorf("invalid difficulty: have %v, want %v", header.Difficulty, expected)
	}
	// Gas limit must be within bounds.
	cap := uint64(0x7fffffffffffffff)
	if header.GasLimit > cap {
		return fmt.Errorf("invalid gasLimit: have %v, max %v", header.GasLimit, cap)
	}
	if header.GasUsed > header.GasLimit {
		return fmt.Errorf("invalid gasUsed: have %d, gasLimit %d", header.GasUsed, header.GasLimit)
	}
	diff := int64(parent.GasLimit) - int64(header.GasLimit)
	if diff < 0 {
		diff *= -1
	}
	limit := parent.GasLimit / params.GasLimitBoundDivisor
	if uint64(diff) >= limit || header.GasLimit < params.MinGasLimit {
		return fmt.Errorf("invalid gas limit: have %d, want %d += %d", header.GasLimit, parent.GasLimit, limit)
	}
	// Block number must be parent + 1.
	if d := new(big.Int).Sub(header.Number, parent.Number); d.Cmp(big.NewInt(1)) != 0 {
		return consensus.ErrInvalidNumber
	}
	// Extra data must be ≤ 32 bytes.
	if uint64(len(header.Extra)) > 32 {
		return fmt.Errorf("extra-data too long: %d > 32", len(header.Extra))
	}
	if seal {
		return p.VerifySeal(chain, header)
	}
	return nil
}

func (p *ProgPoW) verifySeal(chain consensus.ChainReader, header *types.Header) error {
        if p.config.PowMode == ModeFake || p.config.PowMode == ModeFullFake {
                return nil
        }

        number := header.Number.Uint64()
        digest, result := p.hashimoto(header, number)

        if digest != header.MixDigest {
        fmt.Printf("PROGPOW DEBUG invalid mix number=%d period=%d nonce=%d haveMix=%v wantMix=%v result=%x\n",
                number,
                number/10,
                header.Nonce.Uint64(),
                header.MixDigest,
                digest,
                result,
        )

        return fmt.Errorf("invalid mix digest: have %v, want %v", header.MixDigest, digest)
}

        target := new(big.Int).Div(maxUint256, header.Difficulty)
if new(big.Int).SetBytes(result).Cmp(target) > 0 {
        fmt.Printf("PROGPOW DEBUG invalid pow number=%d nonce=%d result=%x target=%x difficulty=%s\n",
                number,
                header.Nonce.Uint64(),
                result,
                target.Bytes(),
                header.Difficulty.String(),
        )
        return errors.New("invalid proof-of-work")
}
return nil
}

// hashimoto computes the ProgPoW digest and result for a header.
func (p *ProgPoW) hashimoto(header *types.Header, blockNumber uint64) (common.Hash, []byte) {
	nonce := header.Nonce.Uint64()

	var hh [32]byte
	copy(hh[:], header.HashNoNonce().Bytes())

	period := blockNumber / progpowPeriod
        seed := keccakF800Short(hh, nonce)
        dag := p.getDAGSlice(blockNumber)
        dagSize := uint64(len(dag)) * 4
        cache := BuildL1Cache(dag)

	mixHash := progpowLoop(seed, dag, cache, dagSize, period)
	finalHash := keccakF800Long(hh, nonce, mixHash)

	// Mix digest: 8 × uint32 LE → 32-byte Hash.
	var digest common.Hash
	for i, w := range mixHash {
		binary.LittleEndian.PutUint32(digest[i*4:], w)
	}
	// Result: 8 × uint32 BE → []byte for difficulty comparison.
	result := make([]byte, 32)
	for i, w := range finalHash {
		binary.BigEndian.PutUint32(result[i*4:], w)
	}
	return digest, result
}

// epochLength mirrors ethash.epochLength (30000 blocks per epoch).
const epochLength = 30000

// getDAGSlice returns the Ethash dataset as []uint32 for ProgPoW to use.
// In normal/shared mode, the real DAG is generated (or loaded from disk) via
// ethash.DatasetSlice and cached per epoch. In test/fake modes, a small
// synthetic stub is returned for fast unit testing.
func (p *ProgPoW) getDAGSlice(blockNumber uint64) []uint32 {
	// Fast path: fake/test modes use a small synthetic DAG.
	if p.config.PowMode == ModeFake || p.config.PowMode == ModeFullFake {
		return p.stubDAG(progpowLanes * progpowDAGLoads * 256)
	}
	if p.config.PowMode == ModeTest {
		return p.stubDAG(progpowLanes * progpowDAGLoads * 256)
	}

	// Real mode: generate/load the Ethash dataset, cached per epoch.
	epoch := blockNumber / epochLength
	p.dagLock.Lock()
	defer p.dagLock.Unlock()

	if p.dagSlice != nil && p.dagEpoch == epoch {
		return p.dagSlice
	}
	p.dagAnchor, p.dagSlice = ethash.DatasetSlice(blockNumber, p.config.DatasetDir, false)
	p.dagEpoch = epoch
	return p.dagSlice
}

// stubDAG returns a small deterministic DAG for test/fake modes.
func (p *ProgPoW) stubDAG(size int) []uint32 {
	dag := make([]uint32, size)
	for i := range dag {
		dag[i] = uint32(i) * 0x9e3779b9
	}
	return dag
}

// mine searches for a valid nonce starting from startNonce.
func (p *ProgPoW) mine(block *types.Block, startNonce uint64, abort chan struct{}, found chan *types.Block) {
	header := block.Header()
	target := new(big.Int).Div(maxUint256, header.Difficulty)
	number := header.Number.Uint64()
	nonce := startNonce

	for {
		select {
		case <-abort:
			return
		default:
		}
		var bn types.BlockNonce
		binary.LittleEndian.PutUint64(bn[:], nonce)
		header.Nonce = bn

		atomic.AddInt64(&p.hashrate, 1)
		digest, result := p.hashimoto(header, number)
		if new(big.Int).SetBytes(result).Cmp(target) <= 0 {
			header.MixDigest = digest
			select {
			case found <- block.WithSeal(header):
			case <-abort:
			}
			return
		}
		nonce++
	}
}

// ----------------------------------------------------------------------------
// Block rewards (ProgPoW chain — 5 EGEM base; adjust in genesis config)
// ----------------------------------------------------------------------------

// OGG Chain — Reward Variables
// S3 Enhanced emission: 700 OGG start, decay 0.999999933042/block
// Split: 45% miner, 40% staking, 7% tribe pool, 8% maintenance
var (
	// Initial block reward: 700 OGG in wei (700 * 10^18)
	oggInitialReward, _   = new(big.Int).SetString("700000000000000000000", 10)

	// Decay factor: 0.999999933042 = 999999933042 / 1000000000000
	// Integer arithmetic only — no floats in consensus code
	oggDecayNumerator     = big.NewInt(999999933042)
	oggDecayDenominator   = big.NewInt(1000000000000)

	// OGG reward split addresses — hardcoded at chain launch, never change
	// Miner (45%) goes to header.Coinbase — dynamic, whoever mined the block
	oggStakingAddress     = common.HexToAddress("0xCd442d7AC675D6c637a960e10913e341508C6672") // OGGStaking contract — 40%
	oggTribePoolAddress   = common.HexToAddress("0xfeaD066Caa900F210B19B9df14aBc38B46a15e66") // OGGTribePool contract — 7%
	oggMaintenanceAddress = common.HexToAddress("0x85ea896411EdFE9dD7fa6F4F5FaA19D2D81cdA5E") // Maintenance wallet — 8%
)

// computeBlockReward calculates the OGG block reward at block number n.
// Formula: reward(n) = initialReward * (decayNumerator / decayDenominator)^n
// Uses binary exponentiation — O(log n), no floats.
func computeBlockReward(blockNum *big.Int) *big.Int {
	reward := new(big.Int).Set(oggInitialReward)
	n := blockNum.Int64()

	if n == 0 {
		return reward
	}

	numPow := new(big.Int).SetInt64(1)
	denPow := new(big.Int).SetInt64(1)
	base := n

	numBase := new(big.Int).Set(oggDecayNumerator)
	denBase := new(big.Int).Set(oggDecayDenominator)

	for base > 0 {
		if base%2 == 1 {
			numPow.Mul(numPow, numBase)
			denPow.Mul(denPow, denBase)
		}
		numBase.Mul(numBase, numBase)
		denBase.Mul(denBase, denBase)
		base /= 2
	}

	reward.Mul(reward, numPow)
	reward.Div(reward, denPow)
	return reward
}

// accumulateRewardsOGG distributes the OGG block reward to four recipients.
// Split: 45% miner, 40% staking, 7% tribe pool, 8% maintenance.
// Miner receives remainder after three fixed splits — absorbs rounding dust.
// No uncle rewards — OGG does not use uncle/ommer rewards.
func accumulateRewardsOGG(_ *params.ChainConfig, state *state.StateDB, header *types.Header, uncles []*types.Header) {
	totalReward := computeBlockReward(header.Number)

	stakingReward := new(big.Int).Mul(totalReward, big.NewInt(40))
	stakingReward.Div(stakingReward, big.NewInt(100))

	tribeReward := new(big.Int).Mul(totalReward, big.NewInt(7))
	tribeReward.Div(tribeReward, big.NewInt(100))

	maintenanceReward := new(big.Int).Mul(totalReward, big.NewInt(8))
	maintenanceReward.Div(maintenanceReward, big.NewInt(100))

	// Miner gets the remainder — always equals 45% plus any rounding dust
	minerReward := new(big.Int).Set(totalReward)
	minerReward.Sub(minerReward, stakingReward)
	minerReward.Sub(minerReward, tribeReward)
	minerReward.Sub(minerReward, maintenanceReward)

	state.AddBalance(header.Coinbase, minerReward)
	state.AddBalance(oggStakingAddress, stakingReward)
	state.AddBalance(oggTribePoolAddress, tribeReward)
	state.AddBalance(oggMaintenanceAddress, maintenanceReward)
}

// ----------------------------------------------------------------------------
// RPC API
// ----------------------------------------------------------------------------

// API exposes ProgPoW mining methods over JSON-RPC.
type API struct{ progpow *ProgPoW }

// GetWork returns a work package for external miners.
// [0] = header hash (without nonce), [1] = seed hash, [2] = target.
// NOTE: this returns work only when the ProgPoW engine has been given a pending
// block via the remote agent pathway (miner/remote_agent.go). The stubs below
// return empty until a full remote-mining pipeline is wired.
func (api *API) GetWork() ([3]string, error) {
	return [3]string{}, errors.New("progpow: remote mining not yet wired (use miner/remote_agent pathway)")
}

// SubmitWork attempts to submit a mined block.
func (api *API) SubmitWork(nonce types.BlockNonce, hash, mixDigest common.Hash) bool {
	return false
}

// SubmitHashrate reports the hashrate of an external miner.
func (api *API) SubmitHashrate(rate common.Hash, id common.Hash) bool {
	return true
}
