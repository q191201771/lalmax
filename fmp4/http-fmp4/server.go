package httpfmp4

import (
	"strings"

	"github.com/gin-gonic/gin"
)

type HttpFmp4Server struct {
}

func NewHttpFmp4Server() *HttpFmp4Server {
	svr := &HttpFmp4Server{}

	return svr
}

func (s *HttpFmp4Server) HandleRequest(c *gin.Context) {
	path := c.Request.URL.Path
	streamid := strings.TrimLeft(path, "/live/m4s/")

	session := NewHttpFmp4Session(streamid)
	session.handleSession(c)
}
