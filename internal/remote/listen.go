package remote

import (
	"bufio"
	"crypto/tls"
	"net"
	"sync"
	"time"
)

// mixedListener takes both HTTPS and plain HTTP on one port: connections
// that open with a TLS handshake come out of Accept as *tls.Conn, so
// http.Server serves them as HTTPS, and the rest come out as they are. The
// handler tells them apart by Request.TLS. Let's Encrypt's http-01 check
// is the one thing served without TLS.
type mixedListener struct {
	net.Listener
	config *tls.Config
	conns  chan net.Conn
	errc   chan error
	once   sync.Once
	done   chan struct{}
}

func newMixedListener(l net.Listener, config *tls.Config) *mixedListener {
	m := &mixedListener{Listener: l, config: config, conns: make(chan net.Conn), errc: make(chan error, 1), done: make(chan struct{})}
	go m.accept()
	return m
}

func (m *mixedListener) accept() {
	for {
		c, err := m.Listener.Accept()
		if err != nil {
			select {
			case m.errc <- err:
			case <-m.done:
			}
			return
		}
		// Waiting for a slow client's first byte mustn't hold up the rest.
		go m.sort(c)
	}
}

func (m *mixedListener) sort(c net.Conn) {
	c.SetReadDeadline(time.Now().Add(10 * time.Second))
	br := bufio.NewReader(c)
	first, err := br.Peek(1)
	c.SetReadDeadline(time.Time{})
	if err != nil {
		c.Close()
		return
	}
	var out net.Conn = &peekedConn{c, br}
	if first[0] == 0x16 { // a TLS handshake record
		out = tls.Server(out, m.config)
	}
	select {
	case m.conns <- out:
	case <-m.done:
		c.Close()
	}
}

func (m *mixedListener) Accept() (net.Conn, error) {
	select {
	case c := <-m.conns:
		return c, nil
	case err := <-m.errc:
		return nil, err
	case <-m.done:
		return nil, net.ErrClosed
	}
}

func (m *mixedListener) Close() error {
	m.once.Do(func() { close(m.done) })
	return m.Listener.Close()
}

// peekedConn reads back the bytes sorting the connection looked at first.
type peekedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *peekedConn) Read(p []byte) (int, error) { return c.r.Read(p) }
