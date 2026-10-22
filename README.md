# lalmax
lalmax是在lal的基础上集成第三方库，可以提供SRT、RTC、mp4、gb28181、onvif等解决方案

# 编译
./build.sh

# 运行
./run.sh或者./lalmax -c conf/lalmax.conf.json

# Console 监控页面

启动后访问 `http://127.0.0.1:1290/console`，可以查看活动流、输入/输出码率、编码信息、会话、播放地址以及 Go 运行状态。单流详情页内置 WebRTC、HTTP-fMP4 和 HTTP-FLV 播放器，其中 FLV 使用本地内嵌的 `mpegts.js`，无需访问公网 CDN。mpegts.js 不支持 PCMA/PCMU 音频，遇到这类流时 Console 会忽略音频并继续预览视频。

Console 默认每 2 秒刷新，并复用 `lalmax.http_config.ctrl_auth_whitelist` 鉴权。如果配置了密钥，可通过 `http://127.0.0.1:1290/console?token=YOUR_TOKEN` 访问。

# 配置说明
lalmax.conf.json 配置主要由 2 部分组成

(1) lalmax: lalmax 扩展能力配置，例如 SRT、RTC、HTTP-FMP4、GB28181 等，具体配置说明见[config.md](./document/config.md)

(2) lal: lal 原生配置，例如 RTMP、RTSP、HTTP-FLV、HLS-TS、录制、鉴权等，具体配置说明见[lal_config.md](./document/lal_config.md)。对外建议统一使用 lalmax 的 API Gateway、HTTP API 和 Hook API 门面；`lal.http_api` 仅建议在调试 lal 原生行为时临时开启，说明见[api_gateway.md](./document/api_gateway.md)、[lal_api.md](./document/lal_api.md)、[hook_api.md](./document/hook_api.md) 与 [hook_plugin_architecture.md](./document/hook_plugin_architecture.md)

旧版平铺配置和 lal_config_path 仍兼容，但推荐使用 lalmax/lal 两个顶层标签维护单个配置文件。

# docker运行
```
docker build -t lalmax:init ./

docker run -it -p 1935:1935 -p 8080:8080 -p 4433:4433 -p 5544:5544 -p 8084:8084 -p 30000-30100:30000-30100/udp -p 1290:1290 -p 6001:6001/udp lalmax:init

```

# 架构

![图片](document/images/init.png)

# 支持的协议
## 推流
(1) RTSP 

(2) SRT

(3) RTMP

(4) RTC(WHIP)

(5) GB28181

具体的推流 URL 地址见[流地址说明](./document/stream_url.md)

## 拉流
(1) RTSP

(2) SRT

(3) RTMP

(4) HLS(S)-TS

(5) HTTP(S)-FLV

(6) HTTP(S)-TS

(7) RTC(WHEP)

(8) HTTP(S)-FMP4

(9) HLS(S)-FMP4/LLHLS


具体的拉流 URL 地址见[流地址说明](./document/stream_url.md)

## [SRT](./document/srt.md)
（1）使用gosrt库

（2）暂时不支持SRT加密

（3）支持H264/H265/AAC

（4）可以对接OBS/VLC

```
推流url
srt://127.0.0.1:6001?streamid=#!::h=test110,m=publish

拉流url
srt://127.0.0.1:6001?streamid=#!::h=test110,m=request
```

## [WebRTC](./document/rtc.md)
（1）支持WHIP推流和WHEP拉流,暂时只支持POST信令

（2）支持H264/G711A/G711U/OPUS

（3）可以对接OBS、vue-wish

（4）WHEP支持对接Safari HEVC

（5）支持datachannel,只支持对接jessibuca播放器

（6）WHIP支持对接OBS 30.2 beta HEVC

datachannel播放地址：webrtc://127.0.0.1:1290/webrtc/play/live/test110

```
WHIP推流url
http(s)://127.0.0.1:1290/webrtc/whip?streamid=test110

WHEP拉流url
http(s)://127.0.0.1:1290/webrtc/whep?streamid=test110
```

## Http-fmp4
(1) 支持H264/H265/AAC/G711A/G711U

```
拉流url
http(s)://127.0.0.1:1290/live/m4s/test110.mp4
```

## HLS(fmp4/Low Latency)
(1) 支持H264/H265/AAC/OPUS

```
拉流url
http(s)://127.0.0.1:1290/live/hls/test110/index.m3u8
```

## [GB28181](./document/gb28181.md)
(1) 作为SIP服务器与设备进行SIP交互,使用单端口/多端口收流

(2) 提供相关API获取设备信息、播放某通道等功能

(3) 支持H264/H265/AAC/G711A/G711U

(4) 支持TCP/UDP


# 基于 lalmax 的应用

## lalmax-nvr

[lalmax-nvr](https://github.com/lalmax-pro/lalmax-nvr) 是基于 lal / lalmax 媒体引擎构建的开源轻量级网络视频录像机（NVR），提供 Web 管理界面，适用于摄像头接入、实时预览和录像回放。

- 接入：RTSP / ONVIF 摄像头、GB28181 设备，以及 RTMP / SRT / WHIP 推流。
- 录像：连续、定时和事件录像计划，录像保留策略与时间轴回放。
- 部署：单二进制内嵌 Web UI，或使用 Docker。

[中文说明](https://github.com/lalmax-pro/lalmax-nvr/blob/main/README.zh.md) · [产品官网](https://lalmax-pro.github.io/lalmax-nvr/)

# QQ交流群
11818248




