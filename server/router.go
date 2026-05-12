package server

import "github.com/gin-gonic/gin"

func (s *LalMaxServer) InitRouter(router *gin.Engine) {
	if router == nil {
		return
	}
	router.Use(s.Cors())

	s.initRtcRouter(router)
	s.initFmp4Router(router)

	auth := Authentication(s.conf.HttpConfig.CtrlAuthWhitelist.Secrets, s.conf.HttpConfig.CtrlAuthWhitelist.IPs)
	s.initHookRouter(router, auth)
	s.initStatRouter(router, auth)
	s.initCtrlRouter(router, auth)
	s.initZlmCompatRouter(router, auth)

	s.initFlvProxy(router)
}
