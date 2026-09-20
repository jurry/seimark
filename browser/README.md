# seimark (browser)

Per-frame metadata in H.264 SEI, for WebRTC senders and receivers in the browser.

A seimark marker carries a stream id, a monotonic sequence number, an origin
timestamp in microseconds, a flag saying whether that time is capture time or
send time, and an optional application payload. The wire format is
`docs/format.md` in this repository; `vectors/` is its conformance suite and the
tests here read it directly, so the TypeScript and Go implementations are held to
the same bytes. The recording is read back with the Go CLI in the repository
root, [`../README.md`](../README.md).

## Install

```sh
npm install @seimark/browser
```

ESM only. Node 22 or later for the tooling; the package itself has no runtime
dependencies.

## Stamping a sender

```js
import { attach } from '@seimark/browser/webrtc';

const sender = pc.addTrack(track, stream);
const handle = attach(sender, {
  onError: (code, message) => console.warn('seimark', code, message),
});

handle.setPayload(new TextEncoder().encode('take 3'));

// Read at the moment of a UI event; no await, no subscription.
console.log(handle.streamId, handle.timeSource, handle.lastMarker);

handle.detach();
```

`attach` must be called **before** the frames it is meant to mark exist — before
`setLocalDescription`, not after the call is up. A transform installed on a
running sender sees `framesSeen: 0`.

`attach` throws synchronously for what is settled up front: no encoded transform
in the browser, a stream id whose length is not 8, a sender with no track, a
sender already attached. Once frames flow it never throws: a frame that cannot be
marked is passed through unchanged, `stats.errors` advances, and `onError` is
called with the first occurrence of each distinct code — a stream failing every
frame reports once, not thirty times a second, while a second, different failure
is still reported. A library that adds metadata to somebody's call must never be the reason
the call stopped.

`lastMarker` and `stats` are refreshed on a coalesced timer,
`lastMarkerIntervalMs` and 100 ms by default, so they lag the stream by at most
one interval.

## Reading a receiver

The other direction: a page that receives a video track, a viewer or the far
end of a call, sees each incoming frame before it is decoded, and `reader`
pulls the marker out of it. That gives the viewer what the recording gives the
Go CLI, but live: the sender's clock on every frame, so end-to-end latency can
be measured with no server involved (the difference to the local clock is
transit time plus a constant clock offset, and its changes are the latency
changes); gaps and duplicates per stream id, so loss and reconnects show up
on the viewer; and the sender's payload, so the viewer knows which frames
belong to which take or event. Subscribing through a media server with a
reader on the page is also the quickest way to see whether that server keeps
SEI.

```js
import { reader } from '@seimark/browser/webrtc';

// A page that subscribes (WHEP, or any viewer that sends the offer):
const t = pc.addTransceiver('video', { direction: 'recvonly' });
const rh = reader(t.receiver, (m) => {
  console.log(m.sequence, m.originTimeUs, m.timeSource);
});
// then createOffer and negotiate; rh.stats has framesSeen, markersFound, gaps, duplicates, streams

// A page that answers an incoming offer may attach in the track event instead:
pc.addEventListener('track', (e) => reader(e.receiver, onMarker));
```

Attach before the local description is set. When the page is the answerer, the
`track` event fires inside `setRemoteDescription(offer)`, before the answer, so
attaching there is in time. When the page is the offerer, the event fires
inside `setRemoteDescription(answer)`, after the local description, and a
transform set then is never wired: the video decodes and the reader sees
nothing. Measured 2026-09-20 on Chromium 153 against MediaMTX over WHEP, four
runs each way.

Gap and duplicate detection is per stream id and wraps with the sequence at
2^32.

## Two entry points

| Import | Contains | Needs a DOM |
|---|---|---|
| `@seimark/browser` | The marker codec, the Annex B and length-prefixed NAL unit walk, `Writer`, `markersIn`, `stripMarkers`. | No |
| `@seimark/browser/webrtc` | `attach`, `reader`, the `RTCRtpScriptTransform` worker and the `createEncodedStreams` fallback. | Yes |

The core entry compiles without the DOM library, so it runs unchanged in Node —
a server-side reader, a test harness, a page that only decodes. ADR 0006 records
why this is one package and not two.

## Using the codec without WebRTC

```js
import { Writer, markersIn, stripMarkers } from '@seimark/browser';

const w = new Writer({ keyframesOnly: false });
const { data, marked, warning } = w.mark(accessUnit, 'annexb', atUs, isKeyframe, payload);
const { markers, warnings } = markersIn(data, 'annexb');
```

`originTimeUs` is a `bigint`: the field is a signed 64-bit microsecond count and
a `number` would lose precision at the top of the range. `attach` hides this from
the page.

## Limitations

- **The `createEncodedStreams` fallback has no browser coverage.** Measured
  2026-09-13: Chromium 153 and Firefox 155 both expose
  `RTCRtpScriptTransform`, and `attach` prefers it, so both take the standard
  path. The fallback is unit-tested through the shared frame handler but no
  browser in the test matrix exercises it end to end.
- **Firefox is not covered by the end-to-end test in CI.** Firefox's H.264
  encoder is the OpenH264 GMP, downloaded at runtime, and the Playwright build
  does not ship it. The Firefox project stays in `playwright.config.ts` and the
  spec fails there rather than skipping, so the gap stays visible.
- **`captureTime` is absent on Chromium sender frames** (Chromium 153, 117
  marked frames), so the probe chooses `'send'` and markers carry flag 0.
  Deriving a capture time from the frame's presentation timestamp is phase 5
  work. Firefox is unmeasured for the reason above.
- **The payload is pushed, not computed per frame.** On the standard path the
  writer runs in a worker, which cannot call back into the page, so
  `setPayload` stores a value that is stamped into every marked frame until it is
  replaced.
- **`detach` on the fallback path is pass-through, not removal.**
  `createEncodedStreams` can be called once per sender and the pipe cannot be
  undone, so frames keep flowing untouched and the handle stops updating.
- **H.264 only.** Another codec needs an ADR.

## Content security policy

The worker is built from a blob URL, so a page with a restrictive policy needs
`worker-src blob:`. `attach` detects a policy that refuses the worker and throws
naming the directive rather than leaving a silent stream that never marks.

```
Content-Security-Policy: worker-src blob:
```

A policy that sets only `default-src` falls back to it for workers, so either
add `worker-src blob:` alongside it or include `blob:` in `default-src` itself.

## Demo page

`demo/index.html` is both ends of the claim on one page. The **Publish** panel
sends a canvas-generated H.264 stream over WHIP and shows the stream id,
sequence, the time source the probe chose and the last marker as they update.
The **Watch** panel subscribes to the same stream over WHEP with `reader` on
the receiver and shows the incoming sequence, the sender's origin time, the
origin-to-here time (transit plus the constant offset between the two clocks),
markers found, gaps and duplicates. Both panels log every marker to the console,
prefixed `publish` or `watch`, so the two directions interleave there.

![The demo page publishing and watching at once. The Publish panel shows stream
id 654340cf09009aab, sequence 103, time source send, frames seen / marked
104 / 104 and errors 0; the Watch panel shows the same incoming stream id
654340cf09009aab, incoming sequence 107, origin to here 13.9 ms, frames seen
100, markers found 103, gaps 0 and duplicates
0.](demo/screenshot.png)

Regenerate it with `npm run demo:screenshot`, which needs a running WHIP server.

The Watch panel talks plain WHEP and does not care what is at the other end, so
pointing it at another server's WHIP and WHEP URLs is a one-page check of
whether that server passes SEI through.

```sh
npm run build
npm run demo          # serves this directory on http://127.0.0.1:8088
```

`npm run demo` asks the MediaMTX API whether a server is up before it serves,
and prints either the WHIP and WHEP URLs or the compose command to start one.
Then open **http://127.0.0.1:8088/demo/**.

Serve `browser/`, not `browser/demo/`: the page imports the built library from
`../dist/webrtc/index.js`, and a server rooted at `demo/` cannot see it, so the
module never loads and the page sits inert.

Publishing needs a WHIP endpoint. Two of its settings decide whether this works
at all, and neither failure is obvious from the page:

- **CORS.** The demo is served from its own port, so the WHIP POST is
  cross-origin. A server that sends no `Access-Control-Allow-Origin` blocks it
  with `TypeError: Failed to fetch` even though `curl` against the same URL
  succeeds.
- **A reachable ICE candidate**, with the media UDP port listening on the host.
  When it is unreachable the page reports `frames seen 0 / 0` and `errors 0`:
  ICE never leaves `checking`, the encoder never starts, and this library is
  never called.

`e2e/WHIP.md` sets up MediaMTX with both configured correctly, and
`e2e/whip-verify.mjs` runs the whole claim end to end: publish from this page,
let the server record it, then read the markers back out of the recording with
the Go CLI and check that every one survived in order.

## Development

Every check in this repository, in the order to run them. Each step is
independent; you can stop after any of them.

```sh
# 0. the Go side (the reference implementation and the CLI)
cd /path/to/seimark
make test lint

# 1. build the browser package and run its tests
cd browser
npm ci
npm run typecheck
npm test                 # node:test, reads ../vectors/, no browser needed
npm run build

# 2. the loopback browser test: real Chromium, real H.264,
#    attach() stamps and reader() reads back in one page
npx playwright install chromium
npm run e2e -- --project=chromium

# 3. the demo page, by hand
npm run demo             # serves this directory on http://127.0.0.1:8088
#    for something to publish to:
#      docker compose -f e2e/docker-compose.yml up -d
#    then open http://127.0.0.1:8088/demo/ and use the WHIP endpoint
#      http://127.0.0.1:8889/seimark/whip

# 4. the full claim, end to end and automatic:
#    browser -> WHIP server -> recording -> Go CLI reads the markers back
#    starts a WHIP server itself if none is running
go build -o /tmp/seimark ../cmd/seimark
SEIMARK_CLI=/tmp/seimark node e2e/whip-verify.mjs
```

Step 4 is the one that proves what this package is for. It publishes from the
demo page, lets the server record it, then reads the recording with the Go CLI
and fails unless every marker survived in order:

```
page:        frames seen / marked 414 / 414 | sequence 413 | errors 0
markers:     417  seq 0..416
gaps:        0   duplicates: 0
wall clock:  14.95s   container: 14.95s   drift: 0 ms
PASS: every frame the browser stamped survived to the recording, in order.
```

Firefox is excluded from step 2 on purpose: its H.264 encoder is a
runtime-downloaded plugin, absent from the Playwright build, so the spec is left
failing rather than skipped. `npm run e2e` without `--project` will show that
failure.

## Licence

MIT.
