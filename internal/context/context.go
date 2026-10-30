// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package context

import (
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/omec-project/af/internal/factory"
	"github.com/omec-project/openapi/v2/models"
)

const (
	// CallbackBasePath is where the PCF posts the Npcf_PolicyAuthorization notifications.
	// The notifUri sent to the PCF is <sbi uri>/naf-callback/v1/app-sessions/<callbackId>.
	CallbackBasePath = "/naf-callback/v1/app-sessions"
)

// AppSession is an Individual Application Session Context created at the PCF.
type AppSession struct {
	CallbackId   string // identifies the session in the notifUri, known before the PCF assigns an id
	AppSessionId string
	ResourceUri  string // Location header returned by the PCF
	PcfApiRoot   string // apiRoot of the PCF that owns the session
	Msisdn       string
	UeIpv4       string
	MedCompNs    []int32 // media component numbers currently at the PCF

	mu *sync.Mutex // serializes the operations on the session at the PCF; shared by the copies
}

// Serialize waits for the other operations that change the session at the PCF and returns the
// function that ends the caller's turn. It is not named Lock/Unlock so that vet does not take the
// (pointer-sharing) copies of the session for copied locks.
func (s *AppSession) Serialize() (done func()) {
	s.mu.Lock()
	return s.mu.Unlock
}

type AFContext struct {
	Name         string
	Scheme       models.UriScheme
	RegisterIPv4 string
	BindingIPv4  string
	SBIPort      int
	PEM          string
	Key          string
	NrfUri       string
	PcfUri       string

	NorthboundBindingIPv4 string
	NorthboundPort        int
	NorthboundAuthToken   string // bearer token the P-CSCF must present; empty disables the check

	MetricsBindingIPv4 string
	MetricsPort        int

	nfMu sync.RWMutex
	nfId string // set at start and again when the NRF assigns the instance id; see NfId

	sessionsMu   sync.Mutex
	sessions     map[string][]*AppSession // by MSISDN
	byCallbackId map[string]*AppSession   // index of sessions, for the notifications of the PCF
}

var afContext = &AFContext{sessions: map[string][]*AppSession{}, byCallbackId: map[string]*AppSession{}}

func AF_Self() *AFContext {
	return afContext
}

// InitAfContext fills the context from the (validated) configuration.
func InitAfContext(self *AFContext) {
	cfg := factory.AfConfig.Configuration
	nfId := cfg.NfInstanceId
	if nfId == "" {
		nfId = uuid.NewString()
	}
	self.SetNfId(nfId)
	self.Name = cfg.AfName
	self.Scheme = models.UriScheme(cfg.Sbi.Scheme)
	self.RegisterIPv4 = cfg.Sbi.RegisterIPv4
	self.BindingIPv4 = cfg.Sbi.BindingIPv4
	self.SBIPort = cfg.Sbi.Port
	if cfg.Sbi.TLS != nil {
		self.PEM = cfg.Sbi.TLS.PEM
		self.Key = cfg.Sbi.TLS.Key
	}
	self.NrfUri = cfg.NrfUri
	self.PcfUri = cfg.PcfUri
	self.NorthboundBindingIPv4 = cfg.Northbound.BindingIPv4
	self.NorthboundPort = cfg.Northbound.Port
	self.NorthboundAuthToken = cfg.Northbound.AuthToken
	self.MetricsBindingIPv4 = cfg.Metrics.BindingIPv4
	self.MetricsPort = cfg.Metrics.Port
}

// NfId returns the NF instance id; the registration, heartbeat and deregistration run in different goroutines.
func (c *AFContext) NfId() string {
	c.nfMu.RLock()
	defer c.nfMu.RUnlock()
	return c.nfId
}

func (c *AFContext) SetNfId(id string) {
	c.nfMu.Lock()
	defer c.nfMu.Unlock()
	c.nfId = id
}

func (c *AFContext) GetSbiUri() string {
	return fmt.Sprintf("%s://%s:%d", c.Scheme, c.RegisterIPv4, c.SBIPort)
}

// NotifUri returns the callback URI the PCF is given for the session with the given callback id.
func (c *AFContext) NotifUri(callbackId string) string {
	return c.GetSbiUri() + CallbackBasePath + "/" + callbackId
}

func (c *AFContext) AddSession(s *AppSession) {
	c.AddSessionLocked(s)()
}

// AddSessionLocked registers the session with its serialization lock already held, so that no other
// operation sees the session before the caller has finished it (e.g. before the PCF has assigned the
// app session id). The returned function releases the lock.
func (c *AFContext) AddSessionLocked(s *AppSession) (done func()) {
	s.mu = &sync.Mutex{}
	s.mu.Lock()
	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()
	if c.sessions == nil {
		c.sessions = map[string][]*AppSession{}
	}
	if c.byCallbackId == nil {
		c.byCallbackId = map[string]*AppSession{}
	}
	c.sessions[s.Msisdn] = append(c.sessions[s.Msisdn], s)
	c.byCallbackId[s.CallbackId] = s
	return s.mu.Unlock
}

// CompleteSession records what the PCF assigned to a session registered before its creation. It
// returns false when the session is gone, e.g. the PCF terminated it before answering the create.
func (c *AFContext) CompleteSession(callbackId, appSessionId, resourceUri, pcfApiRoot string, medCompNs []int32) (*AppSession, bool) {
	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()
	s, ok := c.byCallbackId[callbackId]
	if !ok {
		return nil, false
	}
	s.AppSessionId, s.ResourceUri, s.PcfApiRoot, s.MedCompNs = appSessionId, resourceUri, pcfApiRoot, medCompNs
	cp := *s
	return &cp, true
}

// SetMedCompNs records the media components the session has at the PCF.
func (c *AFContext) SetMedCompNs(callbackId string, ns []int32) {
	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()
	if s, ok := c.byCallbackId[callbackId]; ok {
		s.MedCompNs = ns
	}
}

// SessionCount returns the number of app sessions held.
func (c *AFContext) SessionCount() int {
	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()
	n := 0
	for _, list := range c.sessions {
		n += len(list)
	}
	return n
}

// SessionsOf returns a copy of the sessions of the given MSISDN; the sessions are copies too, so
// the caller can read them while SetMedCompNs updates the stored ones. The copies share the lock.
func (c *AFContext) SessionsOf(msisdn string) []*AppSession {
	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()
	out := make([]*AppSession, len(c.sessions[msisdn]))
	for i, s := range c.sessions[msisdn] {
		cp := *s
		out[i] = &cp
	}
	return out
}

func (c *AFContext) RemoveSession(msisdn, callbackId string) {
	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()
	list := c.sessions[msisdn]
	for i, s := range list {
		if s.CallbackId == callbackId {
			c.sessions[msisdn] = append(list[:i:i], list[i+1:]...)
			delete(c.byCallbackId, callbackId)
			break
		}
	}
	if len(c.sessions[msisdn]) == 0 {
		delete(c.sessions, msisdn)
	}
}

// FindSessionByCallbackId returns a copy of the session with the given callback id, if any.
func (c *AFContext) FindSessionByCallbackId(callbackId string) (*AppSession, bool) {
	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()
	s, ok := c.byCallbackId[callbackId]
	if !ok {
		return nil, false
	}
	cp := *s
	return &cp, true
}
