# seimark

**Type:** LIVING

Per-frame metadata in H.264 SEI: a wall-clock time, a sequence number, a stream identity and a few bytes of your own data, written into every frame by a browser over WebRTC or by a Go program, and read back by Go from a live stream or a recording.

The marker lives inside the compressed frame, so it survives packetisation, media servers that pass SEI through, recording, remuxing and cutting, as long as nobody re-encodes.

## Status

Phases 1 to 4 are done: the Go library reads and writes markers and reads
Annex B streams, MP4, FLV and MPEG-TS files, the CLI has `seimark dump`,
`seimark inject` and `seimark nals`, and the browser package in
[`browser/`](browser/README.md) stamps every outgoing H.264 frame over WebRTC.
Phase 5, MISB ST 0604 compatibility, is next. The format is specified in
[`docs/format.md`](docs/format.md); the roadmap is in
[`specs/roadmap.md`](specs/roadmap.md).

The module is not public yet, so build it from a clone:

```
go build -o seimark ./cmd/seimark
./seimark dump recording.mp4
./seimark inject -start now recording.h264 marked.h264
./seimark nals marked.h264
```

## What it is

- **A format** you can implement anywhere, with test vectors in `vectors/`.
- **A Go library and CLI**: `seimark dump` reads markers from Annex B streams and MP4, FLV and MPEG-TS files, `seimark inject` stamps elementary streams.
- **A browser package** that stamps every outgoing H.264 frame with one import; see [`browser/README.md`](browser/README.md).

## Layout

```
cmd/seimark/  the CLI
marker/       marker body codec
h264/         access units, NAL units, markers, Annex B streams, the writer
container/    the sample type the container readers yield
mp4/          video sample iteration
flv/          FLV video tag iteration
ts/           MPEG-TS demultiplexing
specs/        mission, tech stack, roadmap, ADRs
docs/         format specification, component designs
vectors/      conformance test vectors and stream fixtures
```

## Licence

MIT.
