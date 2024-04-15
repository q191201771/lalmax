package muxer

import (
	"math"

	"github.com/abema/go-mp4"

	"github.com/q191201771/lal/pkg/avc"
	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/naza/pkg/nazalog"
)

type Track struct {
	ID          int
	TimeScale   uint32
	CodecHeader *base.RtmpMsg
	startDTS    int64
	lastDTS     int64
	samples     []*PartSample
}

func (t *Track) NewTrack(trackId int, timescale uint32, header *base.RtmpMsg) *Track {
	return &Track{
		ID:          trackId,
		TimeScale:   timescale,
		CodecHeader: header,
		startDTS:    math.MinInt64,
		lastDTS:     math.MinInt64,
	}
}

func (t *Track) Encode(w *mp4Writer) error {
	/*
		|trak|
		|    |tkhd|
		|    |mdia|
		|    |    |mdhd|
		|    |    |hdlr|
		|    |    |minf|
		|    |    |    |vmhd| (video)
		|    |    |    |smhd| (audio)
		|    |    |    |dinf|
		|    |    |    |    |dref|
		|    |    |    |    |    |url|
		|    |    |    |stbl|
		|    |    |    |    |stsd|
		|    |    |    |    |    |av01| (AV1)
		|    |    |    |    |    |    |av1C|
		|    |    |    |    |    |    |btrt|
		|    |    |    |    |    |vp09| (VP9)
		|    |    |    |    |    |    |vpcC|
		|    |    |    |    |    |    |btrt|
		|    |    |    |    |    |hev1| (H265)
		|    |    |    |    |    |    |hvcC|
		|    |    |    |    |    |    |btrt|
		|    |    |    |    |    |avc1| (H264)
		|    |    |    |    |    |    |avcC|
		|    |    |    |    |    |    |btrt|
		|    |    |    |    |    |mp4v| (MPEG-4/2/1 video, MJPEG)
		|    |    |    |    |    |    |esds|
		|    |    |    |    |    |    |btrt|
		|    |    |    |    |    |Opus| (Opus)
		|    |    |    |    |    |    |dOps|
		|    |    |    |    |    |    |btrt|
		|    |    |    |    |    |mp4a| (MPEG-4/1 audio)
		|    |    |    |    |    |    |esds|
		|    |    |    |    |    |    |btrt|
		|    |    |    |    |    |ac-3| (AC-3)
		|    |    |    |    |    |    |dac3|
		|    |    |    |    |    |    |btrt|
		|    |    |    |    |    |ipcm| (LPCM)
		|    |    |    |    |    |    |pcmC|
		|    |    |    |    |    |    |btrt|
		|    |    |    |    |stts|
		|    |    |    |    |stsc|
		|    |    |    |    |stsz|
		|    |    |    |    |stco|
	*/

	// trak box
	_, err := w.writeBoxStart(&mp4.Trak{})
	if err != nil {
		nazalog.Error("write trak box failed, err:", err)
		return err
	}

	// tkhd box
	if t.CodecHeader.IsVideoKeySeqHeader() {
		var height uint32
		var width uint32

		codecId := t.CodecHeader.VideoCodecId()
		switch codecId {
		case base.RtmpCodecIdAvc:
			// 从sps中解析出宽和高
			sps, _, err := avc.ParseSpsPpsFromSeqHeader(t.CodecHeader.Payload[2:])
			if err != nil {
				nazalog.Error("parse video seq header failed, err:", err)
				return err
			}

			var ctx avc.Context
			if err := avc.ParseSps(sps, &ctx); err != nil {
				nazalog.Info("parse avc sps failed, err:", err)
				return err
			}

			height = ctx.Height
			width = ctx.Width
		}

		_, err = w.writeBox(&mp4.Tkhd{
			FullBox: mp4.FullBox{
				Flags: [3]byte{0, 0, 3},
			},
			TrackID: uint32(t.ID),
			Width:   uint32(width * 65536),  // w *65536
			Height:  uint32(height * 65536), // h *65536
			Matrix:  [9]int32{0x10000, 0, 0, 0, 0x10000, 0, 0, 0, 0x40000000},
		})

		if err != nil {
			nazalog.Error("write video tkhd box failed, err:", err)
			return err
		}
	} else {
		_, err = w.writeBox(&mp4.Tkhd{
			FullBox: mp4.FullBox{
				Flags: [3]byte{0, 0, 3},
			},
			TrackID:        uint32(t.ID),
			AlternateGroup: 1,
			Volume:         256,
			Matrix:         [9]int32{0x10000, 0, 0, 0, 0x10000, 0, 0, 0, 0x40000000},
		})

		if err != nil {
			nazalog.Error("write audio tkhd box failed, err:", err)
			return err
		}
	}

	// mdia
	_, err = w.writeBoxStart(&mp4.Mdia{})
	if err != nil {
		nazalog.Error("write mdia box start failed, err:", err)
		return err
	}

	// mdhd
	_, err = w.writeBox(&mp4.Mdhd{
		Timescale: t.TimeScale,
		Language:  [3]byte{'u', 'n', 'd'},
	})

	if err != nil {
		nazalog.Error("write mdhd box failed, err:", err)
		return err
	}

	// hdlr box
	if t.CodecHeader.IsVideoKeySeqHeader() {
		_, err = w.writeBox(&mp4.Hdlr{
			HandlerType: [4]byte{'v', 'i', 'd', 'e'},
			Name:        "VideoHandler",
		})

		if err != nil {
			nazalog.Error("write video hdlr box failed, err:", err)
			return err
		}
	} else {
		_, err = w.writeBox(&mp4.Hdlr{
			HandlerType: [4]byte{'s', 'o', 'u', 'n'},
			Name:        "SoundHandler",
		})

		if err != nil {
			nazalog.Error("write audio hdlr box failed, err:", err)
			return err
		}
	}

	// minf
	_, err = w.writeBoxStart(&mp4.Minf{})
	if err != nil {
		nazalog.Error("write minf start box failed")
		return err
	}

	if t.CodecHeader.IsVideoKeySeqHeader() {
		//vmhd
		_, err = w.writeBox(&mp4.Vmhd{
			FullBox: mp4.FullBox{
				Flags: [3]byte{0, 0, 1},
			},
		})

		if err != nil {
			nazalog.Error("write vmhd box failed, err:", err)
			return nil
		}
	} else {
		// smhd
		_, err = w.writeBox(&mp4.Smhd{})
		if err != nil {
			nazalog.Error("write smhd box failed, err:", err)
			return err
		}
	}

	// dinf box
	_, err = w.writeBox(&mp4.Dinf{})
	if err != nil {
		nazalog.Error("write dinf box failed, err:", err)
		return err
	}

	// dref/ box
	_, err = w.writeBox(&mp4.Dref{
		EntryCount: 1,
	})
	if err != nil {
		nazalog.Error("write dref box failed, err:", err)
		return err
	}

	// url box
	_, err = w.writeBox(&mp4.Url{
		FullBox: mp4.FullBox{
			Flags: [3]byte{0, 0, 1},
		},
	})
	if err != nil {
		nazalog.Error("write url box failed, err:", err)
		return err
	}

	// </dref> box
	err = w.writeBoxEnd()
	if err != nil {
		nazalog.Error("write dref end box failed, err:", err)
		return err
	}

	// </dinf> box
	err = w.writeBoxEnd()
	if err != nil {
		nazalog.Error("write dinf end box failed, err:", err)
		return err
	}

	// stbl box
	_, err = w.writeBoxStart(&mp4.Stbl{})
	if err != nil {
		nazalog.Error("write stbl start box failed, err:", err)
		return err
	}

	// stsd box
	_, err = w.writeBoxStart(&mp4.Stsd{
		EntryCount: 1,
	})
	if err != nil {
		nazalog.Error("write stsd start box failed, err:", err)
		return err
	}

	if t.CodecHeader.IsVideoKeySeqHeader() {
		codecId := t.CodecHeader.VideoCodecId()
		switch codecId {
		case base.RtmpCodecIdAvc:
			// 从sps中解析出宽和高
			sps, pps, err := avc.ParseSpsPpsFromSeqHeader(t.CodecHeader.Payload[2:])
			if err != nil {
				nazalog.Error("parse video seq header failed, err:", err)
				return err
			}

			var ctx avc.Context
			if err := avc.ParseSps(sps, &ctx); err != nil {
				nazalog.Info("parse avc sps failed, err:", err)
				return err
			}

			height := ctx.Height
			width := ctx.Width

			_, err = w.writeBoxStart(&mp4.VisualSampleEntry{ // avc1
				SampleEntry: mp4.SampleEntry{
					AnyTypeBox: mp4.AnyTypeBox{
						Type: mp4.BoxTypeAv1C(),
					},
					DataReferenceIndex: 1,
				},
				Width:           uint16(width),  // w
				Height:          uint16(height), // h
				Horizresolution: 4718592,
				Vertresolution:  4718592,
				FrameCount:      1,
				Depth:           24,
				PreDefined3:     -1,
			})

			if err != nil {
				nazalog.Error("write avc1 box failed, err:", err)
				return err
			}

			// avcC box
			_, err = w.writeBox(&mp4.AVCDecoderConfiguration{
				AnyTypeBox: mp4.AnyTypeBox{
					Type: mp4.BoxTypeAvcC(),
				},
				ConfigurationVersion:       1,
				Profile:                    ctx.Profile,
				ProfileCompatibility:       sps[2],
				Level:                      ctx.Level,
				LengthSizeMinusOne:         3,
				NumOfSequenceParameterSets: 1,
				SequenceParameterSets: []mp4.AVCParameterSet{
					{
						Length:  uint16(len(sps)), // sps长度
						NALUnit: sps,              // sps nalu
					},
				},
				NumOfPictureParameterSets: 1,
				PictureParameterSets: []mp4.AVCParameterSet{
					{
						Length:  uint16(len(pps)), // pps 长度
						NALUnit: pps,              // pps nalu
					},
				},
			})

			if err != nil {
				nazalog.Error("write avcC box failed, err:", err)
				return err
			}
		case base.RtmpCodecIdHevc:

		}
	} else {

	}

	// btrt box
	if t.CodecHeader.IsVideoKeySeqHeader() {
		_, err = w.writeBox(&mp4.Btrt{
			MaxBitrate: 1000000,
			AvgBitrate: 1000000,
		})
		if err != nil {
			nazalog.Error("write video btrt box failed, err:", err)
			return err
		}
	} else {
		_, err = w.writeBox(&mp4.Btrt{
			MaxBitrate: 128825,
			AvgBitrate: 128825,
		})
		if err != nil {
			nazalog.Error("write audio btrt box failed, err:", err)
			return err
		}
	}

	err = w.writeBoxEnd() // </*>
	if err != nil {
		nazalog.Error("write end box failed, err:", err)
		return err
	}

	err = w.writeBoxEnd() // </stsd>
	if err != nil {
		nazalog.Error("write stsd end box failed, err:", err)
		return err
	}

	// stts box
	_, err = w.writeBox(&mp4.Stts{})
	if err != nil {
		nazalog.Error("write stts box failed, err:", err)
		return err
	}

	// stsc box
	_, err = w.writeBox(&mp4.Stsc{})
	if err != nil {
		nazalog.Error("write stsc box failed, err:", err)
		return err
	}

	// stsz box
	_, err = w.writeBox(&mp4.Stsz{})
	if err != nil {
		nazalog.Error("write stsz box failed, err:", err)
		return err
	}

	// stco box
	_, err = w.writeBox(&mp4.Stco{})
	if err != nil {
		nazalog.Error("write stco box failed, err:", err)
		return err
	}

	err = w.writeBoxEnd() // </stbl>
	if err != nil {
		nazalog.Error("write stbl end box failed, err:", err)
		return err
	}

	err = w.writeBoxEnd() // </minf>
	if err != nil {
		nazalog.Error("write minf end box failed, err:", err)
		return err
	}

	err = w.writeBoxEnd() // </mdia>
	if err != nil {
		nazalog.Error("write mdia end box failed, err:", err)
		return err
	}

	err = w.writeBoxEnd() // </trak>
	if err != nil {
		nazalog.Error("write trak end box failed, err:", err)
		return err
	}

	return nil
}
