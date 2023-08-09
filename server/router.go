package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/q191201771/naza/pkg/nazalog"
)

func (s *LalMaxServer) InitRouter() {
	if s.router != nil {
		// whip
		s.router.POST("/whip", s.HandleWHIP)
		s.router.OPTIONS("/whip", s.HandleWHIP)

		// whep
		s.router.POST("/whep", s.HandleWHEP)
		s.router.OPTIONS("/whep", s.HandleWHEP)

		// http-fmp4/hls/dash
		s.router.GET("/live/m4s/:streamid", s.HandleHttpM4s)
	}
}

func (s *LalMaxServer) HandleWHIP(c *gin.Context) {
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Headers", "*")
	c.Header("Access-Control-Allow-Methods", "*")
	c.Header("Connection", "Close")

	switch c.Request.Method {
	case "OPTIONS":
		c.Status(http.StatusOK)
	case "POST":
		if s.rtcsvr != nil {
			s.rtcsvr.HandleWHIP(c)
		}
	}
}

func (s *LalMaxServer) HandleWHEP(c *gin.Context) {
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Headers", "*")
	c.Header("Access-Control-Allow-Methods", "*")
	c.Header("Connection", "Close")

	switch c.Request.Method {
	case "OPTIONS":
		c.Status(http.StatusOK)
	case "POST":
		if s.rtcsvr != nil {
			s.rtcsvr.HandleWHEP(c)
		}
	}
}

func (s *LalMaxServer) HandleHttpM4s(c *gin.Context) {
	if strings.HasSuffix(c.Request.URL.Path, ".m3u8") {
		s.handleM3u8(c)
	} else if strings.HasSuffix(c.Request.URL.Path, ".mpd") {
		s.handleDash(c)
	} else if strings.HasSuffix(c.Request.URL.Path, ".mp4") {
		s.handleHttpFmp4(c)
	} else {
		c.Status(http.StatusBadRequest)
		return
	}
}

func (s *LalMaxServer) handleHttpFmp4(c *gin.Context) {
	if s.httpfmp4svr != nil {
		s.httpfmp4svr.HandleRequest(c)
	}
}

func (s *LalMaxServer) handleM3u8(c *gin.Context) {
	// TODO 支持hls-fmp4/llhls
	nazalog.Info("handle m3u8")
	c.Status(http.StatusOK)
}

func (s *LalMaxServer) handleDash(c *gin.Context) {
	// TODO 支持dash
	nazalog.Info("handle dash")
	c.Status(http.StatusOK)
}
