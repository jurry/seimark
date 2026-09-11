// Package mp4 iterates the H.264 video samples of an MP4 file.
package mp4

import (
	"errors"
	"fmt"
	"io"
	"iter"

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

var ErrNoVideoTrack = errors.New("seimark: no H.264 video track")

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

// editListOffset returns what to add to a sample's composition time to get its
// presentation time. A positive media time skips the start; a media time of -1
// is an empty edit that delays it.
func editListOffset(moov *mp4.MoovBox, trak *mp4.TrakBox) int64 {
	if trak.Edts == nil || len(trak.Edts.Elst) == 0 || len(trak.Edts.Elst[0].Entries) == 0 {
		return 0
	}
	e := trak.Edts.Elst[0].Entries[0]
	switch {
	case e.MediaTime > 0:
		return -e.MediaTime
	case e.MediaTime < 0 && moov.Mvhd != nil && moov.Mvhd.Timescale != 0:
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
	n := int(stbl.Stsz.SampleNumber)
	for nr := 1; nr <= n; nr++ {
		chunkNr, firstInChunk, err := stbl.Stsc.ChunkNrFromSampleNr(nr)
		if err != nil {
			yield(Sample{}, fmt.Errorf("seimark: sample %d: %w", nr, err))
			return
		}
		offset, err := chunkOffset(stbl, chunkNr)
		if err != nil {
			yield(Sample{}, err)
			return
		}
		for s := firstInChunk; s < nr; s++ {
			offset += int64(stbl.Stsz.GetSampleSize(s))
		}
		size := stbl.Stsz.GetSampleSize(nr)
		data, err := sampleData(f.Mdat, offset, int64(size))
		if err != nil {
			yield(Sample{}, fmt.Errorf("seimark: sample %d data: %w", nr, err))
			return
		}
		dts, _ := stbl.Stts.GetDecodeTime(uint32(nr))
		var cto int32
		if stbl.Ctts != nil {
			cto = stbl.Ctts.GetCompositionTimeOffset(uint32(nr))
		}
		sync := stbl.Stss == nil || stbl.Stss.IsSyncSample(uint32(nr))
		s := Sample{Index: nr - 1, DTS: dts, PTS: int64(dts) + int64(cto) + editOffset, Timescale: timescale, Sync: sync, Data: data}
		if !yield(s, nil) {
			return
		}
	}
}

// sampleData slices the in-memory mdat payload. mp4ff v0.56.0 ReadData rejects
// a range ending exactly at the end of mdat, so the last sample of a file whose
// samples fill the box would be unreadable through it.
func sampleData(mdat *mp4.MdatBox, start, size int64) ([]byte, error) {
	begin := start - int64(mdat.PayloadAbsoluteOffset())
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
		return int64(stbl.Co64.ChunkOffset[chunkNr-1]), nil
	}
	return 0, fmt.Errorf("%w: neither stco nor co64", ErrNoVideoTrack)
}

func fragmentedSamples(f *mp4.File, moov *mp4.MoovBox, trak *mp4.TrakBox, timescale uint32, editOffset int64, yield func(Sample, error) bool) {
	var trex *mp4.TrexBox
	if moov.Mvex != nil {
		trex, _ = moov.Mvex.GetTrex(trak.Tkhd.TrackID)
	}
	index := 0
	for _, seg := range f.Segments {
		for _, frag := range seg.Fragments {
			samples, err := frag.GetFullSamples(trex)
			if err != nil {
				yield(Sample{}, fmt.Errorf("seimark: fragment samples: %w", err))
				return
			}
			for i := range samples {
				fs := &samples[i]
				s := Sample{Index: index, DTS: fs.DecodeTime, PTS: fs.PresentationTime() + editOffset, Timescale: timescale, Sync: fs.IsSync(), Data: fs.Data}
				if !yield(s, nil) {
					return
				}
				index++
			}
		}
	}
}
