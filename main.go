package main

import (
	"log/slog"
	"os"
	"runtime/debug"
	"time"

	"github.com/golang-cz/devslog"
)

var logger *slog.Logger = nil
var pc *PineConnection = nil

func main() {
	logger = configureLogger()
	logger.Info("begin")

	// we have to do this super early in case GOMEMLIMIT is set really low
	goMemLimitEnvVar := os.Getenv("GOMEMLIMIT")
	goGCEnvVar := os.Getenv("GOGC")
	if goMemLimitEnvVar == "" {
		debug.SetMemoryLimit(1024 * 1024 * 64) // 64 megs
	}
	if goGCEnvVar == "" {
		debug.SetGCPercent(10)
	}

	// testPineRequestsAndAnswers()

	// try connecting to every supported emulator on their default slot/port until we get a connection
	for {
		var err error
		logger.Info("trying to connect to known emulators on default slots/ports")
		for target, defaultSlot := range defaultSlotForTargetMap {
			logger.Info("trying connecting to " + target)
			pc, err = NewPineConnection(target, defaultSlot)
			if err != nil {
				logger.Info("failed to connect to " + target + ". Continuing to next emulator target.")
				continue
			}
			err = pc.TestConnection()
			if err != nil {
				logger.Info("test connection for target " + target + " failed. Continuing to next emulator target.")
				pc = nil
				continue
			}
			// looks like we have a working connection
			logger.Info("test connection for target " + target + " succeeded.")
			break
		}
		if pc == nil {
			logger.Info("could not connect to any targets. Sleeping for 5 seconds before reattempting connection")
			time.Sleep(5 * time.Second)
			continue
		}

		serviceAPIRequests()
	}
}

func configureLogger() *slog.Logger {
	var logLevel = new(slog.LevelVar)
	logLevelEnvVar := os.Getenv("WOODY_LOG_LEVEL")
	if logLevelEnvVar != "" {
		switch logLevelEnvVar {
		case "INFO", "info":
			logLevel.Set(slog.LevelInfo)
		case "DEBUG", "debug":
			logLevel.Set(slog.LevelDebug)
		case "WARN", "warn":
			logLevel.Set(slog.LevelWarn)
		case "ERROR", "error":
			logLevel.Set(slog.LevelError)
		default:
			logLevel.Set(slog.LevelInfo)
		}
	}
	var handlerOptions = slog.HandlerOptions{
		AddSource: true,
		Level:     logLevel,
	}
	devSlogOpts := &devslog.Options{
		HandlerOptions:    &handlerOptions,
		MaxSlicePrintSize: 100,
		SortKeys:          true,
	}
	var logger *slog.Logger = slog.New(devslog.NewHandler(os.Stdout, devSlogOpts))
	slog.SetDefault(logger)

	return logger
}

// some code I keep around for testing with PCSX2
func testPineRequestsAndAnswers() {
	logger.Info("creating PineConnection")
	var pc, err = NewPineConnection("pcsx2", 0)
	if err != nil {
		logger.Error("error while creating PineConnection", "err", err)
		os.Exit(1)
	}

	logger.Info("creating requestBytes")
	// requestBytes, err := PineStatusRequest{}.toBytes()
	// requestBytes, err := PineVersionRequest{}.toBytes()
	// requestBytes, err := PineRead8Request{address: 0x35459C}.toBytes()
	// requestBytes, err := PineRead16Request{address: 0x35459C}.toBytes()
	// requestBytes, err := PineRead32Request{address: 0x35459C}.toBytes()
	// requestBytes, err := PineRead64Request{address: 0x35459C}.toBytes()
	// requestBytes, err := PineWrite8Request{address: 0x35459C, data: 0x45}.toBytes() // 0x45 = E
	// requestBytes, err := PineWrite16Request{address: 0x35459C, data: 0x7845}.toBytes() // 0x7845 = xE
	// requestBytes, err := PineWrite32Request{address: 0x35459C, data: 0x69637845}.toBytes() // 0x69637845 = icxE
	// requestBytes, err := PineWrite64Request{address: 0x35459C, data: 0x6962657469637845}.toBytes() // 0x6962657469637845 = ibeticxE
	// requestBytes, err := PineSaveStateRequest{slot: 1}.toBytes()
	// requestBytes, err := PineLoadStateRequest{slot: 1}.toBytes()
	// requestBytes, err := PineTitleRequest{}.toBytes()
	// requestBytes, err := PineIDRequest{}.toBytes()
	// requestBytes, err := PineUUIDRequest{}.toBytes()
	requestBytes, err := PineGameVersionRequest{}.toBytes()
	if err != nil {
		logger.Error("error while creating requestBytes", "err", err)
		os.Exit(1)
	}

	logger.Info("sending requestBytes")
	answerBytes, err := pc.Send(requestBytes)
	if err != nil {
		logger.Error("error while sending requestBytes", "err", err)
		os.Exit(1)
	}

	logger.Info("creating Answer")
	// var answer *PineStatusAnswer = &PineStatusAnswer{}
	// var answer *PineVersionAnswer = &PineVersionAnswer{}
	// var answer *PineRead8Answer = &PineRead8Answer{}
	// var answer *PineRead16Answer = &PineRead16Answer{}
	// var answer *PineRead32Answer = &PineRead32Answer{}
	// var answer *PineRead64Answer = &PineRead64Answer{}
	// var answer *PineWrite8Answer = &PineWrite8Answer{}
	// var answer *PineWrite16Answer = &PineWrite16Answer{}
	// var answer *PineWrite32Answer = &PineWrite32Answer{}
	// var answer *PineWrite64Answer = &PineWrite64Answer{}
	// var answer *PineSaveStateAnswer = &PineSaveStateAnswer{}
	// var answer *PineLoadStateAnswer = &PineLoadStateAnswer{}
	// var answer *PineTitleAnswer = &PineTitleAnswer{}
	// var answer *PineIDAnswer = &PineIDAnswer{}
	// var answer *PineUUIDAnswer = &PineUUIDAnswer{}
	var answer *PineGameVersionAnswer = &PineGameVersionAnswer{}
	err = answer.fromBytes(answerBytes)
	logger.Info("returned Answer",
		"answer", answer,
		"answer.resultCode", answer.resultCode,
		"answer.gameVersion", answer.gameVersion,
		// "answer.uuid", answer.uuid,
		// "answer.id", answer.id,
		// "answer.title", answer.title,
		// "answer.memoryValue", answer.memoryValue,
		// "answer.version", answer.version,
		// "answer.status", answer.status,
	)
}
