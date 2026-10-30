// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package factory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func load(t *testing.T, content string) error {
	t.Helper()
	path := filepath.Join(t.TempDir(), "af.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return InitConfigFactory(path)
}

func TestDefaults(t *testing.T) {
	if err := load(t, "info:\n  version: 1.0.0\n"); err != nil {
		t.Fatal(err)
	}
	c := AfConfig.Configuration
	if c.Sbi.Scheme != "http" || c.Sbi.RegisterIPv4 != AF_DEFAULT_IPV4 || c.Sbi.BindingIPv4 != AF_DEFAULT_IPV4 ||
		c.Sbi.Port != AF_DEFAULT_SBI_PORT || c.Northbound.Port != AF_DEFAULT_NORTHBOUND ||
		c.Metrics.Port != AF_DEFAULT_METRICS_PORT || c.Metrics.BindingIPv4 != AF_DEFAULT_IPV4 ||
		c.NrfUri != AF_DEFAULT_NRFURI || c.Media.SuppFeat != AF_DEFAULT_SUPP_FEAT || c.Media.Dnn != "ims" ||
		c.Media.Audio.QosReference != "qosVoNR" || c.Media.Video.MarBwDl != "2048 Kbps" || len(c.Media.Events) != 2 {
		t.Errorf("unexpected defaults: %+v sbi=%+v media=%+v", c, c.Sbi, c.Media)
	}
	if err := CheckConfigVersion(); err != nil {
		t.Error(err)
	}
}

func TestOverrides(t *testing.T) {
	err := load(t, `
info:
  version: 1.0.0
configuration:
  nrfUri: https://nrf:29510
  pcfUri: http://pcf:8000
  sbi:
    registerIPv4: 10.0.0.5
    port: 9000
  metrics:
    bindingIPv4: 127.0.0.1
    port: 9100
  media:
    dnn: internet
    audio:
      marBwDl: 64 Kbps
    events:
      - event: ANI_REPORT
`)
	if err != nil {
		t.Fatal(err)
	}
	c := AfConfig.Configuration
	if c.NrfUri != "https://nrf:29510" || c.PcfUri != "http://pcf:8000" || c.Sbi.BindingIPv4 != "10.0.0.5" ||
		c.Sbi.Port != 9000 || c.Metrics.Port != 9100 || c.Metrics.BindingIPv4 != "127.0.0.1" || c.Media.Dnn != "internet" || c.Media.Audio.MarBwDl != "64 Kbps" ||
		c.Media.Audio.MarBwUl != "128 Kbps" || len(c.Media.Events) != 1 {
		t.Errorf("overrides not kept: %+v %+v", c, c.Media)
	}
}

func TestInvalid(t *testing.T) {
	for name, tc := range map[string]struct{ cfg, want string }{
		"bad nrf":                 {"configuration:\n  nrfUri: nrf:8000\n", "nrfUri"},
		"bad scheme":              {"configuration:\n  sbi:\n    scheme: ftp\n", "scheme"},
		"https/no tls":            {"configuration:\n  sbi:\n    scheme: https\n", "tls"},
		"bad event":               {"configuration:\n  media:\n    events:\n      - event: NOPE\n", "event"},
		"bad method":              {"configuration:\n  media:\n    events:\n      - event: ANI_REPORT\n        notifMethod: NOPE\n", "notifMethod"},
		"bad pcf":                 {"configuration:\n  pcfUri: ://x\n", "pcfUri"},
		"periodic without period": {"configuration:\n  media:\n    events:\n      - event: QOS_NOTIF\n        notifMethod: PERIODIC\n", "repPeriod"},
		"negative period":         {"configuration:\n  media:\n    events:\n      - event: QOS_NOTIF\n        repPeriod: -1\n", "repPeriod"},
	} {
		err := load(t, tc.cfg)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want error containing %q", name, err, tc.want)
		}
	}
}

func TestWrongVersion(t *testing.T) {
	if err := load(t, "info:\n  version: 9.9.9\n"); err != nil {
		t.Fatal(err)
	}
	if err := CheckConfigVersion(); err == nil {
		t.Error("version mismatch not detected")
	}
}
