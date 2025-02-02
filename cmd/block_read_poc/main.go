package main

import (
	"context"
	"sync"

	"github.com/base-org/pessimism/internal/client"
	"github.com/base-org/pessimism/internal/conduit/models"
	"github.com/base-org/pessimism/internal/conduit/pipeline"
	"github.com/base-org/pessimism/internal/conduit/registry"
	"github.com/base-org/pessimism/internal/config"
	"github.com/base-org/pessimism/internal/logging"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

const (
	outChanID   = 0x420
	interChanID = 0x42
)

func main() {
	/*
		This a simple experimental POC showcasing an implicit CONTRACT_CREATE_TX register pipeline

		This is done to:
		A) Prove that the Oracle and Pipe components operate as expected and are able to channel data between each other
		B) Reason about component construction to better understand how to automate register pipeline creation
		C) Demonstrate a lightweight MVP for the system

	*/

	appCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := config.NewConfig("config.env")

	logging.NewLogger(cfg.LoggerConfig, cfg.IsProduction())

	logging.NoContext().Info("pessimism boot up")

	l1OracleCfg := &config.OracleConfig{
		RPCEndpoint: cfg.L1RpcEndpoint,
		StartHeight: nil,
		EndHeight:   nil}

	// 1. Configure blackhole tx pipe component
	createRegister, err := registry.GetRegister(registry.ContractCreateTX)
	if err != nil {
		logging.NoContext().Fatal("error creating register", zap.Error(err))
	}

	initPipe, success := createRegister.ComponentConstructor.(pipeline.PipeConstructorFunc)
	if !success {
		logging.NoContext().Fatal("could not read component constructor Pipe constructor type")
	}

	inputChan := make(chan models.TransitData)

	createTxPipe, err := initPipe(appCtx, inputChan)
	if err != nil {
		logging.NoContext().Fatal("error during pipe initialization", zap.Error(err))
	}

	register, err := registry.GetRegister(registry.GethBlock)
	if err != nil {
		logging.NoContext().Fatal("error getting register", zap.String("type", string(registry.GethBlock)), zap.Error(err))
	}

	init, success := register.ComponentConstructor.(pipeline.OracleConstructor)
	if !success {
		logging.NoContext().Fatal("Could not read constructor value")
	}

	go func() {
		if routineErr := createTxPipe.EventLoop(); routineErr != nil {
			logging.NoContext().Error("Error received from oracle event loop", zap.Error(routineErr))
		}
	}()

	ethClient := client.EthClient{}
	l1Oracle, err := init(appCtx, pipeline.LiveOracle, l1OracleCfg, &ethClient)
	if err != nil {
		logging.NoContext().Fatal("error initializing oracle", zap.Error(err))
	}

	if err := l1Oracle.AddDirective(interChanID, inputChan); err != nil {
		logging.NoContext().Fatal("error adding directive", zap.Int("interChanID", interChanID), zap.Error(err))
	}

	outputChan := make(chan models.TransitData)

	if err := createTxPipe.AddDirective(outChanID, outputChan); err != nil {
		logging.NoContext().Fatal("error adding directive", zap.Int("outChanID", outChanID), zap.Error(err))
	}

	wg := sync.WaitGroup{}
	wg.Add(1)

	go func() {
		if routineErr := l1Oracle.EventLoop(); routineErr != nil {
			logging.NoContext().Error("Error received from oracle event loop", zap.Error(routineErr))
		}
	}()

	for td := range outputChan {
		logging.NoContext().Info("Received Contract creation Transaction", zap.Any("transitData", td))

		parsedTx, success := td.Value.(types.Transaction)
		if !success {
			logging.NoContext().Error("Could not parse transaction value")
		}

		logging.NoContext().Info("As parsed transaction", zap.Any("parsedTX", parsedTx))
	}
}
