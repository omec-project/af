// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package nfregistration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/omec-project/af/internal/consumer"
	afContext "github.com/omec-project/af/internal/context"
	"github.com/omec-project/af/internal/metrics"
	"github.com/omec-project/openapi/v2/models"
)

func TestRegisterHeartbeatAndReregister(t *testing.T) {
	var puts, patches, deletes atomic.Int32
	nrf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const path = "/nnrf-nfm/v1/nf-instances/nf-1"
		if r.URL.Path != path {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		switch r.Method {
		case http.MethodPut:
			if puts.Add(1) == 1 { // first attempt fails: the AF must retry
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Location", "http://"+r.Host+path)
			w.WriteHeader(http.StatusCreated)
			if _, err := w.Write([]byte(`{"nfInstanceId":"nf-1","nfType":"AF","nfStatus":"REGISTERED","heartBeatTimer":1}`)); err != nil {
				t.Error(err)
			}
		case http.MethodPatch:
			if patches.Add(1) == 1 {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			// the NRF lost the AF: the heartbeat gets 404 and the AF registers again
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusNotFound)
			if _, err := w.Write([]byte(`{"title":"Not Found","status":404}`)); err != nil {
				t.Error(err)
			}
		case http.MethodDelete:
			deletes.Add(1)
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer nrf.Close()

	retryTime = 50 * time.Millisecond
	self := afContext.AF_Self()
	self.SetNfId("nf-1")
	self.NrfUri, self.RegisterIPv4 = nrf.URL, "127.0.0.201"

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		StartNfRegistrationService(ctx)
		close(done)
	}()

	// the heartbeat timer of 1 second is not scaled down, so allow a few heartbeat periods
	deadline := time.Now().Add(15 * time.Second)
	for puts.Load() < 3 && time.Now().Before(deadline) { // failed PUT, PUT, PUT after the failed heartbeat
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("registration service did not stop")
	}
	if puts.Load() < 3 || patches.Load() < 2 {
		t.Errorf("puts=%d patches=%d, want >=3 and >=2", puts.Load(), patches.Load())
	}

	DeregisterNF(context.Background())
	if deletes.Load() != 1 {
		t.Errorf("deregister sent %d deletes", deletes.Load())
	}
}

// metricValue returns the value of the series that starts with prefix in the metrics exposition, 0 if absent.
func metricValue(t *testing.T, prefix string) float64 {
	t.Helper()
	rec := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil))
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if rest, ok := strings.CutPrefix(line, prefix+" "); ok {
			v, err := strconv.ParseFloat(strings.TrimSpace(rest), 64)
			if err != nil {
				t.Fatalf("parse %q: %v", line, err)
			}
			return v
		}
	}
	return 0
}

func TestMetrics_FollowTheRegistrationLifecycle(t *testing.T) {
	origRegister, origUpdate, origDeregister := consumer.SendRegisterNFInstance, consumer.SendUpdateNFInstance, consumer.SendDeregisterNFInstance
	t.Cleanup(func() {
		consumer.SendRegisterNFInstance, consumer.SendUpdateNFInstance, consumer.SendDeregisterNFInstance = origRegister, origUpdate, origDeregister
	})
	profile := models.NewNFProfileWithDefaults()
	profile.SetHeartBeatTimer(1)
	var registers atomic.Int32
	consumer.SendRegisterNFInstance = func(context.Context) (*models.NFProfile, error) {
		registers.Add(1)
		return profile, nil
	}
	consumer.SendUpdateNFInstance = func(context.Context, []models.PatchItem) (*models.NFProfile, *models.ProblemDetails, error) {
		return nil, models.NewProblemDetails(), nil // the NRF lost the AF
	}
	consumer.SendDeregisterNFInstance = func(context.Context) error { return nil }

	const (
		registerOK   = `af_nrf_requests{operation="register",result="success"}`
		heartbeatBad = `af_nrf_requests{operation="heartbeat",result="failure"}`
		deregisterOK = `af_nrf_requests{operation="deregister",result="success"}`
	)
	registerBefore, heartbeatBefore, deregisterBefore := metricValue(t, registerOK), metricValue(t, heartbeatBad), metricValue(t, deregisterOK)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); StartNfRegistrationService(ctx) }()

	deadline := time.Now().Add(10 * time.Second)
	for registers.Load() < 2 && time.Now().Before(deadline) { // register, failed heartbeat, register again
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	if registers.Load() < 2 {
		t.Fatalf("registrations = %d, want at least 2", registers.Load())
	}

	if got := metricValue(t, registerOK) - registerBefore; got < 2 {
		t.Errorf("successful register requests counted = %v, want at least 2", got)
	}
	if got := metricValue(t, heartbeatBad) - heartbeatBefore; got < 1 {
		t.Errorf("failed heartbeats counted = %v, want at least 1", got)
	}
	if got := metricValue(t, "af_nrf_registered"); got != 1 {
		t.Errorf("af_nrf_registered = %v after re-registering, want 1", got)
	}

	DeregisterNF(context.Background())
	if got := metricValue(t, deregisterOK) - deregisterBefore; got != 1 {
		t.Errorf("successful deregister requests counted = %v, want 1", got)
	}
	if got := metricValue(t, "af_nrf_registered"); got != 0 {
		t.Errorf("af_nrf_registered = %v after deregistering, want 0", got)
	}
}
