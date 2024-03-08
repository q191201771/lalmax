package gb28181

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type DeviceInfos struct {
	DeviceItems []*DeviceItem `json:"device_items"`
}
type DeviceItem struct {
	DeviceId string         `json:"device_id"` //父目录Id
	Channels []*ChannelItem `json:"channels"`
}
type ChannelItem struct {
	ChannelId    string        `json:"channel_id"`   // channel id
	Name         string        `json:"name"`         //设备名称
	Manufacturer string        `json:"manufacturer"` //制造厂商
	Owner        string        `json:"owner"`        //设备归属
	CivilCode    string        `json:"civilCode"`    //行政区划编码
	Address      string        `json:"address"`      //地址
	Status       ChannelStatus `json:"status"`       // 状态  on 在线 off离线
	Longitude    string        `json:"longitude"`    // 经度
	Latitude     string        `json:"latitude"`     // 纬度
	StreamName   string        `json:"-"`
}
type PlayInfo struct {
	NetWork    string `json:"network" form:"network" url:"network"`             // 媒体传输类型,tcp/udp,默认udp
	DeviceId   string `json:"device_id" form:"device_id" url:"device_id"`       // 设备 Id
	ChannelId  string `json:"channel_id" form:"channel_id" url:"channel_id"`    // channel id
	StreamName string `json:"stream_name" form:"stream_name" url:"stream_name"` // ssrc对应的流名
}
type ReqPlay struct {
	PlayInfo
}
type RespPlay struct {
	StreamName string `json:"stream_name" form:"stream_name" url:"stream_name"`
}
type ReqStop struct {
	PlayInfo
}
type ReqUpdateNotify struct {
	DeviceId string `json:"device_id" form:"device_id" url:"device_id"` //设备 Id
}

func ResponseErrorWithMsg(c *gin.Context, code ResCode, msg interface{}) {
	c.JSON(http.StatusOK, &ResponseData{
		Code: code,
		Msg:  msg,
		Data: nil,
	})
}

func ResponseSuccess(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, &ResponseData{
		Code: CodeSuccess,
		Msg:  CodeSuccess.Msg(),
		Data: data,
	})
}

type ResCode int64

const (
	CodeSuccess ResCode = 1000 + iota
	CodeInvalidParam
	CodeServerBusy
	CodeDeviceNotRegister
	CodeDeviceStopError
)

var codeMsgMap = map[ResCode]string{
	CodeSuccess:           "success",
	CodeInvalidParam:      "请求参数错误",
	CodeServerBusy:        "服务繁忙",
	CodeDeviceNotRegister: "设备暂时未注册",
	CodeDeviceStopError:   "设备停止播放错误",
}

func (c ResCode) Msg() string {
	msg, ok := codeMsgMap[c]
	if !ok {
		msg = codeMsgMap[CodeServerBusy]
	}
	return msg
}

type ResponseData struct {
	Code ResCode     `json:"code"`
	Msg  interface{} `json:"msg"`
	Data interface{} `json:"data,omitempty"`
}
