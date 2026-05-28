package rtc

import (
	"sync"
	"sync/atomic"

	"github.com/gofrs/uuid"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
	maxlogic "github.com/q191201771/lalmax/logic"
	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/lal/pkg/logic"
	"github.com/q191201771/naza/pkg/nazalog"
)

type whipSession struct {
	streamid       string
	pc             *peerConnection
	lalServer      logic.ILalServer
	lalSession     logic.ICustomizePubSessionContext
	group          *maxlogic.Group
	videoUnpacker  *UnPacker
	audioUnpacker  *UnPacker
	videoReceiver  *webrtc.RTPReceiver
	audioReceiver  *webrtc.RTPReceiver
	pktChan        chan base.AvPacket
	closeChan      chan bool
	subscriberId   string
	publisherID    string
	readBytes      atomic.Uint64
	remoteAddr     atomic.Value
	connectedOnce  sync.Once
	closeOnce      sync.Once
}

func NewWhipSession(streamid string, pc *peerConnection, lalServer logic.ILalServer) *whipSession {
	session, err := lalServer.AddCustomizePubSession(streamid)
	if err != nil {
		nazalog.Error(err)
		return nil
	}

	session.WithOption(func(option *base.AvPacketStreamOption) {
		option.VideoFormat = base.AvPacketStreamVideoFormatAnnexb
	})

	u, _ := uuid.NewV4()

	var group *maxlogic.Group
	if ok, g := maxlogic.GetGroupManagerInstance().GetGroupByStreamName(streamid); ok {
		group = g
	}

	return &whipSession{
		streamid:     streamid,
		pc:           pc,
		lalServer:    lalServer,
		lalSession:   session,
		group:        group,
		publisherID:  session.UniqueKey(),
		pktChan:      make(chan base.AvPacket, 100),
		closeChan:    make(chan bool, 2),
		subscriberId: u.String(),
	}
}

func (conn *whipSession) bindRegistry() {
	maxlogic.RegisterCustomizePub(conn.streamid, conn.publisherID, conn.subscriberId, conn.forceKick)
}

func (conn *whipSession) forceKick() {
	conn.cleanup()
	go conn.kick()
	conn.signalClose()
}

func (conn *whipSession) GetAnswerSDP(offer string) (sdp string) {
	gatherComplete := webrtc.GatheringCompletePromise(conn.pc.PeerConnection)

	conn.pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  string(offer),
	})

	answer, err := conn.pc.CreateAnswer(nil)
	if err != nil {
		nazalog.Error(err)
		return
	}

	err = conn.pc.SetLocalDescription(answer)
	if err != nil {
		nazalog.Error(err)
		return
	}

	<-gatherComplete

	sdp = conn.pc.LocalDescription().SDP
	return
}

func (conn *whipSession) Run() {

	conn.pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		nazalog.Info("peer connection state: ", state.String())

		switch state {
		case webrtc.PeerConnectionStateConnected:
			conn.registerPublisher()
		case webrtc.PeerConnectionStateDisconnected:
			fallthrough
		case webrtc.PeerConnectionStateFailed:
			fallthrough
		case webrtc.PeerConnectionStateClosed:
			conn.closeChan <- true
		}
	})

	if conn.pc.ConnectionState() == webrtc.PeerConnectionStateConnected {
		conn.registerPublisher()
	}

	var videoPt webrtc.PayloadType
	conn.pc.OnTrack(func(tr *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		switch tr.Kind() {
		case webrtc.RTPCodecTypeVideo:
			conn.videoReceiver = receiver
			conn.videoUnpacker = NewUnPacker(tr.Codec().MimeType, tr.Codec().ClockRate, conn.pktChan)
			videoPt = tr.PayloadType()
		case webrtc.RTPCodecTypeAudio:
			conn.audioReceiver = receiver
			mimeType := tr.Codec().MimeType
			if tr.Codec().MimeType == "" {
				// pt为0或者8按照G711U和G711A处理,提高兼容性
				if tr.PayloadType() == 0 {
					mimeType = webrtc.MimeTypePCMU
				} else if tr.PayloadType() == 8 {
					mimeType = webrtc.MimeTypePCMA
				}
			}
			conn.audioUnpacker = NewUnPacker(mimeType, tr.Codec().ClockRate, conn.pktChan)
		}

		for {
			pkt, _, err := tr.ReadRTP()
			if err != nil {
				if err.Error() != "EOF" {
					nazalog.Error(err)
				}
				return
			}

			conn.trackReadBytes(pkt)

			if conn.videoUnpacker != nil && pkt.Header.PayloadType == uint8(videoPt) {
				conn.videoUnpacker.UnPack(pkt)
			} else if conn.audioUnpacker != nil {
				conn.audioUnpacker.UnPack(pkt)
			}
		}
	})

	for {
		select {
		case <-conn.closeChan:
			nazalog.Info("whip connect close, streamid:", conn.streamid)
			conn.cleanup()
			return
		case pkt := <-conn.pktChan:
			if conn.lalSession == nil {
				continue
			}
			conn.lalSession.FeedAvPacket(pkt)
		}
	}
}

func (conn *whipSession) Close() {
	conn.forceKick()
}

func (conn *whipSession) kick() {
	if conn.pc != nil {
		_ = conn.pc.Close()
	}
}

func (conn *whipSession) signalClose() {
	select {
	case conn.closeChan <- true:
	default:
	}
}

func (conn *whipSession) cleanup() {
	conn.closeOnce.Do(func() {
		conn.unregisterPublisher()
		maxlogic.UnregisterCustomizePub(conn.publisherID, conn.subscriberId)
		if conn.lalServer != nil && conn.lalSession != nil {
			conn.lalServer.DelCustomizePubSession(conn.lalSession)
			conn.lalSession = nil
		}
	})
}

func (conn *whipSession) registerPublisher() {
	conn.connectedOnce.Do(func() {
		if conn.group == nil {
			return
		}
		conn.refreshRemoteAddr()
		conn.group.AddPublisher(maxlogic.PublisherInfo{
			PublisherID: conn.publisherID,
			Protocol:    maxlogic.PublisherProtocolWHIP,
			RemoteAddr:  conn.loadRemoteAddr(),
		}, conn)
	})
}

func (conn *whipSession) unregisterPublisher() {
	if conn.group == nil || conn.publisherID == "" {
		return
	}
	conn.group.RemovePublisher(conn.publisherID)
}

func (conn *whipSession) GetPublisherStat() maxlogic.PublisherStat {
	conn.refreshRemoteAddr()
	return maxlogic.PublisherStat{
		RemoteAddr:   conn.loadRemoteAddr(),
		ReadBytesSum: conn.readBytes.Load(),
	}
}

func (conn *whipSession) trackReadBytes(pkt *rtp.Packet) {
	if pkt == nil {
		return
	}
	conn.readBytes.Add(uint64(pkt.MarshalSize()))
}

func (conn *whipSession) refreshRemoteAddr() {
	if remoteAddr := conn.currentRemoteAddr(); remoteAddr != "" {
		conn.remoteAddr.Store(remoteAddr)
	}
}

func (conn *whipSession) currentRemoteAddr() string {
	if conn.videoReceiver != nil {
		if remoteAddr := remoteAddrFromDTLSTransport(conn.videoReceiver.Transport()); remoteAddr != "" {
			return remoteAddr
		}
	}
	if conn.audioReceiver != nil {
		if remoteAddr := remoteAddrFromDTLSTransport(conn.audioReceiver.Transport()); remoteAddr != "" {
			return remoteAddr
		}
	}
	if sctp := conn.pc.SCTP(); sctp != nil {
		return remoteAddrFromDTLSTransport(sctp.Transport())
	}
	return ""
}

func (conn *whipSession) loadRemoteAddr() string {
	if v := conn.remoteAddr.Load(); v != nil {
		if remoteAddr, ok := v.(string); ok {
			return remoteAddr
		}
	}
	return ""
}
