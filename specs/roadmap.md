# Roadmap

**Type:** LIVING

Phases are shippable on their own. Exactly one phase is marked Now. The roadmap changes only at phase boundaries, in its own commit.

## Phase 0 — Constitution and format. Done

- Mission, tech stack, roadmap.
- `docs/format.md` with the marker layout, semantics and a worked example.
- ADRs 0001 to 0004.

## Phase 1 — Go reader and `dump`. Done

- `marker` package: decode the marker body, validate version and flags.
- `h264` package: walk access units in Annex B and length-prefixed form, find SEI NAL units, extract `user_data_unregistered` payloads through mp4ff.
- `mp4` package: iterate video samples of progressive and fragmented MP4 with their timestamps and keyframe flags.
- CLI `seimark dump <file>`: JSON lines or CSV, one line per marked access unit; format detected from content, overridable.
- First test vectors: the worked example from the spec plus synthetic Annex B and MP4 fixtures with golden outputs.

Design in `docs/design/go-library.md` before implementation.

## Phase 2 — Go writer and `inject`. Done

- Stateful writer: stream identity, sequence counter, time source, placement before the first VCL NAL unit, keyframes-only mode, soft cap on the application payload.
- CLI `seimark inject` for Annex B streams: stamp every access unit from a start time and interval.
- CLI `seimark nals`: list NAL units per access unit, for debugging.
- Vectors extended with writer round trips; every vector must survive decode after encode.

## Phase 3 — Browser package. Now

- TypeScript marker codec checked against the same vectors.
- Worker transform over `RTCRtpScriptTransform`, fallback to `createEncodedStreams`.
- Time source: capture time where the browser exposes it, transform time otherwise.
- Last-marker exposure to the page, for correlating UI events with frames.
- A demo page publishing over WHIP.

## Phase 4 — MISB ST 0604 compatibility. Later

- Emit the MISB precision time stamp message alongside the marker, read it where present.
- Conformance check against GStreamer's h264parse and FFmpeg.

## Later

- HEVC.
- Writing MP4 in place.
- Examples: reading markers from a WHEP subscription with Pion, and from a GStreamer appsink in a separate CGO module.
- Published documentation site.
