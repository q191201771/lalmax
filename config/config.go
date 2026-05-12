package config

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
)

var defaultConfig Config

type Config struct {
	SrtConfig        SrtConfig        `json:"srt_config"`      // srt配置
	RtcConfig        RtcConfig        `json:"rtc_config"`      // rtc配置
	HttpConfig       HttpConfig       `json:"http_config"`     // http/https配置
	Fmp4Config       Fmp4Config       `json:"fmp4_config"`     // fmp4配置
	GB28181Config    GB28181Config    `json:"gb28181_config"`  // gb28181配置
	ServerId         string           `json:"server_id"`       // http 通知唯一标识
	HttpNotifyConfig HttpNotifyConfig `json:"http_notify"`     // http 通知配置
	LalSvrConfigPath string           `json:"lal_config_path"` // lal配置文件路径，兼容旧版配置
	LogicConfig      LogicConfig      `json:"logic_config"`    // 扩展流组配置
	LalRawContent    []byte           `json:"-"`               // lal 原始配置内容
	ConfFilePath     string           `json:"-"`               // 配置文件路径，用于持久化
}

type SrtConfig struct {
	Enable bool   `json:"enable"` // srt服务使能配置
	Addr   string `json:"addr"`   // srt服务监听地址
}

type RtcConfig struct {
	Enable          bool     `json:"enable"`              // rtc服务使能配置
	ICEHostNATToIPs []string `json:"ice_host_nat_to_ips"` // rtc服务公网IP，未设置使用内网
	ICEUDPMuxPort   int      `json:"ice_udp_mux_port"`    // rtc udp mux port
	ICETCPMuxPort   int      `json:"ice_tcp_mux_port"`    // rtc tcp mux port
	WriteChanSize   int      `json:"write_chan_size"`
}

type HttpConfig struct {
	ListenAddr        string            `json:"http_listen_addr"`  // http服务监听地址
	EnableHttps       bool              `json:"enable_https"`      // https使能标志
	HttpsListenAddr   string            `json:"https_listen_addr"` // https监听地址
	HttpsCertFile     string            `json:"https_cert_file"`   // https cert 文件
	HttpsKeyFile      string            `json:"https_key_file"`    // https key 文件
	CtrlAuthWhitelist CtrlAuthWhitelist `json:"ctrl_auth_whitelist"`
}

// CtrlAuthWhitelist 控制类接口鉴权。
type CtrlAuthWhitelist struct {
	IPs     []string // 允许访问的远程 IP，零值时不生效
	Secrets []string // 认证信息，零值时不生效
}

type Fmp4Config struct {
	Http Fmp4HttpConfig `json:"http"`
	Hls  Fmp4HlsConfig  `json:"hls"`
}

type Fmp4HttpConfig struct {
	Enable bool `json:"enable"` // http-fmp4使能标志
}

type Fmp4HlsConfig struct {
	Enable          bool `json:"enable"`           // hls使能标志
	SegmentCount    int  `json:"segment_count"`    // 分片个数,llhls默认7个
	SegmentDuration int  `json:"segment_duration"` // hls分片时长,默认1s
	PartDuration    int  `json:"part_duration"`    // llhls part时长,默认200ms
	LowLatency      bool `json:"low_latency"`      // 是否开启llhls
}

type GB28181Config struct {
	Enable            bool               `json:"enable"`             // gb28181使能标志
	ListenAddr        string             `json:"listen_addr"`        // gb28181监听地址
	SipIP             string             `json:"sip_ip"`             // sip 服务器公网IP
	SipPort           uint16             `json:"sip_port"`           // sip 服务器端口，默认 5060
	Serial            string             `json:"serial"`             // sip 服务器 id, 默认 34020000002000000001
	Realm             string             `json:"realm"`              // sip 服务器域，默认 3402000000
	Username          string             `json:"username"`           // sip 服务器账号
	Password          string             `json:"password"`           // sip 服务器密码
	KeepaliveInterval int                `json:"keepalive_interval"` // 心跳包时长
	QuickLogin        bool               `json:"quick_login"`        // 快速登陆,有keepalive就认为在线
	MediaConfig       GB28181MediaConfig `json:"media_config"`       // 媒体服务器配置
}

type GB28181MediaConfig struct {
	MediaIp               string `json:"media_ip"`                 // 流媒体IP,用于在SDP中指定
	ListenPort            uint16 `json:"listen_port"`              // tcp,udp监听端口 默认启动
	MultiPortMaxIncrement uint16 `json:"multi_port_max_increment"` // 多端口范围 ListenPort+1至ListenPort+MultiPortMax
}

// ZlmCompatHookConfig ZLM 兼容 hook URL 配置
// 为什么独立结构体：隔离 ZLM 适配层，lalmax 原有字段保持不变
type ZlmCompatHookConfig struct {
	ZlmOnStreamChanged    string `json:"zlm_on_stream_changed"`
	ZlmOnServerKeepalive  string `json:"zlm_on_server_keepalive"`
	ZlmOnStreamNoneReader string `json:"zlm_on_stream_none_reader"`
	ZlmOnRtpServerTimeout string `json:"zlm_on_rtp_server_timeout"`
	ZlmOnRecordMp4        string `json:"zlm_on_record_mp4"`
	ZlmOnPublish          string `json:"zlm_on_publish"`
	ZlmOnPlay             string `json:"zlm_on_play"`
	ZlmOnStreamNotFound   string `json:"zlm_on_stream_not_found"`
	ZlmOnServerStarted    string `json:"zlm_on_server_started"`
}

// HasZlmHooks 任一 ZLM 兼容 hook 字段有值即返回 true
// 为什么：ZLM 回调与 lalmax 原有回调二选一，此方法为判断条件
func (c ZlmCompatHookConfig) HasZlmHooks() bool {
	return c.ZlmOnStreamChanged != "" ||
		c.ZlmOnServerKeepalive != "" ||
		c.ZlmOnStreamNoneReader != "" ||
		c.ZlmOnRtpServerTimeout != "" ||
		c.ZlmOnRecordMp4 != "" ||
		c.ZlmOnPublish != "" ||
		c.ZlmOnPlay != "" ||
		c.ZlmOnStreamNotFound != ""
}

type HttpNotifyConfig struct {
	Enable               bool   `json:"enable"`
	UpdateIntervalSec    int    `json:"update_interval_sec"`
	KeepaliveIntervalSec int    `json:"keepalive_interval_sec"`
	HookTimeoutSec       int    `json:"hook_timeout_sec"`
	OnServerStart        string `json:"on_server_start"`
	OnUpdate             string `json:"on_update"`
	OnGroupStart         string `json:"on_group_start"`
	OnGroupStop          string `json:"on_group_stop"`
	OnStreamActive       string `json:"on_stream_active"`
	OnPubStart           string `json:"on_pub_start"`
	OnPubStop            string `json:"on_pub_stop"`
	OnSubStart           string `json:"on_sub_start"`
	OnSubStop            string `json:"on_sub_stop"`
	OnRelayPullStart     string `json:"on_relay_pull_start"`
	OnRelayPullStop      string `json:"on_relay_pull_stop"`
	OnRtmpConnect        string `json:"on_rtmp_connect"`
	OnHlsMakeTs          string `json:"on_hls_make_ts"`

	// --- ZLM 兼容 hook 配置 ---
	ZlmCompatHookConfig
}

type LogicConfig struct {
	GopCacheNum          int `json:"gop_cache_num"`
	SingleGopMaxFrameNum int `json:"single_gop_max_frame_num"`
}

func Open(filepath string) error {
	data, err := ioutil.ReadFile(filepath)
	if err != nil {
		return err
	}

	err = Unmarshal(data)
	if err != nil {
		return err
	}
	return nil
}

func Unmarshal(data []byte) error {
	var file struct {
		LalMax json.RawMessage `json:"lalmax"`
		Lal    json.RawMessage `json:"lal"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return err
	}

	var cfg Config
	if len(file.LalMax) != 0 {
		if err := unmarshalConfig(file.LalMax, &cfg); err != nil {
			return err
		}
		cfg.LalRawContent = append([]byte(nil), file.Lal...)
	} else {
		if err := unmarshalConfig(data, &cfg); err != nil {
			return err
		}
	}

	defaultConfig = cfg
	return nil
}

func unmarshalConfig(data []byte, cfg *Config) error {
	if err := json.Unmarshal(data, cfg); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if cfg.LalSvrConfigPath == "" {
		var legacy struct {
			LalSvrConfigPath string `json:"lal_config_path:"`
		}
		if err := json.Unmarshal(data, &legacy); err != nil {
			return err
		}
		cfg.LalSvrConfigPath = legacy.LalSvrConfigPath
	}
	if _, ok := raw["logic_config"]; !ok {
		var legacy struct {
			LogicConfig LogicConfig `json:"hook_config"`
		}
		if err := json.Unmarshal(data, &legacy); err != nil {
			return err
		}
		cfg.LogicConfig = legacy.LogicConfig
	}
	if _, ok := raw["fmp4_config"]; !ok {
		var legacy struct {
			Http Fmp4HttpConfig `json:"httpfmp4_config"`
			Hls  Fmp4HlsConfig  `json:"hls_config"`
		}
		if err := json.Unmarshal(data, &legacy); err != nil {
			return err
		}
		cfg.Fmp4Config.Http = legacy.Http
		cfg.Fmp4Config.Hls = legacy.Hls
	}
	return nil
}

func GetConfig() *Config {
	return &defaultConfig
}

// SaveToFile 将当前配置持久化到配置文件
// 为什么：setServerConfig 动态修改后需落盘，重启后配置仍生效
func (c *Config) SaveToFile() error {
	if c.ConfFilePath == "" {
		return nil
	}

	data, err := os.ReadFile(c.ConfFilePath)
	if err != nil {
		return fmt.Errorf("read config file: %w", err)
	}

	var file map[string]json.RawMessage
	if err := json.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("parse config file: %w", err)
	}

	lalmax, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal lalmax config: %w", err)
	}
	file["lalmax"] = lalmax

	out, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config file: %w", err)
	}
	out = append(out, '\n')

	return os.WriteFile(c.ConfFilePath, out, 0o644)
}
