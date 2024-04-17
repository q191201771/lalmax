package hook

import "github.com/q191201771/lal/pkg/base"

// GopCache gop cache
type GopCache struct {
	videoheader *base.RtmpMsg
	audioheader *base.RtmpMsg

	gopSize              int
	singleGopMaxFrameNum int

	data  []Gop
	first int
	last  int
}

// NewGopCache 创建 gop 缓存
func NewGopCache(gopSize, singleGopMaxFrameNum int) *GopCache {
	num := gopSize + 1
	return &GopCache{
		data:                 make([]Gop, gopSize),
		gopSize:              num,
		singleGopMaxFrameNum: singleGopMaxFrameNum,
	}
}

// Feed 写入缓存
func (c *GopCache) Feed(msg base.RtmpMsg) {
	switch msg.Header.MsgTypeId {
	case base.RtmpTypeIdMetadata:
		return
	case base.RtmpTypeIdAudio:
		if codecID := msg.AudioCodecId(); msg.IsAacSeqHeader() ||
			codecID == base.RtmpSoundFormatG711A ||
			codecID == base.RtmpSoundFormatG711U {
			c.audioheader = &msg
			return
		}
	case base.RtmpTypeIdVideo:
		if msg.IsVideoKeySeqHeader() {
			c.videoheader = &msg
			return
		}
	}

	if c.gopSize > 1 {
		if msg.IsVideoKeyNalu() {
			c.feedNewGop(msg)
		} else {
			c.feedLastGop(msg)
		}
	}
	return
}

func (c *GopCache) feedNewGop(msg base.RtmpMsg) {
	if c.isGopRingFull() {
		c.first = (c.first + 1) % c.gopSize
	}
	c.data[c.last].Clear()
	c.data[c.last].feed(msg)
	c.last = (c.last + 1) % c.gopSize
}

func (c *GopCache) feedLastGop(msg base.RtmpMsg) {
	if c.isGopRingEmpty() {
		return
	}
	idx := (c.last - 1 + c.gopSize) % c.gopSize
	if c.singleGopMaxFrameNum == 0 || c.data[idx].len() <= c.singleGopMaxFrameNum {
		c.data[idx].feed(msg)
	}
}

func (c *GopCache) isGopRingFull() bool {
	return (c.last+1)%c.gopSize == c.first
}
func (c *GopCache) isGopRingEmpty() bool {
	return c.first == c.last
}

func (c *GopCache) Clear() {
	c.audioheader = nil
	c.videoheader = nil
	c.last = 0
	c.first = 0
}

func (c *GopCache) GetGopCount() int {
	return (c.last + c.gopSize - c.first) % c.gopSize
}

func (c *GopCache) GetGopDataAt(pos int) []base.RtmpMsg {
	if pos >= c.GetGopCount() || pos < 0 {
		return nil
	}
	return c.data[(c.first+pos)%c.gopSize].data
}

type Gop struct {
	data []base.RtmpMsg
}

func (g *Gop) feed(msg base.RtmpMsg) {
	g.data = append(g.data, msg)
}

func (g *Gop) Clear() {
	g.data = g.data[:0]
}
func (g *Gop) len() int {
	return len(g.data)
}
