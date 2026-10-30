// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package producer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/omec-project/af/internal/consumer"
	afContext "github.com/omec-project/af/internal/context"
	"github.com/omec-project/af/internal/factory"
	"github.com/omec-project/af/internal/logger"
	"github.com/omec-project/af/internal/metrics"
)

var (
	// ErrInvalidRequest is wrapped by the errors caused by the content of the request.
	ErrInvalidRequest = errors.New("invalid request")
	// ErrNoSession means the UE has no app session.
	ErrNoSession = errors.New("no app session")
)

// CreateAppSession creates an app session at the PCF for the media of the request (N5 create).
func CreateAppSession(ctx context.Context, req MediaRequest) (*afContext.AppSession, error) {
	if err := req.validateMedia(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	self := afContext.AF_Self()
	pcfUri, err := consumer.DiscoverPcfUri(ctx)
	if err != nil {
		return nil, err
	}

	callbackId := uuid.NewString()
	body := buildAppSessionContext(req, factory.AfConfig.Configuration.Media, self.NotifUri(callbackId))
	medComps := medCompNs(req, factory.AfConfig.Configuration.Media)

	// The PCF may notify the AF as soon as it has created the context, possibly before its answer
	// arrives, so the callback id is registered first. The session stays locked until the PCF has
	// assigned its ids, which keeps update and delete from using it half built.
	session := &afContext.AppSession{CallbackId: callbackId, Msisdn: req.Msisdn, UeIpv4: req.UeIpv4}
	done := self.AddSessionLocked(session)
	defer done()

	start := time.Now()
	created, err := consumer.SendPostAppSessions(ctx, pcfUri, body)
	metrics.ObservePcfRequest(metrics.OpCreate, start, err)
	if err != nil {
		self.RemoveSession(req.Msisdn, callbackId)
		return nil, err
	}
	completed, ok := self.CompleteSession(callbackId, created.AppSessionId, created.ResourceUri, created.PcfApiRoot, medComps)
	if !ok {
		// The PCF terminated the session before answering the create; it is no longer valid.
		if err := consumer.SendDeleteAppSession(ctx, created.PcfApiRoot, created.AppSessionId); err != nil {
			logger.ConsumerLog.Debugf("delete of app session %s terminated during creation: %v", created.AppSessionId, err)
		}
		return nil, fmt.Errorf("app session %s was terminated by the PCF during creation", created.AppSessionId)
	}
	logger.ConsumerLog.Infof("app session %s created at PCF %s for MSISDN %s", completed.AppSessionId, completed.PcfApiRoot, completed.Msisdn)
	return completed, nil
}

// ModifyAppSession replaces the media of the latest app session of the UE (N5 update).
func ModifyAppSession(ctx context.Context, req MediaRequest) (*afContext.AppSession, error) {
	if err := req.validateMedia(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	self := afContext.AF_Self()
	sessions := self.SessionsOf(req.Msisdn)
	if len(sessions) == 0 {
		return nil, fmt.Errorf("%w for MSISDN %s", ErrNoSession, req.Msisdn)
	}
	session := sessions[len(sessions)-1]

	// One update at a time per session: the patch depends on the components the session has at the PCF.
	defer session.Serialize()()
	session, ok := self.FindSessionByCallbackId(session.CallbackId)
	if !ok {
		return nil, fmt.Errorf("%w for MSISDN %s", ErrNoSession, req.Msisdn)
	}

	patch := buildUpdatePatch(req, factory.AfConfig.Configuration.Media, session.MedCompNs)
	start := time.Now()
	err := consumer.SendModAppSession(ctx, session.PcfApiRoot, session.AppSessionId, patch)
	metrics.ObservePcfRequest(metrics.OpUpdate, start, err)
	if err != nil {
		var pcfErr *consumer.PcfError
		if errors.As(err, &pcfErr) && pcfErr.StatusCode == http.StatusNotFound {
			// The PCF dropped the session (e.g. the PDU session went away) without a terminate callback.
			self.RemoveSession(session.Msisdn, session.CallbackId)
		}
		return nil, err
	}
	self.SetMedCompNs(session.CallbackId, medCompNs(req, factory.AfConfig.Configuration.Media))
	logger.ConsumerLog.Infof("app session %s updated for MSISDN %s", session.AppSessionId, session.Msisdn)
	return session, nil
}

// DeleteAppSessions deletes every app session of the UE (N5 delete). It returns the ids of the
// sessions it could not delete, which stay known to the AF.
func DeleteAppSessions(ctx context.Context, msisdn string) (deleted int, failed []string, err error) {
	if err := validateMsisdn(msisdn); err != nil {
		return 0, nil, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	self := afContext.AF_Self()
	sessions := self.SessionsOf(msisdn)
	if len(sessions) == 0 {
		return 0, nil, fmt.Errorf("%w for MSISDN %s", ErrNoSession, msisdn)
	}
	for _, listed := range sessions {
		id, status := deleteOne(ctx, self, listed)
		switch status {
		case deletedOk:
			deleted++
		case deleteFailed:
			failed = append(failed, id)
		}
	}
	if len(failed) > 0 {
		return deleted, failed, fmt.Errorf("%d app session(s) could not be deleted", len(failed))
	}
	return deleted, nil, nil
}

type deleteStatus int

const (
	deletedOk deleteStatus = iota
	deleteFailed
	deleteSkipped
)

// deleteOne deletes a session at the PCF and forgets it, one operation at a time per session.
func deleteOne(ctx context.Context, self *afContext.AFContext, listed *afContext.AppSession) (string, deleteStatus) {
	defer listed.Serialize()()
	session, ok := self.FindSessionByCallbackId(listed.CallbackId)
	if !ok {
		return "", deleteSkipped // removed meanwhile (terminated by the PCF or deleted by another request)
	}
	start := time.Now()
	err := consumer.SendDeleteAppSession(ctx, session.PcfApiRoot, session.AppSessionId)
	metrics.ObservePcfRequest(metrics.OpDelete, start, err)
	var pcfErr *consumer.PcfError
	if err != nil && (!errors.As(err, &pcfErr) || pcfErr.StatusCode != http.StatusNotFound) {
		logger.ConsumerLog.Errorf("delete of app session %s failed: %v", session.AppSessionId, err)
		return session.AppSessionId, deleteFailed
	}
	self.RemoveSession(session.Msisdn, session.CallbackId)
	logger.ConsumerLog.Infof("app session %s deleted for MSISDN %s", session.AppSessionId, session.Msisdn)
	return session.AppSessionId, deletedOk
}
