package mp4

import (
	"bytes"
	"errors"
	"testing"

	"github.com/Eyevinn/mp4ff/mp4"

	"github.com/jurry/seimark/container"
)

// fileSpec describes a small progressive MP4 to build in memory. Only the
// fields a test needs are set; buildFile fills in the rest.
type fileSpec struct {
	sampleSizes []uint32
	stts        [][2]uint32 // sample count, delta
	ctts        [][2]int32  // sample count, composition offset
	stsc        [][2]uint32 // first chunk, samples per chunk
	chunks      int
	useCo64     bool
	stss        []uint32
	elst        *mp4.ElstEntry
	mvhdScale   uint32
	stszNumber  uint32 // overrides the sample count in stsz when non-zero
}

// buildFile encodes a one-track H.264 MP4 from spec. Chunk offsets are
// computed after the moov size is known, so they point at real mdat bytes.
func buildFile(t *testing.T, sp *fileSpec) []byte {
	t.Helper()

	stbl := sampleTables(t, sp)
	moov := movie(t, sp, stbl)
	ftyp := mp4.NewFtyp("isom", 0x200, []string{"isom", "avc1"})

	mdat := &mp4.MdatBox{}
	for i, size := range sp.sampleSizes {
		mdat.AddSampleData(bytes.Repeat([]byte{byte(i + 1)}, int(size)))
	}

	setChunkOffsets(stbl, sp, ftyp.Size()+moov.Size()+mdat.HeaderSize())

	f := mp4.NewFile()
	f.AddChild(ftyp, 0)
	f.AddChild(moov, ftyp.Size())
	f.AddChild(mdat, ftyp.Size()+moov.Size())

	var buf bytes.Buffer
	if err := f.Encode(&buf); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

func sampleTables(t *testing.T, sp *fileSpec) *mp4.StblBox {
	t.Helper()

	stbl := mp4.NewStblBox()

	stsd := mp4.NewStsdBox()
	stsd.AddChild(mp4.NewVisualSampleEntryBox("avc1"))
	stbl.AddChild(stsd)

	stts := &mp4.SttsBox{}
	for _, e := range sp.stts {
		stts.SampleCount = append(stts.SampleCount, e[0])
		stts.SampleTimeDelta = append(stts.SampleTimeDelta, e[1])
	}

	stbl.AddChild(stts)

	if len(sp.ctts) > 0 {
		counts := make([]uint32, 0, len(sp.ctts))
		offsets := make([]int32, 0, len(sp.ctts))

		for _, e := range sp.ctts {
			counts = append(counts, uint32(e[0]))
			offsets = append(offsets, e[1])
		}

		ctts := &mp4.CttsBox{}
		if err := ctts.AddSampleCountsAndOffset(counts, offsets); err != nil {
			t.Fatal(err)
		}

		stbl.AddChild(ctts)
	}

	stsc := &mp4.StscBox{}
	for _, e := range sp.stsc {
		if err := stsc.AddEntry(e[0], e[1], 1); err != nil {
			t.Fatal(err)
		}
	}

	stbl.AddChild(stsc)

	number := uint32(len(sp.sampleSizes))
	if sp.stszNumber != 0 {
		number = sp.stszNumber
	}

	stbl.AddChild(&mp4.StszBox{SampleNumber: number, SampleSize: sp.sampleSizes})

	if sp.stss != nil {
		stbl.AddChild(&mp4.StssBox{SampleNumber: sp.stss})
	}

	// Offsets are patched once the moov size is known.
	if sp.useCo64 {
		stbl.AddChild(&mp4.Co64Box{ChunkOffset: make([]uint64, sp.chunks)})
	} else {
		stbl.AddChild(&mp4.StcoBox{ChunkOffset: make([]uint32, sp.chunks)})
	}

	return stbl
}

func movie(t *testing.T, sp *fileSpec, stbl *mp4.StblBox) *mp4.MoovBox {
	t.Helper()

	minf := mp4.NewMinfBox()
	minf.AddChild(stbl)

	hdlr, err := mp4.CreateHdlr("vide")
	if err != nil {
		t.Fatal(err)
	}

	mdia := mp4.NewMdiaBox()
	mdia.AddChild(&mp4.MdhdBox{Timescale: 1000})
	mdia.AddChild(hdlr)
	mdia.AddChild(minf)

	tkhd := mp4.CreateTkhd()
	tkhd.TrackID = 1

	trak := mp4.NewTrakBox()
	trak.AddChild(tkhd)

	if sp.elst != nil {
		edts := &mp4.EdtsBox{}
		edts.AddChild(&mp4.ElstBox{Entries: []mp4.ElstEntry{*sp.elst}})
		trak.AddChild(edts)
	}

	trak.AddChild(mdia)

	mvhd := mp4.CreateMvhd()
	if sp.mvhdScale != 0 {
		mvhd.Timescale = sp.mvhdScale
	}

	moov := mp4.NewMoovBox()
	moov.AddChild(mvhd)
	moov.AddChild(trak)

	return moov
}

// setChunkOffsets lays the samples out chunk by chunk from the mdat payload
// start, following the stsc the spec asked for.
func setChunkOffsets(stbl *mp4.StblBox, sp *fileSpec, payloadStart uint64) {
	offsets := make([]uint64, sp.chunks)
	pos := payloadStart
	sampleNr := 0

	for c := 1; c <= sp.chunks; c++ {
		offsets[c-1] = pos

		for i := 0; i < samplesInChunk(stbl.Stsc, c) && sampleNr < len(sp.sampleSizes); i++ {
			pos += uint64(sp.sampleSizes[sampleNr])
			sampleNr++
		}
	}

	if sp.useCo64 {
		copy(stbl.Co64.ChunkOffset, offsets)

		return
	}

	for i, v := range offsets {
		stbl.Stco.ChunkOffset[i] = uint32(v)
	}
}

func samplesInChunk(stsc *mp4.StscBox, chunkNr int) int {
	n := 0

	for _, e := range stsc.Entries {
		if int(e.FirstChunk) <= chunkNr {
			n = int(e.SamplesPerChunk)
		}
	}

	return n
}

func buildSamples(t *testing.T, sp *fileSpec) []container.Sample {
	t.Helper()

	var out []container.Sample

	for s, err := range VideoSamples(bytes.NewReader(buildFile(t, sp))) {
		if err != nil {
			t.Fatalf("VideoSamples: %v", err)
		}

		out = append(out, s)
	}

	return out
}

func TestVideoSamplesCompositionOffsets(t *testing.T) {
	t.Parallel()

	// Four samples 100 ticks apart, the middle two displayed out of decode
	// order: ctts shifts sample 2 by 200 and samples 3 and 4 by 100.
	samples := buildSamples(t, &fileSpec{
		sampleSizes: []uint32{10, 20, 30, 40},
		stts:        [][2]uint32{{4, 100}},
		ctts:        [][2]int32{{1, 0}, {1, 200}, {2, 100}},
		stsc:        [][2]uint32{{1, 4}},
		chunks:      1,
		stss:        []uint32{1, 3},
	})

	want := []struct {
		dts  uint64
		pts  int64
		sync bool
	}{
		{0, 0, true},
		{100, 300, false},
		{200, 300, true},
		{300, 400, false},
	}

	if len(samples) != len(want) {
		t.Fatalf("got %d samples, want %d", len(samples), len(want))
	}

	for i, w := range want {
		s := samples[i]
		if s.DTS != w.dts || s.PTS != w.pts || s.Sync != w.sync {
			t.Errorf("sample %d: DTS/PTS/Sync = %d/%d/%v, want %d/%d/%v", i, s.DTS, s.PTS, s.Sync, w.dts, w.pts, w.sync)
		}

		if len(s.Data) != 10*(i+1) || s.Data[0] != byte(i+1) {
			t.Errorf("sample %d: data len %d first byte %d", i, len(s.Data), s.Data[0])
		}
	}
}

func TestVideoSamplesTwoChunksAndCo64(t *testing.T) {
	t.Parallel()

	// Two chunks of two samples: the walk must find sample 3 through the
	// second chunk offset, not by running on from the first.
	tests := []struct {
		name    string
		useCo64 bool
	}{
		{"stco", false},
		{"co64", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			samples := buildSamples(t, &fileSpec{
				sampleSizes: []uint32{10, 20, 30, 40},
				stts:        [][2]uint32{{4, 50}},
				stsc:        [][2]uint32{{1, 2}},
				chunks:      2,
				useCo64:     tt.useCo64,
			})

			if len(samples) != 4 {
				t.Fatalf("got %d samples, want 4", len(samples))
			}

			for i, s := range samples {
				if s.DTS != uint64(i*50) || s.PTS != int64(i*50) {
					t.Errorf("sample %d: DTS %d PTS %d", i, s.DTS, s.PTS)
				}

				// No stss: every sample is a sync sample.
				if !s.Sync {
					t.Errorf("sample %d: Sync = false, want true with no stss", i)
				}

				if len(s.Data) != 10*(i+1) || s.Data[0] != byte(i+1) {
					t.Errorf("sample %d: data len %d first byte %d", i, len(s.Data), s.Data[0])
				}
			}
		})
	}
}

func TestVideoSamplesEditList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		elst      mp4.ElstEntry
		mvhdScale uint32
		wantPTS   []int64
	}{
		{
			// A positive media time trims the start: PTS 0 is the sample
			// whose composition time equals the media time.
			name:    "positive media time subtracted",
			elst:    mp4.ElstEntry{SegmentDuration: 300, MediaTime: 100},
			wantPTS: []int64{-100, 0, 100, 200},
		},
		{
			// An empty edit of 1 s at the 600-tick movie timescale delays the
			// track by 1 s, which is 1000 ticks at the 1000-tick media scale.
			name:      "empty edit adds converted segment duration",
			elst:      mp4.ElstEntry{SegmentDuration: 600, MediaTime: -1},
			mvhdScale: 600,
			wantPTS:   []int64{1000, 1100, 1200, 1300},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			samples := buildSamples(t, &fileSpec{
				sampleSizes: []uint32{10, 20, 30, 40},
				stts:        [][2]uint32{{4, 100}},
				stsc:        [][2]uint32{{1, 4}},
				chunks:      1,
				elst:        &tt.elst,
				mvhdScale:   tt.mvhdScale,
			})

			if len(samples) != len(tt.wantPTS) {
				t.Fatalf("got %d samples, want %d", len(samples), len(tt.wantPTS))
			}

			for i, want := range tt.wantPTS {
				if samples[i].PTS != want {
					t.Errorf("sample %d: PTS = %d, want %d", i, samples[i].PTS, want)
				}

				if samples[i].DTS != uint64(i*100) {
					t.Errorf("sample %d: DTS = %d, want %d", i, samples[i].DTS, i*100)
				}
			}
		})
	}
}

func TestVideoSamplesMalformedTables(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec fileSpec
	}{
		{
			// stsc names chunk 2 but stco has one entry.
			name: "chunk missing from stco",
			spec: fileSpec{
				sampleSizes: []uint32{10, 20, 30, 40},
				stts:        [][2]uint32{{4, 100}},
				stsc:        [][2]uint32{{1, 2}},
				chunks:      1,
			},
		},
		{
			name: "stts covers fewer samples than stsz",
			spec: fileSpec{
				sampleSizes: []uint32{10, 20, 30, 40},
				stts:        [][2]uint32{{2, 100}},
				stsc:        [][2]uint32{{1, 4}},
				chunks:      1,
			},
		},
		{
			name: "ctts covers fewer samples than stsz",
			spec: fileSpec{
				sampleSizes: []uint32{10, 20, 30, 40},
				stts:        [][2]uint32{{4, 100}},
				ctts:        [][2]int32{{2, 0}},
				stsc:        [][2]uint32{{1, 4}},
				chunks:      1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := collectErr(t, buildFile(t, &tt.spec))
			if !errors.Is(err, ErrMalformedFile) {
				t.Fatalf("err = %v, want ErrMalformedFile", err)
			}
		})
	}
}
