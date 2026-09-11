# Mission

**Type:** LIVING

## Why seimark exists

Video that leaves a browser over WebRTC carries only relative timing. Once it has passed a media server and landed in a recording, nobody can answer three questions about a given frame: at what wall-clock time was it captured, which frame is it after a reconnect restarted the numbering, and what was the application doing at that moment. Container timestamps are rewritten at every remux, RTP header extensions die at the media server, and side-channel logs drift and have to be aligned by hand.

H.264 has a place for this inside the compressed frame itself: the SEI message of type `user_data_unregistered`. Whatever is written there survives packetisation, a media server that passes it through, recording, remuxing and cutting, as long as nobody re-encodes. Browsers can write there through WebRTC Encoded Transform. The mechanism exists; a correct, documented, cross-browser implementation with a server-side reader does not.

seimark is that implementation: a small, versioned per-frame marker format, a Go library and CLI that read and write it, and a browser library that stamps every outgoing frame.

## Who it is for

- **Engineers running WebRTC ingest** (SRS, MediaMTX, Janus, LiveKit and similar) who need per-frame wall-clock time and identity on the server for latency measurement, quality monitoring and aligning server-side analysis with the client.
- **Teams recording from browsers or phones for frame-accurate work later**: research, telemedicine, inspection, sports analysis, user testing. They need "the frame at 14:03:10.250" and "which frames were lost".
- **Device and robotics developers** streaming from Go or an embedded encoder who want telemetry locked to the frame it belongs to, inside the same file.
- **Anyone synchronising several browser cameras** to one server without hardware timecode.

## In scope

- The marker format: specified in `docs/format.md`, versioned, with test vectors any implementation can check against.
- Go: reading and writing markers in Annex B and length-prefixed access units, iterating MP4 samples, a CLI to dump markers from files and to stamp elementary streams.
- Browser: a TypeScript library that attaches to an `RTCRtpSender` and stamps every H.264 frame, hiding the differences between browser API variants.
- Compatibility with the MISB ST 0604 precision time stamp, so existing tools read the time without knowing this format.
- Documented behaviour of markers across media servers, produced by a sibling project.

## Explicitly out of scope

- **Clock synchronisation.** A marker carries the sender's clock. Estimating offsets between clocks is the consumer's job.
- **Codecs other than H.264** in the first versions. HEVC has SEI and may follow; VP8 and VP9 have no in-bitstream metadata; AV1 has metadata OBUs.
- **Guaranteeing passthrough.** Whether a media server keeps SEI is measured, not promised.
- **Bulk data transport.** Markers are small; a keyframes-only mode exists for larger payloads, but seimark is not a side channel for streaming data.
- **GStreamer or FFmpeg as runtime dependencies** of the library or CLI. Adapters for them may be provided as separate packages or examples.

## Success looks like

- A Go program reads the wall-clock time of any marked frame from a recording in one call.
- A web page stamps its outgoing stream with one import and works in Chrome, Safari and Firefox.
- A third party implements the format in another language from `docs/format.md` and the vectors alone.
