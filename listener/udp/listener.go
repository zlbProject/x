package udp

import (
	"net"
	"sync"

	"github.com/go-gost/core/limiter"
	"github.com/go-gost/core/listener"
	"github.com/go-gost/core/logger"
	md "github.com/go-gost/core/metadata"
	admission "github.com/go-gost/x/admission/wrapper"
	xnet "github.com/go-gost/x/internal/net"
	gudp "github.com/go-gost/x/internal/net/udp" // 使用别名，避免与包名冲突
	traffic_limiter "github.com/go-gost/x/limiter/traffic"
	limiter_wrapper "github.com/go-gost/x/limiter/traffic/wrapper"
	metrics "github.com/go-gost/x/metrics/wrapper"
	stats "github.com/go-gost/x/observer/stats/wrapper"
	"github.com/go-gost/x/registry"
)

func init() {
	registry.ListenerRegistry().Register("udp", NewListener)
}

type udpListener struct {
	ln         net.Listener
	packetConn net.PacketConn // 保存 PacketConn 的引用
	logger     logger.Logger
	md         metadata
	options    listener.Options
	closed     bool
	mu         sync.Mutex
}

func NewListener(opts ...listener.Option) listener.Listener {
	options := listener.Options{}
	for _, opt := range opts {
		opt(&options)
	}
	return &udpListener{
		logger:  options.Logger,
		options: options,
	}
}

func (l *udpListener) Init(md md.Metadata) (err error) {
	if err = l.parseMetadata(md); err != nil {
		return
	}

	network := "udp"
	if xnet.IsIPv4(l.options.Addr) {
		network = "udp4"
	}
	laddr, err := net.ResolveUDPAddr(network, l.options.Addr)
	if err != nil {
		return
	}

	var conn net.PacketConn
	conn, err = net.ListenUDP(network, laddr)
	if err != nil {
		return
	}
	conn = metrics.WrapPacketConn(l.options.Service, conn)
	conn = stats.WrapPacketConn(conn, l.options.Stats)
	conn = admission.WrapPacketConn(l.options.Admission, conn)
	conn = limiter_wrapper.WrapPacketConn(
		conn,
		l.options.TrafficLimiter,
		traffic_limiter.ServiceLimitKey,
		limiter.ScopeOption(limiter.ScopeService),
		limiter.ServiceOption(l.options.Service),
		limiter.NetworkOption(conn.LocalAddr().Network()),
	)

	l.packetConn = conn // 保存 PacketConn

	l.ln = gudp.NewListener(conn, &gudp.ListenConfig{ // 使用别名
		Backlog:        l.md.backlog,
		ReadQueueSize:  l.md.readQueueSize,
		ReadBufferSize: l.md.readBufferSize,
		Keepalive:      l.md.keepalive,
		TTL:            l.md.ttl,
		Logger:         l.logger,
	})
	return
}

func (l *udpListener) Accept() (conn net.Conn, err error) {
	return l.ln.Accept()
}

func (l *udpListener) Addr() net.Addr {
	return l.ln.Addr()
}

func (l *udpListener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return nil
	}
	l.closed = true

	err := l.ln.Close()
	if err != nil {
		l.logger.Warnf("close listener error: %v", err)
		//return err  //Don't return error, continue closing packetconn
	}

	// 关闭 PacketConn
	if l.packetConn != nil {
		err = l.packetConn.Close()
		if err != nil {
			l.logger.Warnf("close packetconn error: %v", err)
		}
	}

	return err
}
