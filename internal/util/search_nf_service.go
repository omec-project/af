// Copyright 2019 free5GC.org
//
// SPDX-License-Identifier: Apache-2.0
//

package util

import (
	"fmt"

	"github.com/omec-project/openapi/v2/models"
)

// SearchNFServiceUri returns the root URI of the given service in the NF profile, or "" if it is not there.
func SearchNFServiceUri(nfProfile models.NFProfileDiscovery, serviceName models.ServiceName,
	nfServiceStatus models.NFServiceStatus,
) (nfUri string) {
	nfServiceList := nfProfile.GetNfServiceList()
	services := make([]models.NFService, 0, len(nfServiceList))
	for _, service := range nfServiceList {
		services = append(services, service)
	}
	if len(services) == 0 {
		services = nfProfile.GetNfServices()
	}
	for _, service := range services {
		if service.GetServiceName() != serviceName || service.GetNfServiceStatus() != nfServiceStatus {
			continue
		}
		port := int32(0)
		if len(service.GetIpEndPoints()) > 0 {
			port = service.GetIpEndPoints()[0].GetPort()
		}
		switch {
		case nfProfile.GetFqdn() != "":
			nfUri = getSbiUri(service.GetScheme(), nfProfile.GetFqdn(), port)
		case service.GetFqdn() != "":
			nfUri = getSbiUri(service.GetScheme(), service.GetFqdn(), port)
		case service.GetApiPrefix() != "":
			nfUri = service.GetApiPrefix()
		case len(service.GetIpEndPoints()) > 0:
			point := service.GetIpEndPoints()[0]
			if point.GetIpv4Address() != "" {
				nfUri = getSbiUri(service.GetScheme(), point.GetIpv4Address(), point.GetPort())
			} else if len(nfProfile.GetIpv4Addresses()) != 0 {
				nfUri = getSbiUri(service.GetScheme(), nfProfile.GetIpv4Addresses()[0], point.GetPort())
			}
		}
		if nfUri != "" {
			break
		}
	}
	return nfUri
}

func getSbiUri(scheme models.UriScheme, host string, port int32) string {
	if port != 0 {
		return fmt.Sprintf("%s://%s:%d", scheme, host, port)
	}
	if scheme == models.URISCHEME_HTTPS {
		return fmt.Sprintf("%s://%s:443", scheme, host)
	}
	return fmt.Sprintf("%s://%s:80", models.URISCHEME_HTTP, host)
}
