// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package factory

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/omec-project/af/internal/logger"
	"github.com/omec-project/openapi/v2/models"
	"go.yaml.in/yaml/v4"
)

const schemeHTTP = "http"

var AfConfig Config

func InitConfigFactory(f string) error {
	content, err := os.ReadFile(f)
	if err != nil {
		return err
	}
	var cfg Config
	if err = yaml.Unmarshal(content, &cfg); err != nil {
		return err
	}
	if cfg.Configuration == nil {
		cfg.Configuration = &Configuration{}
	}
	cfg.Configuration.applyDefaults()
	if err = cfg.Configuration.validate(); err != nil {
		return err
	}
	AfConfig = cfg
	return nil
}

func CheckConfigVersion() error {
	currentVersion := AfConfig.GetVersion()
	if currentVersion != AF_EXPECTED_CONFIG_VERSION {
		return fmt.Errorf("config version is [%s], but expected is [%s]",
			currentVersion, AF_EXPECTED_CONFIG_VERSION)
	}
	logger.CfgLog.Infof("config version [%s]", currentVersion)
	return nil
}

func (c *Configuration) applyDefaults() {
	if c.AfName == "" {
		c.AfName = "AF"
	}
	if c.NrfUri == "" {
		c.NrfUri = AF_DEFAULT_NRFURI
		logger.CfgLog.Infof("nrfUri not set in configuration file. Using %s", c.NrfUri)
	}
	if c.Sbi == nil {
		c.Sbi = &Sbi{}
	}
	if c.Sbi.Scheme == "" {
		c.Sbi.Scheme = schemeHTTP
	}
	if c.Sbi.RegisterIPv4 == "" {
		c.Sbi.RegisterIPv4 = AF_DEFAULT_IPV4
	}
	if c.Sbi.BindingIPv4 == "" {
		c.Sbi.BindingIPv4 = c.Sbi.RegisterIPv4
	}
	if c.Sbi.Port == 0 {
		c.Sbi.Port = AF_DEFAULT_SBI_PORT
	}
	if c.Northbound == nil {
		c.Northbound = &Northbound{}
	}
	if c.Northbound.BindingIPv4 == "" {
		c.Northbound.BindingIPv4 = c.Sbi.BindingIPv4
	}
	if c.Northbound.Port == 0 {
		c.Northbound.Port = AF_DEFAULT_NORTHBOUND
	}
	if c.Metrics == nil {
		c.Metrics = &Metrics{}
	}
	if c.Metrics.BindingIPv4 == "" {
		c.Metrics.BindingIPv4 = c.Sbi.BindingIPv4
	}
	if c.Metrics.Port == 0 {
		c.Metrics.Port = AF_DEFAULT_METRICS_PORT
	}
	if c.Media == nil {
		c.Media = &Media{}
	}
	m := c.Media
	setDefault(&m.Dnn, AF_DEFAULT_DNN)
	setDefault(&m.AfAppId, AF_DEFAULT_AF_APP_ID)
	setDefault(&m.SuppFeat, AF_DEFAULT_SUPP_FEAT)
	if m.Audio == nil {
		m.Audio = &MediaQoS{}
	}
	setDefault(&m.Audio.QosReference, AF_DEFAULT_AUDIO_QOS_REF)
	setDefault(&m.Audio.MarBwDl, "128 Kbps")
	setDefault(&m.Audio.MarBwUl, "128 Kbps")
	setDefault(&m.Audio.RtcpMarBwDl, "128 Kbps")
	setDefault(&m.Audio.RtcpMarBwUl, "128 Kbps")
	if m.Video == nil {
		m.Video = &MediaQoS{}
	}
	setDefault(&m.Video.QosReference, AF_DEFAULT_VIDEO_QOS_REF)
	setDefault(&m.Video.MarBwDl, "2048 Kbps")
	setDefault(&m.Video.MarBwUl, "2048 Kbps")
	setDefault(&m.Video.RtcpMarBwDl, "128 Kbps")
	setDefault(&m.Video.RtcpMarBwUl, "128 Kbps")
	if len(m.Events) == 0 {
		m.Events = []EventConf{
			{
				Event: string(models.AFEVENTPCF_QOS_NOTIF), NotifMethod: string(models.AFNOTIFMETHOD_PERIODIC),
				RepPeriod: AF_DEFAULT_REP_PERIOD,
			},
			{Event: string(models.AFEVENTPCF_ANI_REPORT), NotifMethod: string(models.AFNOTIFMETHOD_ONE_TIME)},
		}
	}
}

func setDefault(field *string, def string) {
	if *field == "" {
		*field = def
	}
}

func (c *Configuration) validate() error {
	if err := validateUri("nrfUri", c.NrfUri); err != nil {
		return err
	}
	if c.PcfUri != "" {
		if err := validateUri("pcfUri", c.PcfUri); err != nil {
			return err
		}
	}
	if c.Northbound.AuthTokenFile != "" {
		token, err := os.ReadFile(c.Northbound.AuthTokenFile)
		if err != nil {
			return fmt.Errorf("northbound.authTokenFile: %w", err)
		}
		c.Northbound.AuthToken = strings.TrimSpace(string(token))
		if c.Northbound.AuthToken == "" {
			return fmt.Errorf("northbound.authTokenFile %s is empty", c.Northbound.AuthTokenFile)
		}
	}
	switch c.Sbi.Scheme {
	case schemeHTTP:
	case "https":
		if c.Sbi.TLS == nil || c.Sbi.TLS.PEM == "" || c.Sbi.TLS.Key == "" {
			return fmt.Errorf("sbi.tls.pem and sbi.tls.key are required when sbi.scheme is https")
		}
	default:
		return fmt.Errorf("unsupported sbi scheme: %s", c.Sbi.Scheme)
	}
	for _, ev := range c.Media.Events {
		if !models.AfEventPcf(ev.Event).IsValid() {
			return fmt.Errorf("invalid media event: %s", ev.Event)
		}
		if ev.NotifMethod != "" && !models.AfNotifMethod(ev.NotifMethod).IsValid() {
			return fmt.Errorf("invalid media event notifMethod: %s", ev.NotifMethod)
		}
		if ev.RepPeriod < 0 {
			return fmt.Errorf("invalid media event repPeriod for %s: %d", ev.Event, ev.RepPeriod)
		}
		if models.AfNotifMethod(ev.NotifMethod) == models.AFNOTIFMETHOD_PERIODIC && ev.RepPeriod == 0 {
			return fmt.Errorf("media event %s: repPeriod is required when notifMethod is PERIODIC", ev.Event)
		}
	}
	return nil
}

func validateUri(name, uri string) error {
	parsedUrl, err := url.ParseRequestURI(uri)
	if err != nil {
		return fmt.Errorf("invalid %s: %w", name, err)
	}
	if parsedUrl.Scheme != schemeHTTP && parsedUrl.Scheme != "https" {
		return fmt.Errorf("unsupported scheme for %s: %s", name, parsedUrl.Scheme)
	}
	if parsedUrl.Hostname() == "" {
		return fmt.Errorf("missing host in %s", name)
	}
	return nil
}
