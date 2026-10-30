// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package nfregistration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	afContext "github.com/omec-project/af/internal/context"
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
	heartbeatUnit = 10 * time.Millisecond
	self := afContext.AF_Self()
	self.SetNfId("nf-1")
	self.NrfUri, self.RegisterIPv4 = nrf.URL, "127.0.0.201"

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		StartNfRegistrationService(ctx)
		close(done)
	}()

	deadline := time.Now().Add(10 * time.Second)
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
