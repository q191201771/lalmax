package gb28181

import (
	"github.com/gin-gonic/gin"
	"sync"
)

type GbLogic struct {
	s *GB28181Server
}

var gbLogic *GbLogic
var once sync.Once

func NewGbLogic(s *GB28181Server) *GbLogic {
	once.Do(func() {
		gbLogic = &GbLogic{
			s: s,
		}
	})
	return gbLogic
}

func (g *GbLogic) GetDeviceInfos(c *gin.Context) {
	deviceInfos := g.s.getDeviceInfos()
	ResponseSuccess(c, deviceInfos)
}
func (g *GbLogic) StartPlay(c *gin.Context) {
	var reqPlay ReqPlay
	if err := c.ShouldBindJSON(&reqPlay); err != nil {
		ResponseErrorWithMsg(c, CodeInvalidParam, CodeInvalidParam.Msg())
	} else {
		ch := g.s.FindChannel(reqPlay.DeviceId, reqPlay.ChannelId)
		if ch == nil {
			ResponseErrorWithMsg(c, CodeDeviceNotRegister, CodeDeviceNotRegister.Msg())

		} else {
			streamName := reqPlay.StreamName
			if len(streamName) == 0 {
				streamName = reqPlay.ChannelId
			}
			ch.TryAutoInvite(&InviteOptions{}, g.s.conf, streamName)
			respPlay := &RespPlay{
				StreamName: streamName,
			}
			ResponseSuccess(c, respPlay)
		}
	}

}
func (g *GbLogic) StopPlay(c *gin.Context) {
	var reqStop ReqStop
	if err := c.ShouldBindJSON(&reqStop); err != nil {
		ResponseErrorWithMsg(c, CodeInvalidParam, CodeInvalidParam.Msg())
	} else {
		ch := g.s.FindChannel(reqStop.DeviceId, reqStop.ChannelId)
		if ch == nil {
			ResponseErrorWithMsg(c, CodeDeviceNotRegister, CodeDeviceNotRegister.Msg())

		} else {
			streamName := reqStop.StreamName
			if len(streamName) == 0 {
				streamName = reqStop.ChannelId
			}
			ch.Bye(streamName)
			ResponseSuccess(c, nil)
		}
	}

}
func (g *GbLogic) UpdateAllNotify(c *gin.Context) {
	g.s.GetAllSyncChannels()
	ResponseSuccess(c, nil)
}
func (g *GbLogic) UpdateNotify(c *gin.Context) {
	var reqUpdateNotify ReqUpdateNotify
	if err := c.ShouldBindJSON(&reqUpdateNotify); err != nil {
		ResponseErrorWithMsg(c, CodeInvalidParam, CodeInvalidParam.Msg())
	} else {
		g.s.GetSyncChannels(reqUpdateNotify.DeviceId)
		ResponseSuccess(c, nil)
	}

}
