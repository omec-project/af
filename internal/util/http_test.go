// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func serve(engine *gin.Engine, header, body string) int {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/x", strings.NewReader(body))
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec.Code
}

func TestRequireBearer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(RequireBearer("s3cret"))
	engine.POST("/x", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	for header, want := range map[string]int{
		"":                   http.StatusUnauthorized,
		"Bearer wrong":       http.StatusUnauthorized,
		"Basic s3cret":       http.StatusUnauthorized,
		"Bearer s3cret":      http.StatusNoContent,
		"Bearer s3cret-more": http.StatusUnauthorized,
	} {
		if got := serve(engine, header, ""); got != want {
			t.Errorf("Authorization %q: status %d, want %d", header, got, want)
		}
	}
}

func TestLimitBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(LimitBody(8))
	engine.POST("/x", func(c *gin.Context) {
		if _, err := c.GetRawData(); err != nil {
			c.Status(http.StatusRequestEntityTooLarge)
			if !IsBodyTooLarge(err) {
				c.Status(http.StatusInternalServerError)
			}
			return
		}
		c.Status(http.StatusNoContent)
	})
	if got := serve(engine, "", "12345678"); got != http.StatusNoContent {
		t.Errorf("body at the limit: status %d", got)
	}
	if got := serve(engine, "", "123456789"); got != http.StatusRequestEntityTooLarge {
		t.Errorf("body over the limit: status %d", got)
	}
}
