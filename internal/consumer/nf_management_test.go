// Copyright (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package consumer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	afContext "github.com/omec-project/af/internal/context"
)

func TestSendRegisterNFInstance_TakesNfIdFromLocation(t *testing.T) {
	for name, location := range map[string]string{
		"nnrf-nfm path":   "http://nrf/nnrf-nfm/v1/nf-instances/assigned-1",
		"unexpected path": "http://nrf/other/assigned-1",
		"no authority":    "nf-instances/assigned-1",
		"empty Location":  "",
		"trailing slash":  "http://nrf/nnrf-nfm/v1/nf-instances/",
	} {
		t.Run(name, func(t *testing.T) {
			nrf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if location != "" {
					w.Header().Set("Location", location)
				}
				w.WriteHeader(http.StatusCreated)
			}))
			defer nrf.Close()
			self := afContext.AF_Self()
			self.NrfUri, self.RegisterIPv4 = nrf.URL, "127.0.0.201"
			self.SetNfId("own-id")

			if _, err := SendRegisterNFInstance(context.Background()); err != nil {
				t.Fatal(err)
			}
			want := "assigned-1"
			if location == "" || location == "http://nrf/nnrf-nfm/v1/nf-instances/" {
				want = "own-id" // nothing usable in the response: keep the ID the AF sent
			}
			if got := self.NfId(); got != want {
				t.Errorf("NfId = %q, want %q", got, want)
			}
		})
	}
}
