# seimark (browser)

**Type:** LIVING

Per-frame metadata in H.264 SEI, for WebRTC senders and receivers in the browser.

A seimark marker carries a stream id, a monotonic sequence number, an origin
timestamp in microseconds, a flag saying whether that time is capture time or
send time, and an optional application payload. The wire format is
`docs/format.md` in this repository; `vectors/` is its conformance suite and the
tests here read it directly, so the TypeScript and Go implementations are held to
the same bytes.

## Install

Not published to npm yet, so `npm install seimark` will not resolve. Until it is:

```sh
npm install /path/to/seimark/browser          # from a local checkout
npm install github:jurry/seimark#main         # once the branch is merged
```

ESM only. Node 22 or later for the tooling; the package itself has no runtime
dependencies.

## Running everything from a clean checkout

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

## Two entry points

| Import | Contains | Needs a DOM |
|---|---|---|
| `seimark` | The marker codec, the SEI container, the Annex B and length-prefixed NAL unit walk, `Writer`, `markersIn`, `stripMarkers`. | No |
| `seimark/webrtc` | `attach`, `reader`, the `RTCRtpScriptTransform` worker and the `createEncodedStreams` fallback. | Yes |

The core entry compiles without the DOM library, so it runs unchanged in Node —
a server-side reader, a test harness, a page that only decodes. ADR 0006 records
why this is one package and not two.

## Stamping a sender

```js
import { attach } from 'seimark/webrtc';

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
called. A library that adds metadata to somebody's call must never be the reason
the call stopped.

`lastMarker` and `stats` are refreshed on a coalesced timer,
`lastMarkerIntervalMs` and 100 ms by default, so they lag the stream by at most
one interval.

## Reading a receiver

```js
import { reader } from 'seimark/webrtc';

pc.addEventListener('track', (e) => {
  const rh = reader(e.receiver, (m) => {
    console.log(m.sequence, m.originTimeUs, m.timeSource);
  });
  // rh.stats has framesSeen, markersFound, gaps, duplicates, streams
});
```

Gap and duplicate detection is per stream id and wraps with the sequence at
2^32.

## Using the codec without WebRTC

```js
import { Writer, markersIn, stripMarkers } from 'seimark';

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
  Deriving a capture time from the frame's presentation timestamp is phase 4
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

## Demo page

`demo/index.html` publishes a canvas-generated H.264 stream over WHIP and shows
the stream id, sequence, the time source the probe chose and the last marker as
they update.

```sh
npm run build
npm run demo          # serves this directory on http://127.0.0.1:8088
```

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

```sh
npm ci
npm run typecheck
npm test          # node:test, reads ../vectors/
npm run build
npm run e2e -- --project=chromium
```

## Licence

MIT.
