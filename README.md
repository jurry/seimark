# seimark

Video that leaves a browser over WebRTC and lands in a recording loses three
answers: at what wall-clock time was a given frame captured, which frame is it
after a reconnect, and what was the application doing at that moment.
Container timestamps are rewritten at every remux and side-channel logs drift.
seimark answers all three by writing a marker inside the frame's own H.264 SEI,
where it survives packetisation, a media server that passes SEI through,
recording, remuxing and cutting, as long as nobody re-encodes. The typical
pipeline: a page stamps its outgoing frames over WebRTC through WHIP or WHEP to
a media server such as MediaMTX or SRS, using the `user_data_unregistered` SEI
message to carry a per-frame wall-clock timestamp for latency measurement and
frame loss detection, and the Go CLI or library reads the markers back out of
the recording.

## What a marker carries

- **Origin time**, in microseconds, and whether it is send time or capture time.
- **A sequence number**, per stream, wrapping at 2^32.
- **A stream id**, eight bytes identifying the source.
- **An optional application payload**, a few bytes of your own data.

The wire layout is in [`docs/format.md`](docs/format.md).

## Quick start

### Stamp from a browser

Not on npm yet, so `npm install seimark` will not resolve; it will once
published. Until then, clone the repository and run
`npm install <path>/seimark/browser`, or from inside another project:

```sh
npm install ../seimark/browser
```

```js
import { attach } from 'seimark/webrtc';

const sender = pc.addTrack(track, stream);
const handle = attach(sender);
// later, at a UI event:
console.log(handle.streamId, handle.lastMarker);
```

`attach` must be called before `setLocalDescription`. The same package also
has `reader`, which reads markers off an incoming track — a WHEP subscriber or
the far end of a call — for live latency, loss and reconnect detection with no
server involved. See [`browser/README.md`](browser/README.md) for the rest,
including the [demo page](browser/README.md#demo-page) that shows both
directions running at once:

![The demo page publishing and watching at once. The Publish panel shows stream
id 654340cf09009aab, sequence 103, time source send, frames seen / marked
104 / 104 and errors 0; the Watch panel shows the same incoming stream id
654340cf09009aab, incoming sequence 107, origin to here 13.9 ms, frames seen
100, markers found 103, gaps 0 and duplicates
0.](browser/demo/screenshot.png)

### Read the markers back

```sh
go install github.com/jurry/seimark/cmd/seimark@latest
```

```sh
ffmpeg -f lavfi -i testsrc=size=320x240:rate=10 -t 3 -c:v libx264 -bf 0 -f h264 raw.h264
seimark inject -start now -stream-id 0011223344556677 raw.h264 marked.h264
ffmpeg -i marked.h264 -c copy marked.ts
seimark dump marked.ts
```

`dump` prints one JSON object per marker. This one is from the MPEG-TS test vector:

```json
{"au":0,"dts":126000,"pts":126000,"timescale":90000,"sync":true,"time":1.4,"marker_index":0,"version":1,"time_source":"send","origin_time":"2026-09-12T21:00:00.000000Z","origin_us":1789246800000000,"sequence":0,"stream_id":"9f3c1a77e2b04d51","payload":"dGVzdHNyYw=="}
```

| Field | Meaning |
|---|---|
| `au` | 0-based index of the access unit or sample |
| `dts`, `pts`, `timescale`, `sync`, `time` | from the container; absent for Annex B |
| `marker_index` | position among the markers of this access unit |
| `version`, `time_source`, `origin_time`, `origin_us`, `sequence`, `stream_id`, `payload` | the marker itself |

## Three ways to use it

**Stamp from a browser.** One import attaches to an `RTCRtpSender` and stamps
every outgoing frame; see [`browser/README.md`](browser/README.md). The page
gets the stream id, the time source it settled on, and the last marker sent,
for correlating a UI event with a frame.

**Read from a browser.** The same package has `reader`, which attaches to an
`RTCRtpReceiver` and reads markers off an incoming track — a WHEP subscriber
or the far end of a call — for live latency, loss and reconnect detection.

**Stamp from Go or the CLI.** `seimark inject` marks every access unit of an
Annex B stream, or only IDR units with `-keyframes-only`; `-start` sets the
origin time of the first unit, `-stream-id` sets the eight-byte id. `seimark
nals` lists the NAL units of every access unit for debugging, marker and
foreign SEI alike.

**Read markers back.** `seimark dump` reads Annex B, MP4, FLV and MPEG-TS,
with the format detected from the file's own bytes or forced with `-format`.
`-out jsonl` (default) or `-out csv` picks the output shape; `-all` also
prints access units that carry no marker. In Go, the three container readers
yield the same sample type:

```sh
go get github.com/jurry/seimark
```

```go
f, err := os.Open("recording.mp4")
// handle err
defer f.Close()

for s, err := range mp4.VideoSamples(f) {
    // handle err
    markers, err := h264.Markers(s.Data, s.Framing.H264())
    // handle err
    for _, m := range markers {
        fmt.Println(m.OriginTime, m.Sequence)
    }
}
```

`flv.VideoSamples(r)` and `ts.VideoSamples(r)` take an `io.Reader` the same
way and yield the same `container.Sample` type, so an FLV or MPEG-TS
recording is read with the same loop.

## What it cannot do

- **No clock synchronisation.** A marker carries the sender's own clock; estimating the offset between clocks is the consumer's job.
- **Passthrough is not promised.** Whether a media server keeps SEI is measured per server, not guaranteed by this library.
- **H.264 only.**
- **`inject` reads Annex B only.** An MP4 input is refused; MP4 in place is a later phase.
- **MP4 is read into memory**, not streamed.
- **FLV: legacy AVC tags only.** Enhanced RTMP FourCC tags end the read with an error naming the codec.
- **A PES without timestamps reports a zero DTS and PTS**, because the transport stream states no other time for it.

## Status

Phases 1 to 4 are done: the Go library reads and writes markers, reads Annex B
streams, MP4, FLV and MPEG-TS files, the CLI has `seimark dump`, `seimark
inject` and `seimark nals`, and the browser package stamps every outgoing
frame over WebRTC. Phase 5, MISB ST 0604 compatibility, is next. The roadmap
is in [`specs/roadmap.md`](specs/roadmap.md); what has shipped so far is
summarised in [`CHANGELOG.md`](CHANGELOG.md).

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
