package server

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"

	config "github.com/q191201771/lalmax/config"
)

// lalRawPorts 从 LalRawContent 中提取 lal 的端口配置
type lalRawPorts struct {
	Rtmp struct {
		Addr     string `json:"addr"`
		SslAddr  string `json:"rtmps_addr"`
	} `json:"rtmp"`
	Rtsp struct {
		Addr     string `json:"addr"`
		SslAddr  string `json:"rtsps_addr"`
	} `json:"rtsp"`
}

// buildZlmServerConfig 将 lalmax 配置转换为 ZLM getServerConfig 响应格式
// 为什么：owl 的 ZLMDriver.Connect 依赖 data[0] 中的 http.port / rtmp.port 等字段来更新端口信息
func buildZlmServerConfig(conf *config.Config) map[string]any {
	cfg := make(map[string]any)

	cfg["general.mediaServerId"] = conf.ServerId

	// 从 lal raw config 中提取 rtmp/rtsp 端口
	var lalPorts lalRawPorts
	if len(conf.LalRawContent) > 0 {
		_ = json.Unmarshal(conf.LalRawContent, &lalPorts)
	}
	cfg["rtmp.port"] = extractPort(lalPorts.Rtmp.Addr)
	cfg["rtmp.sslport"] = extractPort(lalPorts.Rtmp.SslAddr)
	cfg["rtsp.port"] = extractPort(lalPorts.Rtsp.Addr)
	cfg["rtsp.sslport"] = extractPort(lalPorts.Rtsp.SslAddr)
	cfg["http.port"] = extractPort(conf.HttpConfig.ListenAddr)
	cfg["http.sslport"] = extractPort(conf.HttpConfig.HttpsListenAddr)

	// rtp_proxy 端口从 gb28181 配置获取
	cfg["rtp_proxy.port"] = strconv.Itoa(int(conf.GB28181Config.MediaConfig.ListenPort))
	rtpBase := int(conf.GB28181Config.MediaConfig.ListenPort)
	rtpMax := rtpBase + int(conf.GB28181Config.MediaConfig.MultiPortMaxIncrement)
	if rtpBase > 0 && rtpMax > rtpBase {
		cfg["rtp_proxy.port_range"] = fmt.Sprintf("%d-%d", rtpBase+1, rtpMax)
	} else {
		cfg["rtp_proxy.port_range"] = "30000-35000"
	}

	// --- RTC 配置 ---
	if conf.RtcConfig.Enable {
		cfg["rtc.port"] = strconv.Itoa(conf.RtcConfig.ICEUDPMuxPort)
		cfg["rtc.tcpPort"] = strconv.Itoa(conf.RtcConfig.ICETCPMuxPort)
	} else {
		cfg["rtc.port"] = "0"
		cfg["rtc.tcpPort"] = "0"
	}

	// --- Hook 配置 ---
	cfg["hook.enable"] = boolStr(conf.HttpNotifyConfig.Enable)
	cfg["hook.alive_interval"] = strconv.Itoa(conf.HttpNotifyConfig.KeepaliveIntervalSec)
	cfg["hook.on_stream_changed"] = conf.HttpNotifyConfig.ZlmOnStreamChanged
	cfg["hook.on_server_keepalive"] = conf.HttpNotifyConfig.ZlmOnServerKeepalive
	cfg["hook.on_stream_none_reader"] = conf.HttpNotifyConfig.ZlmOnStreamNoneReader
	cfg["hook.on_rtp_server_timeout"] = conf.HttpNotifyConfig.ZlmOnRtpServerTimeout
	cfg["hook.on_record_mp4"] = conf.HttpNotifyConfig.ZlmOnRecordMp4
	cfg["hook.on_server_started"] = conf.HttpNotifyConfig.ZlmOnServerStarted
	cfg["hook.on_publish"] = conf.HttpNotifyConfig.ZlmOnPublish
	cfg["hook.on_play"] = conf.HttpNotifyConfig.ZlmOnPlay
	cfg["hook.on_flow_report"] = ""
	cfg["hook.on_http_access"] = ""
	cfg["hook.on_rtsp_auth"] = ""
	cfg["hook.on_rtsp_realm"] = ""
	cfg["hook.on_shell_login"] = ""
	cfg["hook.on_send_rtp_stopped"] = ""
	cfg["hook.on_server_exited"] = ""
	cfg["hook.on_stream_not_found"] = conf.HttpNotifyConfig.ZlmOnStreamNotFound
	cfg["hook.on_record_ts"] = ""
	hookTimeout := conf.HttpNotifyConfig.HookTimeoutSec
	if hookTimeout <= 0 {
		hookTimeout = 10
	}
	cfg["hook.timeoutSec"] = strconv.Itoa(hookTimeout)
	cfg["hook.retry"] = "1"
	cfg["hook.retry_delay"] = "3"
	cfg["hook.stream_changed_schemas"] = ""

	// --- 默认值填充 ---
	cfg["api.secret"] = ""
	cfg["api.apiDebug"] = "1"

	return cfg
}

// extractPort 从 ":1935" 或 "0.0.0.0:1935" 格式中提取端口号字符串
func extractPort(addr string) string {
	if addr == "" {
		return "0"
	}
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return "0"
	}
	return portStr
}

// parsePortRange 解析 "30000-35000" 格式的端口范围
// 为什么：owl 通过 setServerConfig 下发端口范围字符串，需转换为 min/max int
func parsePortRange(s string) (int, int, bool) {
	idx := strings.Index(s, "-")
	if idx <= 0 || idx == len(s)-1 {
		return 0, 0, false
	}
	minPort, err1 := strconv.Atoi(strings.TrimSpace(s[:idx]))
	maxPort, err2 := strconv.Atoi(strings.TrimSpace(s[idx+1:]))
	if err1 != nil || err2 != nil || minPort <= 0 || maxPort <= minPort {
		return 0, 0, false
	}
	return minPort, maxPort, true
}

func boolStr(v bool) string {
	if v {
		return "1"
	}
	return "0"
}
