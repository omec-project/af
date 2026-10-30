// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package context

import "testing"

func TestSessionStore(t *testing.T) {
	c := &AFContext{Scheme: "http", RegisterIPv4: "10.0.0.1", SBIPort: 8000}
	a := &AppSession{CallbackId: "a", Msisdn: "1"}
	b := &AppSession{CallbackId: "b", Msisdn: "1"}
	d := &AppSession{CallbackId: "d", Msisdn: "2"}
	c.AddSession(a)
	c.AddSession(b)
	c.AddSession(d)

	// SessionsOf returns copies, so compare by callback id
	if got := c.SessionsOf("1"); len(got) != 2 || got[0].CallbackId != "a" || got[1].CallbackId != "b" {
		t.Fatalf("sessions of 1: %v", got)
	}
	if s, ok := c.FindSessionByCallbackId("d"); !ok || s.CallbackId != "d" || s.Msisdn != "2" {
		t.Errorf("find d: %v %v", s, ok)
	}
	if n := c.SessionCount(); n != 3 {
		t.Errorf("session count %d, want 3", n)
	}
	c.RemoveSession("1", "a")
	if got := c.SessionsOf("1"); len(got) != 1 || got[0].CallbackId != "b" {
		t.Errorf("after remove: %v", got)
	}
	c.RemoveSession("1", "b")
	c.RemoveSession("1", "missing") // no-op
	if _, ok := c.FindSessionByCallbackId("b"); ok || len(c.SessionsOf("1")) != 0 {
		t.Errorf("session b still there")
	}
	if n := c.SessionCount(); n != 1 {
		t.Errorf("session count %d, want 1", n)
	}
	if got := c.NotifUri("x"); got != "http://10.0.0.1:8000/naf-callback/v1/app-sessions/x" {
		t.Errorf("notif uri %s", got)
	}
}
