package mpegps

//单元来源于https://github.com/yapingcat/gomedia
import (
	"errors"
	"github.com/q191201771/lal/pkg/avc"
	"github.com/q191201771/lal/pkg/hevc"
)

type psstream struct {
	sid       uint8
	cid       PS_STREAM_TYPE
	pts       uint64
	dts       uint64
	streamBuf []byte
}

func newpsstream(sid uint8, cid PS_STREAM_TYPE) *psstream {
	return &psstream{
		sid:       sid,
		cid:       cid,
		streamBuf: make([]byte, 0, 4096),
	}
}
func (p *psstream) setCid(cid PS_STREAM_TYPE) {
	p.cid = cid
}

type PSDemuxer struct {
	streamMap map[uint8]*psstream
	pkg       *PSPacket
	mpeg1     bool
	cache     []byte
	OnFrame   func(frame []byte, cid PS_STREAM_TYPE, pts uint64, dts uint64)
	//解ps包过程中，解码回调psm，system header，pes包等
	//decodeResult 解码ps包时的产生的错误
	//这个回调主要用于debug，查看是否ps包存在问题
	OnPacket func(pkg Display, decodeResult error)
}

func NewPSDemuxer() *PSDemuxer {
	psdemuxer := &PSDemuxer{
		streamMap: make(map[uint8]*psstream),
		pkg:       new(PSPacket),
		cache:     make([]byte, 0, 256),
		OnFrame:   nil,
		OnPacket:  nil,
	}
	//兼容没有发送psm的ps包
	streamH264 := newpsstream(uint8(PES_STREAM_VIDEO), PS_STREAM_H264)
	streamG711A := newpsstream(uint8(PES_STREAM_AUDIO), PS_STREAM_G711A)
	psdemuxer.streamMap[streamH264.sid] = streamH264
	psdemuxer.streamMap[streamG711A.sid] = streamG711A
	return psdemuxer
}

func (psdemuxer *PSDemuxer) Input(data []byte) error {
	var bs *BitStream
	if len(psdemuxer.cache) > 0 {
		psdemuxer.cache = append(psdemuxer.cache, data...)
		bs = NewBitStream(psdemuxer.cache)
	} else {
		bs = NewBitStream(data)
	}

	saveReseved := func() {
		tmpcache := make([]byte, bs.RemainBytes())
		copy(tmpcache, bs.RemainData())
		psdemuxer.cache = tmpcache
	}

	var ret error = nil
	for !bs.EOS() {
		if mpegerr, ok := ret.(Error); ok {
			if mpegerr.NeedMore() {
				saveReseved()
			}
			break
		}
		if bs.RemainBits() < 32 {
			ret = errNeedMore
			saveReseved()
			break
		}
		prefix_code := bs.NextBits(32)
		switch prefix_code {
		case 0x000001BA: //pack header
			if psdemuxer.pkg.Header == nil {
				psdemuxer.pkg.Header = new(PSPackHeader)
			}
			ret = psdemuxer.pkg.Header.Decode(bs)
			psdemuxer.mpeg1 = psdemuxer.pkg.Header.IsMpeg1
			if psdemuxer.OnPacket != nil {
				psdemuxer.OnPacket(psdemuxer.pkg.Header, ret)
			}
		case 0x000001BB: //system header
			if psdemuxer.pkg.Header == nil {
				return errors.New("psdemuxer.pkg.Header must not be nil")
			}
			if psdemuxer.pkg.System == nil {
				psdemuxer.pkg.System = new(System_header)
			}
			ret = psdemuxer.pkg.System.Decode(bs)
			if psdemuxer.OnPacket != nil {
				psdemuxer.OnPacket(psdemuxer.pkg.System, ret)
			}
		case 0x000001BC: //program stream map
			if psdemuxer.pkg.Psm == nil {
				psdemuxer.pkg.Psm = new(Program_stream_map)
			}
			if ret = psdemuxer.pkg.Psm.Decode(bs); ret == nil {
				for _, streaminfo := range psdemuxer.pkg.Psm.Stream_map {
					if _, found := psdemuxer.streamMap[streaminfo.Elementary_stream_id]; !found {
						stream := newpsstream(streaminfo.Elementary_stream_id, PS_STREAM_TYPE(streaminfo.Stream_type))
						psdemuxer.streamMap[stream.sid] = stream
					} else {
						stream := psdemuxer.streamMap[streaminfo.Elementary_stream_id]
						stream.setCid(PS_STREAM_TYPE(streaminfo.Stream_type))
					}
				}
			}
			if psdemuxer.OnPacket != nil {
				psdemuxer.OnPacket(psdemuxer.pkg.Psm, ret)
			}
		case 0x000001BD, 0x000001BE, 0x000001BF, 0x000001F0, 0x000001F1,
			0x000001F2, 0x000001F3, 0x000001F4, 0x000001F5, 0x000001F6,
			0x000001F7, 0x000001F8, 0x000001F9, 0x000001FA, 0x000001FB:
			if psdemuxer.pkg.CommPes == nil {
				psdemuxer.pkg.CommPes = new(CommonPesPacket)
			}
			ret = psdemuxer.pkg.CommPes.Decode(bs)
		case 0x000001FF: //program stream directory
			if psdemuxer.pkg.Psd == nil {
				psdemuxer.pkg.Psd = new(Program_stream_directory)
			}
			ret = psdemuxer.pkg.Psd.Decode(bs)
		case 0x000001B9: //MPEG_program_end_code
			continue
		default:
			if prefix_code&0xFFFFFFE0 == 0x000001C0 || prefix_code&0xFFFFFFE0 == 0x000001E0 {
				if psdemuxer.pkg.Pes == nil {
					psdemuxer.pkg.Pes = NewPesPacket()
				}
				if psdemuxer.mpeg1 {
					ret = psdemuxer.pkg.Pes.DecodeMpeg1(bs)
				} else {
					ret = psdemuxer.pkg.Pes.Decode(bs)
				}
				if psdemuxer.OnPacket != nil {
					psdemuxer.OnPacket(psdemuxer.pkg.Pes, ret)
				}
				if ret == nil {
					if stream, found := psdemuxer.streamMap[psdemuxer.pkg.Pes.Stream_id]; found {
						if psdemuxer.mpeg1 && stream.cid == PS_STREAM_UNKNOW {
							psdemuxer.guessCodecid(stream)
						}
						psdemuxer.demuxPespacket(stream, psdemuxer.pkg.Pes)
					} else {
						if psdemuxer.mpeg1 {
							stream := newpsstream(psdemuxer.pkg.Pes.Stream_id, PS_STREAM_UNKNOW)
							psdemuxer.streamMap[stream.sid] = stream
							stream.streamBuf = append(stream.streamBuf, psdemuxer.pkg.Pes.Pes_payload...)
							stream.pts = psdemuxer.pkg.Pes.Pts
							stream.dts = psdemuxer.pkg.Pes.Dts
						}
					}
				}
			} else {
				bs.SkipBits(8)
			}
		}
	}

	if ret == nil && len(psdemuxer.cache) > 0 {
		psdemuxer.cache = nil
	}

	return ret
}

func (psdemuxer *PSDemuxer) Flush() {
	for _, stream := range psdemuxer.streamMap {
		if len(stream.streamBuf) == 0 {
			continue
		}
		if psdemuxer.OnFrame != nil {
			psdemuxer.OnFrame(stream.streamBuf, stream.cid, stream.pts/90, stream.dts/90)
		}
	}
}

func (psdemuxer *PSDemuxer) guessCodecid(stream *psstream) {
	if stream.sid&0xE0 == uint8(PES_STREAM_AUDIO) {
		stream.cid = PS_STREAM_AAC
	} else if stream.sid&0xE0 == uint8(PES_STREAM_VIDEO) {
		h264score := 0
		h265score := 0
		SplitFrame(stream.streamBuf, func(nalu []byte) bool {
			h264nalutype := avc.ParseNaluType(nalu[0])
			h265nalutype := hevc.ParseNaluType(nalu[0])
			if h264nalutype == avc.NaluTypeSps ||
				h264nalutype == avc.NaluTypePps ||
				h264nalutype == avc.NaluTypeIdrSlice {
				h264score += 2
			} else if h264nalutype < 5 {
				h264score += 1
			} else if h264nalutype > 20 {
				h264score -= 1
			}

			if h265nalutype == hevc.NaluTypeSps ||
				h265nalutype == hevc.NaluTypePps ||
				h265nalutype == hevc.NaluTypeVps ||
				(h265nalutype >= hevc.NaluTypeSliceBlaWlp && h265nalutype <= hevc.NaluTypeSliceRsvIrapVcl23) {
				h265score += 2
			} else if h265nalutype >= hevc.NaluTypeSliceTrailN && h265nalutype <= hevc.NaluTypeSliceRaslR {
				h265score += 1
			} else if h265nalutype > 40 {
				h265score -= 1
			}
			if h264score > h265score && h264score >= 4 {
				stream.cid = PS_STREAM_H264
			} else if h264score < h265score && h265score >= 4 {
				stream.cid = PS_STREAM_H265
			}
			return true
		})
	}
}

func (psdemuxer *PSDemuxer) demuxPespacket(stream *psstream, pes *PesPacket) error {
	switch stream.cid {
	case PS_STREAM_AAC, PS_STREAM_G711A, PS_STREAM_G711U:
		return psdemuxer.demuxAudio(stream, pes)
	case PS_STREAM_H264, PS_STREAM_H265:
		return psdemuxer.demuxH26x(stream, pes)
	case PS_STREAM_UNKNOW:
		if stream.pts != pes.Pts {
			stream.streamBuf = nil
		}
		stream.streamBuf = append(stream.streamBuf, pes.Pes_payload...)
		stream.pts = pes.Pts
		stream.dts = pes.Dts
	}
	return nil
}

func (psdemuxer *PSDemuxer) demuxAudio(stream *psstream, pes *PesPacket) error {
	if psdemuxer.OnFrame != nil {
		psdemuxer.OnFrame(pes.Pes_payload, stream.cid, pes.Pts/90, pes.Dts/90)
	}
	return nil
}

func (psdemuxer *PSDemuxer) demuxH26x(stream *psstream, pes *PesPacket) error {
	if stream.pts == 0 {
		stream.streamBuf = append(stream.streamBuf, pes.Pes_payload...)
		stream.pts = pes.Pts
		stream.dts = pes.Dts
	} else if stream.pts == pes.Pts || pes.Pts == 0 {
		stream.streamBuf = append(stream.streamBuf, pes.Pes_payload...)
	} else {
		start, sc := FindStartCode(stream.streamBuf, 0)
		for start >= 0 && start < len(stream.streamBuf) {
			end, sc2 := FindStartCode(stream.streamBuf, start+int(sc))
			if end < 0 {
				end = len(stream.streamBuf)
			}
			if stream.cid == PS_STREAM_H264 {
				naluType := H264NaluType(stream.streamBuf[start:])
				if naluType != avc.NaluTypeAud {
					if psdemuxer.OnFrame != nil {
						psdemuxer.OnFrame(stream.streamBuf[start:end], stream.cid, stream.pts/90, stream.dts/90)
					}
				}
			} else if stream.cid == PS_STREAM_H265 {
				naluType := H265NaluType(stream.streamBuf[start:])
				if naluType != hevc.NaluTypeAud {
					if psdemuxer.OnFrame != nil {
						psdemuxer.OnFrame(stream.streamBuf[start:end], stream.cid, stream.pts/90, stream.dts/90)
					}
				}
			}
			start = end
			sc = sc2
		}
		stream.streamBuf = nil
		stream.streamBuf = append(stream.streamBuf, pes.Pes_payload...)
		stream.pts = pes.Pts
		stream.dts = pes.Dts
	}

	return nil
}
