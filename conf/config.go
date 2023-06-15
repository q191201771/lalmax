package config

import (
	"encoding/json"
	"io/ioutil"
)

var defaultConfig Config

type Config struct {
	SrtConfig        SrtConfig `json:"srt_config"`
	RtcConfig        RtcConfig `json:"rtc_config"`
	LalSvrConfigPath string    `json:"lal_config_path"`
}

type SrtConfig struct {
	Enable bool   `json:"enable"`
	Host   string `json:"host"`
	Port   uint16 `json:"port"`
}

type RtcConfig struct {
	Enable          bool     `json:"enable"`
	HttpListenAddr  string   `json:"httpListenAddr"`
	ICEHostNATToIPs []string `json:"iceHostNatToIps"`
	ICEUDPMuxPort   int      `json:"iceUdpMuxPort"`
	ICETCPMuxPort   int      `json:"iceTcpMuxPort"`
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
