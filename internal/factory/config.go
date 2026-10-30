// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package factory

const (
	AF_EXPECTED_CONFIG_VERSION = "1.0.0"

	AF_DEFAULT_IPV4         = "127.0.0.201"
	AF_DEFAULT_SBI_PORT     = 8000
	AF_DEFAULT_NORTHBOUND   = 7782
	AF_DEFAULT_METRICS_PORT = 8080
	AF_DEFAULT_NRFURI       = "http://127.0.0.10:8000"
	// MediaComponentVersioning (3), IMS_SBI (5), NetLoc (6, for ANI_REPORT),
	// EnhancedSubscriptionToNotification (15, for PERIODIC events) and
	// AuthorizationWithRequiredQoS (17), TS 29.514 table 5.8-1. Feature n is the bit of value 2^(n-1).
	AF_DEFAULT_SUPP_FEAT     = "14034"
	AF_DEFAULT_DNN           = "ims"
	AF_DEFAULT_AF_APP_ID     = `+g.3gpp.icsi-ref="urn%3Aurn-7%3A3gpp-service.ims.icsi.mmtel"`
	AF_DEFAULT_AUDIO_QOS_REF = "qosVoNR"
	AF_DEFAULT_VIDEO_QOS_REF = "qosViNR"
	AF_DEFAULT_REP_PERIOD    = 60 // seconds, for the PERIODIC events of the default subscription
)

type Config struct {
	Info          *Info          `yaml:"info"`
	Configuration *Configuration `yaml:"configuration"`
	Logger        *Logger        `yaml:"logger,omitempty"`
	CfgLocation   string         `yaml:"-"`
}

type Info struct {
	Version     string `yaml:"version,omitempty"`
	Description string `yaml:"description,omitempty"`
}

type Logger struct {
	// Level is one of panic|fatal|error|warn|info|debug.
	Level string `yaml:"level,omitempty"`
}

type Configuration struct {
	AfName string `yaml:"afName,omitempty"`
	// NfInstanceId is optional; a random UUID is generated when empty.
	NfInstanceId string `yaml:"nfInstanceId,omitempty"`
	NrfUri       string `yaml:"nrfUri,omitempty"`
	// PcfUri is optional. When set, the PCF is not discovered through the NRF.
	PcfUri     string      `yaml:"pcfUri,omitempty"`
	Sbi        *Sbi        `yaml:"sbi,omitempty"`
	Northbound *Northbound `yaml:"northbound,omitempty"`
	Metrics    *Metrics    `yaml:"metrics,omitempty"`
	Media      *Media      `yaml:"media,omitempty"`
}

// Sbi is the service based interface of the AF: it receives the Npcf_PolicyAuthorization
// notifications (TS 29.514 clause 5.5) and is the address registered at the NRF.
type Sbi struct {
	Scheme       string `yaml:"scheme"`
	RegisterIPv4 string `yaml:"registerIPv4,omitempty"` // IP that is registered at NRF and sent to the PCF.
	BindingIPv4  string `yaml:"bindingIPv4,omitempty"`  // IP used to run the server in the node.
	Port         int    `yaml:"port,omitempty"`
	TLS          *TLS   `yaml:"tls,omitempty"`
}

type TLS struct {
	PEM string `yaml:"pem,omitempty"`
	Key string `yaml:"key,omitempty"`
}

// Northbound is the interface where the IMS (P-CSCF) asks the AF for QoS.
type Northbound struct {
	BindingIPv4 string `yaml:"bindingIPv4,omitempty"`
	Port        int    `yaml:"port,omitempty"`
	// AuthTokenFile is optional. When set, the requests must carry "Authorization: Bearer <token>",
	// where the token is the content of the file (e.g. a mounted Secret). Without it the interface is open.
	AuthTokenFile string `yaml:"authTokenFile,omitempty"`

	AuthToken string `yaml:"-"` // content of AuthTokenFile
}

// Metrics is the endpoint Prometheus scrapes (GET /metrics).
type Metrics struct {
	BindingIPv4 string `yaml:"bindingIPv4,omitempty"`
	Port        int    `yaml:"port,omitempty"`
}

// Media holds the policy authorization parameters the AF puts in the app session contexts.
type Media struct {
	Dnn      string      `yaml:"dnn,omitempty"`
	AfAppId  string      `yaml:"afAppId,omitempty"`
	SuppFeat string      `yaml:"suppFeat,omitempty"`
	Audio    *MediaQoS   `yaml:"audio,omitempty"`
	Video    *MediaQoS   `yaml:"video,omitempty"`
	Events   []EventConf `yaml:"events,omitempty"`
}

type MediaQoS struct {
	QosReference string `yaml:"qosReference,omitempty"`
	MarBwDl      string `yaml:"marBwDl,omitempty"`     // e.g. "128 Kbps"
	MarBwUl      string `yaml:"marBwUl,omitempty"`     // e.g. "128 Kbps"
	RtcpMarBwDl  string `yaml:"rtcpMarBwDl,omitempty"` // bandwidth of the RTCP flows
	RtcpMarBwUl  string `yaml:"rtcpMarBwUl,omitempty"`
}

type EventConf struct {
	Event       string `yaml:"event"`                 // AfEventPcf, e.g. QOS_NOTIF
	NotifMethod string `yaml:"notifMethod,omitempty"` // EVENT_DETECTION|ONE_TIME|PERIODIC
	RepPeriod   int32  `yaml:"repPeriod,omitempty"`   // seconds between notifications; required for PERIODIC
}

func (c *Config) GetVersion() string {
	if c.Info != nil && c.Info.Version != "" {
		return c.Info.Version
	}
	return ""
}
