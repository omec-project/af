// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package consumer

import (
	"context"

	afContext "github.com/omec-project/af/internal/context"
	"github.com/omec-project/af/internal/logger"
	"github.com/omec-project/openapi/v2"
	"github.com/omec-project/openapi/v2/models"
	"github.com/omec-project/openapi/v2/nrfclient"
)

func newNrfClient(nrfUri string) *nrfclient.Client {
	return nrfclient.New(nrfUri, nrfclient.WithLogger(logger.ConsumerLog))
}

// getNfProfile builds the NF profile of the AF. The AF is a pure consumer of
// Npcf_PolicyAuthorization (it only receives notifications), so it exposes no NF service.
func getNfProfile(self *afContext.AFContext) (profile models.NFProfile, err error) {
	if self == nil {
		return profile, openapi.ReportError("AF context has not been initialized. NF profile cannot be built")
	}
	if self.RegisterIPv4 == "" {
		return profile, openapi.ReportError("AF address is empty")
	}
	profile.SetNfInstanceId(self.NfId())
	profile.SetNfType(models.NFTYPE_AF)
	profile.SetNfStatus(models.NFSTATUS_REGISTERED)
	profile.SetIpv4Addresses([]string{self.RegisterIPv4})
	profile.SetAllowedNfTypes([]models.NFType{models.NFTYPE_PCF})
	return profile, nil
}

var SendRegisterNFInstance = func(ctx context.Context) (prof *models.NFProfile, err error) {
	self := afContext.AF_Self()
	nfProfile, err := getNfProfile(self)
	if err != nil {
		return models.NewNFProfileWithDefaults(), err
	}

	result, err := newNrfClient(self.NrfUri).Register(ctx, nfProfile)
	if err != nil {
		return models.NewNFProfileWithDefaults(), err
	}
	if result.Created {
		logger.ConsumerLog.Debugln("AF NF profile registered to the NRF")
		if result.NfInstanceID != "" {
			self.SetNfId(result.NfInstanceID)
		}
	} else {
		logger.ConsumerLog.Debugln("AF NF profile updated with complete replacement")
	}
	return result.Profile, nil
}

var SendDeregisterNFInstance = func(ctx context.Context) error {
	logger.ConsumerLog.Infoln("send Deregister NFInstance")
	self := afContext.AF_Self()

	return newNrfClient(self.NrfUri).Deregister(ctx, self.NfId())
}

// SendUpdateNFInstance sends the heartbeat. A problem details result means the NRF answered with an error.
var SendUpdateNFInstance = func(ctx context.Context, patchItem []models.PatchItem) (receivedNfProfile *models.NFProfile, problemDetails *models.ProblemDetails, err error) {
	logger.ConsumerLog.Debugln("send Update NFInstance")
	self := afContext.AF_Self()

	return newNrfClient(self.NrfUri).Update(ctx, self.NfId(), patchItem)
}
