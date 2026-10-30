// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package producer

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/omec-project/af/internal/factory"
	"github.com/omec-project/openapi/v2/models"
)

const (
	protoUDP = 17 // IP protocol number used in the IPFilterRules

	// Media component numbers inside an app session; they identify the component in updates.
	audioMedCompN int32 = 1
	videoMedCompN int32 = 2
)

// MediaRequest describes the media streams of a UE that need QoS. A port of 0 means "no such stream".
// RTCP ports default to the RTP port + 1 (RFC 3550 clause 11).
type MediaRequest struct {
	Msisdn        string
	UeIpv4        string
	AudioPort     int
	AudioRtcpPort int
	VideoPort     int
	VideoRtcpPort int
}

// Validate checks the fields every request needs.
func (m *MediaRequest) Validate() error {
	return validateMsisdn(m.Msisdn)
}

// validateMsisdn checks the MSISDN is what the GPSI "msisdn-" form carries: 5 to 15 digits
// (TS 29.571 clause 5.4.4.3, Gpsi; the number is in the E.164 format of TS 23.003 without a "+").
// It also keeps arbitrary text out of the GPSI and the logs.
func validateMsisdn(msisdn string) error {
	if msisdn == "" {
		return fmt.Errorf("UE MSISDN is missing")
	}
	if len(msisdn) < 5 || len(msisdn) > 15 {
		return fmt.Errorf("UE MSISDN must have 5 to 15 digits")
	}
	for _, r := range msisdn {
		if r < '0' || r > '9' {
			return fmt.Errorf("UE MSISDN must have digits only")
		}
	}
	return nil
}

// validateMedia also checks the UE address and the ports; used when media is added or changed.
func (m *MediaRequest) validateMedia() error {
	if err := m.Validate(); err != nil {
		return err
	}
	// ParseIP accepts IPv4-mapped IPv6 ("::ffff:10.0.0.1"), which is not an IPv4 literal for the PCF.
	if ip := net.ParseIP(m.UeIpv4); ip == nil || ip.To4() == nil || strings.Contains(m.UeIpv4, ":") {
		return fmt.Errorf("UE IP %q is not an IPv4 address", m.UeIpv4)
	}
	for name, port := range map[string]int{
		"audio": m.AudioPort, "audio RTCP": m.AudioRtcpPort, "video": m.VideoPort, "video RTCP": m.VideoRtcpPort,
	} {
		if port < 0 || port > 65535 {
			return fmt.Errorf("%s port %d is out of range", name, port)
		}
	}
	if m.AudioPort == 0 && m.VideoPort == 0 {
		return fmt.Errorf("no audio or video port given")
	}
	return nil
}

func (m *MediaRequest) withDefaults() MediaRequest {
	out := *m
	if out.AudioPort > 0 && out.AudioRtcpPort == 0 {
		out.AudioRtcpPort = out.AudioPort + 1
	}
	if out.VideoPort > 0 && out.VideoRtcpPort == 0 {
		out.VideoRtcpPort = out.VideoPort + 1
	}
	return out
}

// flowDescriptions are the IPFilterRules (TS 29.214 clause 5.3.8) of a UDP flow of the UE.
func flowDescriptions(ueIp string, port int) []string {
	return []string{
		fmt.Sprintf("permit out %d from any to %s %d", protoUDP, ueIp, port),
		fmt.Sprintf("permit in %d from %s %d to any", protoUDP, ueIp, port),
	}
}

type subComponent struct {
	fNum  int32
	port  int
	usage models.FlowUsage
	bwDl  string
	bwUl  string
}

type mediaComponent struct {
	medCompN int32
	medType  models.MediaType
	qosRef   string
	subs     []subComponent
}

// mediaComponents lists the components for the request; audio comes first, video follows.
func mediaComponents(m MediaRequest, media *factory.Media) []mediaComponent {
	var out []mediaComponent
	if m.AudioPort > 0 {
		q := media.Audio
		out = append(out, mediaComponent{
			medCompN: audioMedCompN, medType: models.MEDIATYPE_AUDIO, qosRef: q.QosReference,
			subs: []subComponent{
				{1, m.AudioPort, models.FLOWUSAGE_NO_INFO, q.MarBwDl, q.MarBwUl},
				{2, m.AudioRtcpPort, models.FLOWUSAGE_RTCP, q.RtcpMarBwDl, q.RtcpMarBwUl},
			},
		})
	}
	if m.VideoPort > 0 {
		q := media.Video
		out = append(out, mediaComponent{
			medCompN: videoMedCompN, medType: models.MEDIATYPE_VIDEO, qosRef: q.QosReference,
			subs: []subComponent{
				{1, m.VideoPort, models.FLOWUSAGE_NO_INFO, q.MarBwDl, q.MarBwUl},
				{2, m.VideoRtcpPort, models.FLOWUSAGE_RTCP, q.RtcpMarBwDl, q.RtcpMarBwUl},
			},
		})
	}
	return out
}

// The keys of the medComponents and medSubComps maps are the medCompN and fNum attributes (TS 29.514
// tables 5.6.2.3-1 and 5.6.2.7-1).
func medCompKey(n int32) string { return strconv.Itoa(int(n)) }
func fNumKey(n int32) string    { return strconv.Itoa(int(n)) }

// medCompNs lists the media component numbers of the request.
func medCompNs(m MediaRequest, media *factory.Media) []int32 {
	var out []int32
	for _, c := range mediaComponents(m, media) {
		out = append(out, c.medCompN)
	}
	return out
}

// mediaCodecs is the codec information of the SDP offer/answer, in the format of TS 29.514 clause 5.6.2.5.
var mediaCodecs = []string{"downlink\noffer\n", "uplink\nanswer\n"}

func eventsSubscription(media *factory.Media, notifUri *string) *models.EventsSubscReqData {
	events := make([]models.AfEventSubscription, 0, len(media.Events))
	for _, ev := range media.Events {
		sub := models.NewAfEventSubscription(models.AfEventPcf(ev.Event))
		if ev.NotifMethod != "" {
			sub.SetNotifMethod(models.AfNotifMethod(ev.NotifMethod))
		}
		if ev.RepPeriod > 0 {
			sub.SetRepPeriod(ev.RepPeriod)
		}
		events = append(events, *sub)
	}
	data := models.NewEventsSubscReqData(events)
	data.AvrgWndw = nil // the constructor defaults it; it only matters for QoS monitoring
	data.NotifUri = notifUri
	return data
}

// buildAppSessionContext builds the body of Npcf_PolicyAuthorization_Create (TS 29.514 table 5.6.2.2.2-1).
func buildAppSessionContext(m MediaRequest, media *factory.Media, notifUri string) models.AppSessionContext {
	m = m.withDefaults()
	// NewAppSessionContextReqData ignores its arguments, so the mandatory IEs are set explicitly.
	req := models.NewAppSessionContextReqDataWithDefaults()
	req.SetNotifUri(notifUri)
	req.SetSuppFeat(media.SuppFeat)
	req.SetAfAppId(media.AfAppId)
	req.SetDnn(media.Dnn)
	req.SetGpsi("msisdn-" + m.Msisdn)
	req.SetUeIpv4(m.UeIpv4)
	req.EvSubsc = eventsSubscription(media, &notifUri)

	comps := map[string]models.MediaComponent{}
	for _, c := range mediaComponents(m, media) {
		comp := models.NewMediaComponent(c.medCompN)
		comp.SetMedType(c.medType)
		comp.SetQosReference(c.qosRef)
		comp.SetCodecs(mediaCodecs)
		subs := map[string]models.MediaSubComponent{}
		for _, s := range c.subs {
			sub := models.NewMediaSubComponent(s.fNum)
			sub.SetFDescs(flowDescriptions(m.UeIpv4, s.port))
			sub.SetFStatus(models.FLOWSTATUS_ENABLED)
			sub.SetMarBwDl(s.bwDl)
			sub.SetMarBwUl(s.bwUl)
			sub.SetFlowUsage(s.usage)
			subs[fNumKey(s.fNum)] = *sub
		}
		comp.SetMedSubComps(subs)
		comps[medCompKey(c.medCompN)] = *comp
	}
	req.SetMedComponents(comps)

	asc := models.NewAppSessionContext()
	asc.SetAscReqData(*req)
	return *asc
}

// buildUpdatePatch builds the body of Npcf_PolicyAuthorization_Update (TS 29.514 table 5.6.2.3.2-1)
// that sets the media components of the request on an existing session: an AppSessionContextUpdateDataPatch,
// i.e. the modifications are inside "ascReqData".
//
// The PATCH is a JSON merge patch (RFC 7396), where a null removes the attribute. The models write only
// the attributes that are set, so the body has only what is meant to change. The components of the
// session that are not in the request (previous) are removed with a nil entry, i.e. a null: it is the
// only null in the body.
func buildUpdatePatch(m MediaRequest, media *factory.Media, previous []int32) models.AppSessionContextUpdateDataPatch {
	m = m.withDefaults()
	comps := map[string]*models.MediaComponentRm{}
	for _, c := range mediaComponents(m, media) {
		comp := models.NewMediaComponentRm(c.medCompN)
		comp.SetMedType(c.medType)
		comp.SetQosReference(c.qosRef)
		comp.SetCodecs(mediaCodecs)
		subs := map[string]*models.MediaSubComponentRm{}
		for _, s := range c.subs {
			sub := models.NewMediaSubComponentRm(s.fNum)
			sub.SetFDescs(flowDescriptions(m.UeIpv4, s.port))
			sub.SetFStatus(models.FLOWSTATUS_ENABLED)
			sub.SetMarBwDl(s.bwDl)
			sub.SetMarBwUl(s.bwUl)
			sub.SetFlowUsage(s.usage)
			subs[fNumKey(s.fNum)] = sub
		}
		comp.SetMedSubComps(subs)
		comps[medCompKey(c.medCompN)] = comp
	}
	for _, n := range previous {
		if _, kept := comps[medCompKey(n)]; !kept {
			comps[medCompKey(n)] = nil
		}
	}

	data := models.NewAppSessionContextUpdateData()
	data.SetMedComponents(comps)
	patch := models.NewAppSessionContextUpdateDataPatch()
	patch.SetAscReqData(*data)
	return *patch
}
