package tcp

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/go-gost/core/limiter"
	"github.com/go-gost/core/listener"
	"github.com/go-gost/core/logger"
	md "github.com/go-gost/core/metadata"
	admission "github.com/go-gost/x/admission/wrapper"
	xnet "github.com/go-gost/x/internal/net"
	"github.com/go-gost/x/internal/net/proxyproto"
	climiter "github.com/go-gost/x/limiter/conn/wrapper"
	limiter_wrapper "github.com/go-gost/x/limiter/traffic/wrapper"
	metrics "github.com/go-gost/x/metrics/wrapper"
	stats "github.com/go-gost/x/observer/stats/wrapper"
	"github.com/go-gost/x/registry"
)

func init() {
	registry.ListenerRegistry().Register("tcp", NewListener)
}

type tcpListener struct {
	ln      net.Listener
	logger  logger.Logger
	md      metadata
	options listener.Options

	connChan  chan net.Conn         // 用于接收新连接的channel
	conns     map[net.Conn]struct{} // 用于存储所有连接
	mu        sync.Mutex            // 保护 conns map 的互斥锁
	closed    bool                  // 标记listener是否已经关闭
	closeChan chan struct{}
}

func NewListener(opts ...listener.Option) listener.Listener {
	options := listener.Options{}
	for _, opt := range opts {
		opt(&options)
	}
	return &tcpListener{
		logger:    options.Logger,
		options:   options,
		connChan:  make(chan net.Conn, 1024), // 可以调整channel的buffer大小
		conns:     make(map[net.Conn]struct{}),
		closeChan: make(chan struct{}),
	}
}

func (l *tcpListener) Init(md md.Metadata) (err error) {
	if err = l.parseMetadata(md); err != nil {
		return
	}

	network := "tcp"
	if xnet.IsIPv4(l.options.Addr) {
		network = "tcp4"
	}

	lc := net.ListenConfig{}
	if l.md.mptcp {
		lc.SetMultipathTCP(true)
		l.logger.Debugf("mptcp enabled: %v", lc.MultipathTCP())
	}
	ln, err := lc.Listen(context.Background(), network, l.options.Addr)
	if err != nil {
		return
	}

	l.logger.Debugf("pp: %d", l.options.ProxyProtocol)

	ln = proxyproto.WrapListener(l.options.ProxyProtocol, ln, 10*time.Second)
	ln = metrics.WrapListener(l.options.Service, ln)
	ln = stats.WrapListener(ln, l.options.Stats)
	ln = admission.WrapListener(l.options.Admission, ln)
	ln = limiter_wrapper.WrapListener(l.options.Service, ln, l.options.TrafficLimiter)
	ln = climiter.WrapListener(l.options.ConnLimiter, ln)
	l.ln = ln

	go l.acceptLoop() // 启动accept loop

	return
}

func (l *tcpListener) acceptLoop() {
	for {
		conn, err := l.ln.Accept()

		if l.closed {
			if conn != nil {
				conn.Close()
			}
			return
		}

		if err != nil {
			l.logger.Errorf("accept error: %v", err)
			select {
			case <-l.closeChan:
				return
			default:
				// 如果没有关闭信号，则短暂休眠后继续尝试
				time.Sleep(time.Millisecond * 100)
				continue
			}
		}

		select {
		case l.connChan <- conn:
		case <-l.closeChan:
			if conn != nil {
				conn.Close()
			}
			return
		}
	}
}

func (l *tcpListener) Accept() (conn net.Conn, err error) {
	select {
	case conn := <-l.connChan:
		l.mu.Lock()
		if l.closed {
			l.mu.Unlock()
			conn.Close()
			return nil, net.ErrClosed
		}
		l.conns[conn] = struct{}{}
		l.mu.Unlock()

		conn = limiter_wrapper.WrapConn(
			conn,
			l.options.TrafficLimiter,
			conn.RemoteAddr().String(),
			limiter.ScopeOption(limiter.ScopeConn),
			limiter.ServiceOption(l.options.Service),
			limiter.NetworkOption(conn.LocalAddr().Network()),
			limiter.SrcOption(conn.RemoteAddr().String()),
		)

		return conn, nil
	case <-l.closeChan:
		return nil, net.ErrClosed
	}
}

func (l *tcpListener) Addr() net.Addr {
	return l.ln.Addr()
}

func (l *tcpListener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return nil // 避免重复关闭
	}

	l.closed = true
	close(l.closeChan) // Signal to stop accept loop

	err := l.ln.Close()

	// 关闭所有连接
	for conn := range l.conns {
		conn.Close()
		delete(l.conns, conn)
	}

	return err
}
