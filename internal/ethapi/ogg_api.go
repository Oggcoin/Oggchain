// ogg_api.go — Custom OGG RPC methods
package ethapi

import (
	"context"
	"math/big"

	"github.com/TeamEGEM/go-egem/common"
	"github.com/TeamEGEM/go-egem/common/hexutil"
	"github.com/TeamEGEM/go-egem/rpc"
)

type OGGBlockReward struct {
	BlockNumber        hexutil.Uint64 `json:"blockNumber"`
	TotalReward        *hexutil.Big   `json:"totalReward"`
	MinerReward        *hexutil.Big   `json:"minerReward"`
	MinerAddress       common.Address `json:"minerAddress"`
	StakingReward      *hexutil.Big   `json:"stakingReward"`
	StakingAddress     common.Address `json:"stakingAddress"`
	TribeReward        *hexutil.Big   `json:"tribeReward"`
	TribeAddress       common.Address `json:"tribeAddress"`
	MaintenanceReward  *hexutil.Big   `json:"maintenanceReward"`
	MaintenanceAddress common.Address `json:"maintenanceAddress"`
}

var (
	oggInitialRewardRPC, _ = new(big.Int).SetString("700000000000000000000", 10)
	oggDecayNumeratorRPC   = big.NewInt(999999933042)
	oggDecayDenominatorRPC = big.NewInt(1000000000000)
	oggStakingAddressRPC     = common.HexToAddress("0xCd442d7AC675D6c637a960e10913e341508C6672")
	oggTribePoolAddressRPC   = common.HexToAddress("0xfeaD066Caa900F210B19B9df14aBc38B46a15e66")
	oggMaintenanceAddressRPC = common.HexToAddress("0x85ea896411EdFE9dD7fa6F4F5FaA19D2D81cdA5E")
)

func computeBlockRewardRPC(blockNum *big.Int) *big.Int {
	reward := new(big.Int).Set(oggInitialRewardRPC)
	n := blockNum.Int64()
	if n == 0 {
		return reward
	}
	numPow := new(big.Int).SetInt64(1)
	denPow := new(big.Int).SetInt64(1)
	base := n
	numBase := new(big.Int).Set(oggDecayNumeratorRPC)
	denBase := new(big.Int).Set(oggDecayDenominatorRPC)
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

type PublicOGGAPI struct {
	b Backend
}

func NewPublicOGGAPI(b Backend) *PublicOGGAPI {
	return &PublicOGGAPI{b: b}
}

func (api *PublicOGGAPI) GetBlockReward(ctx context.Context, blockNr rpc.BlockNumber) (*OGGBlockReward, error) {
	block, err := api.b.BlockByNumber(ctx, blockNr)
	if err != nil {
		return nil, err
	}
	if block == nil {
		return nil, nil
	}
	num := block.Number()
	total := computeBlockRewardRPC(num)

	stakingReward := new(big.Int).Mul(total, big.NewInt(40))
	stakingReward.Div(stakingReward, big.NewInt(100))
	tribeReward := new(big.Int).Mul(total, big.NewInt(7))
	tribeReward.Div(tribeReward, big.NewInt(100))
	maintenanceReward := new(big.Int).Mul(total, big.NewInt(8))
	maintenanceReward.Div(maintenanceReward, big.NewInt(100))
	minerReward := new(big.Int).Set(total)
	minerReward.Sub(minerReward, stakingReward)
	minerReward.Sub(minerReward, tribeReward)
	minerReward.Sub(minerReward, maintenanceReward)

	return &OGGBlockReward{
		BlockNumber:        hexutil.Uint64(num.Uint64()),
		TotalReward:        (*hexutil.Big)(total),
		MinerReward:        (*hexutil.Big)(minerReward),
		MinerAddress:       block.Coinbase(),
		StakingReward:      (*hexutil.Big)(stakingReward),
		StakingAddress:     oggStakingAddressRPC,
		TribeReward:        (*hexutil.Big)(tribeReward),
		TribeAddress:       oggTribePoolAddressRPC,
		MaintenanceReward:  (*hexutil.Big)(maintenanceReward),
		MaintenanceAddress: oggMaintenanceAddressRPC,
	}, nil
}
