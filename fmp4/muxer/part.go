package muxer

import (
	"io"

	"github.com/abema/go-mp4"
	"github.com/q191201771/naza/pkg/nazalog"
)

const (
	trunFlagDataOffsetPreset                       = 0x01
	trunFlagSampleDurationPresent                  = 0x100
	trunFlagSampleSizePresent                      = 0x200
	trunFlagSampleFlagsPresent                     = 0x400
	trunFlagSampleCompositionTimeOffsetPresentOrV1 = 0x800

	sampleFlagIsNonSyncSample = 1 << 16
)

type Part struct {
	SequenceNumber uint32
	Tracks         []*PartTrack
}

func (p *Part) Encode(w io.WriteSeeker) error {
	/*
		|moof|
		|    |mfhd|
		|    |traf|
		|    |traf|
		|    |....|
		|mdat|
	*/

	mw := newMP4Writer(w)

	// moof box
	moofOffset, err := mw.writeBoxStart(&mp4.Moof{})
	if err != nil {
		nazalog.Error("write moof start box failed, err:", err)
		return err
	}

	// mfhd box
	_, err = mw.writeBox(&mp4.Mfhd{
		SequenceNumber: p.SequenceNumber,
	})
	if err != nil {
		nazalog.Error("write mfhd box failed, err:", err)
		return err
	}

	trackLen := len(p.Tracks)
	truns := make([]*mp4.Trun, trackLen)
	trunOffsets := make([]int, trackLen)
	dataOffsets := make([]int, trackLen)
	dataSize := 0

	for i, track := range p.Tracks {
		trun, trunOffset, err := track.Encode(mw)
		if err != nil {
			nazalog.Error("track Encode failed, err:", err)
			return err
		}

		dataOffsets[i] = dataSize
		for _, sample := range track.Samples {
			dataSize += len(sample.Payload)
		}

		truns[i] = trun
		trunOffsets[i] = trunOffset
	}

	// </moof>
	err = mw.writeBoxEnd()
	if err != nil {
		nazalog.Error("write moof end box failed, err:", err)
		return err
	}

	// mdat box
	mdat := &mp4.Mdat{}
	mdat.Data = make([]byte, dataSize)
	pos := 0

	for _, track := range p.Tracks {
		for _, sample := range track.Samples {
			pos += copy(mdat.Data[pos:], sample.Payload)
		}
	}

	mdatOffset, err := mw.writeBox(mdat)
	if err != nil {
		nazalog.Error("write mdat box failed, err", err)
		return err
	}

	for i := range p.Tracks {
		truns[i].DataOffset = int32(dataOffsets[i] + mdatOffset - moofOffset + 8)
		err = mw.rewriteBox(trunOffsets[i], truns[i])
		if err != nil {
			nazalog.Error("rewrite box failed, err:", err)
			return err
		}
	}

	return nil
}

type PartTrack struct {
	ID       int
	BaseTime uint64
	Samples  []*PartSample
}

func (pt *PartTrack) Encode(w *mp4Writer) (*mp4.Trun, int, error) {
	/*
		|traf|
		|    |tfhd|
		|    |tfdt|
		|    |trun|
	*/

	// traf box
	_, err := w.writeBoxStart(&mp4.Traf{})
	if err != nil {
		nazalog.Error("write traf start box failed, err:", err)
		return nil, 0, err
	}

	flags := 0

	// tfhd box
	_, err = w.writeBox(&mp4.Tfhd{
		FullBox: mp4.FullBox{
			Flags: [3]byte{2, byte(flags >> 8), byte(flags)},
		},
		TrackID: uint32(pt.ID),
	})
	if err != nil {
		nazalog.Error("write tfhd box failed, err:", err)
		return nil, 0, err
	}

	// tfdt box
	_, err = w.writeBox(&mp4.Tfdt{
		FullBox: mp4.FullBox{
			Version: 1,
		},
		BaseMediaDecodeTimeV1: pt.BaseTime,
	})
	if err != nil {
		return nil, 0, err
	}

	flags = trunFlagDataOffsetPreset | trunFlagSampleDurationPresent | trunFlagSampleSizePresent

	for _, sample := range pt.Samples {
		if sample.IsNonSyncSample {
			flags |= trunFlagSampleFlagsPresent
		}

		if sample.PTSOffset != 0 {
			flags |= trunFlagSampleCompositionTimeOffsetPresentOrV1
		}
	}

	// trun box
	trun := &mp4.Trun{
		FullBox: mp4.FullBox{
			Version: 1,
			Flags:   [3]byte{0, byte(flags >> 8), byte(flags)},
		},
		SampleCount: uint32(len(pt.Samples)),
	}

	for _, sample := range pt.Samples {
		var flags uint32
		if sample.IsNonSyncSample {
			flags |= sampleFlagIsNonSyncSample
		}

		trun.Entries = append(trun.Entries, mp4.TrunEntry{
			SampleDuration:                sample.Duration,
			SampleSize:                    uint32(len(sample.Payload)),
			SampleFlags:                   flags,
			SampleCompositionTimeOffsetV1: sample.PTSOffset,
		})
	}

	trunOffset, err := w.writeBox(trun)
	if err != nil {
		nazalog.Error("write trun box failed, err:", err)
		return nil, 0, err
	}

	// </traf>
	err = w.writeBoxEnd()
	if err != nil {
		nazalog.Error("write traf end box failed, err:", err)
		return nil, 0, err
	}

	return trun, trunOffset, nil
}

type PartSample struct {
	Duration        uint32
	PTSOffset       int32
	IsNonSyncSample bool
	Payload         []byte
}

func NewPartSampleH26x(ptsOffset int32, randomAccessPresent bool, payload []byte) (*PartSample, error) {
	return &PartSample{
		PTSOffset:       ptsOffset,
		IsNonSyncSample: !randomAccessPresent,
		Payload:         payload,
	}, nil
}
