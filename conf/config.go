package config

import (
	"encoding/json"
	"io/ioutil"
)

var defaultConfig Config

type Config struct {
	SrtConfig        SrtConfig      `json:"srt_config"`      // srt配置
	RtcConfig        RtcConfig      `json:"rtc_config"`      // rtc配置
	HttpConfig       HttpConfig     `json:"http_config"`     // http/https配置
	HttpFmp4Config   HttpFmp4Config `json:"httpfmp4_config"` // http-fmp4配置
	HlsConfig        HlsConfig      `json:"hls_config"`      // hls-fmp4/llhls配置
	LalSvrConfigPath string         `json:"lal_config_path"` // lal配置目录
}

type SrtConfig struct {
	Enable bool   `json:"enable"` // srt服务使能配置
	Addr   string `json:"addr"`   // srt服务监听地址
}

type RtcConfig struct {
	Enable          bool     `json:"enable"`          // rtc服务使能配置
	ICEHostNATToIPs []string `json:"iceHostNatToIps"` // rtc服务内穿ip
	ICEUDPMuxPort   int      `json:"iceUdpMuxPort"`   // rtc udp mux port
	ICETCPMuxPort   int      `json:"iceTcpMuxPort"`   // rtc tcp mux port
}

type HttpConfig struct {
	ListenAddr      string `json:"http_listen_addr"`  // http服务监听地址
	EnableHttps     bool   `json:"enable_https"`      // https使能标志
	HttpsListenAddr string `json:"https_listen_addr"` // https监听地址
	HttpsCertFile   string `json:"https_cert_file"`   // https cert 文件
	HttpsKeyFile    string `json:"https_key_file"`    // https key 文件
}

type HttpFmp4Config struct {
	Enable bool `json:"enable"` // http-fmp4使能标志
}

type HlsConfig struct {
	Enable          bool `json:"enable"`          // hls使能标志
	SegmentCount    int  `json:"segmentCount"`    // 分片个数,llhls默认7个
	SegmentDuration int  `json:"segmentDuration"` // hls分片时长,默认1s
	PartDuration    int  `json:"partDuration"`    // llhls part时长,默认200ms
	LowLatency      bool `json:"lowLatency"`      // 是否开启llhls
}

func Open(filepath string) error {
	data, err := ioutil.ReadFile(filepath)
	if err != nil {
		return err
	}

	err = json.Unmarshal(data, &defaultConfig)
	if err != nil {
		return err
	}
	return nil
}

func GetConfig() *Config {
	return &defaultConfig
}
