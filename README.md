# lalmax
lalmax是以lal为内核的卍解

# 编译
./build.sh

# 运行
./run.sh

![图片](image/init.png)

# 支持的协议
## 推流
(1) RTSP 

(2) SRT

(3) RTMP

(4) RTC(规划中)

## 拉流
(1) RTSP

(2) SRT

(3) RTMP

(4) HLS(S)

(5) HTTP(S)-FLV

(6) HTTP(S)-TS

(7) RTC(规划中)


具体的拉流url地址见https://pengrl.com/lal/#/streamurllist

## SRT
注：

（1）SRT推拉流依赖libsrt库,run.sh中有编译libsrt，如果run.sh无法编译libsrt，需要自己另行libsrt
（2）暂时不支持SRT加密

推流url
srt://127.0.0.1:6001?streamid=#!::r=test110,m=publish

拉流url
srt://127.0.0.1:6001?streamid=#!::r=test110,m=request

