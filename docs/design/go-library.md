# Design: Go library and CLI

**Type:** LIVING. Describes the Go side as designed for phase 1 (reader and `dump`); rewritten as later phases change it. Approved 2026-09-12.

## Shape

Module `github.com/jurry/seimark`, Go 1.26, `CGO_ENABLED=0`, one dependency: `github.com/Eyevinn/mp4ff` v0.56.0.

| Package | Owns | Depends on |
|---|---|---|
| `marker` | The marker body codec from `docs/format.md`: types, constants, `Decode`, `Encode`, errors. | standard library |
| `h264` | Access-unit level work: format detection, NAL unit splitting, finding markers in an access unit, splitting an Annex B byte stream into access units. | `marker`, mp4ff `avc` and `sei` |
| `mp4` | Iterating the video samples of an MP4 file, progressive or fragmented, with timing and sync flags. | mp4ff `mp4` |
| `cmd/seimark` | The CLI. Phase 1: `dump`. | all of the above |

Principles: bytes in, bytes out, no ownership tricks beyond documented subslicing; no state except in the phase 2 writer; iterators (`iter.Seq2`) for anything that can be large; errors returned with context, never logged; nothing in the library prints.

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
- `OriginTime` is stored as microseconds since the Unix epoch; `Encode` rounds to microseconds, so `Decode(Encode(m))` equals `m` only after that rounding, which the tests state explicitly.

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

func DetectFormat(au []byte) Format
func NALUnits(au []byte, f Format) ([][]byte, error)
func Markers(au []byte, f Format) ([]marker.Marker, error)  // error may wrap ErrUnparsableSEI
func AccessUnits(r io.Reader) iter.Seq2[[]byte, error]
```

- **Format detection.** A leading `00 00 01` or `00 00 00 01` is Annex B. Otherwise, if the first four bytes read as a big-endian length between 1 and `len(au) - 4`, the unit is length-prefixed. Anything else is unknown, and `Markers` returns an error for it. Callers that know the format pass it and skip detection.
- **NAL units.** Annex B splitting uses mp4ff `avc.ExtractNalusFromByteStream`. Length-prefixed splitting is our own walker: a four-byte big-endian length, required to be at least 1 and no larger than the bytes left, otherwise an error naming the byte offset. mp4ff `avc.GetNalusFromSample` is not used because it panics on a corrupt length. Returned slices are subslices of `au`.
- **Building a marker NAL unit.** `UserDataSEINAL(uuid [16]byte, body []byte) ([]byte, error)` wraps mp4ff `avc.CreateSEINalu` with one `user_data_unregistered` message. It exists in phase 1 for the fixture generator and the tests; the phase 2 writer builds on it. Verified against the worked example in `docs/format.md`: mp4ff produces the same 44 bytes.
- **Markers.** For every NAL unit of type 6: drop the header byte, run mp4ff `sei.ExtractSEIData`, take messages of type 5, compare the first 16 payload bytes with `marker.FormatUUID()`, `marker.Decode` the rest. Foreign SEI and foreign unregistered UUIDs are skipped without error. An SEI NAL unit mp4ff cannot parse is skipped but not hidden: the scan finishes and returns the markers found together with an error wrapping the exported `ErrUnparsableSEI`, which callers can test with `errors.Is` and treat as advisory — the CLI warns on it and keeps going. A message with the seimark UUID that fails to decode ends the scan at once and returns the markers found so far with that error. The order of the result is the order in the access unit.
- **Access units from a byte stream.** `AccessUnits` reads an Annex B stream incrementally through a `bufio.Reader`, finds start codes, and groups NAL units into access units with this rule: once the current access unit contains a VCL NAL unit (types 1 to 5), the next NAL unit starts a new access unit if it is an access-unit delimiter, SPS, PPS or SEI, or if it is a VCL NAL unit whose `first_mb_in_slice` is zero. One exception carries append placement: an SEI NAL unit that `Markers` finds a seimark marker in joins the current access unit instead of starting a new one, as long as that unit has no marker yet. The test on the single NAL unit is cheap because the unit is small. The limit is one append-placed marker per access unit; a second one starts the next unit. `first_mb_in_slice` is the first Exp-Golomb value after the header; it is zero exactly when the first bit of the byte after the header is 1, so the check is `nal[1] & 0x80 != 0`. Each yielded access unit is Annex B bytes with four-byte start codes, valid input for `Markers` with `FormatAnnexB`. A read error ends the sequence with that error; a final access unit without a trailing start code is yielded before the sequence ends. Leading bytes before the first start code are ignored.

## `mp4`

```go
type Sample struct {
    Index     int    // 0-based position in the track
    DTS       uint64 // decode time in Timescale units
    PTS       int64  // DTS plus composition offset minus the first edit-list media time, if any
    Timescale uint32
    Sync      bool
    Data      []byte // length-prefixed NAL units as stored in the file
}

var ErrNoVideoTrack error
var ErrMalformedFile error

func VideoSamples(r io.ReadSeeker) iter.Seq2[Sample, error]
```

- Decodes the file with mp4ff `mp4.DecodeFile` in normal mode, which reads the file into memory. Lazy mdat reading is a later improvement; phase 1 fixtures are small and the API does not change.
- Picks the first track whose handler is `vide` and whose sample entry is `avc1` or `avc3`. A video track in another codec is passed over, so an H.264 track behind an HEVC one is still found; when no track qualifies the error is `ErrNoVideoTrack` naming the sample entries that were seen.
- Progressive files: iterate with the stbl helpers the way mp4ff's own `mp4ff-nallister` does: `Stsz` for count and size, `Stsc.ChunkNrFromSampleNr` and `Stco` or `Co64` for the byte range, `Stts.GetDecodeTime`, `Ctts.GetCompositionTimeOffset` when present, `Stss` for sync (absent `Stss` means every sample is sync), `mdat.ReadData` for the bytes.
- The tables are validated before the walk, because the mp4ff helpers index them without bounds checks and panic on a file whose tables disagree: `Stsd`, `Mdhd` and `Tkhd` must be present, `Stsc` must have an entry, `stco` or `co64` must be there, `stts` and, when present, `ctts` must cover at least `Stsz.SampleNumber` samples, and every chunk number `Stsc` produces must be in the offset table. A failure is `ErrMalformedFile`. Each sample is then built inside a function that recovers a panic from mp4ff and returns it as `ErrMalformedFile`, so an unforeseen index error is an error and not a crash.
- Fragmented files: for every fragment, `GetFullSamples(trex)`; data, decode time and composition offset come from the full sample. Sync is `!mp4.DecodeSampleFlags(Flags).SampleIsNonSync`, the test ISO 14496-12 defines; mp4ff's `Sample.IsSync` also requires `sample_depends_on == 2`, which the standard does not, so a sync sample written with `depends_on` unknown would be missed. A fragment whose `Moof` or `Mdat` is missing ends the iteration with `ErrMalformedFile`, because mp4ff panics on it.
- Edit list: only the first entry of the first `elst` is honoured. A positive media time is subtracted from every `PTS`; a media time of -1 (an empty edit) adds the entry's segment duration converted from the movie timescale to the track timescale. Anything more elaborate is out of scope.

## `cmd/seimark dump`

```
seimark dump [-format auto|annexb|mp4] [-out jsonl|csv] [-all] FILE
```

- `-format auto` (default) picks `mp4` when the file starts with a box header whose type is `ftyp`, `moov`, `moof` or `styp`, and `annexb` when it starts with a start code; otherwise the command fails with exit code 2 asking for `-format`.
- One record per marker. An access unit with two markers produces two records, `marker_index` 0 and 1. With `-all`, access units without a marker produce one record with the marker fields absent.
- JSON Lines, one object per line, keys in this order:

  | Key | Present | Meaning |
  |---|---|---|
  | `au` | always | 0-based index of the access unit or sample |
  | `dts`, `pts`, `timescale`, `sync` | mp4 only | from `mp4.Sample` |
  | `time` | mp4 only | `pts / timescale` in seconds, float |
  | `marker_index` | when a marker is present | position among the markers of this access unit |
  | `version` | marker | 1 |
  | `time_source` | marker | `"send"` or `"capture"` |
  | `origin_time` | marker | RFC 3339 with six fractional digits, UTC |
  | `origin_us` | marker | integer microseconds since the epoch |
  | `sequence` | marker | integer |
  | `stream_id` | marker | 16 hex characters |
  | `payload` | marker, when the flag is set | base64 |

- CSV has the same columns in the same order, with a header line; absent values are empty.
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
  streams/*.jsonl                  expected `seimark dump -out jsonl` output for each stream fixture
  gen/                             how the fixtures were produced: a shell script for ffmpeg and a small Go program for the insertion
```

- `markers/001-spec-example` and `nal/001-spec-example` are the worked example from `docs/format.md`.
- Negative vectors cover: unsupported version, truncated body, payload flag set without a length, payload length beyond the body.
- The stream fixtures are generated once and committed. The generator uses `marker.Encode` and mp4ff `avc.CreateSEINalu` and inserts the NAL unit before the first VCL NAL unit; in phase 2 the writer replaces that insertion code and the generator calls the writer instead. Fixtures stay under 100 kB each: `testsrc` at 160x120, 10 frames per second, two seconds, keyframe every ten frames.
- Vectors are never edited to make a test pass.

## Tests

- `marker`: table-driven codec tests including every negative vector; round trip after microsecond rounding.
- `h264`: format detection; NAL splitting in both formats including an overrunning length; `Markers` with foreign SEI before and after a marker, with two markers, with a malformed marker; `AccessUnits` on streams with and without delimiters, with SPS and PPS before an IDR, with a marker before the first VCL NAL unit, and on a stream ending without a trailing start code.
- `mp4`: sample count, DTS, PTS and sync on the progressive and fragmented fixtures; `ErrNoVideoTrack` on an audio-only file built in the test.
- Golden: every vector file decoded and compared; `seimark dump` output on every stream fixture compared with its `.jsonl`.
- `Makefile`: `test`, `vet`, `fmt-check`, `build`, all under `CGO_ENABLED=0`; `vectors` regenerates the stream fixtures and needs ffmpeg.

## Out of scope for phase 1

Writing markers into streams, placement options, the stateful writer, `inject`, `nals`, the browser package, MISB compatibility, HEVC, writing MP4.
