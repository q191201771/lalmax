package mediaserver

import (
	"errors"
	"net"
	"sync"
	"time"

	"github.com/q191201771/lal/pkg/logic"
	"github.com/q191201771/naza/pkg/nazalog"
)

type IGbObserver interface {
	CheckSsrc(ssrc uint32) (*MediaInfo, bool)
	NotifyClose(streamName string)
}

type GB28181MediaServer struct {
	listenPort int
	lalServer  logic.ILalServer

	listener net.Listener

	disposeOnce sync.Once
	observer    IGbObserver
}

func NewGB28181MediaServer(listenPort int, observer IGbObserver, lal logic.ILalServer) *GB28181MediaServer {
	return &GB28181MediaServer{
		listenPort: listenPort,
		lalServer:  lal,
		observer:   observer,
	}
}
func (s *GB28181MediaServer) GetListenerPort() uint16 {
	return uint16(s.listenPort)
}
func (s *GB28181MediaServer) Start(listener net.Listener) (err error) {
	s.listener = listener
	if s.listener != nil {
		go func() {
			for {
				if s.listener == nil {
					return
				}
				conn, err := s.listener.Accept()
				if err != nil {
					var ne net.Error
					if ok := errors.As(err, &ne); ok && ne.Timeout() {
						nazalog.Error("Accept failed: timeout error, retrying...")
						time.Sleep(time.Second / 20)
					} else {
						break
					}
				}

				c := NewConn(conn, s.observer, s.lalServer)
				go c.Serve()
			}
		}()
	}
	return
}
func (s *GB28181MediaServer) Dispose() {
	s.disposeOnce.Do(func() {
		if s.listener != nil {
			s.listener.Close()
			s.listener = nil
		}
	})
}
