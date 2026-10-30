// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package consumer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	afContext "github.com/omec-project/af/internal/context"
)

func TestParseAppSessionLocation(t *testing.T) {
	tests := []struct {
		name, location, apiRoot, id string
		wantErr                     bool
	}{
		{"ok", "http://10.0.0.1:8000/npcf-policyauthorization/v1/app-sessions/abc-1", "http://10.0.0.1:8000", "abc-1", false},
		{"https and fqdn", "https://pcf.example.com/npcf-policyauthorization/v1/app-sessions/9", "https://pcf.example.com", "9", false},
		{"empty", "", "", "", true},
		{"no service path", "http://10.0.0.1:8000/app-sessions/abc", "", "", true},
		{"no id", "http://10.0.0.1:8000/npcf-policyauthorization/v1/app-sessions/", "", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			apiRoot, id, err := parseAppSessionLocation(tc.location)
			if (err != nil) != tc.wantErr || apiRoot != tc.apiRoot || id != tc.id {
				t.Errorf("got (%q, %q, %v), want (%q, %q, err=%v)", apiRoot, id, err, tc.apiRoot, tc.id, tc.wantErr)
			}
		})
	}
}

func TestDiscoverPcfUri(t *testing.T) {
	const profile = `{"nfInstanceId":"pcf-1","nfType":"PCF","nfStatus":"REGISTERED","ipv4Addresses":["10.1.1.1"],
	"nfServices":[{"serviceInstanceId":"s1","serviceName":"npcf-policyauthorization","versions":[{"apiVersionInUri":"v1","apiFullVersion":"1.0.0"}],
	"scheme":"http","nfServiceStatus":"REGISTERED","ipEndPoints":[{"ipv4Address":"10.1.1.1","port":8000}]}]}`
	var query string
	nrf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("target-nf-type") != "PCF" {
			if _, err := w.Write([]byte(`{"validityPeriod":10,"nfInstances":[]}`)); err != nil {
				t.Error(err)
			}
			return
		}
		if _, err := w.Write([]byte(`{"validityPeriod":10,"nfInstances":[` + profile + `]}`)); err != nil {
			t.Error(err)
		}
	}))
	defer nrf.Close()

	self := afContext.AF_Self()
	self.NrfUri, self.PcfUri = nrf.URL, ""
	uri, err := DiscoverPcfUri(context.Background())
	if err != nil || uri != "http://10.1.1.1:8000" {
		t.Fatalf("got (%q, %v); query %s", uri, err, query)
	}
	for _, want := range []string{"target-nf-type=PCF", "requester-nf-type=AF", "service-names=npcf-policyauthorization"} {
		if !strings.Contains(query, want) {
			t.Errorf("query %q lacks %s", query, want)
		}
	}

	self.PcfUri = "http://static-pcf:1"
	if uri, err = DiscoverPcfUri(context.Background()); err != nil || uri != "http://static-pcf:1" {
		t.Errorf("static PCF not preferred: %q %v", uri, err)
	}
}
