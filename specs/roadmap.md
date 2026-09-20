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

## Phase 3 — Browser package. Done

- TypeScript marker codec checked against the same vectors, including writer parity: the `testsrc-marked.h264` stream is stripped, restamped and compared byte for byte with what the Go writer produced.
- One npm package with two entry points, the DOM-free core enforced by `lib` (ADR 0006).
- Worker transform over `RTCRtpScriptTransform`, fallback to `createEncodedStreams`. Measured 2026-09-13: Chromium 153 and Firefox 155 both expose the standard API, so the fallback has no browser coverage — it is exercised only through the shared frame handler in the Node tests.
- Time source probed once per stream and constant thereafter (ADR 0007). Chromium 153 does not populate `captureTime` on sender frames, so streams carry send time; Firefox is unmeasured.
- Last-marker exposure to the page on a coalesced timer, for correlating UI events with frames.
- A demo page publishing over WHIP from a canvas source and watching over WHEP.
- A Playwright loopback test. It runs chromium only in CI: Firefox's H.264 encoder is the OpenH264 GMP, downloaded at runtime and absent from the Playwright build, so the Firefox project fails rather than being skipped, to keep the gap visible.

Design in `docs/design/browser-package.md`.

## Phase 4 — FLV and MPEG-TS readers. Done

- `flv` package: iterate the video tags of an FLV file: parameter sets from the AVC sequence header, one sample per AVC video packet with DTS, composition offset and keyframe flag, data as length-prefixed NAL units.
- `ts` package: demultiplex the first H.264 elementary stream of an MPEG-TS file: PAT and PMT, PES with PTS and DTS, access units cut from the payload with the `h264.AccessUnits` rule, data as Annex B, sync from the IDR NAL unit.
- The same sample shape as `mp4`: a shared `container.Sample` with a framing field, so a consumer reads the three containers alike.
- `seimark dump` and `seimark nals` read both, format detected from content: the `FLV` signature, a sync byte at offsets 0 and 188.
- Vectors: the marked Annex B fixture remuxed with `-c copy` into FLV and MPEG-TS, with golden `dump` output for each.
- Why now: SRS records FLV and MediaMTX records MPEG-TS, and the sibling project that measures servers reads recordings only through this library.

Design in `docs/design/go-library.md`.

## Phase 5 — MISB ST 0604 compatibility. Now

- Emit the MISB precision time stamp message alongside the marker, read it where present.
- Conformance check against GStreamer's h264parse and FFmpeg.
- Capture time from the frame's presentation timestamp where `captureTime` is absent: measure the error of a once-sampled main-thread offset first, and only then decide whether it may be written under flag 1 (ADR 0007 rejected writing it unmeasured).
- Measure `captureTime` on Firefox sender frames in an environment whose Firefox has the OpenH264 GMP, and record the result.

## Later

- HEVC.
- Writing MP4 in place.
- Examples: reading markers from a WHEP subscription with Pion, and from a GStreamer appsink in a separate CGO module.
- Published documentation site.
