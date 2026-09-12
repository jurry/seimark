// Package mp4 iterates the H.264 video samples of an MP4 file.
package mp4

import (
	"errors"
	"fmt"
	"io"
	"iter"
	"math"

	"github.com/Eyevinn/mp4ff/mp4"
)

// Sample is one video sample with its timing in the track timescale.
type Sample struct {
	Index     int
	DTS       uint64
	PTS       int64
	Timescale uint32
	Sync      bool
	Data      []byte
}

var (
	ErrNoVideoTrack = errors.New("seimark: no H.264 video track")

	// ErrMalformedFile marks sample tables that contradict each other or run
	// short; the file parses as boxes but cannot be walked sample by sample.
	ErrMalformedFile = errors.New("seimark: malformed mp4 sample tables")
)

// VideoSamples yields the samples of the first H.264 video track. The file is
// read into memory; PTS honours the first edit-list entry only.
func VideoSamples(r io.ReadSeeker) iter.Seq2[Sample, error] {
	return func(yield func(Sample, error) bool) {
		f, err := mp4.DecodeFile(r)
		if err != nil {
			yield(Sample{}, fmt.Errorf("seimark: decode mp4: %w", err))
			return
		}
		moov := f.Moov
		if moov == nil && f.Init != nil {
			moov = f.Init.Moov
		}
		if moov == nil {
			yield(Sample{}, fmt.Errorf("%w: no moov box", ErrNoVideoTrack))
			return
		}
		trak, err := h264Track(moov)
		if err != nil {
			yield(Sample{}, err)
			return
		}
		if err := checkTrackBoxes(trak); err != nil {
			yield(Sample{}, err)
			return
		}
		timescale := trak.Mdia.Mdhd.Timescale
		editOffset := editListOffset(moov, trak)
		if f.IsFragmented() {
			fragmentedSamples(f, moov, trak, timescale, editOffset, yield)
			return
		}
		progressiveSamples(f, trak, timescale, editOffset, yield)
	}
}

func h264Track(moov *mp4.MoovBox) (*mp4.TrakBox, error) {
	for _, trak := range moov.Traks {
		if trak.Mdia == nil || trak.Mdia.Hdlr == nil || trak.Mdia.Hdlr.HandlerType != "vide" {
			continue
		}
		if trak.Mdia.Minf == nil || trak.Mdia.Minf.Stbl == nil || trak.Mdia.Minf.Stbl.Stsd == nil {
			return nil, fmt.Errorf("%w: video track has no stsd", ErrMalformedFile)
		}
		stsd := trak.Mdia.Minf.Stbl.Stsd
		if stsd.AvcX == nil {
			name := "unknown"
			if len(stsd.Children) > 0 {
				name = stsd.Children[0].Type()
			}
			return nil, fmt.Errorf("%w: video track sample entry is %s", ErrNoVideoTrack, name)
		}
		return trak, nil
	}
	return nil, ErrNoVideoTrack
}

// checkTrackBoxes rejects a track missing a box the sample walk dereferences.
func checkTrackBoxes(trak *mp4.TrakBox) error {
	switch {
	case trak.Mdia.Mdhd == nil:
		return fmt.Errorf("%w: track has no mdhd", ErrMalformedFile)
	case trak.Tkhd == nil:
		return fmt.Errorf("%w: track has no tkhd", ErrMalformedFile)
	case trak.Mdia.Minf == nil || trak.Mdia.Minf.Stbl == nil || trak.Mdia.Minf.Stbl.Stsd == nil:
		return fmt.Errorf("%w: track has no stsd", ErrMalformedFile)
	}
	return nil
}

// editListOffset returns what to add to a sample's composition time to get its
// presentation time. A positive media time skips the start; a media time of -1
// is an empty edit that delays it.
func editListOffset(moov *mp4.MoovBox, trak *mp4.TrakBox) int64 {
	if trak.Edts == nil || len(trak.Edts.Elst) == 0 || len(trak.Edts.Elst[0].Entries) == 0 {
		return 0
	}
	e := trak.Edts.Elst[0].Entries[0]
	if e.MediaTime > 0 {
		return -e.MediaTime
	}
	if e.MediaTime < 0 && moov.Mvhd != nil && moov.Mvhd.Timescale != 0 {
		if e.SegmentDuration > math.MaxInt64 {
			return 0
		}
		return int64(e.SegmentDuration) * int64(trak.Mdia.Mdhd.Timescale) / int64(moov.Mvhd.Timescale)
	}
	return 0
}

func progressiveSamples(f *mp4.File, trak *mp4.TrakBox, timescale uint32, editOffset int64, yield func(Sample, error) bool) {
	stbl := trak.Mdia.Minf.Stbl
	if f.Mdat == nil || stbl.Stsz == nil || stbl.Stsc == nil || stbl.Stts == nil {
		yield(Sample{}, fmt.Errorf("%w: sample tables incomplete", ErrNoVideoTrack))
		return
	}
	if err := validateTables(stbl); err != nil {
		yield(Sample{}, err)
		return
	}
	n := stbl.Stsz.SampleNumber
	for nr := uint32(1); nr <= n; nr++ {
		s, err := progressiveSample(f, stbl, nr, timescale, editOffset)
		if err != nil {
			yield(Sample{}, err)
			return
		}
		if !yield(s, nil) {
			return
		}
	}
}

// progressiveSample builds one sample. mp4ff's stbl helpers index their tables
// without bounds checks, so a table this package has not thought to validate
// panics there; recover turns that into an error rather than a crash.
func progressiveSample(
	f *mp4.File, stbl *mp4.StblBox, nr uint32, timescale uint32, editOffset int64,
) (s Sample, err error) {
	defer func() {
		if r := recover(); r != nil {
			s, err = Sample{}, fmt.Errorf("%w: sample %d: %v", ErrMalformedFile, nr, r)
		}
	}()
	chunkNr, firstInChunk, err := stbl.Stsc.ChunkNrFromSampleNr(int(nr))
	if err != nil {
		return Sample{}, fmt.Errorf("seimark: sample %d: %w", nr, err)
	}
	offset, err := chunkOffset(stbl, chunkNr)
	if err != nil {
		return Sample{}, err
	}
	for i := firstInChunk; i < int(nr); i++ {
		offset += int64(stbl.Stsz.GetSampleSize(i))
	}
	size := stbl.Stsz.GetSampleSize(int(nr))
	data, err := sampleData(f.Mdat, offset, int64(size))
	if err != nil {
		return Sample{}, fmt.Errorf("seimark: sample %d data: %w", nr, err)
	}
	dts, _ := stbl.Stts.GetDecodeTime(nr)
	if dts > math.MaxInt64 {
		return Sample{}, fmt.Errorf("seimark: sample %d: decode time %d overflows int64", nr, dts)
	}
	var cto int32
	if stbl.Ctts != nil {
		cto = stbl.Ctts.GetCompositionTimeOffset(nr)
	}
	sync := stbl.Stss == nil || stbl.Stss.IsSyncSample(nr)
	return Sample{
		Index: int(nr) - 1, DTS: dts, PTS: int64(dts) + int64(cto) + editOffset,
		Timescale: timescale, Sync: sync, Data: data,
	}, nil
}

// validateTables checks the cross-table invariants progressiveSample relies on:
// every sample must have a time, a chunk and a chunk offset.
func validateTables(stbl *mp4.StblBox) error {
	if len(stbl.Stsc.Entries) == 0 {
		return fmt.Errorf("%w: stsc has no entries", ErrMalformedFile)
	}
	var chunks int
	switch {
	case stbl.Stco != nil:
		chunks = len(stbl.Stco.ChunkOffset)
	case stbl.Co64 != nil:
		chunks = len(stbl.Co64.ChunkOffset)
	default:
		return fmt.Errorf("%w: neither stco nor co64", ErrMalformedFile)
	}
	n := int(stbl.Stsz.SampleNumber)
	if covered := sttsSamples(stbl.Stts); covered < n {
		return fmt.Errorf("%w: stts covers %d samples, stsz has %d", ErrMalformedFile, covered, n)
	}
	if stbl.Ctts != nil {
		if covered := cttsSamples(stbl.Ctts); covered < n {
			return fmt.Errorf("%w: ctts covers %d samples, stsz has %d", ErrMalformedFile, covered, n)
		}
	}
	for nr := 1; nr <= n; nr++ {
		chunkNr, _, err := stbl.Stsc.ChunkNrFromSampleNr(nr)
		if err != nil {
			return fmt.Errorf("%w: sample %d has no chunk: %w", ErrMalformedFile, nr, err)
		}
		if chunkNr < 1 || chunkNr > chunks {
			return fmt.Errorf("%w: sample %d is in chunk %d of %d", ErrMalformedFile, nr, chunkNr, chunks)
		}
	}
	return nil
}

func sttsSamples(stts *mp4.SttsBox) int {
	total := 0
	for _, c := range stts.SampleCount {
		total += int(c)
	}
	return total
}

func cttsSamples(ctts *mp4.CttsBox) int {
	total := 0
	for i := range ctts.NrSampleCount() {
		total += int(ctts.SampleCount(i))
	}
	return total
}

// sampleData slices the in-memory mdat payload. mp4ff v0.56.0 ReadData rejects
// a range ending exactly at the end of mdat, so the last sample of a file whose
// samples fill the box would be unreadable through it.
func sampleData(mdat *mp4.MdatBox, start, size int64) ([]byte, error) {
	payloadOffset := mdat.PayloadAbsoluteOffset()
	if payloadOffset > math.MaxInt64 {
		return nil, fmt.Errorf("mdat payload offset %d overflows int64", payloadOffset)
	}
	begin := start - int64(payloadOffset)
	end := begin + size
	if begin < 0 || size < 0 || end > int64(len(mdat.Data)) {
		return nil, fmt.Errorf("range %d+%d outside mdat payload of %d bytes", begin, size, len(mdat.Data))
	}
	return mdat.Data[begin:end], nil
}

func chunkOffset(stbl *mp4.StblBox, chunkNr int) (int64, error) {
	switch {
	case stbl.Stco != nil:
		return int64(stbl.Stco.ChunkOffset[chunkNr-1]), nil
	case stbl.Co64 != nil:
		offset := stbl.Co64.ChunkOffset[chunkNr-1]
		if offset > math.MaxInt64 {
			return 0, fmt.Errorf("chunk offset %d overflows int64", offset)
		}
		return int64(offset), nil
	}
	return 0, fmt.Errorf("%w: neither stco nor co64", ErrNoVideoTrack)
}

func fragmentedSamples(
	f *mp4.File, moov *mp4.MoovBox, trak *mp4.TrakBox, timescale uint32, editOffset int64, yield func(Sample, error) bool,
) {
	var trex *mp4.TrexBox
	if moov.Mvex != nil {
		trex, _ = moov.Mvex.GetTrex(trak.Tkhd.TrackID)
	}
	index := 0
	for _, seg := range f.Segments {
		for _, frag := range seg.Fragments {
			if frag.Moof == nil || frag.Mdat == nil {
				yield(Sample{}, fmt.Errorf("%w: fragment without mdat", ErrMalformedFile))
				return
			}
			samples, err := frag.GetFullSamples(trex)
			if err != nil {
				yield(Sample{}, fmt.Errorf("seimark: fragment samples: %w", err))
				return
			}
			for i := range samples {
				fs := &samples[i]
				s := Sample{
					Index: index, DTS: fs.DecodeTime, PTS: fs.PresentationTime() + editOffset,
					Timescale: timescale, Sync: !mp4.DecodeSampleFlags(fs.Flags).SampleIsNonSync, Data: fs.Data,
				}
				if !yield(s, nil) {
					return
				}
				index++
			}
		}
	}
}
