// Package ts iterates the H.264 access units of an MPEG-TS stream. The 33-bit
// PTS and DTS clock is reported as it appears in the stream and is never
// unwrapped, so a recording crossing the roughly 26.5-hour wrap shows a jump.
package ts

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"sort"
	"strconv"
	"strings"

	"github.com/asticode/go-astits"

	"github.com/jurry/seimark/container"
	"github.com/jurry/seimark/h264"
)

var (
	// ErrNoVideoTrack is returned when the first PMT carries no H.264 elementary stream.
	ErrNoVideoTrack = errors.New("seimark: no H.264 video track")

	// ErrMalformedTS marks a stream that does not demultiplex: not MPEG-TS,
	// misaligned packets, or a PES header that does not parse.
	ErrMalformedTS = errors.New("seimark: malformed mpeg-ts")
)

// Timescale is the MPEG-TS 90 kHz clock, the unit of every sample's DTS and PTS.
const Timescale = 90000

// naluTypeMask isolates the NAL unit type from the NAL unit header byte.
const naluTypeMask = 0x1f

// naluTypeIDR is the coded slice of an IDR picture; the first access unit
// containing one is where yielding starts.
const naluTypeIDR = 5

// pes records where one PES packet's payload begins in the concatenated
// elementary stream, and the timestamps that apply to the access unit starting in it.
type pes struct {
	offset int
	dts    uint64
	pts    int64
}

// VideoSamples yields the access units of the first H.264 elementary stream,
// each with the timestamps of the PES packet its first byte arrived in. The
// stream is demultiplexed into memory. Nothing is yielded before the first
// access unit containing an IDR NAL unit, and Framing is always FramingAnnexB.
func VideoSamples(r io.Reader) iter.Seq2[container.Sample, error] {
	return func(yield func(container.Sample, error) bool) {
		payload, packets, err := demux(r)
		if err != nil {
			yield(container.Sample{}, err)

			return
		}

		index := 0
		synced := false
		cursor := 0

		for unit, err := range h264.AccessUnits(bytes.NewReader(payload)) {
			if err != nil {
				yield(container.Sample{}, fmt.Errorf("%w: cut access unit at byte %d: %w", ErrMalformedTS, cursor, err))

				return
			}

			start := nextStartCode(payload, cursor)
			cursor = advance(payload, start, unit)

			sync := containsIDR(unit)
			if !synced && !sync {
				continue
			}

			synced = true

			p := timestampsAt(packets, start)

			s := container.Sample{
				Index: index, DTS: p.dts, PTS: p.pts,
				Timescale: Timescale, Sync: sync,
				Framing: container.FramingAnnexB, Data: unit,
			}
			if !yield(s, nil) {
				return
			}

			index++
		}
	}
}

// demux runs the demultiplexer to completion, returning the concatenated
// payloads of the first H.264 elementary stream and where each PES began in them.
func demux(r io.Reader) ([]byte, []pes, error) {
	d := astits.NewDemuxer(context.Background(), r)

	var (
		payload []byte
		packets []pes
		pid     uint16
		found   bool
	)

	for {
		data, err := d.NextData()
		if errors.Is(err, astits.ErrNoMorePackets) {
			break
		}

		if err != nil {
			return nil, nil, fmt.Errorf("%w: read packet: %w", ErrMalformedTS, err)
		}

		if data.PMT != nil && !found {
			if pid, err = h264PID(data.PMT); err != nil {
				return nil, nil, err
			}

			found = true
		}

		if data.PES == nil || !found || data.PID != pid {
			continue
		}

		packets = append(packets, pes{offset: len(payload), dts: dts(data.PES), pts: pts(data.PES)})
		payload = append(payload, data.PES.Data...)
	}

	if !found {
		return nil, nil, fmt.Errorf("%w: no PMT in the stream", ErrNoVideoTrack)
	}

	return payload, packets, nil
}

// h264PID returns the PID of the first H.264 elementary stream in the PMT.
func h264PID(pmt *astits.PMTData) (uint16, error) {
	seen := make([]string, 0, len(pmt.ElementaryStreams))

	for _, es := range pmt.ElementaryStreams {
		if es.StreamType == astits.StreamTypeH264Video {
			return es.ElementaryPID, nil
		}

		seen = append(seen, strconv.Itoa(int(es.StreamType)))
	}

	return 0, fmt.Errorf("%w: stream types are %s", ErrNoVideoTrack, strings.Join(seen, ", "))
}

func dts(p *astits.PESData) uint64 {
	h := p.Header.OptionalHeader
	if h == nil {
		return 0
	}

	if h.DTS != nil {
		return clock(h.DTS.Base)
	}

	// PTS_DTS_flags == 2 means the DTS equals the PTS.
	if h.PTS != nil {
		return clock(h.PTS.Base)
	}

	return 0
}

// clock narrows a 33-bit clock reference, which go-astits parses into an int64
// and which is never negative, to the unsigned time container.Sample carries.
func clock(base int64) uint64 {
	if base < 0 {
		return 0
	}

	return uint64(base)
}

func pts(p *astits.PESData) int64 {
	h := p.Header.OptionalHeader
	if h == nil || h.PTS == nil {
		return 0
	}

	return h.PTS.Base
}

// startCode is the three-byte Annex B prefix; a four-byte start code ends with it.
func startCode() []byte {
	return []byte{0, 0, 1}
}

// nextStartCode returns the offset in payload of the first start code at or
// after cursor, which is where the next access unit begins.
func nextStartCode(payload []byte, cursor int) int {
	if cursor >= len(payload) {
		return len(payload)
	}

	at := bytes.Index(payload[cursor:], startCode())
	if at < 0 {
		return len(payload)
	}

	return cursor + at
}

// advance steps past the NAL units that unit was cut from, leaving the cursor
// inside the last one. h264.AccessUnits normalises three-byte start codes to
// four and drops trailing zeros, so the unit's own length is not what it
// consumed in payload; counting its NAL units off payload's start codes is.
func advance(payload []byte, start int, unit []byte) int {
	units, err := h264.NALUnits(unit, h264.FormatAnnexB)
	if err != nil {
		return len(payload)
	}

	cursor := start

	for range units {
		cursor = nextStartCode(payload, cursor) + len(startCode())
	}

	return cursor
}

// timestampsAt returns the PES packet in which the byte at start arrived.
func timestampsAt(packets []pes, start int) pes {
	if len(packets) == 0 {
		return pes{}
	}

	i := sort.Search(len(packets), func(i int) bool { return packets[i].offset > start })
	if i == 0 {
		return packets[0]
	}

	return packets[i-1]
}

func containsIDR(unit []byte) bool {
	units, err := h264.NALUnits(unit, h264.FormatAnnexB)
	if err != nil {
		return false
	}

	for _, n := range units {
		if len(n) > 0 && n[0]&naluTypeMask == naluTypeIDR {
			return true
		}
	}

	return false
}
