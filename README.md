# seimark

Per-frame metadata in H.264 SEI: a wall-clock time, a sequence number, a stream identity and a few bytes of your own data, written into every frame by a browser over WebRTC or by a Go program, and read back by Go from a live stream or a recording.

The marker lives inside the compressed frame, so it survives packetisation, media servers that pass SEI through, recording, remuxing and cutting, as long as nobody re-encodes.

## Status

Pre-alpha. The format is specified in [`docs/format.md`](docs/format.md); there is no code yet. The roadmap is in [`specs/roadmap.md`](specs/roadmap.md).

## What it will be

- **A format** you can implement anywhere, with test vectors in `vectors/`.
- **A Go library and CLI**: `seimark dump` reads markers from Annex B streams and MP4 files, `seimark inject` stamps elementary streams.
- **A browser library** that stamps every outgoing H.264 frame with one import.

## Layout

```
specs/      mission, tech stack, roadmap, ADRs
docs/       format specification, component designs
vectors/    conformance test vectors
```

## Licence

MIT.
