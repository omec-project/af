// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package consumer

import (
	"context"
	"net/http"
	"strings"

	afContext "github.com/omec-project/af/internal/context"
	"github.com/omec-project/af/internal/logger"
	"github.com/omec-project/openapi/v2"
	"github.com/omec-project/openapi/v2/Nnrf_NFManagement"
	"github.com/omec-project/openapi/v2/models"
)

func newNfManagementClient(nrfUri string) *Nnrf_NFManagement.APIClient {
	configuration := Nnrf_NFManagement.NewConfiguration()
	serverConfig := &configuration.Servers[0]
	if apiRootVar, exists := serverConfig.Variables["apiRoot"]; exists {
		apiRootVar.DefaultValue = nrfUri
		serverConfig.Variables["apiRoot"] = apiRootVar
	}
	return Nnrf_NFManagement.NewAPIClient(configuration)
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

	client := newNfManagementClient(self.NrfUri)
	request := client.NFInstanceIDDocumentAPI.RegisterNFInstance(ctx, nfProfile.GetNfInstanceId())
	request = request.NFProfile(nfProfile)
	receivedNfProfile, res, err := client.NFInstanceIDDocumentAPI.RegisterNFInstanceExecute(request)
	if err != nil {
		return models.NewNFProfileWithDefaults(), err
	}
	if res == nil {
		return models.NewNFProfileWithDefaults(), openapi.ReportError("no response from server")
	}

	switch res.StatusCode {
	case http.StatusOK: // NFUpdate
		logger.ConsumerLog.Debugln("AF NF profile updated with complete replacement")
		return receivedNfProfile, nil
	case http.StatusCreated: // NFRegister
		if resourceUri := res.Header.Get("Location"); resourceUri != "" {
			self.SetNfId(resourceUri[strings.LastIndex(resourceUri, "/")+1:])
		}
		logger.ConsumerLog.Debugln("AF NF profile registered to the NRF")
		return receivedNfProfile, nil
	default:
		return receivedNfProfile, openapi.ReportError("NRF returned unexpected status code %d", res.StatusCode)
	}
}

var SendDeregisterNFInstance = func(ctx context.Context) error {
	logger.ConsumerLog.Infoln("send Deregister NFInstance")
	self := afContext.AF_Self()

	client := newNfManagementClient(self.NrfUri)
	request := client.NFInstanceIDDocumentAPI.DeregisterNFInstance(ctx, self.NfId())
	res, err := client.NFInstanceIDDocumentAPI.DeregisterNFInstanceExecute(request)
	if err != nil {
		return err
	}
	if res == nil {
		return openapi.ReportError("no response from server")
	}
	if res.StatusCode == http.StatusNoContent {
		return nil
	}
	return openapi.ReportError("unexpected response code %d", res.StatusCode)
}

// SendUpdateNFInstance sends the heartbeat. A problem details result means the NRF answered with an error.
var SendUpdateNFInstance = func(ctx context.Context, patchItem []models.PatchItem) (receivedNfProfile *models.NFProfile, problemDetails *models.ProblemDetails, err error) {
	logger.ConsumerLog.Debugln("send Update NFInstance")
	self := afContext.AF_Self()

	client := newNfManagementClient(self.NrfUri)
	request := client.NFInstanceIDDocumentAPI.UpdateNFInstance(ctx, self.NfId())
	request = request.PatchItem(patchItem)
	receivedNfProfile, res, err := client.NFInstanceIDDocumentAPI.UpdateNFInstanceExecute(request)
	if err != nil {
		if problem, ok := openapi.ErrorModel[models.ProblemDetails](err); ok {
			return models.NewNFProfileWithDefaults(), &problem, nil
		}
		return models.NewNFProfileWithDefaults(), nil, err
	}
	if res == nil {
		return models.NewNFProfileWithDefaults(), nil, openapi.ReportError("no response from server")
	}
	if res.StatusCode == http.StatusOK || res.StatusCode == http.StatusNoContent {
		return receivedNfProfile, nil, nil
	}
	return models.NewNFProfileWithDefaults(), nil, openapi.ReportError("unexpected response code %d", res.StatusCode)
}
