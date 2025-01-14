package registry

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"testing"

	"github.com/base-org/pessimism/internal/conduit/models"
	"github.com/base-org/pessimism/internal/conduit/pipeline"
	"github.com/base-org/pessimism/internal/config"
	"github.com/base-org/pessimism/internal/logging"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type EthClientMocked struct {
	mock.Mock
}

func (ec *EthClientMocked) DialContext(ctx context.Context, rawURL string) error {
	args := ec.Called(ctx, rawURL)
	return args.Error(0)
}

func (ec *EthClientMocked) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	args := ec.Called(ctx, number)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.Header), args.Error(1)
}

func (ec *EthClientMocked) BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error) {
	args := ec.Called(ctx, number)
	return args.Get(0).(*types.Block), args.Error(1)
}

func Test_ConfigureRoutine_Error(t *testing.T) {

	ctx, cancel := context.WithCancel(context.Background())
	logging.NewLogger(nil, false)
	defer cancel()

	testObj := new(EthClientMocked)

	// setup expectations
	testObj.On("DialContext", mock.Anything, "error handle test").Return(errors.New("error handle test"))

	_, err := NewGethBlockOracle(ctx, pipeline.LiveOracle, &config.OracleConfig{
		RPCEndpoint: "error handle test",
	}, testObj)
	assert.Error(t, err)
	assert.EqualError(t, err, "error handle test")
}

func Test_ConfigureRoutine_Pass(t *testing.T) {

	ctx, cancel := context.WithCancel(context.Background())
	logging.NewLogger(nil, false)
	defer cancel()

	testObj := new(EthClientMocked)

	// setup expectations
	testObj.On("DialContext", mock.Anything, "pass test").Return(nil)

	newGethBlockOracleCreated, err := NewGethBlockOracle(ctx, pipeline.LiveOracle, &config.OracleConfig{
		RPCEndpoint: "pass test",
	}, testObj)
	assert.NoError(t, err)
	assert.Equal(t, newGethBlockOracleCreated.Type(), models.Oracle)
}

func Test_GetCurrentHeightFromNetwork(t *testing.T) {

	ctx, cancel := context.WithCancel(context.Background())
	logging.NewLogger(nil, false)
	defer cancel()

	testObj := new(EthClientMocked)

	// setup expectations
	testObj.On("DialContext", mock.Anything, "pass test").Return(nil)
	header := types.Header{
		ParentHash: common.HexToHash("0x123456789"),
		Number:     big.NewInt(5),
	}
	testObj.On("HeaderByNumber", mock.Anything, mock.Anything).Return(&header, nil)

	od := &GethBlockODef{cfg: &config.OracleConfig{
		RPCEndpoint:  "pass test",
		NumOfRetries: 3,
	}, currHeight: nil, client: testObj}

	assert.Equal(t, od.getCurrentHeightFromNetwork(ctx).Number, header.Number)
}

func Test_GetHeightToProcess(t *testing.T) {

	ctx, cancel := context.WithCancel(context.Background())
	logging.NewLogger(nil, false)
	defer cancel()
	testObj := new(EthClientMocked)
	testObj.On("DialContext", mock.Anything, "pass test").Return(nil)
	header := types.Header{
		ParentHash: common.HexToHash("0x123456789"),
		Number:     big.NewInt(5),
	}
	testObj.On("HeaderByNumber", mock.Anything, mock.Anything).Return(&header, nil)

	od := &GethBlockODef{cfg: &config.OracleConfig{
		RPCEndpoint:  "pass test",
		NumOfRetries: 3,
	}, currHeight: big.NewInt(123), client: testObj}

	assert.Equal(t, od.getHeightToProcess(ctx), big.NewInt(123))

	od.currHeight = nil
	od.cfg.StartHeight = big.NewInt(123)
	assert.Equal(t, od.getHeightToProcess(ctx), big.NewInt(123))

	od.currHeight = nil
	od.cfg.StartHeight = nil
	assert.Nil(t, od.getHeightToProcess(ctx))
}

func Test_Backroutine(t *testing.T) {
	logging.NewLogger(nil, false)
	var tests = []struct {
		name        string
		description string

		constructionLogic func() (*GethBlockODef, chan models.TransitData)
		testLogic         func(*testing.T, *GethBlockODef, chan models.TransitData)
	}{

		{
			name:        "Current network height check",
			description: "Check if network height check is less than starting height",

			constructionLogic: func() (*GethBlockODef, chan models.TransitData) {
				testObj := new(EthClientMocked)
				header := types.Header{
					ParentHash: common.HexToHash("0x123456789"),
					Number:     big.NewInt(5),
				}
				// setup expectations
				testObj.On("DialContext", mock.Anything, "pass test").Return(nil)
				testObj.On("HeaderByNumber", mock.Anything, mock.Anything).Return(&header, nil)

				od := &GethBlockODef{cfg: &config.OracleConfig{
					RPCEndpoint:  "pass test",
					NumOfRetries: 3,
				}, currHeight: nil, client: testObj}

				outChan := make(chan models.TransitData)

				return od, outChan
			},

			testLogic: func(t *testing.T, od *GethBlockODef, outChan chan models.TransitData) {

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()

				err := od.BackTestRoutine(ctx, outChan, big.NewInt(7), big.NewInt(10))
				assert.Error(t, err)
				assert.EqualError(t, err, "start height cannot be more than the latest height from network")
			},
		},
		{
			name:        "Successful Height check",
			description: "Ending height cannot be less than the Starting height",

			constructionLogic: func() (*GethBlockODef, chan models.TransitData) {
				testObj := new(EthClientMocked)

				// setup expectations
				testObj.On("DialContext", mock.Anything, "pass test").Return(nil)

				od := &GethBlockODef{cfg: &config.OracleConfig{
					RPCEndpoint:  "pass test",
					NumOfRetries: 3,
				}, currHeight: nil, client: testObj}

				outChan := make(chan models.TransitData)

				return od, outChan
			},

			testLogic: func(t *testing.T, od *GethBlockODef, outChan chan models.TransitData) {

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()

				err := od.BackTestRoutine(ctx, outChan, big.NewInt(2), big.NewInt(1))
				assert.Error(t, err)
				assert.EqualError(t, err, "start height cannot be more than the end height")
			},
		},
		// Leaving this here to help devs test infinite loops
		//
		//{
		//	name:        "Header fetch retry exceeded error check",
		//	description: "Check if the header fetch retry fails after 3 retries, total 4 tries.",
		//
		//	constructionLogic: func() (*GethBlockODef, chan models.TransitData) {
		//		testObj := new(EthClientMocked)
		//
		//		// setup expectations
		//		testObj.On("DialContext", mock.Anything, "pass test").Return(nil)
		//		testObj.On("HeaderByNumber", mock.Anything, mock.Anything).Return(nil, errors.New("no header for you"))
		//
		//		od := &GethBlockODef{cfg: &config.OracleConfig{
		//			RPCEndpoint:  "pass test",
		//			NumOfRetries: 3,
		//		}, currHeight: nil, client: testObj}
		//
		//		outChan := make(chan models.TransitData)
		//		return od, outChan
		//	},
		//
		//	testLogic: func(t *testing.T, od *GethBlockODef, outChan chan models.TransitData) {
		//
		//		ctx, cancel := context.WithCancel(context.Background())
		//		defer cancel()
		//
		//		err := od.BackTestRoutine(ctx, outChan, big.NewInt(1), big.NewInt(2))
		//		assert.Error(t, err)
		//		assert.EqualError(t, err, "no header for you")
		//	},
		// },
		{
			name:        "Backroutine happy path test",
			description: "Backroutine works and channel should have 4 messages waiting.",

			constructionLogic: func() (*GethBlockODef, chan models.TransitData) {
				testObj := new(EthClientMocked)
				header := types.Header{
					ParentHash: common.HexToHash("0x123456789"),
					Number:     big.NewInt(7),
				}
				block := types.NewBlock(&header, nil, nil, nil, trie.NewStackTrie(nil))
				// setup expectations
				testObj.On("DialContext", mock.Anything, "pass test").Return(nil)
				testObj.On("HeaderByNumber", mock.Anything, mock.Anything).Return(&header, nil)
				testObj.On("BlockByNumber", mock.Anything, mock.Anything).Return(block, nil)

				od := &GethBlockODef{cfg: &config.OracleConfig{
					RPCEndpoint:  "pass test",
					NumOfRetries: 3,
				}, currHeight: nil, client: testObj}

				outChan := make(chan models.TransitData, 2)

				return od, outChan
			},

			testLogic: func(t *testing.T, od *GethBlockODef, outChan chan models.TransitData) {

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()

				err := od.BackTestRoutine(ctx, outChan, big.NewInt(5), big.NewInt(6))
				assert.NoError(t, err)
				close(outChan)

				for m := range outChan {
					val := m.Value.(types.Block) //nolint:errcheck // converting to type from any for getting internal values
					assert.Equal(t, val.ParentHash(), common.HexToHash("0x123456789"))
				}
			},
		},
	}

	for i, tc := range tests {
		t.Run(fmt.Sprintf("%d-%s", i, tc.name), func(t *testing.T) {
			od, outChan := tc.constructionLogic()
			tc.testLogic(t, od, outChan)
		})

	}
}

func Test_ReadRoutine(t *testing.T) {
	logging.NewLogger(nil, false)
	var tests = []struct {
		name        string
		description string

		constructionLogic func() (*GethBlockODef, chan models.TransitData)
		testLogic         func(*testing.T, *GethBlockODef, chan models.TransitData)
	}{

		{
			name:        "Current network height check",
			description: "Check if network height check is less than starting height",

			constructionLogic: func() (*GethBlockODef, chan models.TransitData) {
				testObj := new(EthClientMocked)
				header := types.Header{
					ParentHash: common.HexToHash("0x123456789"),
					Number:     big.NewInt(5),
				}
				// setup expectations
				testObj.On("DialContext", mock.Anything, "pass test").Return(nil)
				testObj.On("HeaderByNumber", mock.Anything, mock.Anything).Return(&header, nil)

				od := &GethBlockODef{cfg: &config.OracleConfig{
					RPCEndpoint:  "pass test",
					StartHeight:  big.NewInt(7),
					EndHeight:    big.NewInt(10),
					NumOfRetries: 3,
				}, currHeight: nil, client: testObj}

				outChan := make(chan models.TransitData)

				return od, outChan
			},

			testLogic: func(t *testing.T, od *GethBlockODef, outChan chan models.TransitData) {

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()

				err := od.ReadRoutine(ctx, outChan)
				assert.Error(t, err)
				assert.EqualError(t, err, "start height cannot be more than the latest height from network")
			},
		},
		{
			name:        "Successful Height check 1",
			description: "Ending height cannot be less than the Starting height",

			constructionLogic: func() (*GethBlockODef, chan models.TransitData) {
				testObj := new(EthClientMocked)
				testObj.On("DialContext", mock.Anything, "pass test").Return(nil)
				od := &GethBlockODef{cfg: &config.OracleConfig{
					RPCEndpoint:  "pass test",
					StartHeight:  big.NewInt(2),
					EndHeight:    big.NewInt(1),
					NumOfRetries: 3,
				}, currHeight: nil, client: testObj}
				outChan := make(chan models.TransitData)
				return od, outChan
			},

			testLogic: func(t *testing.T, od *GethBlockODef, outChan chan models.TransitData) {

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()

				err := od.ReadRoutine(ctx, outChan)
				assert.Error(t, err)
				assert.EqualError(t, err, "start height cannot be more than the end height")
			},
		},
		{
			name:        "Successful Height check 2",
			description: "Cannot have start height nil, i.e, latest block and end height configured",

			constructionLogic: func() (*GethBlockODef, chan models.TransitData) {
				testObj := new(EthClientMocked)
				testObj.On("DialContext", mock.Anything, "pass test").Return(nil)
				od := &GethBlockODef{cfg: &config.OracleConfig{
					RPCEndpoint:  "pass test",
					StartHeight:  nil,
					EndHeight:    big.NewInt(1),
					NumOfRetries: 3,
				}, currHeight: nil, client: testObj}
				outChan := make(chan models.TransitData)
				return od, outChan
			},

			testLogic: func(t *testing.T, od *GethBlockODef, outChan chan models.TransitData) {

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()

				err := od.ReadRoutine(ctx, outChan)
				assert.Error(t, err)
				assert.EqualError(t, err, "cannot start with latest block height with end height configured")
			},
		},
		{
			name:        "Number of executions",
			description: "Making sure that number of blocks fetched matches the assumption. Number of messages should be 5, in the channel",

			constructionLogic: func() (*GethBlockODef, chan models.TransitData) {
				testObj := new(EthClientMocked)
				header := types.Header{
					ParentHash: common.HexToHash("0x123456789"),
					Number:     big.NewInt(7),
				}
				block := types.NewBlock(&header, nil, nil, nil, trie.NewStackTrie(nil))
				// setup expectations
				testObj.On("DialContext", mock.Anything, "pass test").Return(nil)
				testObj.On("HeaderByNumber", mock.Anything, mock.Anything).Return(&header, nil)
				testObj.On("BlockByNumber", mock.Anything, mock.Anything).Return(block, nil)

				od := &GethBlockODef{cfg: &config.OracleConfig{
					RPCEndpoint:  "pass test",
					StartHeight:  big.NewInt(1),
					EndHeight:    big.NewInt(5),
					NumOfRetries: 3,
				}, currHeight: nil, client: testObj}
				outChan := make(chan models.TransitData, 10)
				return od, outChan
			},

			testLogic: func(t *testing.T, od *GethBlockODef, outChan chan models.TransitData) {

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()

				err := od.ReadRoutine(ctx, outChan)
				assert.NoError(t, err)
				close(outChan)
				assert.Equal(t, len(outChan), 5)
			},
		},
		// Leaving this here to help devs test infinite loops
		//
		//{
		//	name:        "Latest block check",
		//	description: "Making sure that number of blocks fetched matches the assumption. Number of messages should be 5, in the channel",
		//
		//	constructionLogic: func() (*GethBlockODef, chan models.TransitData) {
		//		testObj := new(EthClientMocked)
		//		header := types.Header{
		//			ParentHash: common.HexToHash("0x123456789"),
		//			Number:     big.NewInt(1),
		//		}
		//		block := types.NewBlock(&header, nil, nil, nil, trie.NewStackTrie(nil))
		//		// setup expectations
		//		testObj.On("DialContext", mock.Anything, "pass test").Return(nil)
		//		testObj.On("HeaderByNumber", mock.Anything, mock.Anything).Return(&header, nil)
		//		testObj.On("BlockByNumber", mock.Anything, mock.Anything).Return(block, nil)
		//
		//		od := &GethBlockODef{cfg: &config.OracleConfig{
		//			RPCEndpoint:  "pass test",
		//			StartHeight:  nil,
		//			EndHeight:    nil,
		//			NumOfRetries: 3,
		//		}, currHeight: nil, client: testObj}
		//		outChan := make(chan models.TransitData, 10)
		//		return od, outChan
		//	},
		//
		//	testLogic: func(t *testing.T, od *GethBlockODef, outChan chan models.TransitData) {
		//
		//		ctx, cancel := context.WithCancel(context.Background())
		//		defer cancel()
		//
		//		err := od.ReadRoutine(ctx, outChan)
		//		assert.NoError(t, err)
		//		close(outChan)
		//		assert.Equal(t, len(outChan), 5)
		//	},
		// },
	}

	for i, tc := range tests {
		t.Run(fmt.Sprintf("%d-%s", i, tc.name), func(t *testing.T) {
			od, outChan := tc.constructionLogic()
			tc.testLogic(t, od, outChan)
		})

	}
}
