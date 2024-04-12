package server

import (
	"lalmax/gb28181"
	"lalmax/hook"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/q191201771/lal/pkg/base"
	"github.com/q191201771/naza/pkg/nazalog"
)

func (s *LalMaxServer) InitRouter(router *gin.Engine) {
	if router != nil {
		router.Use(s.Cors())
		// whip
		router.POST("/whip", s.HandleWHIP)
		router.OPTIONS("/whip", s.HandleWHIP)
		router.DELETE("/whip", s.HandleWHIP)

		// whep
		router.POST("/whep", s.HandleWHEP)
		router.OPTIONS("/whep", s.HandleWHEP)
		router.DELETE("/whep", s.HandleWHEP)

		// http-fmp4
		router.GET("/live/m4s/:streamid", s.HandleHttpFmp4)

		// hls-fmp4/llhls
		router.GET("/live/hls/:streamid/:type", s.HandleHls)

		// onvif
		router.POST("/api/ctrl/onvif/pull", s.HandleOnvifPull)

		// gb
		gbLogic := gb28181.NewGbLogic(s.gbsbr)
		router.GET("/api/gb/device_infos", gbLogic.GetDeviceInfos)
		router.POST("/api/gb/start_play", gbLogic.StartPlay)
		router.POST("/api/gb/stop_play", gbLogic.StopPlay)
		router.POST("/api/gb/update_all_notify", gbLogic.UpdateAllNotify)
		router.POST("/api/gb/update_notify", gbLogic.UpdateNotify)

		// stat
		router.GET("/api/stat/group", s.statGroupHandler)
		router.GET("/api/stat/all_group", s.statAllGroupHandler)
		router.GET("/api/stat/lal_info", s.statLalInfoHandler)

	}
}
func (s *LalMaxServer) Cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		method := c.Request.Method
		c.Header("Access-Control-Allow-Origin", "*")
		//服务器支持的所有跨域请求的方法
		c.Header("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE,UPDATE")
		//允许跨域设置可以返回其他子段，可以自定义字段
		c.Header("Access-Control-Allow-Headers", "*")
		// 允许浏览器（客户端）可以解析的头部 （重要）
		c.Header("Access-Control-Expose-Headers", "Content-Length, Access-Control-Allow-Origin, Access-Control-Allow-Headers")
		//设置缓存时间
		c.Header("Access-Control-Max-Age", "172800")
		//允许客户端传递校验信息比如 cookie (重要)
		c.Header("Access-Control-Allow-Credentials", "true")

		//允许类型校验
		if method == "OPTIONS" {
			c.Status(http.StatusOK)
		}
		c.Next()
	}
}
func (s *LalMaxServer) HandleWHIP(c *gin.Context) {
	switch c.Request.Method {
	case "POST":
		if s.rtcsvr != nil {
			s.rtcsvr.HandleWHIP(c)
		}
	case "DELETE":
		// TODO 实现DELETE
		c.Status(http.StatusOK)
	}
}

func (s *LalMaxServer) HandleWHEP(c *gin.Context) {
	switch c.Request.Method {
	case "POST":
		if s.rtcsvr != nil {
			s.rtcsvr.HandleWHEP(c)
		}
	case "DELETE":
		// TODO 实现DELETE
		c.Status(http.StatusOK)
	}
}

func (s *LalMaxServer) HandleHls(c *gin.Context) {
	if s.hlssvr != nil {
		s.hlssvr.HandleRequest(c)
	} else {
		nazalog.Error("hls is disable")
		c.Status(http.StatusNotFound)
	}
}

func (s *LalMaxServer) HandleHttpFmp4(c *gin.Context) {
	if s.httpfmp4svr != nil {
		s.httpfmp4svr.HandleRequest(c)
	} else {
		nazalog.Error("http-fmp4 is disable")
		c.Status(http.StatusNotFound)
	}
}

func (s *LalMaxServer) HandleOnvifPull(c *gin.Context) {
	if s.onvifsvr != nil {
		s.onvifsvr.HandlePull(c)
	}
}

func (s *LalMaxServer) statGroupHandler(c *gin.Context) {
	var v base.ApiStatGroupResp
	streamName := c.Query("stream_name")
	if streamName == "" {
		v.ErrorCode = base.ErrorCodeParamMissing
		v.Desp = base.DespParamMissing
		c.JSON(http.StatusOK, v)
		return
	}
	v.Data = s.lalsvr.StatGroup(streamName)
	if v.Data == nil {
		v.ErrorCode = base.ErrorCodeGroupNotFound
		v.Desp = base.DespGroupNotFound
		c.JSON(http.StatusOK, v)
		return
	}
	exist, session := hook.GetHookSessionManagerInstance().GetHookSession(streamName)
	if exist {
		v.Data.StatSubs = append(v.Data.StatSubs, session.GetAllConsumer()...)
	}
	v.ErrorCode = base.ErrorCodeSucc
	v.Desp = base.DespSucc
	c.JSON(http.StatusOK, v)
}

func (s *LalMaxServer) statAllGroupHandler(c *gin.Context) {
	var out base.ApiStatAllGroupResp
	out.ErrorCode = base.ErrorCodeSucc
	out.Desp = base.DespSucc
	groups := s.lalsvr.StatAllGroup()
	for i, group := range groups {
		exist, session := hook.GetHookSessionManagerInstance().GetHookSession(group.StreamName)
		if exist {
			groups[i].StatSubs = append(groups[i].StatSubs, session.GetAllConsumer()...)
		}
	}
	out.Data.Groups = groups
	c.JSON(http.StatusOK, out)
}

func (s *LalMaxServer) statLalInfoHandler(c *gin.Context) {
	var v base.ApiStatLalInfoResp
	v.ErrorCode = base.ErrorCodeSucc
	v.Desp = base.DespSucc
	v.Data = s.lalsvr.StatLalInfo()
	c.JSON(http.StatusOK, v)
}
