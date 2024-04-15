package mediaserver

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"lalmax/gb28181/mpegps"
	"net"
	"time"

	"github.com/pion/rtp"
	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/lal/pkg/logic"
	"github.com/q191201771/naza/pkg/nazalog"
)

var (
	ErrInvalidPsData = errors.New("invalid mpegps data")
)

type Frame struct {
	buffer  *bytes.Buffer
	pts     uint64
	dts     uint64
	initPts uint64
	initDts uint64
}

type Conn struct {
	conn       net.Conn
	r          io.Reader
	check      bool
	demuxer    *mpegps.PSDemuxer
	streamName string
	lalServer  logic.ILalServer
	lalSession logic.ICustomizePubSessionContext
	videoFrame Frame
	audioFrame Frame

	rtpPts         uint64
	psPtsZeroTimes int64

	CheckSsrc   func(ssrc uint32) (string, bool)
	NotifyClose func(streamName string)
	buffer      *bytes.Buffer
}

func NewConn(conn net.Conn, lal logic.ILalServer) *Conn {
	c := &Conn{
		conn:      conn,
		r:         conn,
		demuxer:   mpegps.NewPSDemuxer(),
		lalServer: lal,
		buffer:    bytes.NewBuffer(nil),
	}

	c.demuxer.OnFrame = c.OnFrame

	return c
}

func (c *Conn) Serve() (err error) {
	defer func() {
		nazalog.Info("conn close, err:", err)
		c.conn.Close()

		if c.check {
			if c.NotifyClose != nil {
				c.NotifyClose(c.streamName)
			}

			c.lalServer.DelCustomizePubSession(c.lalSession)
		}
	}()

	nazalog.Info("gb28181 conn, remoteaddr:", c.conn.RemoteAddr().String(), " localaddr:", c.conn.LocalAddr().String())

	for {
		c.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		pkt := &rtp.Packet{}
		if c.conn.RemoteAddr().Network() == "udp" {
			buf := make([]byte, 1472)
			n, err := c.conn.Read(buf)
			if err != nil {
				nazalog.Error("conn read failed, err:", err)
				return err
			}

			err = pkt.Unmarshal(buf[:n])
			if err != nil {
				return err
			}
		} else {
			len := make([]byte, 2)
			_, err := io.ReadFull(c.r, len)
			if err != nil {
				return err
			}

			size := binary.BigEndian.Uint16(len)
			buf := make([]byte, size)
			_, err = io.ReadFull(c.r, buf)
			if err != nil {
				return err
			}

			err = pkt.Unmarshal(buf)
			if err != nil {
				return err
			}
		}

		if !c.check && c.CheckSsrc != nil {
			streamName, ok := c.CheckSsrc(pkt.SSRC)
			if !ok {
				nazalog.Error("invalid ssrc:", pkt.SSRC)
				return fmt.Errorf("invalid ssrc:%d", pkt.SSRC)
			}

			c.check = true
			c.streamName = streamName

			nazalog.Info("gb28181 ssrc check success, streamName:", c.streamName)

			session, err := c.lalServer.AddCustomizePubSession(streamName)
			if err != nil {
				nazalog.Error("lal server AddCustomizePubSession failed, err:", err)
				return err
			}

			session.WithOption(func(option *base.AvPacketStreamOption) {
				option.VideoFormat = base.AvPacketStreamVideoFormatAnnexb
			})

			c.lalSession = session
		}
		c.rtpPts = uint64(pkt.Header.Timestamp)
		if c.demuxer != nil {
			c.demuxer.Input(pkt.Payload)
		}
		//c.Demuxer(pkt.Payload)
	}
	return
}

func (c *Conn) Demuxer(data []byte) error {
	c.buffer.Write(data)

	buf := c.buffer.Bytes()
	if len(buf) < 4 {
		return nil
	}

	if buf[0] != 0x00 && buf[1] != 0x00 && buf[2] != 0x01 && buf[3] != 0xBA {
		return ErrInvalidPsData
	}

	packets := splitPsPackets(buf)
	if len(packets) <= 1 {
		return nil
	}

	for i, packet := range packets {
		if i == len(packets)-1 {
			c.buffer = bytes.NewBuffer(packet)
			return nil
		}

		if c.demuxer != nil {
			c.demuxer.Input(packet)
		}
	}

	return nil
}

func (c *Conn) OnFrame(frame []byte, cid mpegps.PS_STREAM_TYPE, pts uint64, dts uint64) {
	payloadType := getPayloadType(cid)
	if payloadType == base.AvPacketPtUnknown {
		return
	}
	//当ps流解析出pts为0时，计数超过10则用rtp的时间戳
	if pts == 0 {
		if c.psPtsZeroTimes >= 0 {
			c.psPtsZeroTimes++
		}
		if c.psPtsZeroTimes > 10 {
			pts = c.rtpPts
			dts = c.rtpPts
		}
	} else {
		c.psPtsZeroTimes = -1
	}
	if payloadType == base.AvPacketPtAac || payloadType == base.AvPacketPtG711A || payloadType == base.AvPacketPtG711U {
		if c.audioFrame.initDts == 0 {
			c.audioFrame.initDts = dts
		}

		if c.audioFrame.initPts == 0 {
			c.audioFrame.initPts = pts
		}

		var pkt base.AvPacket
		pkt.PayloadType = payloadType
		pkt.Timestamp = int64(dts - c.audioFrame.initDts)
		pkt.Pts = int64(pts - c.audioFrame.initPts)
		pkt.Payload = append(pkt.Payload, frame...)
		c.lalSession.FeedAvPacket(pkt)

	} else {
		if c.videoFrame.initPts == 0 {
			c.videoFrame.initPts = pts
		}

		if c.videoFrame.initDts == 0 {
			c.videoFrame.initDts = dts
		}

		if dts-c.videoFrame.initDts != c.videoFrame.dts {
			// 塞入lal中
			var pkt base.AvPacket
			pkt.PayloadType = payloadType
			pkt.Timestamp = int64(c.videoFrame.dts)
			pkt.Pts = int64(c.videoFrame.pts)
			pkt.Payload = append(pkt.Payload, c.videoFrame.buffer.Bytes()...)
			c.lalSession.FeedAvPacket(pkt)

			c.videoFrame.buffer = bytes.NewBuffer(nil)
		}

		if c.videoFrame.buffer == nil {
			c.videoFrame.buffer = bytes.NewBuffer(frame)
		} else {
			c.videoFrame.buffer.Write(frame)
		}

		c.videoFrame.pts = pts - c.videoFrame.initPts
		c.videoFrame.dts = dts - c.videoFrame.initDts
	}
}

func getPayloadType(cid mpegps.PS_STREAM_TYPE) base.AvPacketPt {
	switch cid {
	case mpegps.PS_STREAM_AAC:
		return base.AvPacketPtAac
	case mpegps.PS_STREAM_G711A:
		return base.AvPacketPtG711A
	case mpegps.PS_STREAM_G711U:
		return base.AvPacketPtG711U
	case mpegps.PS_STREAM_H264:
		return base.AvPacketPtAvc
	case mpegps.PS_STREAM_H265:
		return base.AvPacketPtHevc
	}

	return base.AvPacketPtUnknown
}

func splitPsPackets(data []byte) [][]byte {
	startCode := []byte{0x00, 0x00, 0x01, 0xBA}
	start := 0
	var packets [][]byte
	for i := 0; i < len(data); i++ {
		if i+len(startCode) <= len(data) && bytes.Equal(data[i:i+len(startCode)], startCode) {
			if i == 0 {
				continue
			}
			packets = append(packets, data[start:i])
			start = i
		}
	}
	packets = append(packets, data[start:])

	return packets
}
