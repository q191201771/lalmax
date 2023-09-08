package gb28181

import (
	"fmt"
	"math/rand"
	"strconv"
)

type InviteOptions struct {
	Start     int
	End       int
	ssrc      string
	SSRC      uint32
	MediaPort uint16
}

func (o InviteOptions) IsLive() bool {
	return o.Start == 0 || o.End == 0
}

func (o InviteOptions) String() string {
	return fmt.Sprintf("t=%d %d", o.Start, o.End)
}

func (o *InviteOptions) CreateSSRC(serial string) {
	ssrc := make([]byte, 10)
	ssrc[0] = '0'
	copy(ssrc[1:6], serial[3:8])
	randNum := 1000 + rand.Intn(8999)
	copy(ssrc[6:], strconv.Itoa(randNum))
	o.ssrc = string(ssrc)
	_ssrc, _ := strconv.ParseInt(o.ssrc, 10, 0)
	o.SSRC = uint32(_ssrc)
}
