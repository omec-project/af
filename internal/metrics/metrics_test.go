// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// reset clears the collectors, which are process wide, so a test does not see the counts of another
// test or of an earlier run (-count=N).
func reset(t *testing.T) {
	t.Helper()
	httpRequests.Reset()
	httpDuration.Reset()
	pcfRequests.Reset()
	pcfDuration.Reset()
	notifications.Reset()
	nrfRequests.Reset()
	nrfRegistered.Set(0)
}

func scrape(t *testing.T) string {
	t.Helper()
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil))
	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func wantLine(t *testing.T, body, line string) {
	t.Helper()
	if !strings.Contains(body, line+"\n") {
		t.Errorf("metrics do not contain %q:\n%s", line, body)
	}
}

func TestHTTPMiddleware(t *testing.T) {
	reset(t)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(HTTP("test"))
	engine.GET("/items/:id", func(c *gin.Context) { c.Status(http.StatusTeapot) })

	for _, path := range []string{"/items/1", "/items/2", "/nowhere"} {
		engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
	}

	body := scrape(t)
	// Two ids, one route label: the cardinality is bounded by the routes.
	wantLine(t, body, `af_http_requests{code="418",interface="test",route="/items/:id"} 2`)
	wantLine(t, body, `af_http_requests{code="404",interface="test",route="unmatched"} 1`)
	wantLine(t, body, `af_http_request_duration_seconds_count{interface="test",route="/items/:id"} 2`)
}

func TestPcfAndNrfRequests(t *testing.T) {
	reset(t)
	ObservePcfRequest(OpCreate, time.Now(), nil)
	ObservePcfRequest(OpCreate, time.Now(), errors.New("rejected"))
	ObserveNrfRequest(NrfHeartbeat, nil)
	IncNotification(NotifTerminate, ResultUnknownSession)
	SetNrfRegistered(true)

	body := scrape(t)
	wantLine(t, body, `af_pcf_requests{operation="create",result="success"} 1`)
	wantLine(t, body, `af_pcf_requests{operation="create",result="failure"} 1`)
	wantLine(t, body, `af_pcf_request_duration_seconds_count{operation="create"} 2`)
	wantLine(t, body, `af_nrf_requests{operation="heartbeat",result="success"} 1`)
	wantLine(t, body, `af_pcf_notifications{result="unknown_session",type="terminate"} 1`)
	wantLine(t, body, `af_nrf_registered 1`)

	SetNrfRegistered(false)
	wantLine(t, scrape(t), `af_nrf_registered 0`)
}

func TestAppSessions(t *testing.T) {
	n := 3
	if err := RegisterAppSessions(func() int { return n }); err != nil {
		t.Fatal(err)
	}
	wantLine(t, scrape(t), `af_app_sessions 3`)
	n = 1
	wantLine(t, scrape(t), `af_app_sessions 1`)
}
