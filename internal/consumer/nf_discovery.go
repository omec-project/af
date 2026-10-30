// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package consumer

import (
	"context"
	"fmt"

	afContext "github.com/omec-project/af/internal/context"
	"github.com/omec-project/af/internal/util"
	"github.com/omec-project/openapi/v2/Nnrf_NFDiscovery"
	"github.com/omec-project/openapi/v2/models"
)

// DiscoverPcfUri returns the apiRoot of a PCF that offers Npcf_PolicyAuthorization.
// The statically configured PCF is used when there is one, else the NRF is asked.
var DiscoverPcfUri = func(ctx context.Context) (string, error) {
	self := afContext.AF_Self()
	if self.PcfUri != "" {
		return self.PcfUri, nil
	}

	configuration := Nnrf_NFDiscovery.NewConfiguration()
	serverConfig := &configuration.Servers[0]
	if apiRootVar, exists := serverConfig.Variables["apiRoot"]; exists {
		apiRootVar.DefaultValue = self.NrfUri
		serverConfig.Variables["apiRoot"] = apiRootVar
	}
	client := Nnrf_NFDiscovery.NewAPIClient(configuration)
	request := client.NFInstancesStoreAPI.SearchNFInstances(ctx).
		TargetNfType(models.NFTYPE_PCF).
		RequesterNfType(models.NFTYPE_AF).
		ServiceNames([]models.ServiceName{models.SERVICENAME_NPCF_POLICYAUTHORIZATION})
	result, res, err := client.NFInstancesStoreAPI.SearchNFInstancesExecute(request)
	if res != nil && res.Body != nil {
		defer func() { _ = res.Body.Close() }()
	}
	if err != nil {
		return "", fmt.Errorf("NRF discovery of PCF failed: %w", err)
	}
	if result == nil {
		return "", fmt.Errorf("NRF discovery of PCF returned no result")
	}
	for _, profile := range result.NfInstances {
		if uri := util.SearchNFServiceUri(profile, models.SERVICENAME_NPCF_POLICYAUTHORIZATION,
			models.NFSERVICESTATUS_REGISTERED); uri != "" {
			return uri, nil
		}
	}
	return "", fmt.Errorf("no PCF offering %s found at the NRF", models.SERVICENAME_NPCF_POLICYAUTHORIZATION)
}
