package logic

// PublisherStat is the runtime traffic snapshot for a lalmax external publisher.
type PublisherStat struct {
	RemoteAddr    string
	ReadBytesSum  uint64
	WroteBytesSum uint64
}

// PublisherStatProvider exposes runtime traffic stats for ext_pub sessions.
type PublisherStatProvider interface {
	GetPublisherStat() PublisherStat
}
