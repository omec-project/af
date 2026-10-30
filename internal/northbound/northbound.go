// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package northbound is the interface where the IMS (P-CSCF) asks the AF for QoS. It keeps the
// routes and field names of the N5 proxy of aether-docker-components/ims/n5 so the P-CSCF
// configuration works unchanged.
package northbound

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/omec-project/af/internal/consumer"
	"github.com/omec-project/af/internal/logger"
	"github.com/omec-project/af/internal/producer"
	"github.com/omec-project/af/internal/util"
)

// flexString accepts a JSON string or number; the P-CSCF sends numbers as strings.
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*f = flexString(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return fmt.Errorf("expected a string or a number")
	}
	*f = flexString(n.String())
	return nil
}

type mediaBody struct {
	UeIp          flexString `json:"fromUEIP"`
	AudioPort     flexString `json:"fromUEAUDIOPORT"`
	RtcpPort      flexString `json:"fromUERTCPPORT"`
	VideoPort     flexString `json:"fromUEVIDEOPORT"`
	VideoRtcpPort flexString `json:"fromUEVIDEORTCPPORT"`
	Msisdn        flexString `json:"fromUEMSISDN"`
}

func (b *mediaBody) request(audio, video bool) (producer.MediaRequest, error) {
	req := producer.MediaRequest{Msisdn: string(b.Msisdn), UeIpv4: string(b.UeIp)}
	var err error
	parse := func(name string, v flexString, dst *int) {
		if err != nil || v == "" {
			return
		}
		n, convErr := strconv.Atoi(string(v))
		if convErr != nil {
			err = fmt.Errorf("%s %q is not a number", name, v)
			return
		}
		*dst = n
	}
	if audio {
		parse("fromUEAUDIOPORT", b.AudioPort, &req.AudioPort)
		parse("fromUERTCPPORT", b.RtcpPort, &req.AudioRtcpPort)
	}
	if video {
		parse("fromUEVIDEOPORT", b.VideoPort, &req.VideoPort)
		parse("fromUEVIDEORTCPPORT", b.VideoRtcpPort, &req.VideoRtcpPort)
	}
	return req, err
}

type response struct {
	Result       string   `json:"result"`
	AppSessionId string   `json:"appSessionId,omitempty"`
	Failed       []string `json:"failed,omitempty"`
}

// AddService registers the routes under /api/nf. When authToken is not empty the requests must
// carry it as a bearer token.
func AddService(engine *gin.Engine, authToken string) *gin.RouterGroup {
	group := engine.Group("/api/nf")
	if authToken != "" {
		group.Use(util.RequireBearer(authToken))
	}
	group.POST("/createPDUAudio", createPDUAudio)
	group.POST("/createPDUVideo", createPDUVideo)
	group.POST("/patchPDU", patchPDU)
	group.POST("/deletePDU", deletePDU)
	return group
}

func bind(c *gin.Context) (*mediaBody, bool) {
	var body mediaBody
	if err := c.ShouldBindJSON(&body); err != nil {
		if util.IsBodyTooLarge(err) {
			c.JSON(http.StatusRequestEntityTooLarge, response{Result: "request body too large"})
			return nil, false
		}
		c.JSON(http.StatusBadRequest, response{Result: "malformed request body: " + err.Error()})
		return nil, false
	}
	return &body, true
}

func createPDUAudio(c *gin.Context) {
	body, ok := bind(c)
	if !ok {
		return
	}
	req, err := body.request(true, false)
	if err != nil {
		c.JSON(http.StatusBadRequest, response{Result: err.Error()})
		return
	}
	if req.AudioPort == 0 {
		c.JSON(http.StatusBadRequest, response{Result: "fromUEAUDIOPORT is missing"})
		return
	}
	create(c, req, "audio")
}

func createPDUVideo(c *gin.Context) {
	body, ok := bind(c)
	if !ok {
		return
	}
	req, err := body.request(true, true)
	if err != nil {
		c.JSON(http.StatusBadRequest, response{Result: err.Error()})
		return
	}
	if req.VideoPort == 0 {
		c.JSON(http.StatusBadRequest, response{Result: "fromUEVIDEOPORT is missing"})
		return
	}
	create(c, req, "video")
}

func create(c *gin.Context, req producer.MediaRequest, kind string) {
	session, err := producer.CreateAppSession(c.Request.Context(), req)
	if err != nil {
		fail(c, "create "+kind+" app session", err)
		return
	}
	c.JSON(http.StatusCreated, response{Result: kind + " app session created", AppSessionId: session.AppSessionId})
}

func patchPDU(c *gin.Context) {
	body, ok := bind(c)
	if !ok {
		return
	}
	req, err := body.request(true, true)
	if err != nil {
		c.JSON(http.StatusBadRequest, response{Result: err.Error()})
		return
	}
	session, err := producer.ModifyAppSession(c.Request.Context(), req)
	if err != nil {
		fail(c, "update app session", err)
		return
	}
	c.JSON(http.StatusOK, response{Result: "app session updated", AppSessionId: session.AppSessionId})
}

func deletePDU(c *gin.Context) {
	body, ok := bind(c)
	if !ok {
		return
	}
	deleted, failed, err := producer.DeleteAppSessions(c.Request.Context(), string(body.Msisdn))
	if err != nil {
		if len(failed) > 0 {
			logger.NorthboundLog.Errorf("delete app sessions: %v", err)
			c.JSON(http.StatusBadGateway, response{Result: err.Error(), Failed: failed})
			return
		}
		fail(c, "delete app sessions", err)
		return
	}
	c.JSON(http.StatusOK, response{Result: fmt.Sprintf("%d app session(s) deleted", deleted)})
}

// fail maps the error of a producer function to the HTTP status the P-CSCF sees. The detail of
// the error (PCF answers, addresses) is logged, not returned.
func fail(c *gin.Context, op string, err error) {
	logger.NorthboundLog.Errorf("%s: %v", op, err)
	var pcfErr *consumer.PcfError
	status := http.StatusBadGateway
	result := op + " failed"
	switch {
	case errors.Is(err, producer.ErrInvalidRequest):
		status = http.StatusBadRequest
		result = err.Error() // describes the request of the P-CSCF
	case errors.Is(err, producer.ErrNoSession):
		status = http.StatusNotFound
		result = err.Error()
	case errors.As(err, &pcfErr):
		result = fmt.Sprintf("%s: the PCF answered %d", op, pcfErr.StatusCode)
		// The PCF rejected the request: tell the P-CSCF it is a client side error when the PCF said so.
		// 401 and 403 are about the AF's credentials, not the P-CSCF's, so they stay a bad gateway.
		if pcfErr.StatusCode >= 400 && pcfErr.StatusCode < 500 &&
			pcfErr.StatusCode != http.StatusUnauthorized && pcfErr.StatusCode != http.StatusForbidden {
			status = pcfErr.StatusCode
		}
	}
	c.JSON(status, response{Result: result})
}
