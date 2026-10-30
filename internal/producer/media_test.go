// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package producer

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/omec-project/af/internal/factory"
)

const testUeIp = "10.0.0.1"

func testMedia() *factory.Media {
	return &factory.Media{
		Dnn: "ims", AfAppId: "app", SuppFeat: "10034",
		Audio: &factory.MediaQoS{QosReference: "a", MarBwDl: "1 Kbps", MarBwUl: "2 Kbps", RtcpMarBwDl: "3 Kbps", RtcpMarBwUl: "4 Kbps"},
		Video: &factory.MediaQoS{QosReference: "v", MarBwDl: "5 Kbps", MarBwUl: "6 Kbps", RtcpMarBwDl: "7 Kbps", RtcpMarBwUl: "8 Kbps"},
	}
}

func TestUpdatePatchShape(t *testing.T) {
	req := MediaRequest{Msisdn: "12345", UeIpv4: testUeIp, VideoPort: 4000, VideoRtcpPort: 4005}
	media := testMedia()

	raw, err := json.Marshal(buildUpdatePatch(req, media, []int32{audioMedCompN, videoMedCompN}))
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	// keys are medCompN and fNum; the audio component the session had is removed with a null
	if !strings.HasPrefix(got, `{"ascReqData":{"medComponents":{"1":null,"2":{`) || strings.Count(got, "null") != 1 ||
		!strings.Contains(got, `"medSubComps":{"1":{`) {
		t.Errorf("patch must be an AppSessionContextUpdateDataPatch keyed by medCompN/fNum: %s", got)
	}
	// only the video component is in the request; explicit RTCP port is kept
	if !strings.Contains(got, `"qosReference":"v"`) || strings.Contains(got, `"qosReference":"a"`) ||
		!strings.Contains(got, "permit in 17 from 10.0.0.1 4005 to any") {
		t.Errorf("patch content: %s", got)
	}
}

func TestValidateMedia(t *testing.T) {
	ok := MediaRequest{Msisdn: "12345", UeIpv4: testUeIp, AudioPort: 5000}
	if err := ok.validateMedia(); err != nil {
		t.Errorf("valid request rejected: %v", err)
	}
	for name, r := range map[string]MediaRequest{
		"no msisdn":    {UeIpv4: testUeIp, AudioPort: 5000},
		"short msisdn": {Msisdn: "1", UeIpv4: testUeIp, AudioPort: 5000},
		"msisdn text":  {Msisdn: "+1234a", UeIpv4: testUeIp, AudioPort: 5000},
		"no ip":        {Msisdn: "12345", AudioPort: 5000},
		"ipv6":         {Msisdn: "12345", UeIpv4: "fe80::1", AudioPort: 5000},
		"mapped":       {Msisdn: "12345", UeIpv4: "::ffff:10.0.0.1", AudioPort: 5000},
		"no ports":     {Msisdn: "12345", UeIpv4: testUeIp},
		"neg port":     {Msisdn: "12345", UeIpv4: testUeIp, AudioPort: -1, VideoPort: 5},
		"rtcp range":   {Msisdn: "12345", UeIpv4: testUeIp, AudioPort: 5000, AudioRtcpPort: 65536},
	} {
		if err := r.validateMedia(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestRtcpPortDefault(t *testing.T) {
	got := (&MediaRequest{AudioPort: 5000, VideoPort: 6000, VideoRtcpPort: 6010}).withDefaults()
	if got.AudioRtcpPort != 5001 || got.VideoRtcpPort != 6010 {
		t.Errorf("rtcp ports: %+v", got)
	}
}
