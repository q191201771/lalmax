package srt

// #cgo LDFLAGS: -lsrt
// #include <srt/srt.h>
import "C"
import (
	"context"
	"net"
	"strconv"
	"strings"

	"github.com/haivision/srtgo"
	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/lal/pkg/logic"
	"github.com/q191201771/naza/pkg/nazalog"
)

type SrtServer struct {
	host      string
	port      uint16
	lalServer logic.ILalServer
	srtOpt    SrtOption
}
type SrtOption struct {
	Latency           int
	RecvLatency       int
	PeerLatency       int
	TlpktDrop         int
	TsbpdMode         int
	RecvBuf           int
	SendBuf           int
	MaxSendPacketSize int
}

var defaultSrtOption = SrtOption{
	Latency:           300,
	RecvLatency:       300,
	PeerLatency:       300,
	TlpktDrop:         1,
	TsbpdMode:         1,
	RecvBuf:           2 * 1024 * 1024,
	SendBuf:           2 * 1024 * 1024,
	MaxSendPacketSize: 4,
}

type ModSrtOption func(option *SrtOption)

func NewSrtServer(host string, port uint16, lal logic.ILalServer, modOptions ...ModSrtOption) *SrtServer {
	opt := defaultSrtOption
	for _, fn := range modOptions {
		fn(&opt)
	}
	svr := &SrtServer{
		host:      host,
		port:      port,
		lalServer: lal,
		srtOpt:    opt,
	}

	nazalog.Infof("create srt server. host:%s, port:%d", svr.host, svr.port)
	return svr
}

func (s *SrtServer) Run(ctx context.Context) {
	options := make(map[string]string)
	options["blocking"] = "0"
	options["transtype"] = "live"
	options["latency"] = strconv.Itoa(s.srtOpt.Latency)
	options["rcvlatency"] = strconv.Itoa(s.srtOpt.RecvLatency)
	options["peerlatency"] = strconv.Itoa(s.srtOpt.PeerLatency)
	options["tlpktdrop"] = strconv.Itoa(s.srtOpt.TlpktDrop)
	options["tsbpdmode"] = strconv.Itoa(s.srtOpt.TsbpdMode)
	options["sndbuf"] = strconv.Itoa(s.srtOpt.SendBuf)
	options["rcvbuf"] = strconv.Itoa(s.srtOpt.RecvBuf)

	sck := srtgo.NewSrtSocket(s.host, s.port, options)
	defer sck.Close()

	sck.SetSockOptInt(srtgo.SRTO_LOSSMAXTTL, srtgo.SRTO_LOSSMAXTTL)
	sck.SetListenCallback(s.listenCallback)
	if err := sck.Listen(128); err != nil {
		panic(err)
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:

		}
		socket, addr, err := sck.Accept()
		if err != nil {
			nazalog.Error(err)
		}

		go s.Handle(ctx, socket, addr)
	}
}

func (s *SrtServer) Handle(ctx context.Context, socket *srtgo.SrtSocket, addr *net.UDPAddr) {
	var (
		err      error
		streamid *StreamID
	)

	idString, err := socket.GetSockOptString(C.SRTO_STREAMID)
	if err != nil {
		nazalog.Error(err)
		return
	}

	if streamid, err = parseStreamID(idString); err != nil {
		nazalog.Error(err)
		return
	}

	// https://github.com/Haivision/srt/blob/master/docs/features/access-control.md
	switch streamid.Mode {
	case "publish", "PUBLISH":
		// make a new publisher
		publisher := NewPublisher(ctx, streamid.Resource, socket, s)
		session, err := s.lalServer.AddCustomizePubSession(streamid.Resource)
		if err != nil {
			nazalog.Error(err)
		}

		if session != nil {
			session.WithOption(func(option *base.AvPacketStreamOption) {
				option.VideoFormat = base.AvPacketStreamVideoFormatAnnexb
			})
		}

		publisher.SetSession(session)
		publisher.Run()
	case "request", "REQUEST":
		// make a new subscriber
		subscriber := NewSubscriber(ctx, socket, streamid.Resource, s.srtOpt.MaxSendPacketSize)
		subscriber.Run()
	default:
		return
	}
}

func (s *SrtServer) listenCallback(socket *srtgo.SrtSocket, version int, addr *net.UDPAddr, streamid string) bool {
	nazalog.Infof("socket will connect, hsVersion: %d, streamid: %s\n", version, streamid)

	if !strings.Contains(streamid, "#!::") {
		socket.SetRejectReason(srtgo.RejectionReasonBadRequest)
		return false
	}

	id, err := parseStreamID(streamid)
	if err != nil {
		socket.SetRejectReason(srtgo.RejectionReasonBadRequest)
		return false
	}
	if id.Resource == "" {
		socket.SetRejectReason(srtgo.RejectionReasonBadRequest)
		return false
	}
	// check the other stream parameters

	if id.Mode == "" {
		socket.SetRejectReason(srtgo.RejectionReasonBadRequest)
		return false
	}

	return true
}

func (s *SrtServer) Remove(host string, ss logic.ICustomizePubSessionContext) {
	s.lalServer.DelCustomizePubSession(ss)
}
