<!--
Copyright (C) 2026 Intel Corporation

SPDX-License-Identifier: Apache-2.0
-->
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/omec-project/af/badge)](https://scorecard.dev/viewer/?uri=github.com/omec-project/af)
[![GitHub latest release (SemVer)](https://img.shields.io/github/v/release/omec-project/af?sort=semver)](https://github.com/omec-project/af/releases)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://spdx.org/licenses/Apache-2.0.html)

# Application Functions (AF)

Application Function for the Aether SD-Core. It is the N5 consumer of the IMS: the P-CSCF asks the AF for
QoS on a call, and the AF creates, updates and deletes the application session at the PCF
(`Npcf_PolicyAuthorization`, 3GPP TS 29.514). It replaces the Java proxy in `aether-docker-components/ims/n5`.

```
P-CSCF --HTTP--> AF northbound (:7782) --Npcf_PolicyAuthorization--> PCF
                 AF SBI (:8000)        <--notify / terminate-------- PCF
                 AF -- Nnrf_NFManagement / Nnrf_NFDiscovery --> NRF
```

## What it does

- **NRF** (TS 29.510): registers as `AF`, retries until it succeeds, sends the heartbeat at the interval the NRF
  returns (default 60 s), registers again if a heartbeat fails, deregisters on SIGINT/SIGTERM.
- **PCF discovery**: `pcfUri` if configured, else `Nnrf_NFDiscovery` for a PCF that offers `npcf-policyauthorization`.
- **Application sessions** (TS 29.514 clause 5.3.2): create (`POST /app-sessions`), update (`PATCH`, JSON merge
  patch) and delete (`POST /app-sessions/{id}/delete`). Audio and video media components use the flow
  descriptions, bandwidths and QoS references from the config (defaults are those of the n5 templates).
- **Notifications** (TS 29.514 clause 5.5): `<notifUri>/notify` (logged) and `<notifUri>/terminate` (the AF forgets
  the session and deletes the context at the PCF). `notifUri` is
  `<sbi uri>/naf-callback/v1/app-sessions/<id>`, one per session.

## Northbound API (IMS)

Same routes and field names as the n5 proxy, so the P-CSCF config (`N5_PROXY_IP`/`N5_PROXY_PORT`) is unchanged.
All fields are strings or numbers.

| Route | Fields | Result |
|---|---|---|
| `POST /api/nf/createPDUAudio` | `fromUEIP`, `fromUEAUDIOPORT`, `fromUEMSISDN`, opt. `fromUERTCPPORT` | 201 |
| `POST /api/nf/createPDUVideo` | `fromUEIP`, `fromUEVIDEOPORT`, `fromUEMSISDN`, opt. `fromUEAUDIOPORT`, `fromUERTCPPORT`, `fromUEVIDEORTCPPORT` | 201 |
| `POST /api/nf/patchPDU` | as above; applies to the latest session of the MSISDN | 200 |
| `POST /api/nf/deletePDU` | `fromUEMSISDN`; deletes all sessions of the MSISDN | 200 |

`fromUEMSISDN` is 5 to 15 digits (the `msisdn-` GPSI of TS 29.571), no `+`. Request bodies are limited to 1 MiB (413).
When `northbound.authTokenFile` is set, every request needs `Authorization: Bearer <token>` (401 otherwise); without it the
interface is open and the AF logs a warning at start. The interface is plain HTTP, so restrict it to the IMS network.

RTCP ports default to the RTP port + 1. Unlike the n5 proxy, which answers 200 with a text body whatever happened,
the AF answers with a JSON body and a real status: 400 for a bad request, 404 when the UE has no session, the
PCF's 4xx when it rejects the request, 502 otherwise.

## Run

```
make build && bin/af -cfg configs/afcfg.yaml
make test
```

See `configs/afcfg.yaml` for every setting; unset ones take the defaults shown there.

## Metrics

Prometheus scrapes `GET /metrics` on the `metrics` port (default 8080, its own listener), together with the Go and
process metrics.

| Metric | Labels | Meaning |
|---|---|---|
| `af_http_requests` | `interface` (`northbound`\|`sbi`), `route`, `code` | requests received |
| `af_http_request_duration_seconds` | `interface`, `route` | histogram of their duration |
| `af_pcf_requests` | `operation` (`create`\|`update`\|`delete`), `result` (`success`\|`failure`) | `Npcf_PolicyAuthorization` requests sent |
| `af_pcf_request_duration_seconds` | `operation` | histogram of their duration |
| `af_pcf_notifications` | `type` (`notify`\|`terminate`), `result` (`success`\|`unknown_session`\|`malformed`) | notifications received from the PCF |
| `af_nrf_requests` | `operation` (`register`\|`heartbeat`\|`deregister`), `result` | NF management requests sent |
| `af_nrf_registered` | | 1 while registered at the NRF |
| `af_app_sessions` | | app sessions the AF holds |

`route` is the route pattern (`/naf-callback/v1/app-sessions/:callbackId/notify`), not the path, so the label set
stays bounded.

## Known limits

- The update body is an `AppSessionContextUpdateDataPatch` (modifications inside `ascReqData`), as TS 29.514
  requires. The PCF in this repository accepts it and, for older consumers, also a bare `AppSessionContextUpdateData`.
- An update sets the media components in the request and removes the ones of the session that are not in it (`null` in the merge patch).
- IPv4 UEs only.
