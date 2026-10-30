// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package service

import (
	ctxt "context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/omec-project/af/internal/callback"
	afContext "github.com/omec-project/af/internal/context"
	"github.com/omec-project/af/internal/factory"
	"github.com/omec-project/af/internal/logger"
	"github.com/omec-project/af/internal/metrics"
	"github.com/omec-project/af/internal/nfregistration"
	"github.com/omec-project/af/internal/northbound"
	"github.com/omec-project/af/internal/util"
	"github.com/omec-project/util/http2_util"
	utilLogger "github.com/omec-project/util/logger"
	"github.com/urfave/cli/v3"
	"go.uber.org/zap/zapcore"
)

const shutdownTimeout = 10 * time.Second

type AF struct{}

var afCli = []cli.Flag{
	&cli.StringFlag{
		Name:     "cfg",
		Usage:    "af config file",
		Required: true,
	},
}

func (*AF) GetCliCmd() (flags []cli.Flag) {
	return afCli
}

func (af *AF) Initialize(_ ctxt.Context, c *cli.Command) error {
	absPath, err := filepath.Abs(c.String("cfg"))
	if err != nil {
		logger.CfgLog.Errorln(err)
		return err
	}
	if err := factory.InitConfigFactory(absPath); err != nil {
		return err
	}
	if err := factory.CheckConfigVersion(); err != nil {
		return err
	}
	factory.AfConfig.CfgLocation = absPath
	af.setLogLevel()
	return nil
}

func (af *AF) setLogLevel() {
	cfgLogger := factory.AfConfig.Logger
	if cfgLogger == nil || cfgLogger.Level == "" {
		logger.InitLog.Warnln("AF config without log level setting")
		return
	}
	level, err := zapcore.ParseLevel(cfgLogger.Level)
	if err != nil {
		logger.InitLog.Warnf("invalid log level %q: %v", cfgLogger.Level, err)
		return
	}
	logger.SetLogLevel(level)
}

// Timeouts of the servers. The write timeout covers the handlers of the northbound interface, which
// wait for the PCF.
const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 30 * time.Second
	writeTimeout      = 60 * time.Second
)

func newServer(addr string, handler http.Handler) (*http.Server, error) {
	server, err := http2_util.NewServer(addr, os.Getenv("SSLKEYLOGFILE"), handler)
	if server == nil {
		return nil, fmt.Errorf("initialize HTTP server failed: %w", err)
	}
	server.ReadHeaderTimeout = readHeaderTimeout
	server.ReadTimeout = readTimeout
	server.WriteTimeout = writeTimeout
	if err != nil {
		logger.InitLog.Warnf("initialize HTTP server: %+v", err)
	}
	return server, nil
}

// Start runs the AF until it gets SIGINT or SIGTERM, or a server fails.
func (af *AF) Start() error {
	logger.InitLog.Infoln("server started")
	self := afContext.AF_Self()
	afContext.InitAfContext(self)

	// SBI: notifications from the PCF.
	sbiRouter := utilLogger.NewGinWithZap(logger.GinLog)
	sbiRouter.Use(metrics.HTTP(metrics.InterfaceSbi), util.LimitBody(util.MaxBodyBytes))
	callback.AddService(sbiRouter)
	sbiServer, err := newServer(fmt.Sprintf("%s:%d", self.BindingIPv4, self.SBIPort), sbiRouter)
	if err != nil {
		return err
	}

	// Northbound: requests from the P-CSCF.
	nbRouter := utilLogger.NewGinWithZap(logger.GinLog)
	nbRouter.Use(metrics.HTTP(metrics.InterfaceNorthbound), util.LimitBody(util.MaxBodyBytes))
	nbRouter.GET("/healthz", func(c *gin.Context) { c.Status(http.StatusOK) })
	if self.NorthboundAuthToken == "" {
		logger.InitLog.Warnln("northbound.authTokenFile is not set: the northbound interface accepts unauthenticated requests")
	}
	northbound.AddService(nbRouter, self.NorthboundAuthToken)
	nbServer, err := newServer(fmt.Sprintf("%s:%d", self.NorthboundBindingIPv4, self.NorthboundPort), nbRouter)
	if err != nil {
		return err
	}

	// Metrics: scraped by Prometheus.
	if err = metrics.RegisterAppSessions(self.SessionCount); err != nil {
		return fmt.Errorf("register metrics: %w", err)
	}
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", metrics.Handler())
	metricsServer, err := newServer(fmt.Sprintf("%s:%d", self.MetricsBindingIPv4, self.MetricsPort), metricsMux)
	if err != nil {
		return err
	}

	ctx, cancel := ctxt.WithCancel(ctxt.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		nfregistration.StartNfRegistrationService(ctx)
	}()

	serverErr := make(chan error, 3)
	go func() {
		logger.InitLog.Infof("SBI server listening on %s", sbiServer.Addr)
		if self.Scheme == "https" {
			serverErr <- fmt.Errorf("SBI server: %w", sbiServer.ListenAndServeTLS(self.PEM, self.Key))
		} else {
			serverErr <- fmt.Errorf("SBI server: %w", sbiServer.ListenAndServe())
		}
	}()
	go func() {
		logger.InitLog.Infof("northbound server listening on %s", nbServer.Addr)
		serverErr <- fmt.Errorf("northbound server: %w", nbServer.ListenAndServe())
	}()

	go func() {
		logger.InitLog.Infof("metrics server listening on %s", metricsServer.Addr)
		serverErr <- fmt.Errorf("metrics server: %w", metricsServer.ListenAndServe())
	}()

	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signalChannel)

	select {
	case sig := <-signalChannel:
		logger.InitLog.Infof("received %v, terminating AF", sig)
		err = nil
	case err = <-serverErr:
		logger.InitLog.Errorf("%v, terminating AF", err)
	}

	af.Terminate(cancel, &wg, sbiServer, nbServer, metricsServer)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Terminate stops the servers, ends the registration and removes the AF from the NRF.
func (af *AF) Terminate(cancel ctxt.CancelFunc, wg *sync.WaitGroup, servers ...*http.Server) {
	logger.InitLog.Infoln("terminating AF")
	cancel()
	wg.Wait() // registration loop is done; no heartbeat races the deregistration

	ctx, stop := ctxt.WithTimeout(ctxt.Background(), shutdownTimeout)
	defer stop()
	for _, server := range servers {
		if err := server.Shutdown(ctx); err != nil {
			logger.InitLog.Errorf("server %s shutdown: %v", server.Addr, err)
		}
	}
	nfregistration.DeregisterNF(ctx)
	logger.InitLog.Infoln("AF terminated")
}
