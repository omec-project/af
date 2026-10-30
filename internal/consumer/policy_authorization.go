// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package consumer

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/omec-project/af/internal/logger"
	"github.com/omec-project/openapi/v2"
	"github.com/omec-project/openapi/v2/Npcf_PolicyAuthorization"
	"github.com/omec-project/openapi/v2/models"
)

// policyAuthPath is the path that follows the apiRoot in the URI of the Npcf_PolicyAuthorization resources.
const policyAuthPath = "/npcf-policyauthorization/"

func newPolicyAuthClient(apiRoot string) *Npcf_PolicyAuthorization.APIClient {
	configuration := Npcf_PolicyAuthorization.NewConfiguration()
	serverConfig := &configuration.Servers[0]
	if apiRootVar, exists := serverConfig.Variables["apiRoot"]; exists {
		apiRootVar.DefaultValue = apiRoot
		serverConfig.Variables["apiRoot"] = apiRootVar
	}
	return Npcf_PolicyAuthorization.NewAPIClient(configuration)
}

// CreatedAppSession is the result of a successful Npcf_PolicyAuthorization_Create.
type CreatedAppSession struct {
	Context      *models.AppSessionContext
	ResourceUri  string // Location header
	PcfApiRoot   string
	AppSessionId string
}

// SendPostAppSessions creates an Individual Application Session Context (TS 29.514 clause 5.3.2.2).
var SendPostAppSessions = func(ctx context.Context, pcfApiRoot string, appSessionContext models.AppSessionContext) (*CreatedAppSession, error) {
	client := newPolicyAuthClient(pcfApiRoot)
	request := client.ApplicationSessionsCollectionAPI.PostAppSessions(ctx).AppSessionContext(appSessionContext)
	received, res, err := client.ApplicationSessionsCollectionAPI.PostAppSessionsExecute(request)
	if res != nil && res.Body != nil {
		defer closeBody("PostAppSessions", res)
	}
	if err != nil {
		return nil, wrapErr(res, err)
	}
	if res == nil {
		return nil, openapi.ReportError("no response from server")
	}
	if res.StatusCode != http.StatusCreated {
		return nil, openapi.ReportError("PCF returned unexpected status code %d", res.StatusCode)
	}
	location := res.Header.Get("Location")
	apiRoot, appSessionId, err := parseAppSessionLocation(location)
	if err != nil {
		return nil, wrapErr(res, err)
	}
	return &CreatedAppSession{Context: received, ResourceUri: location, PcfApiRoot: apiRoot, AppSessionId: appSessionId}, nil
}

// SendModAppSession modifies an Individual Application Session Context (TS 29.514 clause 5.3.2.3).
// The body is a JSON merge patch; see producer.buildUpdatePatch.
var SendModAppSession = func(ctx context.Context, pcfApiRoot, appSessionId string, patch models.AppSessionContextUpdateDataPatch) error {
	client := newPolicyAuthClient(pcfApiRoot)
	request := client.IndividualApplicationSessionContextDocumentAPI.ModAppSession(ctx, appSessionId).
		AppSessionContextUpdateDataPatch(patch)
	_, res, err := client.IndividualApplicationSessionContextDocumentAPI.ModAppSessionExecute(request)
	if res != nil && res.Body != nil {
		defer closeBody("ModAppSession", res)
	}
	if err != nil {
		return wrapErr(res, err)
	}
	if res == nil {
		return openapi.ReportError("no response from server")
	}
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusNoContent {
		return openapi.ReportError("PCF returned unexpected status code %d", res.StatusCode)
	}
	return nil
}

// SendDeleteAppSession deletes an Individual Application Session Context (TS 29.514 clause 5.3.2.4).
var SendDeleteAppSession = func(ctx context.Context, pcfApiRoot, appSessionId string) error {
	client := newPolicyAuthClient(pcfApiRoot)
	request := client.IndividualApplicationSessionContextDocumentAPI.DeleteAppSession(ctx, appSessionId)
	_, res, err := client.IndividualApplicationSessionContextDocumentAPI.DeleteAppSessionExecute(request)
	if res != nil && res.Body != nil {
		defer closeBody("DeleteAppSession", res)
	}
	if err != nil {
		return wrapErr(res, err)
	}
	if res == nil {
		return openapi.ReportError("no response from server")
	}
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusNoContent {
		return openapi.ReportError("PCF returned unexpected status code %d", res.StatusCode)
	}
	return nil
}

// parseAppSessionLocation splits <apiRoot>/npcf-policyauthorization/<version>/app-sessions/<id>.
func parseAppSessionLocation(location string) (apiRoot, appSessionId string, err error) {
	idx := strings.Index(location, policyAuthPath)
	last := strings.LastIndex(location, "/")
	if location == "" || idx < 0 || last <= idx || last == len(location)-1 {
		return "", "", fmt.Errorf("invalid Location header from PCF: %q", location)
	}
	return location[:idx], location[last+1:], nil
}

// PcfError is an error answer of the PCF.
type PcfError struct {
	StatusCode int
	Err        error
}

func (e *PcfError) Error() string { return fmt.Sprintf("PCF answered %d: %v", e.StatusCode, e.Err) }
func (e *PcfError) Unwrap() error { return e.Err }

// wrapErr adds the HTTP status code to the error when there was a response. The message of the
// generated client is replaced by the problem details when the PCF sent any.
func wrapErr(res *http.Response, err error) error {
	if res == nil {
		return err
	}
	if problem, ok := openapi.ErrorModel[models.ProblemDetails](err); ok {
		err = fmt.Errorf("%s: %s (cause %q)", res.Status, problem.GetTitle()+" "+problem.GetDetail(), problem.GetCause())
	}
	return &PcfError{StatusCode: res.StatusCode, Err: err}
}

func closeBody(op string, res *http.Response) {
	if err := res.Body.Close(); err != nil {
		logger.ConsumerLog.Errorf("%s response body cannot close: %+v", op, err)
	}
}
