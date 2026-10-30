// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package callback implements the notification endpoints the PCF calls on the AF
// (Npcf_PolicyAuthorization callbacks, TS 29.514 clause 5.5).
package callback

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/omec-project/af/internal/consumer"
	afContext "github.com/omec-project/af/internal/context"
	"github.com/omec-project/af/internal/logger"
	"github.com/omec-project/af/internal/metrics"
	"github.com/omec-project/af/internal/util"
	"github.com/omec-project/openapi/v2"
	"github.com/omec-project/openapi/v2/models"
	"github.com/omec-project/openapi/v2/utils"
)

const terminateDeleteTimeout = 10 * time.Second

// AddService registers the callback routes: <notifUri>/notify and <notifUri>/terminate, where
// notifUri is <sbi uri>/naf-callback/v1/app-sessions/<callbackId>.
func AddService(engine *gin.Engine) *gin.RouterGroup {
	group := engine.Group(afContext.CallbackBasePath)
	group.POST("/:callbackId/notify", HTTPEventNotification)
	group.POST("/:callbackId/terminate", HTTPTerminationRequest)
	return group
}

func lookup(c *gin.Context, notifType string) (*afContext.AppSession, bool) {
	callbackId := c.Param("callbackId")
	session, ok := afContext.AF_Self().FindSessionByCallbackId(callbackId)
	if !ok {
		metrics.IncNotification(notifType, metrics.ResultUnknownSession)
		logger.CallbackLog.Warnf("notification for unknown callback id %s", callbackId)
		problem := utils.ProblemDetails("Not Found", http.StatusNotFound, "app session context not found")
		problem.SetCause("CONTEXT_NOT_FOUND")
		c.JSON(http.StatusNotFound, problem)
		return nil, false
	}
	return session, true
}

func decode(c *gin.Context, notifType string, v any) bool {
	body, err := c.GetRawData()
	if err == nil {
		err = openapi.Decode(v, body, "application/json")
	}
	if err != nil {
		metrics.IncNotification(notifType, metrics.ResultMalformed)
		logger.CallbackLog.Errorf("malformed notification: %v", err)
		if util.IsBodyTooLarge(err) {
			c.JSON(http.StatusRequestEntityTooLarge, utils.ProblemDetails("Payload Too Large", http.StatusRequestEntityTooLarge, "request body too large"))
			return false
		}
		c.JSON(http.StatusBadRequest, utils.ProblemDetailsMalformedRequestSyntax("[Request Body] "+err.Error()))
		return false
	}
	return true
}

// HTTPEventNotification handles Npcf_PolicyAuthorization_Notify (TS 29.514 clause 5.5.2).
func HTTPEventNotification(c *gin.Context) {
	session, ok := lookup(c, metrics.NotifEvent)
	if !ok {
		return
	}
	var notif models.EventsNotification
	if !decode(c, metrics.NotifEvent, &notif) {
		return
	}
	metrics.IncNotification(metrics.NotifEvent, metrics.ResultSuccess)
	for _, ev := range notif.EvNotifs {
		logger.CallbackLog.Infof("event %s for app session %s (MSISDN %s)", ev.Event, session.AppSessionId, session.Msisdn)
	}
	if len(notif.EvNotifs) == 0 {
		logger.CallbackLog.Infof("notification without events for app session %s (MSISDN %s)", session.AppSessionId, session.Msisdn)
	}
	c.Status(http.StatusNoContent)
}

// HTTPTerminationRequest handles Npcf_PolicyAuthorization_Notify termination (TS 29.514 clause 5.5.3).
// The PCF asks the AF to end the session; the AF forgets it and deletes the context at the PCF.
func HTTPTerminationRequest(c *gin.Context) {
	session, ok := lookup(c, metrics.NotifTerminate)
	if !ok {
		return
	}
	var info models.TerminationInfo
	if !decode(c, metrics.NotifTerminate, &info) {
		return
	}
	metrics.IncNotification(metrics.NotifTerminate, metrics.ResultSuccess)
	logger.CallbackLog.Infof("termination request (%s) for app session %s (MSISDN %s)",
		info.TermCause, session.AppSessionId, session.Msisdn)

	afContext.AF_Self().RemoveSession(session.Msisdn, session.CallbackId)
	c.Status(http.StatusNoContent)

	if session.AppSessionId == "" {
		// The create is still in flight; it finds the session gone and deletes the context itself.
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), terminateDeleteTimeout)
		defer cancel()
		// 404 is expected when the PCF already dropped the context.
		if err := consumer.SendDeleteAppSession(ctx, session.PcfApiRoot, session.AppSessionId); err != nil {
			logger.CallbackLog.Debugf("delete of terminated app session %s: %v", session.AppSessionId, err)
		}
	}()
}
