// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package metrics exposes the Prometheus metrics of the AF.
package metrics

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// labelOperation is the label of the metrics that count the requests to the PCF and the NRF.
const (
	labelOperation = "operation"
	labelResult    = "result"
)

// Values of the interface label of the HTTP metrics.
const (
	InterfaceNorthbound = "northbound"
	InterfaceSbi        = "sbi"
)

// Values of the operation label of the PCF metrics.
const (
	OpCreate = "create"
	OpUpdate = "update"
	OpDelete = "delete"
)

// Values of the operation label of the NRF metrics.
const (
	NrfRegister   = "register"
	NrfHeartbeat  = "heartbeat"
	NrfDeregister = "deregister"
)

// Values of the type label of the notification metric.
const (
	NotifEvent     = "notify"
	NotifTerminate = "terminate"
)

// Values of the result label.
const (
	ResultSuccess = "success"
	ResultFailure = "failure"
	// ResultUnknownSession: a notification for a callback id the AF does not know.
	ResultUnknownSession = "unknown_session"
	// ResultMalformed: a notification whose body could not be decoded.
	ResultMalformed = "malformed"
)

var (
	httpRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "af_http_requests",
		Help: "Counter of HTTP requests received by the AF, by interface, route and status code",
	}, []string{"interface", "route", "code"})

	httpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "af_http_request_duration_seconds",
		Help:    "Duration of the HTTP requests received by the AF, by interface and route",
		Buckets: prometheus.DefBuckets,
	}, []string{"interface", "route"})

	pcfRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "af_pcf_requests",
		Help: "Counter of Npcf_PolicyAuthorization requests sent to the PCF, by operation and result",
	}, []string{labelOperation, labelResult})

	pcfDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "af_pcf_request_duration_seconds",
		Help:    "Duration of the Npcf_PolicyAuthorization requests sent to the PCF, by operation",
		Buckets: prometheus.DefBuckets,
	}, []string{labelOperation})

	notifications = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "af_pcf_notifications",
		Help: "Counter of notifications received from the PCF, by type and result",
	}, []string{"type", labelResult})

	nrfRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "af_nrf_requests",
		Help: "Counter of NF management requests sent to the NRF, by operation and result",
	}, []string{labelOperation, labelResult})

	nrfRegistered = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "af_nrf_registered",
		Help: "1 while the AF is registered at the NRF, 0 otherwise",
	})
)

func init() {
	prometheus.MustRegister(httpRequests, httpDuration, pcfRequests, pcfDuration, notifications, nrfRequests, nrfRegistered)
}

var (
	appSessionsMu        sync.Mutex
	appSessionsCollector prometheus.Collector
)

// RegisterAppSessions exports the number of app sessions the AF holds, read at each scrape.
// Calling it again replaces the previous gauge.
func RegisterAppSessions(count func() int) error {
	appSessionsMu.Lock()
	defer appSessionsMu.Unlock()
	if appSessionsCollector != nil {
		prometheus.Unregister(appSessionsCollector)
		appSessionsCollector = nil
	}
	collector := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "af_app_sessions",
		Help: "Number of application sessions the AF holds at the PCF",
	}, func() float64 { return float64(count()) })
	if err := prometheus.Register(collector); err != nil {
		return err
	}
	appSessionsCollector = collector
	return nil
}

// Handler serves the metrics in the Prometheus exposition format.
func Handler() http.Handler {
	return promhttp.Handler()
}

// HTTP returns a gin middleware that counts and times the requests of an interface. The route label is
// the registered route pattern, so it stays bounded; requests that match no route are labelled "unmatched".
func HTTP(iface string) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		httpRequests.WithLabelValues(iface, route, strconv.Itoa(c.Writer.Status())).Inc()
		httpDuration.WithLabelValues(iface, route).Observe(time.Since(start).Seconds())
	}
}

// ObservePcfRequest records a request to the PCF that started at start and ended with err.
func ObservePcfRequest(operation string, start time.Time, err error) {
	pcfRequests.WithLabelValues(operation, result(err)).Inc()
	pcfDuration.WithLabelValues(operation).Observe(time.Since(start).Seconds())
}

// IncNotification counts a notification received from the PCF.
func IncNotification(notifType, result string) {
	notifications.WithLabelValues(notifType, result).Inc()
}

// ObserveNrfRequest records a request to the NRF that ended with err.
func ObserveNrfRequest(operation string, err error) {
	nrfRequests.WithLabelValues(operation, result(err)).Inc()
}

// SetNrfRegistered records whether the AF is registered at the NRF.
func SetNrfRegistered(registered bool) {
	if registered {
		nrfRegistered.Set(1)
		return
	}
	nrfRegistered.Set(0)
}

func result(err error) string {
	if err != nil {
		return ResultFailure
	}
	return ResultSuccess
}
