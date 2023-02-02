package osmosis

import (
	"github.com/CosmWasm/wasmd/x/wasm"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/std"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/x/auth"
	"github.com/cosmos/cosmos-sdk/x/auth/tx"
	authz "github.com/cosmos/cosmos-sdk/x/authz/module"
	"github.com/cosmos/cosmos-sdk/x/bank"
	"github.com/cosmos/cosmos-sdk/x/capability"
	"github.com/cosmos/cosmos-sdk/x/crisis"
	"github.com/cosmos/cosmos-sdk/x/distribution"
	distrclient "github.com/cosmos/cosmos-sdk/x/distribution/client"
	feegrant "github.com/cosmos/cosmos-sdk/x/feegrant/module"
	"github.com/cosmos/cosmos-sdk/x/gov"
	"github.com/cosmos/cosmos-sdk/x/mint"
	"github.com/cosmos/cosmos-sdk/x/params"
	paramsclient "github.com/cosmos/cosmos-sdk/x/params/client"
	"github.com/cosmos/cosmos-sdk/x/slashing"
	"github.com/cosmos/cosmos-sdk/x/staking"
	"github.com/cosmos/cosmos-sdk/x/upgrade"
	upgradeclient "github.com/cosmos/cosmos-sdk/x/upgrade/client"
	"github.com/cosmos/ibc-go/v3/modules/apps/transfer"
	ibc "github.com/cosmos/ibc-go/v3/modules/core"
	osmosisEpochs "github.com/osmosis-labs/osmosis/v13/x/epochs"
	osmosisGamm "github.com/osmosis-labs/osmosis/v13/x/gamm"
	osmosisIncentives "github.com/osmosis-labs/osmosis/v13/x/incentives"
	osmosisLockup "github.com/osmosis-labs/osmosis/v13/x/lockup"
	osmosisMint "github.com/osmosis-labs/osmosis/v13/x/mint"
	osmosisPoolIncentives "github.com/osmosis-labs/osmosis/v13/x/pool-incentives"
	osmosisSuperfluid "github.com/osmosis-labs/osmosis/v13/x/superfluid"
	osmosisTokenFactory "github.com/osmosis-labs/osmosis/v13/x/tokenfactory"
	osmosisTxFees "github.com/osmosis-labs/osmosis/v13/x/txfees"
)

var modules = []module.AppModuleBasic{
	auth.AppModuleBasic{},
	authz.AppModuleBasic{},
	bank.AppModuleBasic{},
	capability.AppModuleBasic{},
	gov.NewAppModuleBasic(
		paramsclient.ProposalHandler, distrclient.ProposalHandler, upgradeclient.ProposalHandler, upgradeclient.CancelProposalHandler,
	),
	crisis.AppModuleBasic{},
	distribution.AppModuleBasic{},
	feegrant.AppModuleBasic{},
	mint.AppModuleBasic{},
	params.AppModuleBasic{},
	slashing.AppModuleBasic{},
	staking.AppModuleBasic{},
	upgrade.AppModuleBasic{},
	transfer.AppModuleBasic{},
	ibc.AppModuleBasic{},
	wasm.AppModuleBasic{},
	osmosisGamm.AppModuleBasic{},
	osmosisEpochs.AppModuleBasic{},
	osmosisIncentives.AppModuleBasic{},
	osmosisLockup.AppModuleBasic{},
	osmosisMint.AppModuleBasic{},
	osmosisPoolIncentives.AppModuleBasic{},
	osmosisSuperfluid.AppModuleBasic{},
	osmosisTokenFactory.AppModuleBasic{},
	osmosisTxFees.AppModuleBasic{},
}

type Codec struct {
	InterfaceRegistry types.InterfaceRegistry
	Marshaler         codec.Codec
	TxConfig          client.TxConfig
	Amino             *codec.LegacyAmino
}

func MakeCodec() Codec {
	modBasic := module.NewBasicManager(modules...)
	encodingConfig := makeCodecConfig()
	std.RegisterLegacyAminoCodec(encodingConfig.Amino)
	std.RegisterInterfaces(encodingConfig.InterfaceRegistry)
	modBasic.RegisterLegacyAminoCodec(encodingConfig.Amino)
	modBasic.RegisterInterfaces(encodingConfig.InterfaceRegistry)
	return encodingConfig
}

func makeCodecConfig() Codec {
	interfaceRegistry := types.NewInterfaceRegistry()
	marshaler := codec.NewProtoCodec(interfaceRegistry)
	return Codec{
		InterfaceRegistry: interfaceRegistry,
		Marshaler:         marshaler,
		TxConfig:          tx.NewTxConfig(marshaler, tx.DefaultSignModes),
		Amino:             codec.NewLegacyAmino(),
	}
}
