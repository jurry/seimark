# seimark

Per-frame metadata in H.264 SEI: a wall-clock time, a sequence number, a stream identity and a few bytes of your own data, written into every frame by a browser over WebRTC or by a Go program, and read back by Go from a live stream or a recording.

The marker lives inside the compressed frame, so it survives packetisation, media servers that pass SEI through, recording, remuxing and cutting, as long as nobody re-encodes.

## Status

Phase 1 is done: the Go library reads markers from Annex B streams and MP4
files, and `seimark dump` prints them. There is no writer and no browser
library yet. The format is specified in [`docs/format.md`](docs/format.md);
the roadmap is in [`specs/roadmap.md`](specs/roadmap.md).

The module is not public yet, so build it from a clone:

```
go build -o seimark ./cmd/seimark
./seimark dump recording.mp4
```

## What it will be

- **A format** you can implement anywhere, with test vectors in `vectors/`.
- **A Go library and CLI**: `seimark dump` reads markers from Annex B streams and MP4 files, `seimark inject` stamps elementary streams.
- **A browser library** that stamps every outgoing H.264 frame with one import.

## Layout

```
cmd/seimark/  the CLI
marker/       marker body codec
h264/         access units, NAL units, markers, Annex B streams
mp4/          video sample iteration
specs/        mission, tech stack, roadmap, ADRs
docs/         format specification, component designs
vectors/      conformance test vectors and stream fixtures
```

## Licence

MIT.
