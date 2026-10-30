// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package northbound

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/omec-project/af/internal/callback"
	afContext "github.com/omec-project/af/internal/context"
	"github.com/omec-project/af/internal/factory"
	"github.com/omec-project/openapi/v2"
	"github.com/omec-project/openapi/v2/models"
)

const pathCreatePDUAudio = "/api/nf/createPDUAudio"

type fakePcf struct {
	*httptest.Server
	mu       sync.Mutex
	created  []models.AppSessionContext
	raw      []string // raw request bodies of the creates and patches
	patches  []map[string]any
	deletes  []string
	rejectOn int // status the PCF answers to a create, 0 = accept
}

func newFakePcf(t *testing.T) *fakePcf {
	t.Helper()
	p := &fakePcf{}
	const base = "/npcf-policyauthorization/v1/app-sessions"
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+base, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var asc models.AppSessionContext
		if err := openapi.Decode(&asc, body, "application/json"); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.rejectOn != 0 {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(p.rejectOn)
			if _, werr := w.Write([]byte(`{"title":"rejected","status":` + strconv.Itoa(p.rejectOn) + `}`)); werr != nil {
				t.Error(werr)
			}
			return
		}
		p.created = append(p.created, asc)
		p.raw = append(p.raw, string(body))
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Location", "http://"+r.Host+base+"/sess-1")
		w.WriteHeader(http.StatusCreated)
		if _, werr := w.Write(body); werr != nil {
			t.Error(werr)
		}
	})
	mux.HandleFunc("PATCH "+base+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var patch map[string]any
		if r.Header.Get("Content-Type") != "application/merge-patch+json" {
			http.Error(w, "wrong content type", http.StatusUnsupportedMediaType)
			return
		}
		if err := json.Unmarshal(body, &patch); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		p.mu.Lock()
		p.patches = append(p.patches, patch)
		p.raw = append(p.raw, string(body))
		p.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if _, werr := w.Write([]byte(`{}`)); werr != nil {
			t.Error(werr)
		}
	})
	mux.HandleFunc("POST "+base+"/{id}/delete", func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.deletes = append(p.deletes, r.PathValue("id"))
		p.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	p.Server = httptest.NewServer(mux)
	t.Cleanup(p.Close)
	return p
}

func setup(t *testing.T, pcfUri string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := "info:\n  version: 1.0.0\nconfiguration:\n  pcfUri: " + pcfUri + "\n"
	path := filepath.Join(t.TempDir(), "af.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := factory.InitConfigFactory(path); err != nil {
		t.Fatal(err)
	}
	self := afContext.AF_Self()
	afContext.InitAfContext(self)
	for _, s := range self.SessionsOf("55510") {
		self.RemoveSession(s.Msisdn, s.CallbackId)
	}
	engine := gin.New()
	AddService(engine, "")
	callback.AddService(engine)
	return engine
}

func post(engine *gin.Engine, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func TestAudioVideoPatchDeleteLifecycle(t *testing.T) {
	pcf := newFakePcf(t)
	engine := setup(t, pcf.URL)

	// P-CSCF sends every field as a string.
	rec := post(engine, pathCreatePDUAudio, `{"fromUEIP":"10.45.0.2","fromUEAUDIOPORT":"49170","fromUEMSISDN":"55510"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create audio: status %d body %s", rec.Code, rec.Body)
	}
	if len(pcf.created) != 1 {
		t.Fatalf("PCF got %d creates", len(pcf.created))
	}
	// the wire format must use the camelCase names of TS 29.514, not the Go field names
	for _, key := range []string{`"ascReqData"`, `"notifUri":"http`, `"suppFeat":"14034"`, `"gpsi":"msisdn-55510"`, `"ueIpv4":"10.45.0.2"`, `"medComponents"`, `"fDescs"`, `"repPeriod":60`} {
		if !strings.Contains(pcf.raw[0], key) {
			t.Errorf("create body lacks %s: %s", key, pcf.raw[0])
		}
	}
	req := pcf.created[0].AscReqData.Get()
	if req.GetGpsi() != "msisdn-55510" || req.GetUeIpv4() != "10.45.0.2" || req.GetDnn() != "ims" {
		t.Errorf("unexpected req data: gpsi=%s ue=%s dnn=%s", req.GetGpsi(), req.GetUeIpv4(), req.GetDnn())
	}
	if req.SuppFeat == "" || !strings.Contains(req.NotifUri, afContext.CallbackBasePath+"/") {
		t.Errorf("mandatory IEs wrong: suppFeat=%q notifUri=%q", req.SuppFeat, req.NotifUri)
	}
	if req.EvSubsc == nil || req.EvSubsc.GetNotifUri() != req.NotifUri {
		t.Errorf("evSubsc must carry the notifUri: %+v", req.EvSubsc)
	}
	comps := req.GetMedComponents()
	if len(comps) != 1 {
		t.Fatalf("want 1 media component, got %d", len(comps))
	}
	audio := comps["1"]
	if audio.GetMedType() != models.MEDIATYPE_AUDIO || audio.GetQosReference() != "qosVoNR" {
		t.Errorf("audio component: %+v", audio)
	}
	subs := audio.GetMedSubComps()
	if got := subs["1"].FDescs; len(got) != 2 || got[0] != "permit out 17 from any to 10.45.0.2 49170" ||
		got[1] != "permit in 17 from 10.45.0.2 49170 to any" {
		t.Errorf("RTP flow descriptions: %v", got)
	}
	rtcp := subs["2"]
	if got := rtcp.FDescs; len(got) != 2 || got[0] != "permit out 17 from any to 10.45.0.2 49171" ||
		rtcp.GetFlowUsage() != models.FLOWUSAGE_RTCP {
		t.Errorf("RTCP flow (default port+1): %v usage=%v", got, rtcp.GetFlowUsage())
	}

	// video call
	rec = post(engine, "/api/nf/createPDUVideo", `{"fromUEIP":"10.45.0.2","fromUEAUDIOPORT":49170,"fromUEVIDEOPORT":"49180","fromUEMSISDN":"55510"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create video: status %d body %s", rec.Code, rec.Body)
	}
	if n := len(pcf.created[1].AscReqData.Get().GetMedComponents()); n != 2 {
		t.Errorf("video call should carry audio and video components, got %d", n)
	}
	if got := len(afContext.AF_Self().SessionsOf("55510")); got != 2 {
		t.Fatalf("AF tracks %d sessions, want 2", got)
	}

	// re-INVITE changes the video port
	rec = post(engine, "/api/nf/patchPDU", `{"fromUEIP":"10.45.0.2","fromUEAUDIOPORT":"49170","fromUEVIDEOPORT":"49190","fromUEMSISDN":"55510"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: status %d body %s", rec.Code, rec.Body)
	}
	if last := pcf.raw[len(pcf.raw)-1]; strings.Contains(last, "null") || !strings.HasPrefix(last, `{"ascReqData":{"medComponents"`) ||
		!strings.Contains(last, "permit in 17 from 10.45.0.2 49190 to any") {
		t.Errorf("patch body (must not contain nulls, which a merge patch treats as removals): %s", last)
	}
	if len(pcf.patches) != 1 {
		t.Errorf("PCF got %d patches", len(pcf.patches))
	}

	rec = post(engine, "/api/nf/deletePDU", `{"fromUEMSISDN":"55510"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: status %d body %s", rec.Code, rec.Body)
	}
	if len(pcf.deletes) != 2 || len(afContext.AF_Self().SessionsOf("55510")) != 0 {
		t.Errorf("deletes=%v remaining=%d", pcf.deletes, len(afContext.AF_Self().SessionsOf("55510")))
	}
	if rec = post(engine, "/api/nf/deletePDU", `{"fromUEMSISDN":"55510"}`); rec.Code != http.StatusNotFound {
		t.Errorf("second delete: status %d, want 404", rec.Code)
	}
}

func TestPatchRemovesDroppedMedia(t *testing.T) {
	pcf := newFakePcf(t)
	engine := setup(t, pcf.URL)
	if rec := post(engine, "/api/nf/createPDUVideo", `{"fromUEIP":"10.45.0.2","fromUEAUDIOPORT":"49170","fromUEVIDEOPORT":"49180","fromUEMSISDN":"55510"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create video: %d %s", rec.Code, rec.Body)
	}
	// the call drops the video: the merge patch removes the video component (medCompN 2) with a null
	if rec := post(engine, "/api/nf/patchPDU", `{"fromUEIP":"10.45.0.2","fromUEAUDIOPORT":"49170","fromUEMSISDN":"55510"}`); rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body)
	}
	last := pcf.raw[len(pcf.raw)-1]
	if !strings.HasPrefix(last, `{"ascReqData":{"medComponents":{"1":{`) || !strings.Contains(last, `"2":null`) || strings.Count(last, "null") != 1 {
		t.Errorf("patch body must remove component 2 and have no other null: %s", last)
	}
	// and a second patch with audio only must not remove it again
	if rec := post(engine, "/api/nf/patchPDU", `{"fromUEIP":"10.45.0.2","fromUEAUDIOPORT":"49170","fromUEMSISDN":"55510"}`); rec.Code != http.StatusOK {
		t.Fatalf("second patch: %d %s", rec.Code, rec.Body)
	}
	if last = pcf.raw[len(pcf.raw)-1]; strings.Contains(last, "null") {
		t.Errorf("removed component sent again: %s", last)
	}
}

func TestInvalidRequests(t *testing.T) {
	pcf := newFakePcf(t)
	engine := setup(t, pcf.URL)
	for name, tc := range map[string]struct{ path, body string }{
		"no msisdn":        {pathCreatePDUAudio, `{"fromUEIP":"10.45.0.2","fromUEAUDIOPORT":"49170"}`},
		"no ue ip":         {pathCreatePDUAudio, `{"fromUEAUDIOPORT":"49170","fromUEMSISDN":"55510"}`},
		"ipv6 ue ip":       {pathCreatePDUAudio, `{"fromUEIP":"::1","fromUEAUDIOPORT":"49170","fromUEMSISDN":"55510"}`},
		"no audio port":    {pathCreatePDUAudio, `{"fromUEIP":"10.45.0.2","fromUEMSISDN":"55510"}`},
		"port not number":  {pathCreatePDUAudio, `{"fromUEIP":"10.45.0.2","fromUEAUDIOPORT":"abc","fromUEMSISDN":"55510"}`},
		"port too big":     {pathCreatePDUAudio, `{"fromUEIP":"10.45.0.2","fromUEAUDIOPORT":"70000","fromUEMSISDN":"55510"}`},
		"no video port":    {"/api/nf/createPDUVideo", `{"fromUEIP":"10.45.0.2","fromUEAUDIOPORT":"49170","fromUEMSISDN":"55510"}`},
		"not json":         {pathCreatePDUAudio, `nope`},
		"delete no msisdn": {"/api/nf/deletePDU", `{}`},
	} {
		if rec := post(engine, tc.path, tc.body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400 (body %s)", name, rec.Code, rec.Body)
		}
	}
	if len(pcf.created) != 0 {
		t.Errorf("invalid requests reached the PCF")
	}
	if rec := post(engine, "/api/nf/patchPDU", `{"fromUEIP":"10.45.0.2","fromUEAUDIOPORT":"49170","fromUEMSISDN":"55510"}`); rec.Code != http.StatusNotFound {
		t.Errorf("patch without session: status %d, want 404", rec.Code)
	}
}

func TestPcfRejectionIsReported(t *testing.T) {
	// 4xx of the PCF is passed on, except 401/403 (the AF's credentials); the PCF's text is not.
	for pcfStatus, want := range map[int]int{
		http.StatusUnprocessableEntity: http.StatusUnprocessableEntity,
		http.StatusForbidden:           http.StatusBadGateway,
		http.StatusUnauthorized:        http.StatusBadGateway,
		http.StatusInternalServerError: http.StatusBadGateway,
	} {
		pcf := newFakePcf(t)
		pcf.rejectOn = pcfStatus
		engine := setup(t, pcf.URL)
		rec := post(engine, pathCreatePDUAudio, `{"fromUEIP":"10.45.0.2","fromUEAUDIOPORT":"49170","fromUEMSISDN":"55510"}`)
		if rec.Code != want {
			t.Errorf("PCF %d: status %d, want %d (body %s)", pcfStatus, rec.Code, want, rec.Body)
		}
		if strings.Contains(rec.Body.String(), "rejected") || strings.Contains(rec.Body.String(), pcf.URL) {
			t.Errorf("PCF %d: PCF detail leaked to the P-CSCF: %s", pcfStatus, rec.Body)
		}
		if got := len(afContext.AF_Self().SessionsOf("55510")); got != 0 {
			t.Errorf("rejected session was stored")
		}
	}
}

func TestCallbacks(t *testing.T) {
	pcf := newFakePcf(t)
	engine := setup(t, pcf.URL)
	if rec := post(engine, pathCreatePDUAudio, `{"fromUEIP":"10.45.0.2","fromUEAUDIOPORT":"49170","fromUEMSISDN":"55510"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	notifUri := pcf.created[0].AscReqData.Get().NotifUri
	path := notifUri[strings.Index(notifUri, afContext.CallbackBasePath):]

	if rec := post(engine, path+"/notify", `{"evSubsUri":"x","evNotifs":[{"event":"QOS_NOTIF","flows":[{"medCompN":1}]}]}`); rec.Code != http.StatusNoContent {
		t.Errorf("notify: %d %s", rec.Code, rec.Body)
	}
	if rec := post(engine, path+"/notify", `{`); rec.Code != http.StatusBadRequest {
		t.Errorf("malformed notify: %d", rec.Code)
	}
	if rec := post(engine, afContext.CallbackBasePath+"/unknown/notify", `{"evSubsUri":"x"}`); rec.Code != http.StatusNotFound {
		t.Errorf("unknown id: %d", rec.Code)
	}

	if rec := post(engine, path+"/terminate", `{"termCause":"PDU_SESSION_TERMINATION","resUri":"x"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("terminate: %d %s", rec.Code, rec.Body)
	}
	if got := len(afContext.AF_Self().SessionsOf("55510")); got != 0 {
		t.Errorf("terminated session still stored")
	}
	// the AF asks the PCF to delete the context it was told to end
	for range 200 {
		pcf.mu.Lock()
		n := len(pcf.deletes)
		pcf.mu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("AF did not delete the terminated context at the PCF")
}
