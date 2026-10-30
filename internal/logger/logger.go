// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	log                *zap.Logger
	AppLog             *zap.SugaredLogger
	InitLog            *zap.SugaredLogger
	CfgLog             *zap.SugaredLogger
	ContextLog         *zap.SugaredLogger
	ConsumerLog        *zap.SugaredLogger
	CallbackLog        *zap.SugaredLogger
	NorthboundLog      *zap.SugaredLogger
	GinLog             *zap.SugaredLogger
	NrfRegistrationLog *zap.SugaredLogger
	atomicLevel        zap.AtomicLevel
)

func init() {
	atomicLevel = zap.NewAtomicLevelAt(zap.InfoLevel)
	config := zap.Config{
		Level:            atomicLevel,
		Development:      false,
		Encoding:         "console",
		EncoderConfig:    zap.NewProductionEncoderConfig(),
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}

	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	config.EncoderConfig.LevelKey = "level"
	config.EncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	config.EncoderConfig.CallerKey = "caller"
	config.EncoderConfig.EncodeCaller = zapcore.ShortCallerEncoder
	config.EncoderConfig.MessageKey = "message"
	config.EncoderConfig.StacktraceKey = ""

	var err error
	log, err = config.Build()
	if err != nil {
		panic(err)
	}

	AppLog = log.Sugar().With("component", "AF", "category", "App")
	InitLog = log.Sugar().With("component", "AF", "category", "Init")
	CfgLog = log.Sugar().With("component", "AF", "category", "CFG")
	ContextLog = log.Sugar().With("component", "AF", "category", "Context")
	ConsumerLog = log.Sugar().With("component", "AF", "category", "Consumer")
	CallbackLog = log.Sugar().With("component", "AF", "category", "Callback")
	NorthboundLog = log.Sugar().With("component", "AF", "category", "Northbound")
	GinLog = log.Sugar().With("component", "AF", "category", "GIN")
	NrfRegistrationLog = log.Sugar().With("component", "AF", "category", "NrfRegistration")
}

// SetLogLevel: set the log level (panic|fatal|error|warn|info|debug)
func SetLogLevel(level zapcore.Level) {
	CfgLog.Infoln("set log level:", level)
	atomicLevel.SetLevel(level)
}
