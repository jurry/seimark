# ADR 0006: One browser package with two entry points

**Type:** SNAPSHOT. Date: 2026-09-13. Status: accepted.

## Context

Phase 3 adds a TypeScript implementation of the format. It has two audiences that
do not overlap. One is a WebRTC page: it wants `attach(sender)` and never touches
a NAL unit. The other has no `RTCRtpSender` at all: a Node-side reader of a
recorded stream, the vectors conformance suite, a test harness, a page that only
decodes. The second audience is named in the mission, and it is the audience the
vectors tests belong to, since they run in Node against `../vectors/` with no
browser.

There is also a build constraint peculiar to this library. The standard WebRTC
encoded transform, `RTCRtpScriptTransform`, is constructed with a `Worker`. The
tech stack forbids bundler-specific tricks, so the worker is created from a blob
URL — and a blob worker has no module resolution: it cannot `import` the codec at
runtime. The codec's compiled JavaScript therefore has to be present in the
worker's source text.

## Decision

One npm package, `seimark`, published from `browser/` of this repository, with
two entry points built from one source tree:

- `seimark` — the marker codec, the SEI container, the NAL unit walk in both
  framings, `Writer`, `markersIn`, `stripMarkers`.
- `seimark/webrtc` — `attach`, `reader`, the `RTCRtpScriptTransform` worker and
  the `createEncodedStreams` fallback.

The boundary is enforced by the compiler, not by convention: `tsconfig.core.json`
compiles the core with `lib: es2022` and no DOM, so a reference to
`RTCRtpSender`, `Worker` or `document` in core is a compile error.
`tsconfig.webrtc.json` adds DOM for the second entry.

The worker's source is produced by an esbuild step that emits core's compiled
JavaScript as a string constant imported by the webrtc entry.

`exports` names both entries with the `types` condition first in each, because a
top-level `types` field is ignored once `exports` exists. `typesVersions` repeats
the subpath so consumers on `moduleResolution: node`, which ignores `exports`
entirely, still resolve types for `seimark/webrtc`.

## Consequences

- The codec is tested in Node, on `node:test`, with no browser and no mock of a
  browser API. That is where the vectors suite lives and it is the majority of
  the tests.
- Web Crypto and `performance` are the only globals core needs, and both exist in
  Node 22 and in a worker, so core needs no polyfill and no DOM shim.
- The embedded codec is by construction the same bytes the vectors suite has just
  validated, because it is built from the same tree in the same command.
- The build has an ordering requirement: the worker source must be generated
  before `tsc` runs. `npm run typecheck`, `npm test` and `npm run build` each
  begin with `build:worker` so a fresh checkout cannot typecheck against a stale
  or missing constant.
- A consumer still writes one dependency and one or two imports, so the
  no-bundler-tricks rule in the tech stack holds.

## Alternatives rejected

- **One entry point.** Everything under `seimark`. The codec would then compile
  with the DOM library, and nothing would stop a later change from reaching for
  `RTCEncodedVideoFrame` inside the codec — the separation would be a comment
  rather than a compile error. It also forces a DOM-free consumer to load the
  transform code it cannot use.

- **Two packages, `seimark` and `seimark-webrtc`.** Buys the same separation and
  costs more than it is worth here, for the blob-worker reason above. The worker
  must embed the codec's compiled JavaScript, so across two packages the
  published `seimark-webrtc` would carry a frozen copy of whichever `seimark`
  version was current when it was built. That is precisely the version skew the
  split exists to prevent, and it would be invisible: a consumer upgrading
  `seimark` would get a new reader and an old writer inside the worker, still
  stamping the old bytes. It also doubles the release process for a library whose
  two halves always ship together.

- **Building the worker with `Function.prototype.toString`.** A common trick for
  inline workers. It breaks on class fields, on any minifier that renames
  captured bindings, and it cannot carry the module graph the codec has. esbuild
  emitting a string constant is the same idea done by the build rather than at
  runtime.
