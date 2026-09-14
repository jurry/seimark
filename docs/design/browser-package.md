# Design: browser package

**Type:** LIVING. Describes the TypeScript side as built in phase 3: the marker codec, the writer and reader, the WebRTC Encoded Transform integration, the demo page and what the browser measurements showed. Rewritten as later phases change it.

## Shape

One npm package, `seimark`, in `browser/` of this repository. Two entry points built from one source tree.

| Entry | Owns | `lib` | Depends on |
|---|---|---|---|
| `seimark` | The marker body codec from `docs/format.md`, the SEI container, the NAL unit walk and insert in both framings, `Writer`, `markersIn`. | `es2022`, no DOM | nothing |
| `seimark/webrtc` | `attach`, the `RTCRtpScriptTransform` worker, the `createEncodedStreams` fallback, the time source probe. | `es2022` and DOM | `seimark` |

ESM only. Core has no runtime dependencies; the development dependencies are TypeScript, esbuild and Playwright. Tests run on `node:test`, because the tech stack forbids third-party assertion frameworks. `specs/tech-stack.md` carries all three in its approved table.

`browser/README.md` is the package's own document: the two entry points, a usage example for each, and the limitations measured in this phase. `browser/demo/index.html` is the WHIP publisher described below.

The package stays in this repository because `vectors/` is the conformance suite for every implementation of the format and the Go and TypeScript sides must be checked against the same files. Tests read `../vectors/` directly; the published tarball does not carry them.

### Why two entry points and not two packages

The split exists so the codec can be tested in Node without a browser and used by consumers that have no `RTCRtpSender`: a server-side TypeScript reader, a test harness, a page that only decodes. `lib` is what enforces it — core compiles with no DOM library, so a WebRTC reference in core is a compile error rather than a convention.

Two packages would buy the same separation and cost more than it is worth here, for one reason that is specific to this library. A worker created from a blob URL cannot import the codec at runtime, so the worker's source text has to contain core's compiled JavaScript. Inside one package that is one build producing two entries from one tree, and the embedded codec is by construction the same bytes the vectors suite has just validated. Across two packages it becomes the compiled source of one package embedded in another, which is the version skew the split was meant to avoid.

The embedding is an esbuild invocation that emits core's compiled JavaScript as a string constant in the webrtc entry. It is not done with `Function.prototype.toString`, which breaks on class fields and under any minifier. The consumer still sees one import, so the no-bundler-tricks rule in the tech stack holds.

`exports` names both entries with the `types` condition first in each, because a top-level `types` field is ignored once `exports` exists. `typesVersions` repeats the subpath so that consumers on `moduleResolution: node`, which ignores `exports` entirely, still resolve types for `seimark/webrtc`.

## `seimark`

The exported surface is `browser/src/index.ts`, and the generated
`dist/index.d.ts` is its authority; it is not transcribed here, because a copy
in prose goes stale the first time a signature changes. What follows is why the
surface has the shape it has.

The names mirror `marker` and `h264` in the Go library so that a reader of one finds the other. The behaviour mirrors them too, and the vectors hold both to it.

### Codec

`decodeMarker` reads the layout in `docs/format.md` exactly. Reserved flag bits are ignored. Trailing bytes after the body are ignored, so a future minor addition does not break a version 1 reader. A version other than 1 is `unsupported_version`; a body shorter than its fields require, including a payload length that overruns the body, is `truncated`.

`encodeMarker` returns the body only. The payload flag is set when `payload` is not null; a non-null empty payload is written as a payload of length zero, which is how the Go encoder behaves. Neither `004`, a truncated error case, nor `006`, which carries a five-byte payload, covers it, so `vectors/markers/008-empty-payload` was added for it: without a vector the rule is pinned down by nothing and the two implementations could drift.

A payload longer than `PAYLOAD_HARD_LIMIT` is `payload_too_large`. The soft limit is the writer's concern, not the codec's.

`sequence` is a `number` holding a uint32 and wraps modulo 2^32, as the format says. `encodeMarker` throws `invalid_argument` for a `streamId` whose length is not 8, and for an `originTimeUs` outside the signed 64-bit range: both are caller mistakes, and `truncated` means the input was shorter than the format needs, which would mislead a caller switching on the code.

`originTimeUs` is a `bigint`, not a `number`. The field is a signed 64-bit count of microseconds and the vectors include a negative time; a `number` would decode `007-negative-time` incorrectly and would lose precision at the top of the range. The cost is ergonomics at the boundary, which `attach` hides from the page.

`streamId` is a `Uint8Array` of exactly 8 bytes. A different length is a programmer error and throws from the `Writer` constructor rather than being silently padded.

### Access units

`RTCEncodedVideoFrame.data` for H.264 carries **Annex B** bytes: NAL units separated by three-byte or four-byte start codes. libwebrtc's encoder output and its RTP packetiser work on start codes, WebKit converts the VideoToolbox output to Annex B before handing frames to WebRTC, and the receive-side depacketiser reassembles with start codes. The RTP packetiser finds NAL unit boundaries by scanning for them, so a frame rewritten in any other framing does not packetise and the call carries nothing.

This is the framing the browser package walks. A length-prefixed walker is kept as well, because the same core is used off the wire by the Node-side consumers named in the mission, but Annex B is the path the transform takes and the one the smoke test exercises.

```ts
export type Framing = 'annexb' | 'length';
export function detectFraming(au: Uint8Array): Framing | null;
export function nalUnits(au: Uint8Array, framing: Framing): Uint8Array[];
```

Detection mirrors the Go rule: a leading `00 00 01` or `00 00 00 01` is Annex B, otherwise a first four bytes that read as a big-endian length within the buffer is length-prefixed, otherwise null. The transform passes `'annexb'` explicitly and never pays for detection.

`nalUnits` returns subarrays of the input, never copies. In Annex B it accepts both start code lengths and strips trailing zero bytes from a unit, since a three-byte start code preceded by a zero is indistinguishable from a four-byte one. In length-prefixed framing a length of zero, or one that overruns the buffer, is `truncated` with the byte offset in the message.

Zero-length units, which back-to-back start codes produce, are dropped and do not reach the output of `mark`. This matches the Go writer, and it means a rebuilt access unit is not always byte-identical to its input plus a marker. That is stated here because it is the kind of difference a round-trip test otherwise discovers as a surprise.

**Start code width is preserved per unit.** `mark` records the width each NAL unit arrived with and re-emits it, and writes the marker with a four-byte start code. Rewriting every unit to one width would change the frame's length and byte positions for no reason, and some packetisers are sensitive to the leading unit's start code.

`markersIn` walks the units, takes those of type 6, undoes emulation prevention, parses the SEI messages, and decodes those whose UUID is seimark's. Foreign SEI and foreign unregistered UUIDs are skipped without error.

Emulation prevention, in both directions, is implemented here rather than taken from a dependency: it is a dozen lines, and core has no dependencies by design.

### Writing the SEI NAL unit

The Go side gets the container from mp4ff. Core has no dependency, so it builds and parses the container itself, and `vectors/nal/001-spec-example.bin` is what holds it to the format:

- NAL header byte `0x06`.
- `payloadType` 5, coded as a run of `0xFF` bytes plus a final byte. Fixed at one byte here, since 5 is the only type written.
- `payloadSize` = 16 + body length, coded the same way. A body over 239 bytes pushes the size past 255 and needs the `0xFF` run, and the payload limit allows 65535, so the run is written and parsed, not assumed away.
- The 16 UUID bytes, then the body.
- `rbsp_trailing_bits`, one `0x80` byte.
- Emulation prevention applied across the whole RBSP after the header byte.

The parser reads the same coding, including a `0xFF` run, and treats a size that overruns the unit as `unparsable_sei`.

### Writer

`mark` inserts one marker into the access unit and returns a new buffer; it never aliases the input. Placement follows **ADR 0005**: after any access unit delimiter, parameter sets and existing SEI NAL units, and before the first VCL NAL unit, types 1 to 5. An access unit with no VCL NAL unit is `no_vcl`. An access unit that already carries a seimark marker is `already_marked`.

This placement is worth stating plainly because the obvious shortcut in a browser transform, concatenating the SEI NAL unit onto the end of `frame.data`, is what most published examples do and is what ADR 0005 rejected: in a keyframes-only stream an appended marker and a prepended marker produce the same bytes, and the reader attributes the marker to the wrong picture in one of the two cases.

`mark` takes `isKeyframe` from the caller instead of scanning for an IDR NAL unit. The transform already knows, from `frame.type`, and that is the authoritative answer for the encoder that produced the frame. Keeping it a parameter is what allows core to have no browser knowledge.

`keyframesOnly` leaves a non-keyframe access unit unmarked and returns it unchanged with `marked: false`. The sequence counter advances only on marked units, so a consumer counting gaps counts marked frames, which is the same rule as Go.

`streamId` omitted means eight bytes from `crypto.getRandomValues`. `crypto` is available in workers and in Node 19 and later, so no polyfill is needed.

### Advisory conditions

Go returns two conditions alongside a usable result: `ErrPayloadAboveSoftLimit` from `Mark`, which marked the unit anyway, and `ErrUnparsableSEI` from `Markers`, which finished the scan. A thrown exception cannot also return a value, so a TypeScript translation that only throws would have to choose between losing the data and hiding the condition.

Core therefore separates the two kinds. A condition that makes the result meaningless throws: `unsupported_version`, `truncated`, `payload_too_large`, `no_vcl`, `already_marked`. A condition that leaves a usable result is returned with it: `mark` returns `warning`, non-null for `payload_above_soft_limit`, and `markersIn` returns `warnings`, carrying one `unparsable_sei` per SEI NAL unit it could not parse while the scan continued. A message that carries the seimark UUID and fails to decode still throws, because a marker this reader is supposed to understand and cannot is a real failure and not foreign data.

The transform layer collapses this into never-throw: it catches the throwing kind and reports it through `onError`, and passes the returned kind to `onError` too.

### Reading and stripping

`markersIn` is the read path and it is a function, not a class. A `Reader` class was considered and dropped; it held no state and was `markersIn` with a `this`.

The receive-side state a subscriber actually wants is gap and duplicate detection, which is per stream id and not per access unit, so it lives in the transform layer where the stream exists, not in the codec. `reader()` keeps expected-next-sequence per stream id and reports gaps, which is also what the smoke test's continuity assertion needs.

`stripMarkers` removes every SEI NAL unit carrying a seimark marker and rebuilds the access unit in its framing, mirroring Go. It is what lets a page forward an already-stamped stream: strip, then mark again. Without it `already_marked` is a dead end for a relay.

`hasIDR` exists for callers of core that have no `frame.type` to consult, which is what the Go-parity tests are.

## `seimark/webrtc`

The exported surface is `browser/src/webrtc/index.ts`. The receive side gets its
own handle: a sender's carries one stream id, `framesMarked` and `setPayload`,
none of which mean anything on a receiver, which sees whatever stream ids arrive
and marks nothing.

### Application payload

The payload cannot be a callback. On the standard path the writer runs in a worker and a function on the main thread is not reachable from it; a callback in the API would work on the fallback path and silently fail on the standard one.

Instead the page pushes: `handle.setPayload(bytes)` stores the value, and on the worker path posts it to the worker, where it is held and written into every marked frame until it is replaced or set to null. The page therefore controls what is stamped, at the cost of not being able to compute a value per frame from the main thread, which is not possible across the worker boundary anyway.

A payload above the soft limit is reported once through `onError` when it is set, not once per frame.

### Choosing the API

`RTCRtpScriptTransform` where the constructor exists, `createEncodedStreams` otherwise.

The plan for this phase assumed Chrome still shipped only the older, non-standards-track `createEncodedStreams`, and that the fallback would therefore be the path Chrome took every day. That is no longer true. Measured on 2026-09-13 with the Playwright browser builds this repository tests against:

| Browser | `RTCRtpScriptTransform` | `createEncodedStreams` |
|---|---|---|
| Chromium 153 | yes | yes |
| Firefox 155 | yes | no |

Since `attach` prefers the standard API, both browsers take it. **The `createEncodedStreams` fallback has no browser coverage.** It is exercised only through `FrameHandler`, the frame-handling function both paths share, which the Node tests drive directly with a fake frame. That covers the marking logic and none of the plumbing that is specific to the fallback: the `createEncodedStreams` call itself, the `pipeThrough`/`pipeTo` chain, and the pass-through form of `detach`. The fallback is kept because it is the only path in Chromium versions older than the standard API's arrival there, but it is now a compatibility shim and the design no longer claims it carries equal test weight.

The difference the package hides: the standard API runs the transform in a worker and is constructed with one, the fallback returns a readable and a writable on the main thread. The same writer code runs in both, which is why `Writer` takes its clock and its keyframe flag as inputs rather than reaching for them.

The frame-handling function is exported from its own module and takes a frame-like object, a `Writer` and a clock. Both paths call it, and the Node tests call it directly, so the logic is tested without a worker and without `onrtctransform` wiring.

**`attach` must be called before the frames it is to mark exist.** A transform installed on a sender that is already negotiated and sending sees `framesSeen: 0` on a call that is visibly working: the frames that already exist are never routed through it. In practice this means attaching after `addTrack` and before `setLocalDescription`. Nothing in the encoded transform specification says so and nothing in the API reports it, which is why it is stated here; the demo page and the loopback fixture both attach in that order.

`attach` throws synchronously, before any frame flows, for conditions settled up front: no encoded transform in the browser (`unsupported_browser`), a stream id whose length is not 8 or a sender with no track (`invalid_argument`), a sender already attached (`already_attached`), and a blob worker that the page's content security policy refuses (`csp_blocked`). These four are the integration's own codes and are distinct from the codec's, so a caller switching on `code` can tell a setup mistake from a bad frame. The last one is a real deployment failure — a policy without `worker-src blob:` blocks the worker the package builds — so it is detected at attach and reported as a policy error naming the directive, not left as a silent stream that never marks.

`detach` differs by path. On the standard path the transform is removed from the sender. On the fallback, `createEncodedStreams` can be called once per sender and the pipe cannot be undone, so `detach` puts the transform into pass-through: frames continue to flow untouched, and the handle stops updating. The behaviour is documented rather than hidden, because a page that detaches and expects the pipeline gone would otherwise be surprised.

`frame.type` is `'key'` or `'delta'`; a frame with neither, or one whose data holds no VCL NAL unit, is passed through unmarked and counted, not reported per frame. A per-frame error callback on a stream that produces them at 30 a second is a log flood, not a diagnostic.

### Time source

The flag in `docs/format.md` says whether the time is the moment of capture or the moment of sending. The source is decided once, when the first frame arrives, and stays constant for the stream.

The probe reads `getMetadata()` on the first frame. `captureTime` present means capture time and flag 1. Absent means time of sending and flag 0, taken as `performance.timeOrigin + performance.now()` in the transform, converted to microseconds.

`captureTime` is specified for outgoing frames: WebRTC Encoded Transform §2.1.1 says that when the frame's owner is an encoder the user agent sets the capture time slot from the capture timestamp, by the method the Absolute Capture Time draft describes. Whether a given browser populates it on the send path is an implementation matter, and this design deferred the answer to the smoke test rather than assume it.

**The measurement, 2026-09-13.**

- **Chromium 153: `captureTime` is absent on sender frames.** The probe chose `'send'`; all 117 marked frames of the loopback run carry flag 0. This is the end-to-end result, not a feature test.
- **Firefox 155: not measured.** No frame ever reached the transform, so the probe never ran. Firefox's H.264 encoder is the OpenH264 GMP, which it fetches at runtime, and the Playwright build ships only `gmp-clearkey`; with H.264 pinned it encodes nothing. The spec's output reports `captureTimeSeen: false` for Firefox, but that is the field's default, not a finding. Firefox's answer is unknown.

So today every seimark stream on Chromium carries send time under flag 0, and a consumer that needs capture time cannot get it from this library there. ADR 0007 records the decision and both states of the measurement. Measuring Firefox is phase 4 work.

`getMetadata` returns the value shifted to be relative to `performance.timeOrigin`, so converting it to Unix microseconds needs the time origin of the context that reads it. On the standard path that is the **worker's** `performance.timeOrigin`, not the document's; the two differ by however long the worker took to start. The conversion is therefore done where the frame is read, in the same context whose origin applies, and never by passing a raw `captureTime` across `postMessage` to be converted on the other side. The same rule makes send time correct on both paths.

Deciding once rather than per frame is deliberate. A per-frame decision would be truthful frame by frame and useless in aggregate: a consumer measuring latency across a stream would silently average two different quantities. A constant flag keeps a stream's markers comparable with each other, which is what the format exists for.

Where `captureTime` is absent, a capture time could still be derived from `metadata.timestamp`, the raw frame's presentation time, plus an offset measured once by the page. That is left to phase 4 and gated on a measurement of the offset's error, because the pairing is sampled on the main thread while the transform runs in a worker, and an estimate written under flag 1 would spend the flag's credibility on a convenience.

### Failing without breaking the call

No failure in the package propagates into the stream pipeline. A frame that cannot be marked is enqueued unchanged, `stats.errors` advances, and `onError` is called with the error and the frame count.

The reason is in the Streams standard rather than in the encoded transform specification, which does not cover it: a throw from a `transform` callback errors both sides of the transform stream, the error propagates through the pipe chain, and no further chunks are processed. In an encoded transform that is the end of the video. A library that adds metadata to somebody's call must never be the reason the call stopped.

`onError` reports the first occurrence of each distinct code and then stays quiet for that code, so a stream failing every frame is a diagnosis rather than a log flood, while a second, different failure is still reported. `stats.errors` counts every one.

`onError` receives a code and a message, not a `SeimarkError`. On the worker path the error crosses `postMessage`, and structured clone reduces an `Error` subclass to a plain `Error`, dropping both the prototype and the `code` field, so an object would arrive stripped of the only part worth switching on. Passing the code explicitly behaves the same on both paths.

The page's own `onError` is called outside the transform on both paths, so a callback that throws cannot error the pipeline it was meant to report on.

This includes errors that are the page's fault, such as a payload above the hard limit. Reporting them and continuing is better than ending the call to punish a caller mistake; `onError` and the counters make them impossible to miss.

This is the never-throw half of the rule; the synchronous throws listed above cover only what is settled before any media flows.

### Last marker

`handle.lastMarker` is a snapshot the page reads synchronously, with no await and no event subscription, so that correlating a UI event with a frame is one property read at the moment of the event.

On the worker path the worker posts the latest marker to the main thread on a coalesced timer, `lastMarkerIntervalMs` and 100 ms by default, rather than once per frame. At 30 frames a second a per-frame message is 30 structured clones a second for a consumer that reads the value at human speed. The same coalescing runs on the fallback path so that both behave alike.

`stats` is updated on the same timer, so the counters lag the stream by at most one interval. That is documented rather than hidden, because a page showing them will otherwise wonder why they are not exact.

## Tests

The conformance suite is the heart of the phase and runs in Node, with no browser. Tests use `node:test` and `node:assert` from the standard library, not a third-party runner: the tech stack forbids third-party assertion frameworks, and the Go side uses standard library `testing` for the same reason.

- **Vectors, reading `../vectors/`.** Every `markers/NNN.bin` decodes to its `.json`, or throws the error code the `.json` names. Every `nal/NNN.bin` yields the marker array its `.json` holds. Every decodable body survives an encode and decode round trip unchanged. `nal/001-spec-example.bin` is also produced by the encoder and compared byte for byte, which is what holds the SEI container, the size coding and the emulation prevention to the specification.
- **Writer parity with Go.** `vectors/streams/testsrc-marked.h264` is Annex B and its `.jsonl` holds what the Go side reads from it. `test/vectors-stream.test.ts` asserts both directions: the TypeScript reader parses the file and produces the same markers, and the writer strips the markers from all 20 access units and restamps them from the `.jsonl`, which must reproduce the Go writer's bytes exactly. 20 of 20 are byte-identical. This works because core keeps the Annex B walker, which the wire needs anyway.
- **Transform behaviour, against a fake frame.** A plain object with `data`, `type` and `getMetadata` drives the exported frame-handling function directly: the probe, never-throw, sequence continuity, keyframes-only, pass-through after `detach`, payload replacement, and the coalesced last-marker timer.
- **One browser smoke test, Playwright.** A loopback `RTCPeerConnection` in one page: attach to the sender, read markers off the receiver, assert the sequence is continuous, the stream id matches and there are no gaps or duplicates. It is the only test that proves a real encoded-transform path works against a real encoder, and it is what produced the `captureTime` measurement above.

  It runs **chromium only** in CI. The Firefox project is still in `playwright.config.ts` and the spec **fails** there rather than being skipped. That is deliberate: with both browsers on the standard API, Firefox is the only browser in the matrix with no fallback to fall back to, so a skip would quietly turn "the standard API is unverified on Firefox" into a green run. The failure names the cause — a browser that negotiates H.264 and then encodes nothing has no H.264 encoder in its build — so it reads as an environment gap rather than a seimark defect. It is isolated: with no seimark in the path, H.264 pinned encodes 0 frames and VP8 pinned encodes 81.

Vectors are never edited to make a test pass. A failing vector means the code or the specification is wrong; which one is decided, and that is what is fixed.

One vector was added this phase, `markers/008-empty-payload`: a marker whose payload flag is set with length zero, which no earlier vector covered and which both implementations must agree on.

## CI

A `browser` job beside the Go `checks` job in `.github/workflows/ci.yml`, running in `browser/`: `npm ci`, then `npm run typecheck` (`tsc --noEmit` against both tsconfigs), `npm test` (`node --test`), `npm run build`, then `npx playwright install --with-deps chromium` and the loopback spec with `--project=chromium`.

Node 22, the `engines` floor, so CI fails on anything the floor cannot run rather than passing on a newer runtime.

`npm ci` needs `browser/package-lock.json`, which is committed deliberately: the three development dependencies are what the build and the tests run on, and a resolved lockfile is what makes a CI run reproduce a local one.

The type check, the vectors and the unit tests gate every pull request. The browser test is the slower tail of the same job.

## Demo page

`browser/demo/index.html`: a static page that publishes over WHIP. A canvas source so no camera is needed, an endpoint field and an optional bearer token, `attach` on the sender before `setLocalDescription`, a field that pushes an application payload through `setPayload`, and a live display of the stream id, the sequence, the time source the probe chose, the origin time and the last marker's payload. It POSTs the offer as `application/sdp`, takes the answer from the 201 body and the resource from `Location`, and DELETEs that resource on stop. It reads `handle.lastMarker` and `handle.stats` on a 200 ms poll, which is the same coalescing the handle already applies. It imports `../dist/webrtc/index.js` directly: no CDN, no bundler, no dependency. It is the manual test for the phase and the starting point for the latency page that follows.

It is written against the WHIP specification and this format. It is not derived from any existing publisher: an SEI appended after the frame data, with another UUID and a text body, is a different format with a placement ADR 0005 rejected.

## Decisions this phase records

- **ADR 0006**: one package with two entry points, and the worker that embeds the compiled codec.
- **ADR 0007**: the time source is probed once per stream and stays constant, with the Chromium measurement recorded and Firefox recorded as outstanding.

`specs/tech-stack.md` gained `node:test`, esbuild and Playwright in the approved table, and its browser API row now carries the measured matrix. The browser entry carries no framework and the published package has no runtime dependency.
