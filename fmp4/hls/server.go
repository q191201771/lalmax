package hls

import (
	"net/http"
	"strings"
	"sync"
	"time"

	config "lalmax/conf"

	"github.com/gin-gonic/gin"
	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/naza/pkg/nazalog"
)

type HlsServer struct {
	sessions        sync.Map
	conf            config.HlsConfig
	invalidSessions sync.Map
}

func NewHlsServer(conf config.HlsConfig) *HlsServer {
	svr := &HlsServer{
		conf: conf,
	}

	go svr.cleanInvalidSession()

	return svr
}

func (s *HlsServer) NewHlsSession(streamName string) {
	nazalog.Info("new hls session, streamName:", streamName)
	session := NewHlsSession(streamName, s.conf)
	s.sessions.Store(streamName, session)
}

func (s *HlsServer) OnMsg(streamName string, msg base.RtmpMsg) {
	value, ok := s.sessions.Load(streamName)
	if ok {
		session := value.(*HlsSession)
		session.OnMsg(msg)
	}
}

func (s *HlsServer) OnStop(streamName string) {
	value, ok := s.sessions.Load(streamName)
	if ok {
		session := value.(*HlsSession)
		s.invalidSessions.Store(session.SessionId, session)
		s.sessions.Delete(session)
	}
}

func (s *HlsServer) HandleRequest(ctx *gin.Context) {
	path := ctx.Request.URL.Path
	path = strings.TrimLeft(path, "/live/hls/")

	params := strings.Split(path, "/")
	if len(params) == 1 {
		ctx.Status(http.StatusFound)
		return
	}

	streamName := params[0]
	value, ok := s.sessions.Load(streamName)
	if ok {
		session := value.(*HlsSession)
		session.HandleRequest(ctx)
	}
}

func (s *HlsServer) cleanInvalidSession() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.invalidSessions.Range(func(k, v interface{}) bool {
			session := v.(*HlsSession)
			nazalog.Info("clean invalid session, streamName:", session.streamName, " sessionId:", k)
			session.OnStop()
			s.invalidSessions.Delete(k)
			return true
		})
	}
}
