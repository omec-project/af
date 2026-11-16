// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package nfregistration adapts the AF to the shared NRF registration service. The AF's registration
// does not depend on dynamic configuration, so it registers once at startup and stays registered.
package nfregistration

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/omec-project/af/internal/consumer"
	"github.com/omec-project/af/internal/logger"
	"github.com/omec-project/af/internal/metrics"
	"github.com/omec-project/openapi/v2/models"
	"github.com/omec-project/openapi/v2/nrfregistration"
)

// retryTime is the interval between failed registration attempts.
var retryTime = 10 * time.Second

// The AF has no registration config: the service is parameterised with an empty struct.
type afRegistrationService = nrfregistration.Service[struct{}]

var (
	serviceMutex sync.Mutex
	service      *afRegistrationService
)

// getService returns the service, building it on first use so that retryTime can be set before.
func getService() *afRegistrationService {
	serviceMutex.Lock()
	defer serviceMutex.Unlock()
	if service == nil {
		service = newService()
	}
	return service
}

func newService() *afRegistrationService {
	// The consumer functions are looked up on every call so tests can replace them.
	svc, err := nrfregistration.New(nrfregistration.Config[struct{}]{
		NfName:    "AF",
		Log:       logger.NrfRegistrationLog,
		RetryTime: retryTime,
		IsEmpty:   func(struct{}) bool { return false },
		Register: func(ctx context.Context, _ struct{}) (*models.NFProfile, error) {
			nfProfile, err := consumer.SendRegisterNFInstance(ctx)
			if ctx.Err() == nil {
				metrics.ObserveNrfRequest(metrics.NrfRegister, err)
			}
			if err != nil {
				return nil, err
			}
			metrics.SetNrfRegistered(true)
			return nfProfile, nil
		},
		Update: func(ctx context.Context, patch []models.PatchItem) (*models.NFProfile, *models.ProblemDetails, error) {
			nfProfile, problemDetails, err := consumer.SendUpdateNFInstance(ctx, patch)
			if ctx.Err() == nil {
				hbErr := err
				if problemDetails != nil && hbErr == nil {
					hbErr = fmt.Errorf("problem details: %+v", problemDetails)
				}
				metrics.ObserveNrfRequest(metrics.NrfHeartbeat, hbErr)
			}
			if problemDetails != nil || (err != nil && ctx.Err() == nil) {
				metrics.SetNrfRegistered(false)
			}
			return nfProfile, problemDetails, err
		},
		Deregister: func(ctx context.Context) error {
			err := consumer.SendDeregisterNFInstance(ctx)
			metrics.ObserveNrfRequest(metrics.NrfDeregister, err)
			metrics.SetNrfRegistered(false)
			return err
		},
	})
	if err != nil {
		panic(err) // the config above is static, so this is a programming error
	}
	return svc
}

// StartNfRegistrationService registers the AF at the NRF, retrying until it succeeds, and then keeps the
// registration alive with heartbeats. If a heartbeat fails the AF registers again. It returns when ctx is done.
func StartNfRegistrationService(ctx context.Context) {
	getService().StartStatic(ctx, struct{}{})
}

// DeregisterNF stops the heartbeat and removes the AF from the NRF.
var DeregisterNF = func(ctx context.Context) {
	getService().Deregister(ctx)
}
