package gb28181

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	udpTransport "github.com/pion/transport/v3/udp"
	config "github.com/q191201771/lalmax/conf"
	"github.com/q191201771/lalmax/gb28181/mediaserver"

	"github.com/ghettovoice/gosip"
	"github.com/ghettovoice/gosip/log"
	"github.com/ghettovoice/gosip/sip"
	"github.com/q191201771/lal/pkg/logic"
	"github.com/q191201771/naza/pkg/nazalog"
	"golang.org/x/net/html/charset"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

type IMediaOpObserver interface {
	OnStartMediaServer(netWork string, singlePort bool, deviceId string, channelId string) *mediaserver.GB28181MediaServer
	OnStopMediaServer(netWork string, singlePort bool, deviceId string, channelId string) error
}
type GB28181Server struct {
	conf              config.GB28181Config
	RegisterValidity  time.Duration // 注册有效期，单位秒，默认 3600
	HeartbeatInterval time.Duration // 心跳间隔，单位秒，默认 60
	RemoveBanInterval time.Duration // 移除禁止设备间隔,默认600s
	keepaliveInterval int

	lalServer logic.ILalServer

	udpAvailConnPool *AvailConnPool
	tcpAvailConnPool *AvailConnPool

	MediaServerMap sync.Map
	disposeOnce    sync.Once
}

const MaxRegisterCount = 3

var (
	logger log.Logger
	sipsvr gosip.Server
)

func init() {
	logger = log.NewDefaultLogrusLogger().WithPrefix("LalMaxServer")
}

func NewGB28181Server(conf config.GB28181Config, lal logic.ILalServer) *GB28181Server {
	if conf.ListenAddr == "" {
		conf.ListenAddr = "0.0.0.0"
	}

	if conf.SipNetwork == "" {
		conf.SipNetwork = "udp"
	}

	if conf.SipPort == 0 {
		conf.SipPort = 5060
	}
	if conf.KeepaliveInterval == 0 {
		conf.KeepaliveInterval = 60
	}
	if conf.Serial == "" {
		conf.Serial = "34020000002000000001"
	}

	if conf.Realm == "" {
		conf.Realm = "3402000000"
	}

	if conf.MediaConfig.MediaIp == "" {
		conf.MediaConfig.MediaIp = "0.0.0.0"
	}

	if conf.MediaConfig.ListenPort == 0 {
		conf.MediaConfig.ListenPort = 30000
	}
	if conf.MediaConfig.MultiPortMaxIncrement == 0 {
		conf.MediaConfig.MultiPortMaxIncrement = 3000
	}
	gb28181Server := &GB28181Server{
		conf:              conf,
		RegisterValidity:  3600 * time.Second,
		HeartbeatInterval: 60 * time.Second,
		RemoveBanInterval: 600 * time.Second,
		keepaliveInterval: conf.KeepaliveInterval,
		lalServer:         lal,
		udpAvailConnPool:  NewAvailConnPool(conf.MediaConfig.ListenPort+1, conf.MediaConfig.ListenPort+conf.MediaConfig.MultiPortMaxIncrement),
		tcpAvailConnPool:  NewAvailConnPool(conf.MediaConfig.ListenPort+1, conf.MediaConfig.ListenPort+conf.MediaConfig.MultiPortMaxIncrement),
	}
	gb28181Server.tcpAvailConnPool.onListenWithPort = func(port uint16) (net.Listener, error) {
		return net.Listen("tcp", fmt.Sprintf(":%d", port))
	}

	gb28181Server.udpAvailConnPool.onListenWithPort = func(port uint16) (net.Listener, error) {
		addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", port))
		if err != nil {
			nazalog.Error("gb28181 media server udp listen failed,err:", err)
			return nil, err
		}

		return udpTransport.Listen("udp", addr)
	}
	return gb28181Server
}

func (s *GB28181Server) Start() {
	srvConf := gosip.ServerConfig{}

	if s.conf.SipIP != "" {
		srvConf.Host = s.conf.SipIP
	}
	sipsvr = gosip.NewServer(srvConf, nil, nil, logger)
	sipsvr.OnRequest(sip.REGISTER, s.OnRegister)
	sipsvr.OnRequest(sip.MESSAGE, s.OnMessage)
	sipsvr.OnRequest(sip.NOTIFY, s.OnNotify)
	sipsvr.OnRequest(sip.BYE, s.OnBye)

	addr := s.conf.ListenAddr + ":" + strconv.Itoa(int(s.conf.SipPort))
	err := sipsvr.Listen(s.conf.SipNetwork, addr)
	if err != nil {
		nazalog.Fatal(err)
	}

	nazalog.Info("gb28181 sip listen success, network:", s.conf.SipNetwork, " listen addr:", addr)

	go s.startJob()
}
func (s *GB28181Server) Dispose() {
	s.disposeOnce.Do(
		func() {
			s.MediaServerMap.Range(func(_, value any) bool {
				mediaServer := value.(*mediaserver.GB28181MediaServer)
				mediaServer.Dispose()
				return true
			})
		})
}
func (s *GB28181Server) OnStartMediaServer(netWork string, singlePort bool, deviceId string, channelId string) *mediaserver.GB28181MediaServer {
	isTcpFlag := false
	if netWork == "tcp" {
		isTcpFlag = true
	}
	var mediasvr *mediaserver.GB28181MediaServer
	if singlePort {
		if isTcpFlag {
			value, ok := s.MediaServerMap.Load(fmt.Sprintf("%s%d", "tcp", s.conf.MediaConfig.ListenPort))
			if ok {
				mediasvr = value.(*mediaserver.GB28181MediaServer)
			}
		} else {
			value, ok := s.MediaServerMap.Load(fmt.Sprintf("%s%d", "udp", s.conf.MediaConfig.ListenPort))
			if ok {
				mediasvr = value.(*mediaserver.GB28181MediaServer)
			}
		}
	} else {
		value, ok := s.MediaServerMap.Load(fmt.Sprintf("%s%s", deviceId, channelId))
		if ok {
			mediasvr = value.(*mediaserver.GB28181MediaServer)
		}
	}
	var listener net.Listener
	var err error
	var port uint16
	if mediasvr == nil {
		if singlePort {
			if isTcpFlag {
				mediasvr = mediaserver.NewGB28181MediaServer(int(s.conf.MediaConfig.ListenPort), fmt.Sprintf("%s%d", "tcp", s.conf.MediaConfig.ListenPort), s, s.lalServer)
				listener, err = s.tcpAvailConnPool.ListenWithPort(s.conf.MediaConfig.ListenPort)
				if err != nil {
					nazalog.Error("gb28181 media server tcp Listen failed:%s", err.Error())
					return nil
				}
				s.MediaServerMap.Store(fmt.Sprintf("%s%d", "tcp", s.conf.MediaConfig.ListenPort), mediasvr)
			} else {
				mediasvr = mediaserver.NewGB28181MediaServer(int(s.conf.MediaConfig.ListenPort), fmt.Sprintf("%s%d", "udp", s.conf.MediaConfig.ListenPort), s, s.lalServer)
				listener, err = s.udpAvailConnPool.ListenWithPort(s.conf.MediaConfig.ListenPort)
				if err != nil {
					nazalog.Error("gb28181 media server udp Listen failed:%s", err.Error())
					return nil
				}
				s.MediaServerMap.Store(fmt.Sprintf("%s%d", "udp", s.conf.MediaConfig.ListenPort), mediasvr)
			}
		} else {
			mediaKey := ""
			if isTcpFlag {
				listener, port, err = s.tcpAvailConnPool.Acquire()
				if err != nil {
					nazalog.Error("gb28181 media server tcp acquire failed:%s", err.Error())
					return nil
				}
				mediaKey = fmt.Sprintf("%s%d", "tcp", port)
			} else {
				listener, port, err = s.udpAvailConnPool.Acquire()
				if err != nil {
					nazalog.Error("gb28181 media server udp acquire failed:%s", err.Error())
					return nil
				}
				mediaKey = fmt.Sprintf("%s%d", "tcp", port)
			}
			mediasvr = mediaserver.NewGB28181MediaServer(int(port), mediaKey, s, s.lalServer)
			s.MediaServerMap.Store(fmt.Sprintf("%s%s", deviceId, channelId), mediasvr)
		}
		go mediasvr.Start(listener)
	}
	return mediasvr
}
func (s *GB28181Server) OnStopMediaServer(netWork string, singlePort bool, deviceId string, channelId string) error {
	isTcpFlag := false
	if netWork == "tcp" {
		isTcpFlag = true
	}
	var mediasvr *mediaserver.GB28181MediaServer
	if singlePort {
		if isTcpFlag {
			key := fmt.Sprintf("%s%d", "tcp", s.conf.MediaConfig.ListenPort)
			value, ok := s.MediaServerMap.Load(key)
			if ok {
				mediasvr = value.(*mediaserver.GB28181MediaServer)
				s.MediaServerMap.Delete(key)
			}
		} else {
			key := fmt.Sprintf("%s%d", "udp", s.conf.MediaConfig.ListenPort)
			value, ok := s.MediaServerMap.Load(key)
			if ok {
				mediasvr = value.(*mediaserver.GB28181MediaServer)
				s.MediaServerMap.Delete(key)
			}
		}
	} else {
		key := fmt.Sprintf("%s%s", deviceId, channelId)
		value, ok := s.MediaServerMap.Load(key)
		if ok {
			mediasvr = value.(*mediaserver.GB28181MediaServer)
			s.MediaServerMap.Delete(key)
		}
	}
	if mediasvr != nil {
		mediasvr.Dispose()

	}
	return nil
}
func (s *GB28181Server) CheckSsrc(ssrc uint32) (*mediaserver.MediaInfo, bool) {
	var isValidSsrc bool
	var mediaInfo *mediaserver.MediaInfo

	Devices.Range(func(_, value any) bool {
		d := value.(*Device)
		d.channelMap.Range(func(key, value any) bool {
			ch := value.(*Channel)
			if ch.MediaInfo.Ssrc == ssrc {
				isValidSsrc = true
				mediaInfo = &ch.MediaInfo
				return false
			}
			return true
		})
		if isValidSsrc {
			return false
		}
		return true
	})

	if isValidSsrc {
		return mediaInfo, true
	}

	return nil, false
}
func (s *GB28181Server) GetMediaInfoByKey(key string) (*mediaserver.MediaInfo, bool) {
	var isValidMediaInfo bool
	var mediaInfo *mediaserver.MediaInfo

	Devices.Range(func(_, value any) bool {
		d := value.(*Device)
		d.channelMap.Range(func(_, value any) bool {
			ch := value.(*Channel)
			if ch.MediaInfo.MediaKey == key {
				isValidMediaInfo = true
				mediaInfo = &ch.MediaInfo
				return false
			}
			return true
		})
		if isValidMediaInfo {
			return false
		}
		return true
	})

	if isValidMediaInfo {
		return mediaInfo, true
	}

	return nil, false
}

func (s *GB28181Server) NotifyClose(streamName string) {
	var ok bool
	Devices.Range(func(_, value any) bool {
		d := value.(*Device)
		d.channelMap.Range(func(key, value any) bool {
			ch := value.(*Channel)
			if ch.MediaInfo.StreamName == streamName {
				if ch.MediaInfo.IsInvite {
					ch.Bye(streamName)
				}
				ch.MediaInfo.Clear()
				ok = true
				return false
			}
			return true
		})
		if ok {
			return false
		}
		return true
	})
}

func (s *GB28181Server) startJob() {
	statusTick := time.NewTicker(s.HeartbeatInterval / 2)
	banTick := time.NewTicker(s.RemoveBanInterval)
	for {
		select {
		case <-banTick.C:
			if s.conf.Username != "" || s.conf.Password != "" {
				s.removeBanDevice()
			}
		case <-statusTick.C:
			s.statusCheck()
		}
	}
}

func (s *GB28181Server) removeBanDevice() {
	DeviceRegisterCount.Range(func(key, value interface{}) bool {
		if value.(int) > MaxRegisterCount {
			DeviceRegisterCount.Delete(key)
		}
		return true
	})
}

// statusCheck
// -  当设备超过 3 倍心跳时间未发送过心跳（通过 UpdateTime 判断）, 视为离线
// - 	当设备超过注册有效期内为发送过消息，则从设备列表中删除
// UpdateTime 在设备发送心跳之外的消息也会被更新，相对于 LastKeepaliveAt 更能体现出设备最会一次活跃的时间
func (s *GB28181Server) statusCheck() {
	Devices.Range(func(key, value any) bool {
		d := value.(*Device)
		if int(time.Since(d.LastKeepaliveAt).Seconds()) > s.keepaliveInterval*3 {
			Devices.Delete(key)
			nazalog.Warn("Device Keepalive timeout, id:", d.ID, " LastKeepaliveAt:", d.LastKeepaliveAt, " updateTime:", d.UpdateTime)
		} else if time.Since(d.UpdateTime) > s.HeartbeatInterval*3 {
			d.Status = DeviceOfflineStatus
			d.channelMap.Range(func(key, value any) bool {
				ch := value.(*Channel)
				ch.Status = ChannelOffStatus
				return true
			})
			nazalog.Warn("Device offline, id:", d.ID, " registerTime:", d.RegisterTime, " updateTime:", d.UpdateTime)
		}
		return true
	})
}
func (s *GB28181Server) getDeviceInfos() (deviceInfos *DeviceInfos) {
	deviceInfos = &DeviceInfos{
		DeviceItems: make([]*DeviceItem, 0),
	}
	Devices.Range(func(key, value any) bool {
		d := value.(*Device)
		d.Status = DeviceOfflineStatus
		deviceItem := &DeviceItem{
			DeviceId: d.ID,
			Channels: make([]*ChannelItem, 0),
		}
		d.channelMap.Range(func(key, value any) bool {
			ch := value.(*Channel)
			channel := &ChannelItem{
				ChannelId:    ch.ChannelId,
				Name:         ch.Name,
				Manufacturer: ch.Manufacturer,
				Owner:        ch.Owner,
				CivilCode:    ch.CivilCode,
				Address:      ch.Address,
				Status:       ch.Status,
				Longitude:    ch.Longitude,
				Latitude:     ch.Latitude,
				StreamName:   ch.StreamName,
			}
			deviceItem.Channels = append(deviceItem.Channels, channel)
			return true
		})
		deviceInfos.DeviceItems = append(deviceInfos.DeviceItems, deviceItem)
		return true
	})
	return deviceInfos
}
func (s *GB28181Server) GetAllSyncChannels() {
	Devices.Range(func(key, value any) bool {
		d := value.(*Device)
		d.syncChannels(s.conf)
		return true
	})
}
func (s *GB28181Server) GetSyncChannels(deviceId string) bool {
	if v, ok := Devices.Load(deviceId); ok {
		d := v.(*Device)
		d.syncChannels(s.conf)
		return true
	} else {
		return false
	}
}
func (s *GB28181Server) FindChannel(deviceId string, channelId string) (channel *Channel) {
	if v, ok := Devices.Load(deviceId); ok {
		d := v.(*Device)
		if ch, ok := d.channelMap.Load(channelId); ok {
			channel = ch.(*Channel)
			return channel
		} else {
			return nil
		}
	} else {
		return nil
	}
}
func (s *GB28181Server) OnRegister(req sip.Request, tx sip.ServerTransaction) {
	from, ok := req.From()
	if !ok || from.Address == nil {
		nazalog.Error("OnRegister, no from")
		return
	}
	id := from.Address.User().String()

	nazalog.Info("OnRegister", " id:", id, " source:", req.Source(), " req:", req.String())

	isUnregister := false
	if exps := req.GetHeaders("Expires"); len(exps) > 0 {
		exp := exps[0]
		expSec, err := strconv.ParseInt(exp.Value(), 10, 32)
		if err != nil {
			nazalog.Error(err)
			return
		}
		if expSec == 0 {
			isUnregister = true
		}
	} else {
		nazalog.Error("has no expire header")
		return
	}

	nazalog.Info("OnRegister", " isUnregister:", isUnregister, " id:", id, " source:", req.Source(), " destination:", req.Destination())

	if len(id) != 20 {
		nazalog.Error("invalid id: ", id)
		return
	}

	passAuth := false
	// 不需要密码情况
	if s.conf.Username == "" && s.conf.Password == "" {
		passAuth = true
	} else {
		// 需要密码情况 设备第一次上报，返回401和加密算法
		if hdrs := req.GetHeaders("Authorization"); len(hdrs) > 0 {
			authenticateHeader := hdrs[0].(*sip.GenericHeader)
			auth := &Authorization{sip.AuthFromValue(authenticateHeader.Contents)}

			// 有些摄像头没有配置用户名的地方，用户名就是摄像头自己的国标id
			var username string
			if auth.Username() == id {
				username = id
			} else {
				username = s.conf.Username
			}

			if dc, ok := DeviceRegisterCount.LoadOrStore(id, 1); ok && dc.(int) > MaxRegisterCount {
				response := sip.NewResponseFromRequest("", req, http.StatusForbidden, "Forbidden", "")
				tx.Respond(response)
				return
			} else {
				// 设备第二次上报，校验
				_nonce, loaded := DeviceNonce.Load(id)
				if loaded && auth.Verify(username, s.conf.Password, s.conf.Realm, _nonce.(string)) {
					passAuth = true
				} else {
					DeviceRegisterCount.Store(id, dc.(int)+1)
				}
			}
		}
	}

	if passAuth {
		var d *Device
		if isUnregister {
			tmpd, ok := Devices.LoadAndDelete(id)
			if ok {
				nazalog.Info("Unregister Device, id:", id)
				d = tmpd.(*Device)
			} else {
				return
			}
		} else {
			if v, ok := Devices.Load(id); ok {
				d = v.(*Device)
				s.RecoverDevice(d, req)
			} else {
				d = s.StoreDevice(id, req)
			}
		}

		DeviceNonce.Delete(id)
		DeviceRegisterCount.Delete(id)
		resp := sip.NewResponseFromRequest("", req, http.StatusOK, "OK", "")
		to, _ := resp.To()
		resp.ReplaceHeaders("To", []sip.Header{&sip.ToHeader{Address: to.Address, Params: sip.NewParams().Add("tag", sip.String{Str: RandNumString(9)})}})
		resp.RemoveHeader("Allow")
		expires := sip.Expires(3600)
		resp.AppendHeader(&expires)
		resp.AppendHeader(&sip.GenericHeader{
			HeaderName: "Date",
			Contents:   time.Now().Format(TIME_LAYOUT),
		})
		_ = tx.Respond(resp)

		if !isUnregister {
			//订阅设备更新
			go d.syncChannels(s.conf)
		}
	} else {
		nazalog.Info("OnRegister unauthorized, id:", id, " source:", req.Source(), " destination:", req.Destination())
		response := sip.NewResponseFromRequest("", req, http.StatusUnauthorized, "Unauthorized", "")
		_nonce, _ := DeviceNonce.LoadOrStore(id, RandNumString(32))
		auth := fmt.Sprintf(
			`Digest realm="%s",algorithm=%s,nonce="%s"`,
			s.conf.Realm,
			"MD5",
			_nonce.(string),
		)
		response.AppendHeader(&sip.GenericHeader{
			HeaderName: "WWW-Authenticate",
			Contents:   auth,
		})
		_ = tx.Respond(response)
	}
}

func (s *GB28181Server) OnMessage(req sip.Request, tx sip.ServerTransaction) {
	from, _ := req.From()
	id := from.Address.User().String()
	nazalog.Info("SIP<-OnMessage, id:", id, " source:", req.Source(), " req:", req.String())
	temp := &struct {
		XMLName      xml.Name
		CmdType      string
		SN           int // 请求序列号，一般用于对应 request 和 response
		DeviceID     string
		DeviceName   string
		Manufacturer string
		Model        string
		Channel      string
		DeviceList   []ChannelInfo `xml:"DeviceList>Item"`
		SumNum       int           // 录像结果的总数 SumNum，录像结果会按照多条消息返回，可用于判断是否全部返回
	}{}
	decoder := xml.NewDecoder(bytes.NewReader([]byte(req.Body())))
	decoder.CharsetReader = charset.NewReaderLabel
	err := decoder.Decode(temp)
	if err != nil {
		err = DecodeGbk(temp, []byte(req.Body()))
		if err != nil {
			nazalog.Error("decode catelog err:", err)
		}
	}
	if v, ok := Devices.Load(id); ok {
		d := v.(*Device)
		switch d.Status {
		case DeviceOfflineStatus, DeviceRecoverStatus:
			s.RecoverDevice(d, req)
			//go d.syncChannels(s.conf)
		case DeviceRegisterStatus:
			d.Status = DeviceOnlineStatus
		}
		d.UpdateTime = time.Now()

		var body string
		switch temp.CmdType {
		case "Keepalive":
			d.LastKeepaliveAt = time.Now()
			//callID !="" 说明是订阅的事件类型信息
			//if d.lastSyncTime.IsZero() {
			//	go d.syncChannels(s.conf)
			//}
		case "Catalog":
			d.UpdateChannels(s.conf, temp.DeviceList...)
		case "DeviceInfo":
			// 主设备信息
			d.Name = temp.DeviceName
			d.Manufacturer = temp.Manufacturer
			d.Model = temp.Model
		case "Alarm":
			d.Status = DeviceAlarmedStatus
			body = BuildAlarmResponseXML(d.ID)
		default:
			nazalog.Warn("Not supported CmdType, CmdType:", temp.CmdType, " body:", req.Body())
			response := sip.NewResponseFromRequest("", req, http.StatusBadRequest, "", "")
			tx.Respond(response)
			return
		}

		tx.Respond(sip.NewResponseFromRequest("", req, http.StatusOK, "OK", body))
	} else {
		if s.conf.QuickLogin {
			switch temp.CmdType {
			case "Keepalive":
				d := s.StoreDevice(id, req)
				d.LastKeepaliveAt = time.Now()
				tx.Respond(sip.NewResponseFromRequest("", req, http.StatusOK, "OK", ""))
				go d.syncChannels(s.conf)
				return
			}
		}

		nazalog.Warn("Unauthorized message, device not found, id:", id)
		tx.Respond(sip.NewResponseFromRequest("", req, http.StatusBadRequest, "device not found", ""))
	}
}

func (s *GB28181Server) OnNotify(req sip.Request, tx sip.ServerTransaction) {
	from, _ := req.From()
	id := from.Address.User().String()
	if v, ok := Devices.Load(id); ok {
		d := v.(*Device)
		d.UpdateTime = time.Now()
		temp := &struct {
			XMLName    xml.Name
			CmdType    string
			DeviceID   string
			Time       string           //位置订阅-GPS时间
			Longitude  string           //位置订阅-经度
			Latitude   string           //位置订阅-维度
			DeviceList []*notifyMessage `xml:"DeviceList>Item"` //目录订阅
		}{}
		decoder := xml.NewDecoder(bytes.NewReader([]byte(req.Body())))
		decoder.CharsetReader = charset.NewReaderLabel
		err := decoder.Decode(temp)
		if err != nil {
			err = DecodeGbk(temp, []byte(req.Body()))
			if err != nil {
				nazalog.Error("decode catelog failed, err:", err)
			}
		}
		var body string
		switch temp.CmdType {
		case "Catalog":
			//目录状态
			d.UpdateChannelStatus(temp.DeviceList, s.conf)
		case "MobilePosition":
			//更新channel的坐标
			d.UpdateChannelPosition(temp.DeviceID, temp.Time, temp.Longitude, temp.Latitude)
		default:
			nazalog.Warn("Not supported CmdType, cmdType:", temp.CmdType, " body:", req.Body())
			response := sip.NewResponseFromRequest("", req, http.StatusBadRequest, "", "")
			tx.Respond(response)
			return
		}

		tx.Respond(sip.NewResponseFromRequest("", req, http.StatusOK, "OK", body))
	} else {
		tx.Respond(sip.NewResponseFromRequest("", req, http.StatusBadRequest, "device not found", ""))
	}
}

func (s *GB28181Server) OnBye(req sip.Request, tx sip.ServerTransaction) {
	callIdStr := ""
	if callId, ok := req.CallID(); ok {
		callIdStr = callId.Value()
	}
	from, _ := req.From()
	devId := from.Address.User().String()
	if _d, ok := Devices.Load(devId); ok {
		d := _d.(*Device)
		d.channelMap.Range(func(key, value any) bool {
			ch := value.(*Channel)
			if ch.GetCallId() == callIdStr {
				ch.byeClear()
				return false
			}
			return true
		})
	}
	tx.Respond(sip.NewResponseFromRequest("", req, http.StatusOK, "OK", ""))
}

func (s *GB28181Server) StoreDevice(id string, req sip.Request) (d *Device) {
	from, _ := req.From()
	deviceAddr := sip.Address{
		DisplayName: from.DisplayName,
		Uri:         from.Address,
	}
	deviceIp := req.Source()
	if _d, ok := Devices.Load(id); ok {
		d = _d.(*Device)
		d.UpdateTime = time.Now()
		d.NetAddr = deviceIp
		d.addr = deviceAddr
		nazalog.Info("UpdateDevice, netaddr:", d.NetAddr)
	} else {
		servIp := req.Recipient().Host()

		sipIp := s.conf.SipIP
		mediaIp := s.conf.MediaConfig.MediaIp
		d = &Device{
			ID:           id,
			RegisterTime: time.Now(),
			UpdateTime:   time.Now(),
			Status:       DeviceRegisterStatus,
			addr:         deviceAddr,
			sipIP:        sipIp,
			mediaIP:      mediaIp,
			NetAddr:      deviceIp,
		}
		d.WithMediaServer(s)
		nazalog.Info("StoreDevice, deviceIp:", deviceIp, " serverIp:", servIp, " mediaIp:", mediaIp, " sipIP:", sipIp)
		Devices.Store(id, d)
	}

	return d
}

func (s *GB28181Server) RecoverDevice(d *Device, req sip.Request) {
	from, _ := req.From()
	d.addr = sip.Address{
		DisplayName: from.DisplayName,
		Uri:         from.Address,
	}
	deviceIp := req.Source()
	servIp := req.Recipient().Host()
	sipIp := s.conf.SipIP
	mediaIp := sipIp
	d.Status = DeviceRegisterStatus
	d.sipIP = sipIp
	d.mediaIP = mediaIp
	d.NetAddr = deviceIp
	d.UpdateTime = time.Now()

	nazalog.Info("RecoverDevice, deviceIp:", deviceIp, " serverIp:", servIp, " mediaIp:", mediaIp, " sipIP:", sipIp)
}

func RandNumString(n int) string {
	numbers := "0123456789"
	return randStringBySoure(numbers, n)
}

func RandString(n int) string {
	letterBytes := "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	return randStringBySoure(letterBytes, n)
}

// https://github.com/kpbird/golang_random_string
func randStringBySoure(src string, n int) string {
	randomness := make([]byte, n)

	rand.Seed(time.Now().UnixNano())
	_, err := rand.Read(randomness)
	if err != nil {
		panic(err)
	}

	l := len(src)

	// fill output
	output := make([]byte, n)
	for pos := range output {
		random := randomness[pos]
		randomPos := random % uint8(l)
		output[pos] = src[randomPos]
	}

	return string(output)
}

func DecodeGbk(v interface{}, body []byte) error {
	bodyBytes, err := GbkToUtf8(body)
	if err != nil {
		return err
	}
	decoder := xml.NewDecoder(bytes.NewReader(bodyBytes))
	decoder.CharsetReader = charset.NewReaderLabel
	err = decoder.Decode(v)
	return err
}

func GbkToUtf8(s []byte) ([]byte, error) {
	reader := transform.NewReader(bytes.NewReader(s), simplifiedchinese.GBK.NewDecoder())
	d, e := ioutil.ReadAll(reader)
	if e != nil {
		return s, e
	}
	return d, nil
}

type notifyMessage struct {
	ChannelInfo

	//状态改变事件 ON:上线,OFF:离线,VLOST:视频丢失,DEFECT:故障,ADD:增加,DEL:删除,UPDATE:更新(必选)
	Event string
}
