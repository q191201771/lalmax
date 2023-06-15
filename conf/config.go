package config

import (
	"encoding/json"
	"io/ioutil"
)

var defaultConfig Config

type Config struct {
	SrtConfig        SrtConfig `json:"srt_config"`      // srt配置
	RtcConfig        RtcConfig `json:"rtc_config"`      // rtc配置
	LalSvrConfigPath string    `json:"lal_config_path"` // lal配置目录
}

type SrtConfig struct {
	Enable bool   `json:"enable"` // srt服务使能配置
	Host   string `json:"host"`   // srt服务监听host
	Port   uint16 `json:"port"`   // srt服务监听端口
}

type RtcConfig struct {
	Enable          bool     `json:"enable"`          // rtc服务使能配置
	HttpListenAddr  string   `json:"httpListenAddr"`  // rtc服务http监听地址
	ICEHostNATToIPs []string `json:"iceHostNatToIps"` // rtc服务内穿ip
	ICEUDPMuxPort   int      `json:"iceUdpMuxPort"`   // rtc udp mux port
	ICETCPMuxPort   int      `json:"iceTcpMuxPort"`   // rtc tcp mux port
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
