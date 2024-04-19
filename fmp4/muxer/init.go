package muxer

import (
	"io"

	"github.com/abema/go-mp4"
	"github.com/q191201771/naza/pkg/nazalog"
)

type Init struct {
	Tracks []*Track
}

func (i *Init) Encode(w io.WriteSeeker) error {
	/*
		|ftyp|
		|moov|
		|    |mvhd|
		|    |trak|
		|    |trak|
		|    |....|
		|    |mvex|
		|    |    |trex|
		|    |    |trex|
		|    |    |....|
	*/

	mw := newMP4Writer(w)

	// ftyp box
	_, err := mw.writeBox(&mp4.Ftyp{
		MajorBrand:   [4]byte{'m', 'p', '4', '2'},
		MinorVersion: 1,
		CompatibleBrands: []mp4.CompatibleBrandElem{
			{
				CompatibleBrand: [4]byte{'m', 'p', '4', '1'},
			},
			{
				CompatibleBrand: [4]byte{'m', 'p', '4', '2'},
			},
			{
				CompatibleBrand: [4]byte{'i', 's', 'o', 'm'},
			},
			{
				CompatibleBrand: [4]byte{'h', 'l', 's', 'f'},
			},
		},
	})

	if err != nil {
		nazalog.Error("write ftyp box failed, err:", err)
		return err
	}

	// moov box
	_, err = mw.writeBoxStart(&mp4.Moov{})
	if err != nil {
		nazalog.Error("write moov box failed, err:", err)
		return err
	}

	// mvhd box
	_, err = mw.writeBox(&mp4.Mvhd{
		Timescale:   1000,
		Rate:        65536,
		Volume:      256,
		Matrix:      [9]int32{0x00010000, 0, 0, 0, 0x00010000, 0, 0, 0, 0x40000000},
		NextTrackID: 4294967295,
	})

	if err != nil {
		nazalog.Error("write mvhd box failed, err:", err)
		return err
	}

	// track box
	for _, track := range i.Tracks {
		err = track.Encode(mw)
		if err != nil {
			nazalog.Error("track encode failed, err:", err)
			return err
		}
	}

	// mvex box
	_, err = mw.writeBoxStart(&mp4.Mvex{})
	if err != nil {
		nazalog.Error("write mvex box failed, err:", err)
		return err
	}

	// trex box
	for _, track := range i.Tracks {
		_, err = mw.writeBox(&mp4.Trex{
			TrackID:                       uint32(track.ID),
			DefaultSampleDescriptionIndex: 1,
		})

		if err != nil {
			nazalog.Error("write trex box failed, err:", err)
			return err
		}
	}

	// </mvex>
	err = mw.writeBoxEnd()
	if err != nil {
		nazalog.Error("write mvex end box failed, err:", err)
		return err
	}

	// </moov>
	err = mw.writeBoxEnd()
	if err != nil {
		nazalog.Error("write moov end box failed, err:", err)
		return err
	}

	return nil
}
