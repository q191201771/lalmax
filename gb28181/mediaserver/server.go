package mediaserver

import (
	"net"

	"github.com/q191201771/lal/pkg/logic"
	"github.com/q191201771/naza/pkg/nazalog"

	udpTransport "github.com/pion/transport/v3/udp"
)

type GB28181MediaServer struct {
	tcpListenAddr string
	udpListenAddr string
	lalServer     logic.ILalServer

	CheckSsrcFunc   func(ssrc uint32) (string, bool)
	NotifyCloseFunc func(streamName string)
}

func NewGB28181MediaServer(tcpaddr, udpaddr string, lal logic.ILalServer) *GB28181MediaServer {
	return &GB28181MediaServer{
		tcpListenAddr: tcpaddr,
		udpListenAddr: udpaddr,
		lalServer:     lal,
	}
}

func (s *GB28181MediaServer) Start() (err error) {
	go func() {
		listener, err := net.Listen("tcp", s.tcpListenAddr)
		if err != nil {
			nazalog.Error("gb28181 media server tcp listen failed,err:", err)
			return
		}

		nazalog.Info("gb28181 media server tcp listen success, addr:", s.tcpListenAddr)

		for {
			conn, err := listener.Accept()
			if err != nil {
				nazalog.Error("gb28181 media server tcp accept failed, err:", err)
				continue
			}

			c := NewConn(conn, s.lalServer)
			c.CheckSsrc = s.CheckSsrc
			c.NotifyClose = s.NotifyClose
			go c.Serve()
		}
	}()

	go func() {
		addr, err := net.ResolveUDPAddr("udp", s.udpListenAddr)
		if err != nil {
			nazalog.Error("gb28181 media server udp listen failed,err:", err)
			return
		}

		listener, err := udpTransport.Listen("udp", addr)
		if err != nil {
			nazalog.Error("gb28181 media server udp listen failed,err:", err)
			return
		}

		nazalog.Info("gb28181 media server udp listen success, addr:", s.udpListenAddr)

		for {
			conn, err := listener.Accept()
			if err != nil {
				continue
			}

			c := NewConn(conn, s.lalServer)
			c.CheckSsrc = s.CheckSsrc
			c.NotifyClose = s.NotifyClose
			go c.Serve()
		}

	}()

	return
}

func (s *GB28181MediaServer) CheckSsrc(ssrc uint32) (string, bool) {
	if s.CheckSsrcFunc != nil {
		return s.CheckSsrcFunc(ssrc)
	}

	return "", false
}

func (s *GB28181MediaServer) NotifyClose(streamName string) {
	if s.NotifyCloseFunc != nil {
		s.NotifyCloseFunc(streamName)
	}

}
