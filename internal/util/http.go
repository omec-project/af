// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// MaxBodyBytes is the largest request body the AF reads; the JSON bodies it receives are far smaller.
const MaxBodyBytes = 1 << 20

// LimitBody makes reading a request body fail once it exceeds limit bytes.
func LimitBody(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}

// IsBodyTooLarge reports whether err comes from a body that exceeded the LimitBody limit.
func IsBodyTooLarge(err error) bool {
	var tooLarge *http.MaxBytesError
	return errors.As(err, &tooLarge)
}

// RequireBearer rejects the requests that do not carry "Authorization: Bearer <token>".
func RequireBearer(token string) gin.HandlerFunc {
	want := []byte(token)
	return func(c *gin.Context) {
		got, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(got), want) != 1 {
			c.Header("WWW-Authenticate", "Bearer")
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Next()
	}
}
