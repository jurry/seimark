# ADR 0007: The time source is probed once per stream and stays constant

**Type:** SNAPSHOT. Date: 2026-09-13. Status: accepted.

## Context

The marker carries an origin timestamp and a flag saying what that timestamp is:
the moment the frame was captured, or the moment it was handed to the sender.
`docs/format.md` defines the two values; it does not say how a writer decides
between them.

In the browser the choice is not the writer's to make freely. WebRTC Encoded
Transform §2.1.1 says that when an encoded frame's owner is an encoder, the user
agent sets the frame's capture time from the capture timestamp, by the method the
Absolute Capture Time draft describes, and `frame.getMetadata().captureTime`
exposes it. Whether a given browser actually populates that slot on the send path
is an implementation matter. The phase 3 design deferred the question to the
browser smoke test rather than assume an answer.

The alternative is available on every frame: `performance.timeOrigin +
performance.now()` at the moment the transform sees it, which is send time and
flag 0.

### What the smoke test measured

Measured 2026-09-13 by `browser/e2e/loopback.spec.ts`, a loopback
`RTCPeerConnection` with a canvas source and H.264 pinned by
`setCodecPreferences`:

- **Chromium 153: `captureTime` is ABSENT on sender frames.** The probe chose
  `'send'`. 117 frames were marked and every one carries flag 0. This is an
  end-to-end measurement, not an inference from a feature test.
- **Firefox 155: UNMEASURED.** Firefox's H.264 encoder is the OpenH264 GMP,
  which it downloads at runtime; the Playwright browser build ships only
  `gmp-clearkey` and this environment cannot fetch the GMP. Isolated with no
  seimark in the path, H.264 pinned encodes 0 frames while VP8 pinned encodes 81.
  No frame ever reached the transform, so the probe never ran. The spec's output
  reports `captureTimeSeen: false` for Firefox, but that is the field's default
  value and not a finding about Firefox.

So one browser is measured and says absent, and the other is unknown. Both
possible answers therefore remain live, and the decision has to hold for either.

## Decision

The time source is decided once, on the first frame of the stream, and is
constant for the life of the stream.

The probe reads `getMetadata()` on the first frame that reaches the transform.
`captureTime` present means capture time and flag 1; absent means send time and
flag 0. Every subsequent frame of that stream is written with the flag the probe
chose, without re-reading `captureTime`.

`captureTime` is reported relative to the reading context's
`performance.timeOrigin`, and on the standard path that context is the worker,
whose origin differs from the document's by however long the worker took to
start. The conversion to Unix microseconds is therefore done in the same context
that reads the frame, and a raw `captureTime` is never passed across
`postMessage` to be converted on the other side. The same rule makes send time
correct on both paths.

`handle.timeSource` exposes the probe's choice to the page.

## Consequences

- A stream's markers are comparable with each other. A consumer measuring latency
  across a stream measures one quantity throughout, which is what the format
  exists for.
- A stream whose browser does populate `captureTime` on some frames and not
  others is written entirely with the flag the first frame implied. Frames after
  the first that carry a capture time are stamped with send time anyway. This is
  the deliberate cost: a per-frame decision would be truthful frame by frame and
  useless in aggregate, because it would silently average two different
  quantities under one flag.
- On Chromium today every seimark stream carries flag 0. A consumer that needs
  capture time cannot get it from this library on Chromium, and must not read
  flag 0 as an approximation of capture time.
- The probe runs on the first frame, so `handle.timeSource` reads `'send'` until
  that frame arrives. The value is a default, not yet a measurement, during that
  window.
- Firefox stays outstanding. The measurement needs an environment whose Firefox
  has the OpenH264 GMP; until then the answer for Firefox is unknown, not
  `'send'`.

## Alternatives rejected

- **Decide per frame.** Read `captureTime` on every frame and set the flag from
  what that frame has. Truthful frame by frame, useless in aggregate, as above.
- **Assume capture time is available and write flag 1.** The measurement says
  Chromium does not populate it, so the flag would be a lie on the browser most
  streams run on.
- **Derive capture time from `metadata.timestamp` plus an offset the page
  measures once, and write flag 1.** The frame's presentation timestamp is
  available where `captureTime` is not, and pairing it once with a wall clock
  would give a usable estimate. Rejected for phase 3 and deferred to phase 4,
  gated on a measurement of the offset's error: the pairing is sampled on the
  main thread while the transform runs in a worker, and writing an estimate under
  flag 1 would spend the flag's credibility on a convenience. A consumer cannot
  tell an estimated capture time from a real one.
- **Let the page choose the flag.** Moves a question about the browser's
  behaviour to a caller with less information than the library has. The page can
  still pass `timeSource` to `Writer` directly when it is driving the codec off
  the wire; it is `attach` that probes.
