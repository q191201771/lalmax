package rtppush

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/q191201771/lal/pkg/aac"
	"github.com/q191201771/lal/pkg/avc"
	lalbase "github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/lal/pkg/hevc"
	"github.com/q191201771/lal/pkg/rtprtcp"
	"github.com/q191201771/lalmax/gb28181/mpegps"
	"github.com/q191201771/naza/pkg/nazalog"
	"github.com/q191201771/naza/pkg/unique"
)

const (
	lowerPushNetworkUDP = "udp"
	lowerPushNetworkTCP = "tcp"

	// 单个 RTP 包承载的 PS 数据最大长度，避免 UDP 包过大导致分片。
	lowerPushRtpPacketMax = 1400
	// 启动阶段待处理 RTMP 消息的最大缓存数量，不是持续发送队列长度。
	lowerPushQueueMax = 256
)

var lowerPushUnique = unique.NewSingleGenerator("GBLOWERPUSH")

// LowerPushSession 表示 lalmax 作为 GB28181 下级平台，
// 主动向上级平台推送 RTP 媒体流。
// 这里只支持主动 UDP/TCP 模式。
type LowerPushSession struct {
	uniqueKey  string
	streamName string

	network   string
	localIP   string
	localPort int
	peerIP    string
	peerPort  int

	udpConn net.Conn
	tcpConn net.Conn

	log nazalog.Logger

	disposeOnce sync.Once

	writeBytes uint64

	psMuxer *mpegps.PsMuxer
	ssrc    uint32
	seq     uint16

	videoID uint8
	audioID uint8

	videoHeader *lalbase.RtmpMsg
	audioHeader *lalbase.RtmpMsg
	ascCtx      *aac.AscContext

	videoCodec []byte
	pending    []lalbase.RtmpMsg

	ready        bool
	onlyAudio    bool
	waitKeyFrame bool
	eraseSei     bool
}

// NewLowerPushSession 创建下级平台推流会话，并绑定 PS 到 RTP 的发送回调。
func NewLowerPushSession() *LowerPushSession {
	s := &LowerPushSession{
		uniqueKey: lowerPushUnique.GenUniqueKey(),
		log:       nazalog.GetGlobalLogger(),
		eraseSei:  true,
	}
	s.log = s.log.WithPrefix(s.uniqueKey)
	s.psMuxer = mpegps.NewPsMuxer()
	s.psMuxer.OnPacket = func(pkg []byte, pts uint64) {
		for _, pkt := range s.packRtp(pkg, uint32(pts)) {
			if err := s.WriteRtpPacket(pkt); err != nil {
				s.log.Warnf("gb28181 lower push write rtp failed. err=%v", err)
				return
			}
		}
	}
	return s
}

// WithStreamName 设置业务流名称，便于日志和外部管理。
func (s *LowerPushSession) WithStreamName(streamName string) *LowerPushSession {
	s.streamName = streamName
	return s
}

// WithLogPrefix 设置日志前缀，用于区分不同推流会话。
func (s *LowerPushSession) WithLogPrefix(prefix string) *LowerPushSession {
	s.log = s.log.WithPrefix(prefix)
	return s
}

// SetLocalIP 设置本地绑定 IP，为空时由系统自动选择。
func (s *LowerPushSession) SetLocalIP(localIP string) {
	s.localIP = localIP
}

// SetLocalPort 设置本地绑定端口，为 0 时由系统自动分配。
func (s *LowerPushSession) SetLocalPort(localPort int) {
	s.localPort = localPort
}

// SetPeerIP 设置上级平台接收 RTP 的 IP。
func (s *LowerPushSession) SetPeerIP(peerIP string) {
	s.peerIP = peerIP
}

// SetPeerPort 设置上级平台接收 RTP 的端口。
func (s *LowerPushSession) SetPeerPort(peerPort int) {
	s.peerPort = peerPort
}

// SetSsrc 设置 RTP 包中的 SSRC。
func (s *LowerPushSession) SetSsrc(ssrc uint32) {
	s.ssrc = ssrc
}

// Start 按指定网络类型启动推流连接，当前支持 UDP 和 TCP 主动连接。
func (s *LowerPushSession) Start(network string) error {
	switch network {
	case lowerPushNetworkUDP:
		return s.startUDP()
	case lowerPushNetworkTCP:
		return s.startTCP()
	default:
		return fmt.Errorf("gb28181 lower push invalid network: %s", network)
	}
}

// startUDP 建立到上级平台的 UDP 连接。
func (s *LowerPushSession) startUDP() error {
	laddr := &net.UDPAddr{Port: s.localPort}
	if s.localIP != "" {
		laddr.IP = net.ParseIP(s.localIP)
	}
	raddr := &net.UDPAddr{
		IP:   net.ParseIP(s.peerIP),
		Port: s.peerPort,
	}
	if raddr.IP == nil || raddr.Port == 0 {
		return fmt.Errorf("gb28181 lower push invalid udp peer addr: %s:%d", s.peerIP, s.peerPort)
	}

	conn, err := net.DialUDP(lowerPushNetworkUDP, laddr, raddr)
	if err != nil {
		return err
	}
	s.network = lowerPushNetworkUDP
	s.udpConn = conn
	s.log.Infof("gb28181 lower push udp ready. local=%s remote=%s", conn.LocalAddr(), conn.RemoteAddr())
	return nil
}

// startTCP 建立到上级平台的 TCP 连接。
func (s *LowerPushSession) startTCP() error {
	localAddr := &net.TCPAddr{Port: s.localPort}
	if s.localIP != "" {
		localAddr.IP = net.ParseIP(s.localIP)
	}
	dialer := &net.Dialer{
		LocalAddr: localAddr,
		Timeout:   3 * time.Second,
	}
	conn, err := dialer.Dial(lowerPushNetworkTCP, net.JoinHostPort(s.peerIP, fmt.Sprintf("%d", s.peerPort)))
	if err != nil {
		return err
	}
	s.network = lowerPushNetworkTCP
	s.tcpConn = conn
	s.log.Infof("gb28181 lower push tcp ready. local=%s remote=%s", conn.LocalAddr(), conn.RemoteAddr())
	return nil
}

// OnMsg 接收 RTMP 消息，完成启动阶段缓存、音视频头解析和后续 PS/RTP 推送。
func (s *LowerPushSession) OnMsg(msg lalbase.RtmpMsg) {
	if s.consumeControlMsg(msg) {
		if !s.ready && s.shouldDrain(msg) {
			s.drain()
		}
		return
	}

	if s.ready {
		if err := s.feedRtmpMsg(msg); err != nil {
			s.log.Warnf("gb28181 lower push feed msg failed. err=%v, msg=%s", err, msg.DebugString())
		}
		return
	}

	s.pending = append(s.pending, msg.Clone())
	if s.shouldDrain(msg) || len(s.pending) >= lowerPushQueueMax {
		s.drain()
	}
}

// OnStop 在上游流停止时释放推流连接。
func (s *LowerPushSession) OnStop() {
	_ = s.Dispose()
}

// WriteRtpPacket 按当前网络类型发送已经封装好的 RTP 包。
func (s *LowerPushSession) WriteRtpPacket(pkt rtprtcp.RtpPacket) error {
	if s.network == "" {
		return lalbase.ErrSessionNotStarted
	}
	switch s.network {
	case lowerPushNetworkUDP:
		return s.writeUDP(pkt.Raw)
	case lowerPushNetworkTCP:
		return s.writeTCP(pkt.Raw)
	default:
		return fmt.Errorf("gb28181 lower push invalid network state: %s", s.network)
	}
}

// WriteRtpPsPacket 写入单个 RTP/PS 包。
// 在 TCP 模式下，既支持原始 RTP 负载，也支持带 2 字节长度前缀的 RTP 包。
// 在 UDP 模式下，只发送 RTP 负载本身。
func (s *LowerPushSession) WriteRtpPsPacket(buf []byte) error {
	if s.network == "" {
		return lalbase.ErrSessionNotStarted
	}
	payload := buf
	if len(buf) >= 2 && len(buf) == int(uint16(buf[0])<<8|uint16(buf[1]))+2 {
		payload = buf[2:]
	}
	switch s.network {
	case lowerPushNetworkUDP:
		return s.writeUDP(payload)
	case lowerPushNetworkTCP:
		return s.writeTCP(payload)
	default:
		return fmt.Errorf("gb28181 lower push invalid network state: %s", s.network)
	}
}

// writeUDP 通过 UDP 直接发送 RTP 负载。
func (s *LowerPushSession) writeUDP(payload []byte) error {
	if s.udpConn == nil {
		return lalbase.ErrSessionNotStarted
	}
	n, err := s.udpConn.Write(payload)
	s.writeBytes += uint64(n)
	return err
}

// writeTCP 按 GB28181 TCP 传输格式添加 2 字节长度前缀后发送 RTP 负载。
func (s *LowerPushSession) writeTCP(payload []byte) error {
	if s.tcpConn == nil {
		return lalbase.ErrSessionNotStarted
	}
	header := []byte{byte(len(payload) >> 8), byte(len(payload))}
	n, err := s.tcpConn.Write(append(header, payload...))
	s.writeBytes += uint64(n)
	return err
}

// Dispose 关闭底层连接，重复调用是安全的。
func (s *LowerPushSession) Dispose() error {
	var retErr error
	s.disposeOnce.Do(func() {
		if s.udpConn != nil {
			retErr = s.udpConn.Close()
			s.udpConn = nil
		}
		if s.tcpConn != nil {
			if err := s.tcpConn.Close(); retErr == nil {
				retErr = err
			}
			s.tcpConn = nil
		}
	})
	return retErr
}

// UniqueKey 返回当前推流会话的唯一标识。
func (s *LowerPushSession) UniqueKey() string {
	return s.uniqueKey
}

// StreamName 返回业务流名称，未设置时使用会话唯一标识。
func (s *LowerPushSession) StreamName() string {
	if s.streamName == "" {
		return s.uniqueKey
	}
	return s.streamName
}

// LocalAddr 返回当前连接的本地地址。
func (s *LowerPushSession) LocalAddr() net.Addr {
	if s.udpConn != nil {
		return s.udpConn.LocalAddr()
	}
	if s.tcpConn != nil {
		return s.tcpConn.LocalAddr()
	}
	return nil
}

// RemoteAddr 返回当前连接的远端地址。
func (s *LowerPushSession) RemoteAddr() net.Addr {
	if s.udpConn != nil {
		return s.udpConn.RemoteAddr()
	}
	if s.tcpConn != nil {
		return s.tcpConn.RemoteAddr()
	}
	return nil
}

// consumeControlMsg 处理元数据、音视频序列头和不支持的消息，返回 true 表示不再进入媒体发送流程。
func (s *LowerPushSession) consumeControlMsg(msg lalbase.RtmpMsg) bool {
	switch msg.Header.MsgTypeId {
	case lalbase.RtmpTypeIdMetadata:
		return true
	case lalbase.RtmpTypeIdVideo:
		if len(msg.Payload) < 2 {
			return true
		}
		if msg.IsVideoKeySeqHeader() {
			if err := s.updateVideoHeader(msg); err != nil {
				s.log.Warnf("gb28181 lower push parse video seq header failed. err=%v", err)
			}
			return true
		}
		if msg.IsEnhanced() && !msg.IsEnchanedHevcNalu() {
			return true
		}
	case lalbase.RtmpTypeIdAudio:
		if len(msg.Payload) < 1 {
			return true
		}
		switch msg.AudioCodecId() {
		case lalbase.RtmpSoundFormatAac:
			if len(msg.Payload) < 2 {
				return true
			}
			if msg.IsAacSeqHeader() {
				if err := s.updateAacHeader(msg); err != nil {
					s.log.Warnf("gb28181 lower push parse aac seq header failed. err=%v", err)
				}
				return true
			}
		case lalbase.RtmpSoundFormatG711A:
			if s.audioID == 0 {
				s.audioID = s.psMuxer.AddStream(mpegps.PsStreamG711A)
			}
			if s.audioHeader == nil {
				cloned := msg.Clone()
				s.audioHeader = &cloned
			}
		case lalbase.RtmpSoundFormatG711U:
			if s.audioID == 0 {
				s.audioID = s.psMuxer.AddStream(mpegps.PsStreamG711U)
			}
			if s.audioHeader == nil {
				cloned := msg.Clone()
				s.audioHeader = &cloned
			}
		case lalbase.RtmpSoundFormatOpus:
			return true
		}
	}
	return false
}

// updateVideoHeader 解析并缓存 H264/H265 序列头，同时注册对应的 PS 视频流。
func (s *LowerPushSession) updateVideoHeader(msg lalbase.RtmpMsg) error {
	if msg.IsAvcKeySeqHeader() {
		if s.videoID == 0 {
			s.videoID = s.psMuxer.AddStream(mpegps.PsStreamH264)
		}
		codec, err := avc.SpsPpsSeqHeader2Annexb(msg.Payload)
		if err != nil {
			return err
		}
		s.videoCodec = append(s.videoCodec[:0], codec...)
	} else if msg.IsHevcKeySeqHeader() {
		if s.videoID == 0 {
			s.videoID = s.psMuxer.AddStream(mpegps.PsStreamH265)
		}
		var (
			codec []byte
			err   error
		)
		if msg.IsEnhanced() {
			codec, err = hevc.VpsSpsPpsEnhancedSeqHeader2Annexb(msg.Payload)
		} else {
			codec, err = hevc.VpsSpsPpsSeqHeader2Annexb(msg.Payload)
		}
		if err != nil {
			return err
		}
		s.videoCodec = append(s.videoCodec[:0], codec...)
	}

	cloned := msg.Clone()
	s.videoHeader = &cloned
	return nil
}

// updateAacHeader 解析并缓存 AAC 序列头，同时注册对应的 PS 音频流。
func (s *LowerPushSession) updateAacHeader(msg lalbase.RtmpMsg) error {
	if s.audioID == 0 {
		s.audioID = s.psMuxer.AddStream(mpegps.PsStreamAac)
	}
	ascCtx, err := aac.NewAscContext(msg.Payload[2:])
	if err != nil {
		return err
	}
	s.ascCtx = ascCtx
	cloned := msg.Clone()
	s.audioHeader = &cloned
	return nil
}

// shouldDrain 判断启动阶段缓存是否已经满足发送条件。
func (s *LowerPushSession) shouldDrain(msg lalbase.RtmpMsg) bool {
	if s.videoHeader != nil && s.audioHeader != nil {
		return true
	}
	if s.videoHeader != nil && msg.Header.MsgTypeId == lalbase.RtmpTypeIdVideo && !msg.IsVideoKeySeqHeader() {
		return true
	}
	if s.videoHeader == nil && s.audioHeader != nil && msg.Header.MsgTypeId == lalbase.RtmpTypeIdAudio {
		if len(msg.Payload) == 0 {
			return false
		}
		if msg.AudioCodecId() == lalbase.RtmpSoundFormatAac {
			return !msg.IsAacSeqHeader()
		}
		return true
	}
	return false
}

// drain 结束启动阶段缓存，将已缓存消息按顺序送入打包流程。
func (s *LowerPushSession) drain() {
	if s.ready {
		return
	}
	s.ready = true
	s.onlyAudio = s.videoHeader == nil && s.audioHeader != nil
	s.waitKeyFrame = s.videoHeader != nil

	for i := range s.pending {
		if err := s.feedRtmpMsg(s.pending[i]); err != nil {
			s.log.Warnf("gb28181 lower push drain msg failed. err=%v, msg=%s", err, s.pending[i].DebugString())
		}
	}
	s.pending = nil
}

// feedRtmpMsg 按消息类型分发音频或视频数据。
func (s *LowerPushSession) feedRtmpMsg(msg lalbase.RtmpMsg) error {
	switch msg.Header.MsgTypeId {
	case lalbase.RtmpTypeIdVideo:
		if s.onlyAudio {
			return nil
		}
		return s.feedVideo(msg)
	case lalbase.RtmpTypeIdAudio:
		return s.feedAudio(msg)
	default:
		return nil
	}
}

// feedVideo 将 RTMP 视频帧转换为 Annex-B 格式，并写入 PS 复用器。
func (s *LowerPushSession) feedVideo(msg lalbase.RtmpMsg) error {
	startIndex := 5
	if msg.IsEnchanedHevcNalu() {
		startIndex = msg.GetEnchanedHevcNaluIndex()
	}
	if len(msg.Payload) <= startIndex {
		return nil
	}

	var (
		buf            []byte
		appendCodec    bool
		sps            []byte
		pps            []byte
		vps            []byte
		err            error
		isH264         = msg.VideoCodecId() == lalbase.RtmpCodecIdAvc
		videoPayload   = msg.Payload[startIndex:]
		appendStartCde = avc.NaluStartCode4
	)

	err = avc.IterateNaluAvcc(videoPayload, func(nal []byte) {
		if len(nal) == 0 {
			return
		}

		if isH264 {
			switch avc.ParseNaluType(nal[0]) {
			case avc.NaluTypeSps:
				sps = nal
			case avc.NaluTypePps:
				pps = nal
				if len(sps) != 0 {
					s.videoCodec = s.videoCodec[:0]
					s.videoCodec = append(s.videoCodec, appendStartCde...)
					s.videoCodec = append(s.videoCodec, sps...)
					s.videoCodec = append(s.videoCodec, appendStartCde...)
					s.videoCodec = append(s.videoCodec, pps...)
				}
			case avc.NaluTypeIdrSlice:
				if !appendCodec && len(s.videoCodec) != 0 {
					buf = append(buf, s.videoCodec...)
					appendCodec = true
				}
				buf = append(buf, appendStartCde...)
				buf = append(buf, nal...)
				s.waitKeyFrame = false
			case avc.NaluTypeSei:
				if !s.eraseSei {
					buf = append(buf, appendStartCde...)
					buf = append(buf, nal...)
				}
			default:
				if s.waitKeyFrame {
					return
				}
				buf = append(buf, appendStartCde...)
				buf = append(buf, nal...)
			}
			return
		}

		switch hevc.ParseNaluType(nal[0]) {
		case hevc.NaluTypeVps:
			vps = nal
		case hevc.NaluTypeSps:
			sps = nal
		case hevc.NaluTypePps:
			pps = nal
			if len(vps) != 0 && len(sps) != 0 {
				s.videoCodec = s.videoCodec[:0]
				s.videoCodec = append(s.videoCodec, appendStartCde...)
				s.videoCodec = append(s.videoCodec, vps...)
				s.videoCodec = append(s.videoCodec, appendStartCde...)
				s.videoCodec = append(s.videoCodec, sps...)
				s.videoCodec = append(s.videoCodec, appendStartCde...)
				s.videoCodec = append(s.videoCodec, pps...)
			}
		case hevc.NaluTypeSei, hevc.NaluTypeSeiSuffix:
			if !s.eraseSei {
				buf = append(buf, appendStartCde...)
				buf = append(buf, nal...)
			}
		default:
			if hevc.IsIrapNalu(hevc.ParseNaluType(nal[0])) {
				if !appendCodec && len(s.videoCodec) != 0 {
					buf = append(buf, s.videoCodec...)
					appendCodec = true
				}
				buf = append(buf, appendStartCde...)
				buf = append(buf, nal...)
				s.waitKeyFrame = false
				return
			}
			if s.waitKeyFrame {
				return
			}
			buf = append(buf, appendStartCde...)
			buf = append(buf, nal...)
		}
	})
	if err != nil {
		return err
	}
	if len(buf) == 0 || s.videoID == 0 {
		return nil
	}
	return s.psMuxer.Write(s.videoID, buf, uint64(msg.Pts()), uint64(msg.Dts()))
}

// feedAudio 将 RTMP 音频帧转换为 PS 支持的音频负载，并写入 PS 复用器。
func (s *LowerPushSession) feedAudio(msg lalbase.RtmpMsg) error {
	if s.waitKeyFrame {
		return nil
	}
	if len(msg.Payload) == 0 {
		return nil
	}

	switch msg.AudioCodecId() {
	case lalbase.RtmpSoundFormatAac:
		if len(msg.Payload) <= 2 || s.ascCtx == nil || s.audioID == 0 {
			return nil
		}
		buf := s.ascCtx.PackAdtsHeader(len(msg.Payload) - 2)
		buf = append(buf, msg.Payload[2:]...)
		return s.psMuxer.Write(s.audioID, buf, uint64(msg.Dts()), uint64(msg.Dts()))
	case lalbase.RtmpSoundFormatG711A, lalbase.RtmpSoundFormatG711U:
		if len(msg.Payload) <= 1 || s.audioID == 0 {
			return nil
		}
		return s.psMuxer.Write(s.audioID, msg.Payload[1:], uint64(msg.Dts()), uint64(msg.Dts()))
	default:
		return nil
	}
}

// nextSeq 生成下一个 RTP 序列号。
func (s *LowerPushSession) nextSeq() uint16 {
	s.seq++
	return s.seq
}

// packRtp 将一帧完整的 PS 数据按 RTP 最大负载长度切片，仅在最后一包设置 Marker。
func (s *LowerPushSession) packRtp(buf []byte, timestamp uint32) []rtprtcp.RtpPacket {
	var out []rtprtcp.RtpPacket
	for offset := 0; offset < len(buf); {
		size := len(buf) - offset
		mark := uint8(1)
		if size > lowerPushRtpPacketMax {
			size = lowerPushRtpPacketMax
			mark = 0
		}

		h := rtprtcp.MakeDefaultRtpHeader()
		h.Mark = mark
		h.PacketType = uint8(lalbase.AvPacketPtAvc)
		h.Seq = s.nextSeq()
		h.Timestamp = timestamp
		h.Ssrc = s.ssrc

		out = append(out, rtprtcp.MakeRtpPacket(h, buf[offset:offset+size]))
		offset += size
	}
	return out
}
