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

```sh
npm install seimark
```

ESM only. Node 22 or later for the tooling; the package itself has no runtime
dependencies.

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
