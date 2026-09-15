// Package flv iterates the AVC video tags of an FLV stream.
package flv

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"iter"

	"github.com/jurry/seimark/container"
)

var (
	// ErrNotFLV marks a stream that does not begin with an FLV version 1 header.
	ErrNotFLV = errors.New("seimark: not an FLV stream")

	// ErrNoVideoTrack is returned when the video tags are not legacy AVC, which
	// is what an enhanced-RTMP FourCC tag signals.
	ErrNoVideoTrack = errors.New("seimark: no H.264 video track")

	// ErrMalformedFLV marks a tag that runs past the end of the stream or a
	// configuration record this reader cannot honour.
	ErrMalformedFLV = errors.New("seimark: malformed flv")
)

// Timescale is the unit of every FLV timestamp: milliseconds.
const Timescale = 1000

const (
	signatureSize  = 3
	headerSize     = 9
	tagHeaderSize  = 11
	prevTagSize    = 4
	avcHeaderSize  = 5
	compOffsetSize = 3
	flvVersion     = 1
)

const (
	tagTypeAudio  = 8
	tagTypeVideo  = 9
	tagTypeScript = 18
)

const (
	codecIDAVC        = 7
	frameTypeKeyframe = 1
	exHeaderFlag      = 0x80
	fourCCSize        = 4
	codecIDMask       = 0x0F
	bitsPerByte       = 8
)

const (
	packetTypeSequenceHeader = 0
	packetTypeNALUnits       = 1
	packetTypeEndOfSequence  = 2
)

const (
	configLengthSizeOffset = 4
	configSPSCountOffset   = 5
	lengthSizeMask         = 0x03
	spsCountMask           = 0x1F
	requiredLengthSize     = 4
	parameterSetSizeBytes  = 2
)

// Stream is an FLV stream being read. It exposes the parameter sets of the
// sequence header, which the samples themselves do not carry.
type Stream struct {
	r      io.Reader
	offset int64

	sps []byte
	pps []byte

	// lengthSize is 0 until a sequence header has been read; a NAL tag before
	// that cannot be trusted, because its prefix size would be a guess.
	lengthSize byte
}

// VideoSamples yields the AVC samples of an FLV stream. Framing is always
// FramingLengthPrefixed and Timescale is always 1000. It is the one-call form
// of NewStream for callers that do not need the parameter sets.
func VideoSamples(r io.Reader) iter.Seq2[container.Sample, error] {
	return func(yield func(container.Sample, error) bool) {
		s, err := NewStream(r)
		if err != nil {
			yield(container.Sample{}, err)

			return
		}

		s.Samples()(yield)
	}
}

// NewStream reads and validates the FLV header. The parameter sets are only
// populated once Samples has walked past the sequence header.
func NewStream(r io.Reader) (*Stream, error) {
	head := make([]byte, headerSize)
	if _, err := io.ReadFull(r, head); err != nil {
		return nil, fmt.Errorf("%w: reading the header: %w", ErrNotFLV, err)
	}

	if string(head[:signatureSize]) != "FLV" || head[signatureSize] != flvVersion {
		return nil, fmt.Errorf("%w: signature is % x", ErrNotFLV, head[:signatureSize+1])
	}

	declared := binary.BigEndian.Uint32(head[signatureSize+2:])
	if declared < headerSize {
		return nil, fmt.Errorf("%w: header size %d is below %d", ErrNotFLV, declared, headerSize)
	}

	s := &Stream{r: r, offset: headerSize}
	if extra := int64(declared) - headerSize; extra > 0 {
		if err := s.discard(extra); err != nil {
			return nil, err
		}
	}

	return s, nil
}

// SPS returns the sequence parameter set of the configuration record, or nil
// when no sequence header has been read yet.
func (s *Stream) SPS() []byte { return s.sps }

// PPS returns the picture parameter set of the configuration record, or nil
// when no sequence header has been read yet.
func (s *Stream) PPS() []byte { return s.pps }

// Samples yields the AVC sample tags of the stream in file order.
func (s *Stream) Samples() iter.Seq2[container.Sample, error] {
	return func(yield func(container.Sample, error) bool) {
		index := 0

		for {
			tagType, ts, data, err := s.nextTag()
			if errors.Is(err, io.EOF) {
				return
			}

			if err != nil {
				yield(container.Sample{}, err)

				return
			}

			if tagType != tagTypeVideo {
				continue
			}

			sample, ok, err := s.videoSample(ts, data, index)
			if err != nil {
				yield(container.Sample{}, err)

				return
			}

			if !ok {
				continue
			}

			if !yield(sample, nil) {
				return
			}

			index++
		}
	}
}

// nextTag reads one tag. A clean end of stream at a tag boundary is io.EOF.
func (s *Stream) nextTag() (tagType byte, ts uint32, data []byte, err error) {
	// A well-formed stream ends with the previous-tag size of the last tag and
	// nothing after it, so io.EOF here is the clean end of the tag list.
	if _, err := io.ReadFull(s.r, make([]byte, prevTagSize)); err != nil {
		if errors.Is(err, io.EOF) {
			return 0, 0, nil, io.EOF
		}

		return 0, 0, nil, fmt.Errorf("%w: previous-tag size at offset %d: %w", ErrMalformedFLV, s.offset, err)
	}

	s.offset += prevTagSize
	tagOffset := s.offset

	h := make([]byte, tagHeaderSize)
	if _, err := io.ReadFull(s.r, h); err != nil {
		if errors.Is(err, io.EOF) {
			return 0, 0, nil, io.EOF
		}

		return 0, 0, nil, fmt.Errorf("%w: tag header at offset %d: %w", ErrMalformedFLV, tagOffset, err)
	}

	s.offset += tagHeaderSize
	tagType = h[0]
	size := uint32(h[1])<<16 | uint32(h[2])<<8 | uint32(h[3])
	ts = uint32(h[7])<<24 | uint32(h[4])<<16 | uint32(h[5])<<8 | uint32(h[6])

	data = make([]byte, size)
	if _, err := io.ReadFull(s.r, data); err != nil {
		return 0, 0, nil, fmt.Errorf("%w: tag of %d bytes at offset %d: %w", ErrMalformedFLV, size, tagOffset, err)
	}

	s.offset += int64(size)

	return tagType, ts, data, nil
}

// videoSample turns one video tag into a sample. ok is false for the tags that
// carry no picture: the sequence header and the end of sequence.
func (s *Stream) videoSample(ts uint32, data []byte, index int) (container.Sample, bool, error) {
	if len(data) < avcHeaderSize {
		return container.Sample{}, false, fmt.Errorf(
			"%w: video tag of %d bytes at offset %d", ErrMalformedFLV, len(data), s.offset,
		)
	}

	if data[0]&exHeaderFlag != 0 {
		return container.Sample{}, false, fmt.Errorf(
			"%w: enhanced RTMP codec %q", ErrNoVideoTrack, data[1:1+fourCCSize],
		)
	}

	if data[0]&codecIDMask != codecIDAVC {
		return container.Sample{}, false, fmt.Errorf("%w: codec id %d", ErrNoVideoTrack, data[0]&codecIDMask)
	}

	payload := data[avcHeaderSize:]

	switch data[1] {
	case packetTypeSequenceHeader:
		return container.Sample{}, false, s.readConfigurationRecord(payload)
	case packetTypeEndOfSequence:
		return container.Sample{}, false, nil
	case packetTypeNALUnits:
	default:
		return container.Sample{}, false, fmt.Errorf("%w: AVC packet type %d", ErrMalformedFLV, data[1])
	}

	if s.lengthSize == 0 {
		return container.Sample{}, false, fmt.Errorf(
			"%w: NAL unit tag at offset %d before any sequence header", ErrMalformedFLV, s.offset,
		)
	}

	return container.Sample{
		Index:     index,
		DTS:       uint64(ts),
		PTS:       int64(ts) + int64(compositionOffset(data[2:avcHeaderSize])),
		Timescale: Timescale,
		Sync:      data[0]>>4 == frameTypeKeyframe,
		Framing:   container.FramingLengthPrefixed,
		Data:      append([]byte(nil), payload...),
	}, true, nil
}

// compositionOffset sign-extends the 24-bit offset the tag carries.
func compositionOffset(b []byte) int32 {
	v := int32(b[0])<<16 | int32(b[1])<<8 | int32(b[2])
	if b[0]&0x80 != 0 {
		v -= 1 << (compOffsetSize * bitsPerByte)
	}

	return v
}

// readConfigurationRecord keeps the parameter sets and the NAL length size.
func (s *Stream) readConfigurationRecord(rec []byte) error {
	if len(rec) <= configSPSCountOffset {
		return fmt.Errorf("%w: configuration record of %d bytes", ErrMalformedFLV, len(rec))
	}

	lengthSize := rec[configLengthSizeOffset]&lengthSizeMask + 1
	if lengthSize != requiredLengthSize {
		return fmt.Errorf("%w: NAL length size %d, only %d is read", ErrMalformedFLV, lengthSize, requiredLengthSize)
	}

	s.lengthSize = lengthSize

	rest := rec[configSPSCountOffset+1:]

	sps, rest, err := parameterSets(rest, int(rec[configSPSCountOffset]&spsCountMask))
	if err != nil {
		return err
	}

	if len(rest) == 0 {
		return fmt.Errorf("%w: configuration record has no PPS count", ErrMalformedFLV)
	}

	pps, _, err := parameterSets(rest[1:], int(rest[0]))
	if err != nil {
		return err
	}

	if len(sps) > 0 {
		s.sps = sps[0]
	}

	if len(pps) > 0 {
		s.pps = pps[0]
	}

	return nil
}

// parameterSets reads count two-byte-length-prefixed sets and returns the rest.
func parameterSets(b []byte, count int) (sets [][]byte, rest []byte, err error) {
	sets = make([][]byte, 0, count)

	for range count {
		if len(b) < parameterSetSizeBytes {
			return nil, nil, fmt.Errorf("%w: configuration record ends inside a parameter set length", ErrMalformedFLV)
		}

		n := int(binary.BigEndian.Uint16(b))

		b = b[parameterSetSizeBytes:]
		if len(b) < n {
			return nil, nil, fmt.Errorf("%w: parameter set of %d bytes, %d left", ErrMalformedFLV, n, len(b))
		}

		sets = append(sets, b[:n])
		b = b[n:]
	}

	return sets, b, nil
}

// discard skips n bytes of a header longer than the nine this reader knows.
func (s *Stream) discard(n int64) error {
	skipped, err := io.CopyN(io.Discard, s.r, n)
	s.offset += skipped

	if err != nil {
		return fmt.Errorf("%w: skipping %d header bytes: %w", ErrNotFLV, n, err)
	}

	return nil
}
