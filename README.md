# lalmax
lalmax是以lal为内核的卍解

# 编译
./build.sh

# 运行
./run.sh

# docker运行
```
docker build -t lalmax:init ./

docker run -it -p 1935:1935 -p 8080:8080 -p 4433:4433 -p 5544:5544 -p 8083:8083 -p 8084:8084 -p 30000-30100:30000-30100/udp -p 1290:1290 -p 6001:6001/udp lalmax:init

```

# 架构

![图片](image/init.png)

# 支持的协议
## 推流
(1) RTSP 

(2) SRT

(3) RTMP

(4) RTC(WHIP)

具体的推流url地址见https://pengrl.com/lal/#/streamurllist（除了srt/whip）

## 拉流
(1) RTSP

(2) SRT

(3) RTMP

(4) HLS(S)

(5) HTTP(S)-FLV

(6) HTTP(S)-TS

(7) RTC(WHEP)


具体的拉流url地址见https://pengrl.com/lal/#/streamurllist（除了srt/whep）

## [SRT](./document/srt.md)
（1）使用gosrt库

（2）暂时不支持SRT加密

（3）支持H264/H265/AAC

（4）可以对接OBS/VLC

推流url
srt://127.0.0.1:6001?streamid=publish:test110

拉流url
srt://127.0.0.1:6001?streamid=test110

## [WebRTC](./document/rtc.md)
（1）支持WHIP推流和WHEP拉流,暂时只支持POST信令

（2）支持H264/G711A/G711U,后续支持opus音频

（3）可以对接OBS、vue-wish

WHIP推流url
http(s)://127.0.0.1:1290/whip?streamid=test110

WHEP拉流url
http(s)://127.0.0.1:1290/whep?streamid=test110

## Http-fmp4
拉流url
http(s)://127.0.0.1:1290/live/m4s/test110.mp4



