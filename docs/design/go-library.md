# Design: Go library and CLI

**Type:** LIVING. Describes the Go side: the marker codec, the access-unit and writer work in `h264`, the container readers for MP4, FLV and MPEG-TS, and the CLI. Rewritten as later phases change it.

## Shape

Module `github.com/jurry/seimark`, Go 1.26, `CGO_ENABLED=0`, two dependencies: `github.com/Eyevinn/mp4ff` v0.56.0 and `github.com/asticode/go-astits` for MPEG-TS demultiplexing (ADR 0008).

| Package | Owns | Depends on |
|---|---|---|
| `marker` | The marker body codec from `docs/format.md`: types, constants, `Decode`, `Encode`, errors. | standard library |
| `h264` | Access-unit level work: format detection, NAL unit splitting, finding markers in an access unit, splitting an Annex B byte stream into access units, and the writer that puts markers into access units. | `marker`, mp4ff `avc` and `sei` |
| `container` | The `Sample` type the three readers yield. | standard library |
| `mp4` | Iterating the video samples of an MP4 file, progressive or fragmented, with timing and sync flags. | `container`, mp4ff `mp4` |
| `flv` | Iterating the AVC video tags of an FLV stream with timing and keyframe flags. | `container`, standard library |
| `ts` | Demultiplexing the first H.264 elementary stream of an MPEG-TS stream into access units. | `container`, `h264`, go-astits |
| `cmd/seimark` | The CLI: `dump`, `inject`, `nals`. | all of the above |

Principles: bytes in, bytes out, no ownership tricks beyond documented subslicing; no state except in the writer; iterators (`iter.Seq2`) for anything that can be large; errors returned with context, never logged; nothing in the library prints.

## `marker`

```go
const Version = 1
const FixedSize = 22                 // body bytes without the optional payload
const PayloadSoftLimit = 4096
const PayloadHardLimit = 65535
func FormatUUID() [16]byte // returns {0x44, 0xa7, 0x3c, 0xb9, 0xb3, 0x6c, 0x45, 0x9a, 0x8f, 0x1a, 0xa3, 0xaa, 0x43, 0x1f, 0x62, 0x4a}

type TimeSource uint8
const (
    TimeSend    TimeSource = 0
    TimeCapture TimeSource = 1
)

type Marker struct {
    TimeSource TimeSource
    OriginTime time.Time // UTC, microsecond precision; Decode truncates to microseconds
    Sequence   uint32
    StreamID   [8]byte
    Payload    []byte    // nil when the payload flag is clear; may be empty but non-nil when the flag is set with length 0
}

var (
    ErrUnsupportedVersion error
    ErrTruncated          error
    ErrPayloadTooLarge    error
)

func Decode(body []byte) (Marker, error)
func (m Marker) Encode() ([]byte, error)
func IsFormatUUID(uuid []byte) bool
```

- `Decode` reads exactly the layout in `docs/format.md`. Reserved flag bits are ignored. Trailing bytes after the body are ignored, so a future minor addition does not break version 1 readers.
- `Encode` returns the body only. The payload flag is set when `Payload != nil`. Payload length above `PayloadHardLimit` is `ErrPayloadTooLarge`; the soft limit is the writer's concern, not the codec's.
- `OriginTime` is stored as microseconds since the Unix epoch; `Encode` truncates to microseconds, so `Decode(Encode(m))` equals `m` only after that truncation, which the tests state explicitly.

## `h264`

```go
type Format int
const (
    FormatUnknown Format = iota
    FormatAnnexB
    FormatLengthPrefixed
)

var (
    ErrUnknownFormat error
    ErrUnparsableSEI error
)

type SEIMessage struct {
    Type    uint
    UUID    [marker.UUIDSize]byte
    HasUUID bool
    Marker  *marker.Marker
    Payload []byte
}

func DetectFormat(au []byte) Format
func NALUnits(au []byte, f Format) ([][]byte, error)
func SEIMessages(nal []byte) ([]SEIMessage, error)          // error may wrap ErrUnparsableSEI
func Markers(au []byte, f Format) ([]marker.Marker, error)  // error may wrap ErrUnparsableSEI
func AccessUnits(r io.Reader) iter.Seq2[[]byte, error]
func AccessUnitsAt(r io.Reader) iter.Seq2[AccessUnit, error] // the same cut, with each unit's offset in the input
```

- **Format detection.** A leading `00 00 01` or `00 00 00 01` is Annex B. Otherwise, if the first four bytes read as a big-endian length between 1 and `len(au) - 4`, the unit is length-prefixed. Anything else is unknown, and `Markers` returns an error for it. Callers that know the format pass it and skip detection.
- **NAL units.** Annex B splitting uses mp4ff `avc.ExtractNalusFromByteStream`. Length-prefixed splitting is our own walker: a four-byte big-endian length, required to be at least 1 and no larger than the bytes left, otherwise an error naming the byte offset. mp4ff `avc.GetNalusFromSample` is not used because it panics on a corrupt length. Returned slices are subslices of `au`.
- **Building a marker NAL unit.** `UserDataSEINAL(uuid [16]byte, body []byte) ([]byte, error)` wraps mp4ff `avc.CreateSEINalu` with one `user_data_unregistered` message. It exists in phase 1 for the fixture generator and the tests; the phase 2 writer builds on it. Verified against the worked example in `docs/format.md`: mp4ff produces the same 44 bytes.
- **SEI messages.** `SEIMessages` classifies the messages of one SEI NAL unit, header byte included: it drops the header byte, runs mp4ff `sei.ExtractSEIData`, and for every message records the payload type, the unregistered UUID where there is one, and the decoded seimark marker where the UUID is seimark's. A NAL unit too short to hold an RBSP, or one mp4ff cannot parse, is `ErrUnparsableSEI`. `Markers` and `seimark nals` are both built on it, so there is one walk and the two commands cannot disagree.
- **Markers.** For every NAL unit of type 6: `SEIMessages`, then keep the decoded markers. Foreign SEI and foreign unregistered UUIDs are skipped without error. An SEI NAL unit mp4ff cannot parse is skipped but not hidden: the scan finishes and returns the markers found together with an error wrapping the exported `ErrUnparsableSEI`, which callers can test with `errors.Is` and treat as advisory — the CLI warns on it and keeps going. A message with the seimark UUID that fails to decode ends the scan at once and returns the markers found so far with that error. The order of the result is the order in the access unit.
- **Access units from a byte stream.** `AccessUnits` reads an Annex B stream incrementally through a `bufio.Reader`, finds start codes, and groups NAL units into access units with the standard rule and no exception: once the current access unit contains a VCL NAL unit (types 1 to 5), the next NAL unit starts a new access unit if it is an access-unit delimiter, SPS, PPS or SEI, or if it is a VCL NAL unit whose `first_mb_in_slice` is zero. `first_mb_in_slice` is the first Exp-Golomb value after the header; it is zero exactly when the first bit of the byte after the header is 1, so the check is `nal[1] & 0x80 != 0`. Each yielded access unit is Annex B bytes with four-byte start codes, valid input for `Markers` with `FormatAnnexB`. A read error ends the sequence with that error; a final access unit without a trailing start code is yielded before the sequence ends. Leading bytes before the first start code are ignored. `AccessUnitsAt` is the one implementation; it makes the same cut and also reports where in the input each unit's first start code sits, and `AccessUnits` is the offset-free view of it. The offset exists because a unit's own bytes are re-framed and so cannot be measured back onto the input, which is what `ts` needs to attribute timestamps.

## `container`

The three container readers yield the same type, so a consumer reads MP4, FLV and MPEG-TS alike and `dump` has one emit loop.

```go
type Framing int
const (
    FramingLengthPrefixed Framing = iota // MP4 and FLV: four-byte big-endian lengths
    FramingAnnexB                        // MPEG-TS: start codes
)

type Sample struct {
    Index     int     // 0-based position in the stream
    DTS       uint64  // decode time in Timescale units
    PTS       int64   // presentation time in Timescale units
    Timescale uint32
    Sync      bool
    Framing   Framing // how Data carries its NAL units
    Data      []byte
}

func (f Framing) H264() h264.Format
func (f Framing) String() string // "length-prefixed" or "annexb", via H264
```

- `Framing` is the one field the mp4-only struct did not have: MP4 and FLV store length-prefixed NAL units, MPEG-TS carries Annex B, and a consumer that hardcoded `h264.FormatLengthPrefixed` would silently find no markers in a transport stream. Carrying the framing with the bytes makes the mistake impossible.
- The type lives in its own package rather than in `mp4`, so `flv` and `ts` do not import an MP4 parser to name their result, and rather than in `h264`, which knows nothing about containers.
- `Framing.H264` maps to the `h264.Format` constant the marker and NAL calls take. It is the only place the mapping is written.
- This replaces `mp4.Sample`, which is an API break: `mp4.VideoSamples` now yields `container.Sample`. The module is at v0 and the only consumers are in this repository and the sibling measurement project, so the break is taken rather than kept as an alias.

## `mp4`

```go
var ErrNoVideoTrack error
var ErrMalformedFile error

func VideoSamples(r io.ReadSeeker) iter.Seq2[container.Sample, error]
```

- Yields `container.Sample` with `Framing` always `FramingLengthPrefixed`. `PTS` is DTS plus the composition offset minus the first edit-list media time, if any.
- Decodes the file with mp4ff `mp4.DecodeFile` in normal mode, which reads the file into memory. Lazy mdat reading is a later improvement; phase 1 fixtures are small and the API does not change.
- Picks the first track whose handler is `vide` and whose sample entry is `avc1` or `avc3`. A video track in another codec is passed over, so an H.264 track behind an HEVC one is still found; when no track qualifies the error is `ErrNoVideoTrack` naming the sample entries that were seen.
- Progressive files: iterate with the stbl helpers the way mp4ff's own `mp4ff-nallister` does: `Stsz` for count and size, `Stsc.ChunkNrFromSampleNr` and `Stco` or `Co64` for the byte range, `Stts.GetDecodeTime`, `Ctts.GetCompositionTimeOffset` when present, `Stss` for sync (absent `Stss` means every sample is sync), `mdat.ReadData` for the bytes.
- The tables are validated before the walk, because the mp4ff helpers index them without bounds checks and panic on a file whose tables disagree: `Stsd`, `Mdhd` and `Tkhd` must be present, `Stsc` must have an entry, `stco` or `co64` must be there, `stts` and, when present, `ctts` must cover at least `Stsz.SampleNumber` samples, and every chunk number `Stsc` produces must be in the offset table. A failure is `ErrMalformedFile`. Each sample is then built inside a function that recovers a panic from mp4ff and returns it as `ErrMalformedFile`, so an unforeseen index error is an error and not a crash.
- Fragmented files: for every fragment, `GetFullSamples(trex)`; data, decode time and composition offset come from the full sample. Sync is `!mp4.DecodeSampleFlags(Flags).SampleIsNonSync`, the test ISO 14496-12 defines; mp4ff's `Sample.IsSync` also requires `sample_depends_on == 2`, which the standard does not, so a sync sample written with `depends_on` unknown would be missed. A fragment whose `Moof` or `Mdat` is missing ends the iteration with `ErrMalformedFile`, because mp4ff panics on it.
- Edit list: only the first entry of the first `elst` is honoured. A positive media time is subtracted from every `PTS`; a media time of -1 (an empty edit) adds the entry's segment duration converted from the movie timescale to the track timescale. Anything more elaborate is out of scope.

## `flv`

```go
var (
    ErrNotFLV       error
    ErrNoVideoTrack error
    ErrMalformedFLV error
)

const Timescale = 1000 // FLV timestamps are milliseconds

func VideoSamples(r io.Reader) iter.Seq2[container.Sample, error]
func (s *Stream) SPS() []byte
func (s *Stream) PPS() []byte
```

- Hand-written on the standard library. FLV is a nine-byte header, then tags of a one-byte type, a three-byte data size, a three-byte timestamp with a fourth byte of high bits, a three-byte stream id, the data, and a four-byte previous-tag size. Reading that needs no library, and the only Go libraries that offer it bring a second MP4 parser with them (ADR 0008).
- **`io.Reader`, not `io.ReadSeeker`.** An FLV file is a forward list of tags with no index to seek to, and the recordings this reads come off a pipe or an HTTP body as often as off a disk. The same holds for `ts`; only `mp4` needs seeking, because its sample tables may sit behind the media data.
- **Legacy AVC only.** A video tag whose codec id is 7 in the low nibble of its first data byte is AVC. Its second byte is the AVC packet type: 0 is the sequence header holding the `AVCDecoderConfigurationRecord`, from which the SPS and PPS are kept and exposed; 1 is a NAL unit sample; 2 is the end of sequence and is skipped. Only the first SPS and the first PPS of the record are kept. Bytes 3 to 5 are the composition offset, signed 24-bit.
- **Enhanced RTMP is not read.** A tag whose first data byte has the high bit set carries a FourCC codec identifier rather than a codec id, which is how `hvc1`, `av01` and the enhanced `avc1` are signalled. Those tags end the iteration with `ErrNoVideoTrack`, naming the FourCC, rather than being silently skipped: a file of enhanced tags would otherwise dump as an empty stream and look like a file without markers. Reading them is a later phase if a server that writes them turns up.
- **Timing.** `DTS` is the tag timestamp in milliseconds, `Timescale` is 1000, `PTS` is `DTS` plus the composition offset. A tag timestamp is unsigned 24 bits plus an eight-bit extension, so it does not wrap inside a recording of any plausible length.
- **Sync** is the frame type in the high nibble of the first data byte: 1 is a keyframe.
- **Data** is the tag's remaining bytes, which are already length-prefixed NAL units with the length size from the configuration record. A length size other than 4 is `ErrMalformedFLV`, because `container.Sample` promises four-byte lengths and rewriting the prefixes to hide a 1- or 2-byte size would copy every sample for a case no encoder in this path produces.
- Audio, script and other tags are skipped. A file whose first three bytes are not `FLV` is `ErrNotFLV`. A tag whose declared data size runs past the end of the stream is `ErrMalformedFLV` naming the tag offset. The reader allocates the declared size before reading the body, which the 24-bit size field bounds at 16 MiB.
- Parameter sets are not spliced into the samples. A marker reader does not need them, and inventing an in-band SPS the file does not contain would change the bytes a consumer sees. `SPS` and `PPS` expose them for a consumer that does need them.

## `ts`

```go
var (
    ErrNoVideoTrack error
    ErrMalformedTS  error
)

const Timescale = 90000 // the MPEG-TS 90 kHz clock

func VideoSamples(r io.Reader) iter.Seq2[container.Sample, error]
```

- Demultiplexing is go-astits (ADR 0008): PAT, PMT, packet reassembly and PES header parsing, none of which is specific to this project. Above it, our own code does the two things that are: concatenating the PES payloads of the elementary stream and cutting access units out of the result with the same `h264.AccessUnits` rule the Annex B reader uses. One rule, one implementation, so the three readers cannot disagree about where an access unit begins.
- **Stream selection.** The first PMT elementary stream whose type is H.264 (0x1B) is read; the rest, audio included, are dropped. No H.264 stream in the first PMT is `ErrNoVideoTrack` naming the stream types that were seen, which matches `mp4.ErrNoVideoTrack`; a stream carrying no PMT at all is the same error. Stream types are named by number, because go-astits has no `String` method on `StreamType`. Video packets arriving before the first PMT are dropped, since until the PMT is read there is no way to tell which PID carries the video.
- **Sync from the IDR NAL unit.** A recording usually starts mid-stream, so the first access units may be non-IDR pictures referring to frames that are not there and may be missing their parameter sets. Nothing is yielded until an access unit containing an IDR NAL unit (type 5) is seen; from there every access unit is yielded. `Index` counts from 0 at that first IDR, not at the first packet, so the indices in `dump` output are contiguous.
- **Timing.** The PTS and DTS of a PES packet apply to the access unit that *starts* in that packet. A PES packet can carry more than one access unit and an access unit can span several PES packets, so the rule is: when the cut yields an access unit, it takes the timestamps of the PES packet in which its first byte arrived. An access unit that begins before the first PES with a timestamp, which can only be at the start of a recording, is dropped with the rest of the pre-IDR units. `Timescale` is 90000. A PES without a DTS takes DTS from PTS, which is what `PTS_DTS_flags == 2` means. A PES carrying no timestamps at all gives its access unit a DTS and a PTS of zero, because the stream states no other time for it.
- **Wrapping is not unwrapped.** The 33-bit clock wraps after about 26.5 hours. Timestamps are reported as they appear in the stream, so a recording that crosses a wrap shows a jump. Unwrapping would need a heuristic about how far back a timestamp may legitimately go, and a marker carries its own wall-clock time, which is the answer to the question a jump would otherwise raise. Documented here and in the `ts` package documentation.
- **Data** is Annex B with four-byte start codes, as `h264.AccessUnits` produces, so `Framing` is `FramingAnnexB`.
- **Attributing a timestamp needs the payload offset, not a byte count.** `h264.AccessUnits` normalises three-byte start codes to four and drops trailing zeros, so the yielded units are not the same length as the bytes they were cut from: a measured 12-byte input yields 14 bytes of units. Summing the unit lengths to track position in the payload therefore drifts, and a PES boundary lands on the wrong access unit. The offset comes from the cutter instead: `h264.AccessUnitsAt` reports, with each unit, the position in its input of the start code the unit begins at, and the reader binary-searches the PES table for the packet whose byte range contains it. A future streaming rewrite must keep that property.
- A stream that is not MPEG-TS, or whose packets do not align, is `ErrMalformedTS`; go-astits' own errors are wrapped with the context of what was being read.

## `cmd/seimark dump`

```
seimark dump [-format auto|annexb|mp4|flv|ts] [-out jsonl|csv] [-all] FILE
```

- `-format auto` (default) reads the first bytes and picks: `mp4` for a box header whose type is `ftyp`, `moov`, `moof` or `styp`; `flv` for the signature `FLV` followed by version 1; `ts` for the sync byte `0x47` at offsets 0 and 188, which distinguishes a transport stream from a file that merely starts with `0x47`; `annexb` for a start code. Otherwise the command fails with exit code 2 asking for `-format`. MP4 is tested first because a box header can otherwise be mistaken for nothing else; the TS test needs 189 bytes of read-ahead, so the sniff buffer grew to that.
- One record per marker. An access unit with two markers produces two records, `marker_index` 0 and 1. With `-all`, access units without a marker produce one record with the marker fields absent.
- JSON Lines, one object per line, keys in this order:

  | Key | Present | Meaning |
  |---|---|---|
  | `au` | always | 0-based index of the access unit or sample |
  | `dts`, `pts`, `timescale`, `sync` | container formats | from `container.Sample`; absent for Annex B, which has no timing |
  | `time` | container formats | `pts / timescale` in seconds, float |
  | `marker_index` | when a marker is present | position among the markers of this access unit |
  | `version` | marker | 1 |
  | `time_source` | marker | `"send"` or `"capture"` |
  | `origin_time` | marker | RFC 3339 with six fractional digits, UTC |
  | `origin_us` | marker | integer microseconds since the epoch |
  | `sequence` | marker | integer |
  | `stream_id` | marker | 16 hex characters |
  | `payload` | marker, when the flag is set | base64 |

- CSV has the same columns in the same order, with a header line; absent values are empty. The column names are unchanged from phase 1, so a consumer reading MP4 output keeps working and FLV and MPEG-TS fill the same columns; only `timescale` differs, 1000 for FLV and 90000 for MPEG-TS against the MP4 track timescale.
- The three container readers go through one emit loop over `container.Sample`, with `Framing.H264()` giving the format for the marker scan. Annex B keeps its own loop, because it has an access-unit index and no timing.
- A decode error in one access unit is a warning on stderr with the index and the error, and processing continues. Exit code 0 when the file was processed to the end, 1 when it could not be opened or read, 2 for usage errors, which includes an input whose format cannot be told from its first bytes. `-h` prints the flags and exits 0. Warnings do not change the exit code in phase 1.

## Test vectors

```
vectors/
  markers/NNN-name.bin      marker body bytes
  markers/NNN-name.json     expected decode, same keys as the CLI marker fields, plus "error" for negative vectors
  nal/NNN-name.bin          a whole SEI NAL unit with emulation prevention applied
  nal/NNN-name.json         expected markers found in that NAL unit
  streams/testsrc-marked.h264      Annex B fixture with a marker before the first VCL NAL unit of every access unit
  streams/testsrc-marked.mp4       the same, remuxed with ffmpeg -c copy, progressive
  streams/testsrc-marked-frag.mp4  the same, fragmented
  streams/testsrc-marked.flv       the same, remuxed with ffmpeg -c copy
  streams/testsrc-marked.ts        the same, remuxed with ffmpeg -c copy
  streams/*.jsonl                  expected `seimark dump -out jsonl` output for each stream fixture
  gen/                             how the fixtures were produced: a shell script for ffmpeg and a small Go program for the insertion
```

- `markers/001-spec-example` and `nal/001-spec-example` are the worked example from `docs/format.md`.
- Negative vectors cover: unsupported version, truncated body, payload flag set without a length, payload length beyond the body.
- The stream fixtures are generated once and committed. The generator uses `marker.Encode` and mp4ff `avc.CreateSEINalu` and inserts the NAL unit before the first VCL NAL unit; in phase 2 the writer replaced that insertion code and the generator calls the writer instead. Fixtures stay under 100 kB each: `testsrc` at 160x120, 10 frames per second, two seconds, keyframe every ten frames.
- The FLV and MPEG-TS fixtures are the same marked Annex B stream remuxed with `ffmpeg -c copy`, so the coded bytes and therefore the markers are identical across all five stream fixtures. That is the point: the same twenty markers must come out of five containers. What differs in the golden output is the timing columns, and the MPEG-TS `au` count, because ffmpeg's TS muxer repeats the parameter sets and the reader syncs from the first IDR.
- Vectors are never edited to make a test pass.

## Tests

- `marker`: table-driven codec tests including every negative vector; round trip after microsecond truncation.
- `h264`: format detection; NAL splitting in both formats including an overrunning length; `Markers` with foreign SEI before and after a marker, with two markers, with a malformed marker; `AccessUnits` on streams with and without delimiters, with SPS and PPS before an IDR, with a marker before the first VCL NAL unit, and on a stream ending without a trailing start code.
- `mp4`: sample count, DTS, PTS and sync on the progressive and fragmented fixtures; `ErrNoVideoTrack` on an audio-only file built in the test.
- `flv`: sample count, DTS, PTS, composition offset and sync on the fixture; SPS and PPS from the sequence header; a non-FLV header is `ErrNotFLV`; an enhanced-RTMP FourCC tag is `ErrNoVideoTrack` naming the FourCC; a tag whose size runs past the end is `ErrMalformedFLV`; a configuration record with a length size of 2 is `ErrMalformedFLV`; audio and script tags are skipped.
- `ts`: sample count, PTS and DTS on the fixture, with the timestamps of the PES in which each access unit starts; access units before the first IDR are dropped and `Index` starts at 0; a PES without a DTS takes DTS from PTS; a PMT without an H.264 stream is `ErrNoVideoTrack`; truncated packets are `ErrMalformedTS`.
- `container`: `Framing.H264` maps both values.
- Golden: every vector file decoded and compared; `seimark dump` output on every stream fixture compared with its `.jsonl`.
- Cross-container: the markers `dump` finds in the five stream fixtures are the same twenty, with the same sequences, origin times and stream id. The test compares the marker columns only, since the timing columns are the containers' own.
- `Makefile`: `test`, `vet`, `lint`, `fmt-check`, `build`, all under `CGO_ENABLED=0`; `vectors` regenerates the stream fixtures and needs ffmpeg.

## The writer, in `h264` (phase 2)

The writer is a library for live use first: a Go publisher or capture pipeline calls it once per frame with the clock reading of that frame. Offline stamping of files is the same call driven by `inject`.

```go
type WriterOptions struct {
    StreamID      [marker.StreamIDSize]byte // zero value: eight random bytes
    KeyframesOnly bool
    TimeSource    marker.TimeSource
}

type Writer struct{ /* stream id, next sequence, options */ }

func NewWriter(opts WriterOptions) (*Writer, error)
func (w *Writer) StreamID() [marker.StreamIDSize]byte
func (w *Writer) Sequence() uint32 // the sequence the next marked unit receives
func (w *Writer) Mark(au []byte, f Format, at time.Time, payload []byte) (out []byte, marked bool, err error)

var ErrAlreadyMarked error
var ErrPayloadAboveSoftLimit error // advisory: the unit was marked

func StripMarkers(au []byte, f Format) ([]byte, error)
```

- **`Mark`** returns a new access unit in the same format as its input: Annex B with four-byte start codes, or four-byte length prefixes. It never aliases the input. The marker body carries `at` truncated to microseconds, the writer's stream id, the next sequence number and the payload; `TimeSource` from the options goes into the flags.
- **Sequence** starts at 0 and increments once per marked unit, wrapping modulo 2^32, as the format says. Units left unmarked by `KeyframesOnly` do not consume a number.
- **Keyframes** are access units that contain an IDR NAL unit (type 5). With `KeyframesOnly`, other units are returned unchanged with `marked == false`.
- **Already marked** units make `Mark` return `ErrAlreadyMarked` and leave the sequence untouched; the caller decides. `StripMarkers` removes every SEI NAL unit that carries a seimark marker and returns the unit rebuilt in its own format, which is how `inject` and the fixture generator get a clean starting point.
- **Payload** above `marker.PayloadHardLimit` is an error and nothing is marked; above `marker.PayloadSoftLimit` the unit is marked and `ErrPayloadAboveSoftLimit` is returned alongside it, so a caller can log without losing the frame.
- **Placement** follows `docs/format.md` and has no options: the marker goes after any AUD, SPS, PPS and existing SEI and before the first VCL NAL unit; a unit without a VCL NAL unit is `ErrNoVCL`. ADR 0005.

## `seimark inject`

```
seimark inject [-start RFC3339|now] [-fps N] [-stream-id HEX16] [-keyframes-only] IN OUT
```

- Annex B input only in this phase; an MP4 input is refused with exit code 2 and a message that MP4 comes later.
- Every access unit is marked, or only IDR units with `-keyframes-only`.
- **Time of unit i** is `start + i / rate`, computed in integer microseconds. `-start` defaults to the current time. The rate comes from `-fps` when given, otherwise from the SPS VUI timing when `timing_info_present_flag` is set: `rate = time_scale / (2 * num_units_in_tick)`, which mp4ff exposes on `avc.SPS.VUI`; the fixture encodes 10 fps as `num_units_in_tick = 1`, `time_scale = 20`. With neither, exit code 2 and a message asking for `-fps`. A raw stream has no other timing; variable frame rate is the MP4 path's job in a later phase.
- An access unit without a picture, such as a trailing SPS and PPS after a cut, is written unchanged and reported; the final line on stderr says how many units were marked out of how many were read.
- Input that already carries markers is refused with exit code 1; strip first with a later `-replace` if it is ever wanted.
- `OUT` is created or truncated. Exit codes as for `dump`.

## `seimark nals`

```
seimark nals [-format auto|annexb|mp4|flv|ts] FILE
```

Text output for debugging, one block per access unit: the unit index and, when the input is a container format, its DTS and PTS; then one line per NAL unit with its type name, size in bytes, and for SEI NAL units the messages found: seimark markers decoded (sequence, origin time, stream id, payload size), other unregistered user data with their UUID in hex, other message types by number. Same format detection and exit codes as `dump`, and the same one loop over `container.Sample`.

## Fixture generator

`vectors/gen` calls the writer instead of inserting NAL units itself: fixed stream id, start `2026-09-12T21:00:00Z`, 10 fps, payload `testsrc` on the first unit. Regenerating must reproduce the committed fixtures byte for byte, and a test proves the equivalent without ffmpeg: strip the markers from the committed Annex B fixture, mark it again with the same parameters, compare bytes.

## Tests, phase 2

- Writer: placement before VCL on a unit with parameter sets, and after an existing foreign SEI; a unit with no VCL NAL unit is `ErrNoVCL`; keyframes-only marks the IDR unit and leaves the other unchanged; `ErrAlreadyMarked` leaves the sequence untouched; soft and hard payload limits; length-prefixed input produces length-prefixed output; the output never aliases the input; empty NAL units are skipped without a panic; the marker NAL unit reproduces the worked example; capture time source at sequence 7 reproduces marker vector 006; the time is truncated to microseconds; `NewWriter` draws a different random stream id each time.
- `StripMarkers`: only marker SEI NAL units are removed; stripping an unmarked unit is a no-op; length-prefixed input; strip then mark again returns the same bytes; empty NAL units are skipped.
- Fixture: the committed Annex B fixture is exactly what the writer produces from its own stripped NAL units with the generator's parameters.
- `AccessUnits`: the standard rule with no marker exception — an SEI NAL unit after the last VCL NAL unit starts the next access unit; the `[SPS PPS IDR][nonIDR][IDR]` shape with only the IDR units marked reads back with the markers on units 0 and 2.
- `inject`: the stripped fixture injected with the fixture's parameters gives 20 records with the right times and sequences; `-fps` overrides the SPS; `-keyframes-only` gives two markers and reads back on the standard rule; a unit without a picture is written unchanged and reported, with `marked 0 of 1 access units`; the fixture reports `marked 20 of 20`, keyframes-only `marked 2 of 20`; `OUT` equal to `IN` exits 2 and leaves the input untouched; a stream without VUI timing and without `-fps` exits 2; `-fps NaN`, `Inf`, `0` and `-1` exit 2; `rateFromSPSValue` on a constructed SPS without usable timing is `errNoRate`; a read error during the rate probe exits 1; a marked input exits 1; MP4 input exits 2; the zero stream id exits 2.
- `h264.SEIMessages`: the spec example NAL unit gives one message with a decoded marker; a foreign user-data NAL unit gives one message with a UUID and no marker; a one-byte NAL unit is `ErrUnparsableSEI`.
- `nals`: the fixture lists 20 units, each with one seimark SEI line, and shows the x264 user data as foreign; the MP4 fixture the same with DTS and PTS; a one-byte SEI NAL unit prints `unparsable` and `dump` agrees that there is no marker.

## Tests, phase 4

- Format detection: each of the five stream fixtures sniffs to its own format; a file of `0x47` bytes with no sync byte at 188 does not sniff as `ts`; a file shorter than the read-ahead does not crash the sniff; an unrecognisable file is a usage error.
- `dump` and `nals` on the FLV and MPEG-TS fixtures against their golden output, with `-format` given and with `auto`.
- The mp4 migration: `mp4.VideoSamples` yields `container.Sample` with `FramingLengthPrefixed`, and the existing MP4 golden output is unchanged, which is the proof that the shared type did not alter the emit path.

## Later phases

MISB ST 0604 compatibility, MP4 input and output for `inject`, lazy MP4 reading, enhanced-RTMP FLV tags, HEVC.
