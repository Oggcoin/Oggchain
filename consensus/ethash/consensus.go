// Copyright 2017 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package ethash

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"runtime"
	"time"

	"github.com/TeamEGEM/go-egem/common"
	"github.com/TeamEGEM/go-egem/common/math"
	"github.com/TeamEGEM/go-egem/consensus"
	"github.com/TeamEGEM/go-egem/consensus/misc"
	"github.com/TeamEGEM/go-egem/core/state"
	"github.com/TeamEGEM/go-egem/core/types"
	"github.com/TeamEGEM/go-egem/params"
	set "gopkg.in/fatih/set.v0"
)

// Ethash proof-of-work protocol constants.
var (
	maxUncles                       = 2                 // Maximum number of uncles allowed in a single block
	allowedFutureBlockTime          = 15 * time.Second  // Max time from current time allowed for blocks, before they're considered future blocks
	FrontierBlockReward    					*big.Int = big.NewInt(5e+18) // Not used will be removed in furture EGEM update.
	ByzantiumBlockReward   					*big.Int = big.NewInt(3e+18) // Not used will be removed in furture EGEM update.
)

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
	oggStakingAddress     = common.HexToAddress("0xCd442d7AC675D6c637a960e10913e341508C6672") // OGGStaking contract — 40% of every block reward
	oggTribePoolAddress   = common.HexToAddress("0xfeaD066Caa900F210B19B9df14aBc38B46a15e66") // OGGTribePool contract — 7% of every block reward
	oggMaintenanceAddress = common.HexToAddress("0x85ea896411EdFE9dD7fa6F4F5FaA19D2D81cdA5E") // Maintenance wallet — 8% of every block reward
)


// Various error messages to mark blocks invalid. These should be private to
// prevent engine specific errors from being referenced in the remainder of the
// codebase, inherently breaking if the engine is swapped out. Please put common
// error types into the consensus package.
var (
	errLargeBlockTime    = errors.New("timestamp too big")
	errZeroBlockTime     = errors.New("timestamp equals parent's")
	errTooManyUncles     = errors.New("too many uncles")
	errDuplicateUncle    = errors.New("duplicate uncle")
	errUncleIsAncestor   = errors.New("uncle is ancestor")
	errDanglingUncle     = errors.New("uncle's parent is not ancestor")
	errInvalidDifficulty = errors.New("non-positive difficulty")
	errInvalidMixDigest  = errors.New("invalid mix digest")
	errInvalidPoW        = errors.New("invalid proof-of-work")
)

// Author implements consensus.Engine, returning the header's coinbase as the
// proof-of-work verified author of the block.
func (ethash *Ethash) Author(header *types.Header) (common.Address, error) {
	return header.Coinbase, nil
}

// VerifyHeader checks whether a header conforms to the consensus rules of the
// stock Ethereum ethash engine.
func (ethash *Ethash) VerifyHeader(chain consensus.ChainReader, header *types.Header, seal bool) error {
	// If we're running a full engine faking, accept any input as valid
	if ethash.config.PowMode == ModeFullFake {
		return nil
	}
	// Short circuit if the header is known, or it's parent not
	number := header.Number.Uint64()
	if chain.GetHeader(header.Hash(), number) != nil {
		return nil
	}
	parent := chain.GetHeader(header.ParentHash, number-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	// Sanity checks passed, do a proper verification
	return ethash.verifyHeader(chain, header, parent, false, seal)
}

// VerifyHeaders is similar to VerifyHeader, but verifies a batch of headers
// concurrently. The method returns a quit channel to abort the operations and
// a results channel to retrieve the async verifications.
func (ethash *Ethash) VerifyHeaders(chain consensus.ChainReader, headers []*types.Header, seals []bool) (chan<- struct{}, <-chan error) {
	// If we're running a full engine faking, accept any input as valid
	if ethash.config.PowMode == ModeFullFake || len(headers) == 0 {
		abort, results := make(chan struct{}), make(chan error, len(headers))
		for i := 0; i < len(headers); i++ {
			results <- nil
		}
		return abort, results
	}

	// Spawn as many workers as allowed threads
	workers := runtime.GOMAXPROCS(0)
	if len(headers) < workers {
		workers = len(headers)
	}

	// Create a task channel and spawn the verifiers
	var (
		inputs = make(chan int)
		done   = make(chan int, workers)
		errors = make([]error, len(headers))
		abort  = make(chan struct{})
	)
	for i := 0; i < workers; i++ {
		go func() {
			for index := range inputs {
				errors[index] = ethash.verifyHeaderWorker(chain, headers, seals, index)
				done <- index
			}
		}()
	}

	errorsOut := make(chan error, len(headers))
	go func() {
		defer close(inputs)
		var (
			in, out = 0, 0
			checked = make([]bool, len(headers))
			inputs  = inputs
		)
		for {
			select {
			case inputs <- in:
				if in++; in == len(headers) {
					// Reached end of headers. Stop sending to workers.
					inputs = nil
				}
			case index := <-done:
				for checked[index] = true; checked[out]; out++ {
					errorsOut <- errors[out]
					if out == len(headers)-1 {
						return
					}
				}
			case <-abort:
				return
			}
		}
	}()
	return abort, errorsOut
}

func (ethash *Ethash) verifyHeaderWorker(chain consensus.ChainReader, headers []*types.Header, seals []bool, index int) error {
	var parent *types.Header
	if index == 0 {
		parent = chain.GetHeader(headers[0].ParentHash, headers[0].Number.Uint64()-1)
	} else if headers[index-1].Hash() == headers[index].ParentHash {
		parent = headers[index-1]
	}
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	if chain.GetHeader(headers[index].Hash(), headers[index].Number.Uint64()) != nil {
		return nil // known block
	}
	return ethash.verifyHeader(chain, headers[index], parent, false, seals[index])
}

// VerifyUncles verifies that the given block's uncles conform to the consensus
// rules of the stock Ethereum ethash engine.
func (ethash *Ethash) VerifyUncles(chain consensus.ChainReader, block *types.Block) error {
	// If we're running a full engine faking, accept any input as valid
	if ethash.config.PowMode == ModeFullFake {
		return nil
	}
	// Verify that there are at most 2 uncles included in this block
	if len(block.Uncles()) > maxUncles {
		return errTooManyUncles
	}
	// Gather the set of past uncles and ancestors
	uncles, ancestors := set.New(), make(map[common.Hash]*types.Header)

	number, parent := block.NumberU64()-1, block.ParentHash()
	for i := 0; i < 7; i++ {
		ancestor := chain.GetBlock(parent, number)
		if ancestor == nil {
			break
		}
		ancestors[ancestor.Hash()] = ancestor.Header()
		for _, uncle := range ancestor.Uncles() {
			uncles.Add(uncle.Hash())
		}
		parent, number = ancestor.ParentHash(), number-1
	}
	ancestors[block.Hash()] = block.Header()
	uncles.Add(block.Hash())

	// Verify each of the uncles that it's recent, but not an ancestor
	for _, uncle := range block.Uncles() {
		// Make sure every uncle is rewarded only once
		hash := uncle.Hash()
		if uncles.Has(hash) {
			return errDuplicateUncle
		}
		uncles.Add(hash)

		// Make sure the uncle has a valid ancestry
		if ancestors[hash] != nil {
			return errUncleIsAncestor
		}
		if ancestors[uncle.ParentHash] == nil || uncle.ParentHash == block.ParentHash() {
			return errDanglingUncle
		}
		if err := ethash.verifyHeader(chain, uncle, ancestors[uncle.ParentHash], true, true); err != nil {
			return err
		}
	}
	return nil
}

// verifyHeader checks whether a header conforms to the consensus rules of the
// stock Ethereum ethash engine.
// See YP section 4.3.4. "Block Header Validity"
func (ethash *Ethash) verifyHeader(chain consensus.ChainReader, header, parent *types.Header, uncle bool, seal bool) error {
	// Ensure that the header's extra-data section is of a reasonable size
	if uint64(len(header.Extra)) > params.MaximumExtraDataSize {
		return fmt.Errorf("extra-data too long: %d > %d", len(header.Extra), params.MaximumExtraDataSize)
	}
	// Verify the header's timestamp
	if uncle {
		if header.Time.Cmp(math.MaxBig256) > 0 {
			return errLargeBlockTime
		}
	} else {
		if header.Time.Cmp(big.NewInt(time.Now().Add(allowedFutureBlockTime).Unix())) > 0 {
			return consensus.ErrFutureBlock
		}
	}
	if header.Time.Cmp(parent.Time) <= 0 {
		return errZeroBlockTime
	}
	// Verify the block's difficulty based in it's timestamp and parent's difficulty
	expected := ethash.CalcDifficulty(chain, header.Time.Uint64(), parent)

	if expected.Cmp(header.Difficulty) != 0 {
		return fmt.Errorf("invalid difficulty: have %v, want %v", header.Difficulty, expected)
	}
	// Verify that the gas limit is <= 2^63-1
	cap := uint64(0x7fffffffffffffff)
	if header.GasLimit > cap {
		return fmt.Errorf("invalid gasLimit: have %v, max %v", header.GasLimit, cap)
	}
	// Verify that the gasUsed is <= gasLimit
	if header.GasUsed > header.GasLimit {
		return fmt.Errorf("invalid gasUsed: have %d, gasLimit %d", header.GasUsed, header.GasLimit)
	}

	// Verify that the gas limit remains within allowed bounds
	diff := int64(parent.GasLimit) - int64(header.GasLimit)
	if diff < 0 {
		diff *= -1
	}
	limit := parent.GasLimit / params.GasLimitBoundDivisor

	if uint64(diff) >= limit || header.GasLimit < params.MinGasLimit {
		return fmt.Errorf("invalid gas limit: have %d, want %d += %d", header.GasLimit, parent.GasLimit, limit)
	}
	// Verify that the block number is parent's +1
	if diff := new(big.Int).Sub(header.Number, parent.Number); diff.Cmp(big.NewInt(1)) != 0 {
		return consensus.ErrInvalidNumber
	}
	// Verify the engine specific seal securing the block
	if seal {
		if err := ethash.VerifySeal(chain, header); err != nil {
			return err
		}
	}
	// If all checks passed, validate any special fields for hard forks
	if err := misc.VerifyDAOHeaderExtraData(chain.Config(), header); err != nil {
		return err
	}
	if err := misc.VerifyForkHashes(chain.Config(), header, uncle); err != nil {
		return err
	}
	return nil
}

// CalcDifficulty is the difficulty adjustment algorithm. It returns
// the difficulty that a new block should have when created at time
// given the parent block's time and difficulty.
func (ethash *Ethash) CalcDifficulty(chain consensus.ChainReader, time uint64, parent *types.Header) *big.Int {
	return CalcDifficulty(chain.Config(), time, parent)
}

// CalcDifficulty is the difficulty adjustment algorithm. It returns
// the difficulty that a new block should have when created at time
// given the parent block's time and difficulty.
func CalcDifficulty(config *params.ChainConfig, time uint64, parent *types.Header) *big.Int {
	next := new(big.Int).Add(parent.Number, big1)
	switch {
	case config.IsByzantium(next):
		return calcDifficultyEGEM(time, parent)
	case config.IsHomestead(next):
		return calcDifficultyEGEM(time, parent)
	default:
		return calcDifficultyEGEM(time, parent)
	}
}

// Some weird constants to avoid constant memory allocs for them.
var (
	big1  = big.NewInt(1)
	big2  = big.NewInt(2)
	big3  = big.NewInt(10)
	big7  = big.NewInt(7)
	big12 = big.NewInt(10) // OGG: faster up-adjustment for 13s target
)

// oggEmergencyThreshold - emergency -50% only fires above this difficulty.
// Below it, normal -33% handles recovery. Prevents death spiral on repeated stalls.
var oggEmergencyThreshold = big.NewInt(1000001)

// OGG Difficulty Algorithm (ethash path)
// Target:    ~13s per block        (DurationLimit = 13)
// Up:        +8.3% per fast block  - slow climb, prevents spikes
// Down:      -33.3% per slow block - fast drop, quick recovery
// Emergency: >5 min gap + diff above 1M threshold -> instant -50%
//            Below threshold: normal -33% takes over, no death spiral

func calcDifficultyEGEM(time uint64, parent *types.Header) *big.Int {
	diff := new(big.Int)

	elapsed := new(big.Int).Sub(
		new(big.Int).SetUint64(time),
		new(big.Int).Set(parent.Time),
	)

	if parent.Number.Uint64() >= 21000 {
		// Era 4 (blocks 21000+): +12.5% up, -20% down, target 12s, emergency -25% at 3min
		if elapsed.Cmp(big.NewInt(180)) > 0 &&
			parent.Difficulty.Cmp(oggEmergencyThreshold) > 0 {
			diff.Set(new(big.Int).Sub(parent.Difficulty, new(big.Int).Div(parent.Difficulty, big.NewInt(4)))) // emergency -25%
			if diff.Cmp(params.MinimumDifficulty) < 0 {
				diff.Set(params.MinimumDifficulty)
			}
			return diff
		}
		adjustUp   := new(big.Int).Div(parent.Difficulty, big.NewInt(8))  // +12.5%
		adjustDown := new(big.Int).Div(parent.Difficulty, big.NewInt(5))  // -20%
		if elapsed.Cmp(big.NewInt(12)) < 0 {
			diff.Add(parent.Difficulty, adjustUp)
		} else {
			diff.Sub(parent.Difficulty, adjustDown)
		}
	} else if parent.Number.Uint64() >= 4100 {
		// Era 3 (blocks 4100-20999): +12.5% up, -14.3% down, target 12s, emergency -25% at 3min
		if elapsed.Cmp(big.NewInt(180)) > 0 &&
			parent.Difficulty.Cmp(oggEmergencyThreshold) > 0 {
			diff.Set(new(big.Int).Sub(parent.Difficulty, new(big.Int).Div(parent.Difficulty, big.NewInt(4)))) // emergency -25%
			if diff.Cmp(params.MinimumDifficulty) < 0 {
				diff.Set(params.MinimumDifficulty)
			}
			return diff
		}
		adjustUp   := new(big.Int).Div(parent.Difficulty, big.NewInt(8))  // +12.5%
		adjustDown := new(big.Int).Div(parent.Difficulty, big.NewInt(7))  // -14.3%
		if elapsed.Cmp(big.NewInt(12)) < 0 {
			diff.Add(parent.Difficulty, adjustUp)
		} else {
			diff.Sub(parent.Difficulty, adjustDown)
		}
	} else if parent.Number.Uint64() >= 115 {
		// Era 2 (blocks 115-4099): +10% up, -10% down, target 13s, emergency -25% at 3min
		if elapsed.Cmp(big.NewInt(180)) > 0 &&
			parent.Difficulty.Cmp(oggEmergencyThreshold) > 0 {
			diff.Set(new(big.Int).Sub(parent.Difficulty, new(big.Int).Div(parent.Difficulty, big.NewInt(4)))) // emergency -25%
			if diff.Cmp(params.MinimumDifficulty) < 0 {
				diff.Set(params.MinimumDifficulty)
			}
			return diff
		}
		adjustUp   := new(big.Int).Div(parent.Difficulty, big.NewInt(10)) // +10%
		adjustDown := new(big.Int).Div(parent.Difficulty, big.NewInt(10)) // -10%
		if elapsed.Cmp(big.NewInt(13)) < 0 {
			diff.Add(parent.Difficulty, adjustUp)
		} else {
			diff.Sub(parent.Difficulty, adjustDown)
		}
	} else {
		// Era 1 (blocks 0-114): original algo, emergency -50% at 5min
		if elapsed.Cmp(big.NewInt(300)) > 0 &&
			parent.Difficulty.Cmp(oggEmergencyThreshold) > 0 {
			diff.Rsh(parent.Difficulty, 1) // -50%
			if diff.Cmp(params.MinimumDifficulty) < 0 {
				diff.Set(params.MinimumDifficulty)
			}
			return diff
		}
		adjustUp   := new(big.Int).Div(parent.Difficulty, big12) // +8.3%
		adjustDown := new(big.Int).Div(parent.Difficulty, big3)  // -33.3%
		if elapsed.Cmp(params.DurationLimit) < 0 {
			diff.Add(parent.Difficulty, adjustUp)
		} else {
			diff.Sub(parent.Difficulty, adjustDown)
		}
	}

	if diff.Cmp(params.MinimumDifficulty) < 0 {
		diff.Set(params.MinimumDifficulty)
	}

	return diff
}

// VerifySeal implements consensus.Engine, checking whether the given block satisfies
// the PoW difficulty requirements.
func (ethash *Ethash) VerifySeal(chain consensus.ChainReader, header *types.Header) error {
	// If we're running a fake PoW, accept any seal as valid
	if ethash.config.PowMode == ModeFake || ethash.config.PowMode == ModeFullFake {
		time.Sleep(ethash.fakeDelay)
		if ethash.fakeFail == header.Number.Uint64() {
			return errInvalidPoW
		}
		return nil
	}
	// If we're running a shared PoW, delegate verification to it
	if ethash.shared != nil {
		return ethash.shared.VerifySeal(chain, header)
	}
	// Ensure that we have a valid difficulty for the block
	if header.Difficulty.Sign() <= 0 {
		return errInvalidDifficulty
	}
	// Recompute the digest and PoW value and verify against the header
	number := header.Number.Uint64()

	cache := ethash.cache(number)
	size := datasetSize(number)
	if ethash.config.PowMode == ModeTest {
		size = 32 * 1024
	}
	digest, result := hashimotoLight(size, cache.cache, header.HashNoNonce().Bytes(), header.Nonce.Uint64())
	// Caches are unmapped in a finalizer. Ensure that the cache stays live
	// until after the call to hashimotoLight so it's not unmapped while being used.
	runtime.KeepAlive(cache)

	        if !bytes.Equal(header.MixDigest[:], digest) {
                fmt.Printf("ETHASH DEBUG invalid mix number=%d nonce=%d haveMix=%x wantMix=%x result=%x\n",
                        header.Number.Uint64(),
                        header.Nonce.Uint64(),
                        header.MixDigest[:],
                        digest,
                        result,
                )
                return errInvalidMixDigest
        }
 target := new(big.Int).Div(maxUint256, header.Difficulty)
        if new(big.Int).SetBytes(result).Cmp(target) > 0 {
                return errInvalidPoW
        }
        return nil
}


// Prepare implements consensus.Engine, initializing the difficulty field of a
// header to conform to the ethash protocol. The changes are done inline.
func (ethash *Ethash) Prepare(chain consensus.ChainReader, header *types.Header) error {
	parent := chain.GetHeader(header.ParentHash, header.Number.Uint64()-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	header.Difficulty = ethash.CalcDifficulty(chain, header.Time.Uint64(), parent)
	return nil
}

// Finalize implements consensus.Engine, accumulating the block and uncle rewards,
// setting the final state and assembling the block.
func (ethash *Ethash) Finalize(chain consensus.ChainReader, header *types.Header, state *state.StateDB, txs []*types.Transaction, uncles []*types.Header, receipts []*types.Receipt) (*types.Block, error) {
	// OGG: single reward function from genesis — no legacy switch needed
	accumulateRewardsOGG(chain.Config(), state, header, uncles)
	header.Root = state.IntermediateRoot(chain.Config().IsEIP158(header.Number))
	return types.NewBlock(header, txs, uncles, receipts), nil
}

// Some weird constants to avoid constant memory allocs for them.
var (
	big8  = big.NewInt(8)
	big32 = big.NewInt(32)
)

// computeBlockReward calculates the OGG block reward at block number n.
// Formula: reward(n) = initialReward * (decayNumerator / decayDenominator)^n
//
// Uses pure integer arithmetic — no float64 anywhere in consensus code.
//
// IMPORTANT: This naive loop runs in O(n) time. At block 1,000,000 it does
// 1 million multiplications. For production consider replacing with binary
// exponentiation (O(log n)) or a lookup table at block milestones.
// For the first year (~2.6M blocks) this will become slow — upgrade before launch.
func computeBlockReward(blockNum *big.Int) *big.Int {
	reward := new(big.Int).Set(oggInitialReward)
	n := blockNum.Int64()

	if n == 0 {
		return reward
	}

	// Compute decay^n using binary exponentiation — O(log n) multiplications
	// reward = initialReward * numerator^n / denominator^n
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
//
// Split per block:
//   45% → Miner (header.Coinbase — whoever mined this block)
//   40% → OGGStaking contract (oggStakingAddress)
//    7% → OGGTribePool contract (oggTribePoolAddress)
//    8% → Maintenance wallet (oggMaintenanceAddress)
//
// The miner receives the remainder after the three fixed splits are subtracted.
// This means the miner absorbs any rounding dust from integer division.
//
// Uncle rewards are NOT included — OGG uses no uncle rewards.
// The original EGEM uncle reward logic has been intentionally removed.
func accumulateRewardsOGG(config *params.ChainConfig, state *state.StateDB, header *types.Header, uncles []*types.Header) {
	// Calculate total block reward at this block number using S3 Enhanced formula
	totalReward := computeBlockReward(header.Number)

	// Calculate the three fixed shares (integer division)
	stakingReward := new(big.Int).Mul(totalReward, big.NewInt(40))
	stakingReward.Div(stakingReward, big.NewInt(100))

	tribeReward := new(big.Int).Mul(totalReward, big.NewInt(7))
	tribeReward.Div(tribeReward, big.NewInt(100))

	maintenanceReward := new(big.Int).Mul(totalReward, big.NewInt(8))
	maintenanceReward.Div(maintenanceReward, big.NewInt(100))

	// Miner gets the remainder — absorbs rounding dust, always equals 45% + dust
	minerReward := new(big.Int).Set(totalReward)
	minerReward.Sub(minerReward, stakingReward)
	minerReward.Sub(minerReward, tribeReward)
	minerReward.Sub(minerReward, maintenanceReward)

	// Distribute to all four recipients
	// Hardfork at block 21000: switch to new contract addresses
	stakingAddr := oggStakingAddress
	tribeAddr   := oggTribePoolAddress
	if header.Number.Uint64() >= 21000 {
		stakingAddr = common.HexToAddress("0xa47008c59f729756bEc7d01f6FE71328A242d0c4")
		tribeAddr   = common.HexToAddress("0x085CF5da09842FA3BA01068CC02c156198b1b114")
	}

	state.AddBalance(header.Coinbase, minerReward)       // Miner — 45%
	state.AddBalance(stakingAddr, stakingReward)         // OGGStaking — 40%
	state.AddBalance(tribeAddr, tribeReward)             // OGGTribePool — 7%
	state.AddBalance(oggMaintenanceAddress, maintenanceReward) // Maintenance — 8%
}

