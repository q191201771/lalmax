package srt

// #cgo LDFLAGS: -lsrt
// #include <srt/srt.h>
import "C"
import (
	"context"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/haivision/srtgo"
	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/lal/pkg/logic"
	"github.com/q191201771/naza/pkg/nazalog"

	config "lalmax/conf"
)

type SrtServer struct {
	host       string
	port       uint16
	lalServer  logic.ILalServer
	mu         sync.Mutex
	publishers map[string]*Publisher
}

func NewSrtServer(conf config.SrtConfig, lal logic.ILalServer) *SrtServer {
	svr := &SrtServer{
		host:       conf.Host,
		port:       conf.Port,
		lalServer:  lal,
		publishers: make(map[string]*Publisher),
	}

	nazalog.Infof("create srt server. host:%s, port:%d", svr.host, svr.port)
	return svr
}

func (s *SrtServer) Run(ctx context.Context) {
	options := make(map[string]string)
	options["transtype"] = "live"

	sck := srtgo.NewSrtSocket(s.host, s.port, options)
	defer sck.Close()

	sck.SetSockOptInt(srtgo.SRTO_LOSSMAXTTL, srtgo.SRTO_LOSSMAXTTL)
	sck.SetListenCallback(s.listenCallback)
	if err := sck.Listen(1); err != nil {
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
		err error
		//pub      *Publisher
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

	switch streamid.Mode {
	case "publish", "PUBLISH":
		// make a new publisher
		publisher := NewPublisher(ctx, streamid.Host, streamid.User, socket, s)

		session, err := s.lalServer.AddCustomizePubSession(streamid.Host)
		if err != nil {
			nazalog.Error(err)
		}

		if session != nil {
			session.WithOption(func(option *base.AvPacketStreamOption) {
				option.VideoFormat = base.AvPacketStreamVideoFormatAnnexb
			})
		}

		publisher.SetSession(session)
		s.Register(streamid.Host, publisher)
		go publisher.Run()
	case "play", "PLAY", "subscribe", "SUBSCRIBE":
		// make a new subscriber
		subscriber := NewSubscriber(ctx, socket)
		pub, ok := s.publishers[streamid.Host]
		if !ok || pub == nil {
			nazalog.Error(err)
			return
		}
		pub.subscribers = append(pub.subscribers, subscriber)
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
	if id.Host == "" {
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

func (s *SrtServer) Register(host string, publisher *Publisher) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.publishers[host] = publisher
}

func (s *SrtServer) Remove(host string, ss logic.ICustomizePubSessionContext) {
	s.mu.Lock()
	defer s.mu.Unlock()

	time.Sleep(5 * time.Second)
	s.lalServer.DelCustomizePubSession(ss)
	if _, ok := s.publishers[host]; ok {
		delete(s.publishers, host)
	}
}
