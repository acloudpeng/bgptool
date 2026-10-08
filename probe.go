package main

import (
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"os"
	"sync"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// reply is the answer to one probe.
type reply struct {
	ttl   int
	from  netip.Addr
	rtt   time.Duration
	final bool // the destination answered (echo reply / unreachable from the target)
}

type probe struct {
	ttl  int
	sent time.Time
}

// prober sends ICMP echo requests with a chosen TTL / hop limit and matches the answers
// (time exceeded from routers, echo reply from the target) by echo id and sequence.
type prober struct {
	dst     netip.Addr
	v6      bool
	conn    *icmp.PacketConn
	id      int
	mu      sync.Mutex
	seq     uint16
	pending map[uint16]probe
	replies chan reply
	done    chan struct{}
}

var errPermission = errors.New("permission")

func newProber(dst netip.Addr) (*prober, error) {
	p := &prober{dst: dst, v6: dst.Is6(), id: os.Getpid() & 0xffff, pending: map[uint16]probe{},
		replies: make(chan reply, 256), done: make(chan struct{})}
	network, addr := "ip4:icmp", "0.0.0.0"
	if p.v6 {
		network, addr = "ip6:ipv6-icmp", "::"
	}
	c, err := icmp.ListenPacket(network, addr)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return nil, errPermission
		}
		return nil, err
	}
	p.conn = c
	go p.readLoop()
	return p, nil
}

func (p *prober) close() {
	close(p.done)
	p.conn.Close()
}

// send transmits one probe with the given TTL.
func (p *prober) send(ttl int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seq++
	seq := p.seq
	var typ icmp.Type = ipv4.ICMPTypeEcho
	if p.v6 {
		typ = ipv6.ICMPTypeEchoRequest
		if err := p.conn.IPv6PacketConn().SetHopLimit(ttl); err != nil {
			return err
		}
	} else if err := p.conn.IPv4PacketConn().SetTTL(ttl); err != nil {
		return err
	}
	payload := make([]byte, 24)
	copy(payload, "bgptool traceroute probe")
	msg := icmp.Message{Type: typ, Body: &icmp.Echo{ID: p.id, Seq: int(seq), Data: payload}}
	b, err := msg.Marshal(nil) // the kernel fills in the ICMPv6 checksum
	if err != nil {
		return err
	}
	p.pending[seq] = probe{ttl: ttl, sent: time.Now()}
	_, err = p.conn.WriteTo(b, &net.IPAddr{IP: p.dst.AsSlice()})
	return err
}

// expire forgets probes older than d (they count as lost).
func (p *prober) expire(d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for s, pr := range p.pending {
		if time.Since(pr.sent) > d {
			delete(p.pending, s)
		}
	}
}

func (p *prober) readLoop() {
	buf := make([]byte, 1500)
	proto := 1
	if p.v6 {
		proto = 58
	}
	for {
		n, from, err := p.conn.ReadFrom(buf)
		now := time.Now()
		if err != nil {
			select {
			case <-p.done:
				return
			default:
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return
		}
		msg, err := icmp.ParseMessage(proto, buf[:n])
		if err != nil {
			continue
		}
		var id, seq int
		final := false
		switch body := msg.Body.(type) {
		case *icmp.Echo:
			if msg.Type != ipv4.ICMPTypeEchoReply && msg.Type != ipv6.ICMPTypeEchoReply {
				continue
			}
			id, seq, final = body.ID, body.Seq, true
		case *icmp.TimeExceeded:
			id, seq = p.inner(body.Data)
		case *icmp.DstUnreach:
			id, seq = p.inner(body.Data)
			final = true
		default:
			continue
		}
		if id != p.id {
			continue // another program's ping
		}
		p.mu.Lock()
		pr, ok := p.pending[uint16(seq)]
		delete(p.pending, uint16(seq))
		p.mu.Unlock()
		if !ok {
			continue
		}
		addr, _ := netip.AddrFromSlice(from.(*net.IPAddr).IP)
		addr = addr.Unmap()
		select {
		case p.replies <- reply{ttl: pr.ttl, from: addr, rtt: now.Sub(pr.sent), final: final && addr == p.dst}:
		case <-p.done:
			return
		}
	}
}

// inner extracts the echo id / sequence of our probe quoted in an ICMP error.
func (p *prober) inner(data []byte) (id, seq int) {
	off := 40 // IPv6 header
	if !p.v6 {
		if len(data) < 1 {
			return -1, 0
		}
		off = int(data[0]&0x0f) * 4
	}
	if len(data) < off+8 {
		return -1, 0
	}
	return int(binary.BigEndian.Uint16(data[off+4:])), int(binary.BigEndian.Uint16(data[off+6:]))
}
