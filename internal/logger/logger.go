// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package logger

import (
	utilLogger "github.com/omec-project/util/logger"
	"go.uber.org/zap/zapcore"
)

var component = utilLogger.NewComponent("AF")

var (
	AppLog             = component.Category("App")
	InitLog            = component.Category("Init")
	CfgLog             = component.Category("CFG")
	ContextLog         = component.Category("Context")
	ConsumerLog        = component.Category("Consumer")
	CallbackLog        = component.Category("Callback")
	NorthboundLog      = component.Category("Northbound")
	GinLog             = component.Category("GIN")
	NrfRegistrationLog = component.Category("NrfRegistration")
)

// SetLogLevel: set the log level (panic|fatal|error|warn|info|debug)
func SetLogLevel(level zapcore.Level) {
	CfgLog.Infoln("set log level:", level)
	component.SetLevel(level)
}
