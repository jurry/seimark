# Changelog

Format: [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## 0.1.0 - unreleased

- Constitution, format specification and ADRs 0001 to 0004 (phase 0).
- Go library and `seimark dump`: read markers from Annex B streams and MP4,
  CSV or JSON lines output, test vectors from a worked example (phase 1).
- Go writer and `seimark inject`: stamp Annex B access units from a start
  time, keyframes-only mode, `seimark nals` for debugging (phase 2).
- Browser package: a TypeScript marker codec checked against the same
  vectors as the Go writer, `attach` for stamping an `RTCRtpSender` over
  WebRTC, DOM-free core and `webrtc` entry points, a Playwright loopback
  test (phase 3).
- `flv` and `ts` packages: read markers from FLV and MPEG-TS recordings with
  the same sample type as MP4; `seimark dump` and `seimark nals` detect the
  format from the file's own bytes (phase 4).
- Demo page publishing a canvas source over WHIP and watching it back over
  WHEP with `reader`, for a one-page check of whether a media server passes
  SEI through.
