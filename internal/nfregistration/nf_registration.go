// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package nfregistration

import (
	"context"
	"fmt"
	"time"

	"github.com/omec-project/af/internal/consumer"
	"github.com/omec-project/af/internal/logger"
	"github.com/omec-project/af/internal/metrics"
	"github.com/omec-project/openapi/v2/models"
)

var (
	retryTime     = 10 * time.Second
	heartbeatUnit = time.Second
)

const defaultHeartbeatTimer int32 = 60

// StartNfRegistrationService registers the AF at the NRF, retrying until it succeeds, and then keeps the
// registration alive with heartbeats. If a heartbeat fails the AF registers again. It returns when ctx is done.
func StartNfRegistrationService(ctx context.Context) {
	logger.NrfRegistrationLog.Infoln("started NF registration to NRF service")
	for {
		heartbeatTimer, ok := register(ctx)
		if !ok {
			logger.NrfRegistrationLog.Infoln("NF registration service shutting down")
			return
		}
		if !heartbeatLoop(ctx, heartbeatTimer) {
			logger.NrfRegistrationLog.Infoln("NF registration service shutting down")
			return
		}
		logger.NrfRegistrationLog.Debugln("NF heartbeat failed. Trying to register again")
	}
}

// register sends RegisterNFInstance until it succeeds. ok is false if ctx was cancelled first.
func register(ctx context.Context) (heartbeatTimer int32, ok bool) {
	var interval time.Duration
	for {
		select {
		case <-ctx.Done():
			return 0, false
		case <-time.After(interval):
		}
		nfProfile, err := consumer.SendRegisterNFInstance(ctx)
		if ctx.Err() == nil {
			metrics.ObserveNrfRequest(metrics.NrfRegister, err)
		}
		if err != nil {
			logger.NrfRegistrationLog.Errorf("register AF instance to NRF failed: %v. Will retry in %s", err, retryTime)
			interval = retryTime
			continue
		}
		logger.NrfRegistrationLog.Infoln("register AF instance to NRF succeeded")
		metrics.SetNrfRegistered(true)
		return nfProfile.GetHeartBeatTimer(), true
	}
}

// heartbeatLoop sends the heartbeat every heartbeatTimer seconds until one fails (returns true)
// or ctx is cancelled (returns false).
func heartbeatLoop(ctx context.Context, heartbeatTimer int32) (failed bool) {
	if heartbeatTimer <= 0 {
		heartbeatTimer = defaultHeartbeatTimer
	}
	interval := time.Duration(heartbeatTimer) * heartbeatUnit
	logger.NrfRegistrationLog.Debugf("started heartbeat timer: %d sec", heartbeatTimer)
	patchItem := []models.PatchItem{{
		Op:    models.PATCHOPERATION_REPLACE,
		Path:  "/nfStatus",
		Value: models.NFSTATUS_REGISTERED,
	}}
	for {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(interval):
		}
		nfProfile, problemDetails, err := consumer.SendUpdateNFInstance(ctx, patchItem)
		if ctx.Err() == nil {
			hbErr := err
			if problemDetails != nil && hbErr == nil {
				hbErr = fmt.Errorf("problem details: %+v", problemDetails)
			}
			metrics.ObserveNrfRequest(metrics.NrfHeartbeat, hbErr)
		}
		if problemDetails != nil {
			metrics.SetNrfRegistered(false)
			logger.NrfRegistrationLog.Warnf("AF update NF instance (heartbeat) problem details: %+v", problemDetails)
			return true
		}
		if err != nil {
			if ctx.Err() != nil {
				return false
			}
			metrics.SetNrfRegistered(false)
			logger.NrfRegistrationLog.Warnln("AF update NF instance (heartbeat) error:", err.Error())
			return true
		}
		logger.NrfRegistrationLog.Debugln("AF update NF instance (heartbeat) succeeded")
		if t := nfProfile.GetHeartBeatTimer(); t > 0 {
			interval = time.Duration(t) * heartbeatUnit
		}
	}
}

// DeregisterNF removes the AF from the NRF.
var DeregisterNF = func(ctx context.Context) {
	err := consumer.SendDeregisterNFInstance(ctx)
	metrics.ObserveNrfRequest(metrics.NrfDeregister, err)
	metrics.SetNrfRegistered(false)
	if err != nil {
		logger.NrfRegistrationLog.Warnln("deregister instance from NRF error:", err.Error())
		return
	}
	logger.NrfRegistrationLog.Infoln("deregister instance from NRF successful")
}
