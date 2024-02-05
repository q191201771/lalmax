package gb28181

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

type DeviceInfos struct {
	DeviceItems []*DeviceItem
}
type DeviceItem struct {
	ParentID   string     `json:"parent_id"` //父目录Id
	DeviceInfo DeviceInfo `json:"device_info"`
}
type DeviceInfo struct {
	DeviceID     string        `json:"device_id"`    // 设备id
	Name         string        `json:"name"`         //设备名称
	Manufacturer string        `json:"manufacturer"` //制造厂商
	Owner        string        `json:"owner"`        //设备归属
	CivilCode    string        `json:"civilCode"`    //行政区划编码
	Address      string        `json:"address"`      //地址
	Status       ChannelStatus `json:"status"`       // 状态  on 在线 off离线
	Longitude    string        `json:"longitude"`    // 经度
	Latitude     string        `json:"latitude"`     // 纬度
	StreamName   string        `json:"streamName"`
}
type PlayInfo struct {
	ParentID   string `json:"parent_id" form:"parent_id" url:"parent_id"` //父目录Id
	DeviceID   string `json:"device_id" form:"device_id" url:"device_id"` // 设备id
	StreamName string `json:"stream_name" form:"stream_name" url:"stream_name"`
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
	ParentID string `json:"parent_id" form:"parent_id" url:"parent_id"` //父目录Id
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
)

var codeMsgMap = map[ResCode]string{
	CodeSuccess:           "success",
	CodeInvalidParam:      "请求参数错误",
	CodeServerBusy:        "服务繁忙",
	CodeDeviceNotRegister: "设备暂时未注册",
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
