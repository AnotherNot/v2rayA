package approuter

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

// socks5Dial opens a CONNECT through the local SOCKS inbound of a class. That
// inbound is where the core decides which outbound the flow leaves through, so
// the relay never needs to know what the class means.
func socks5Dial(proxy string, dst net.IP, dstPort int) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", proxy, 5*time.Second)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			conn.Close()
		}
	}()
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return nil, err
	}
	// Greeting: version 5, one method, "no authentication". The inbound is on
	// loopback and carries no credentials; a different method list would be
	// refused by the core.
	if _, err := conn.Write([]byte{5, 1, 0}); err != nil {
		return nil, err
	}
	var greeting [2]byte
	if _, err := io.ReadFull(conn, greeting[:]); err != nil {
		return nil, err
	}
	if greeting[0] != 5 || greeting[1] != 0 {
		return nil, fmt.Errorf("the class inbound at %s did not accept a no-authentication greeting: %v", proxy, greeting)
	}
	request := []byte{5, 1, 0}
	if v4 := dst.To4(); v4 != nil {
		request = append(request, 1)
		request = append(request, v4...)
	} else if v16 := dst.To16(); v16 != nil {
		request = append(request, 4)
		request = append(request, v16...)
	} else {
		return nil, fmt.Errorf("%v is not an IP address", dst)
	}
	request = binary.BigEndian.AppendUint16(request, uint16(dstPort))
	if _, err := conn.Write(request); err != nil {
		return nil, err
	}
	// Reply: version, status, reserved, address type, address, port. The bound
	// address is of no use here, only the status is.
	var head [4]byte
	if _, err := io.ReadFull(conn, head[:]); err != nil {
		return nil, err
	}
	if head[0] != 5 || head[1] != 0 {
		return nil, fmt.Errorf("the class inbound at %s refused %s: status %d",
			proxy, net.JoinHostPort(dst.String(), strconv.Itoa(dstPort)), head[1])
	}
	var skip int
	switch head[3] {
	case 1:
		skip = 4
	case 4:
		skip = 16
	case 3:
		var length [1]byte
		if _, err := io.ReadFull(conn, length[:]); err != nil {
			return nil, err
		}
		skip = int(length[0])
	default:
		return nil, fmt.Errorf("the class inbound at %s answered with address type %d", proxy, head[3])
	}
	if _, err := io.CopyN(io.Discard, conn, int64(skip+2)); err != nil {
		return nil, err
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return nil, err
	}
	ok = true
	return conn, nil
}

// splice copies in both directions and closes each direction when its copy is
// done. A direction that is not closed makes a client reading to EOF wait
// forever, and so does the relay.
func splice(a, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(a, b); closeWrite(a); done <- struct{}{} }()
	go func() { _, _ = io.Copy(b, a); closeWrite(b); done <- struct{}{} }()
	<-done
	<-done
}

func closeWrite(c net.Conn) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
		return
	}
	_ = c.Close()
}

// udpAssoc is a SOCKS5 UDP association the relay holds open for one flow. ctrl
// is the TCP control connection, which the association lives and dies with;
// conn is the connected UDP socket the framed datagrams travel on; dst is the
// single destination this flow's datagrams name.
type udpAssoc struct {
	ctrl    net.Conn
	conn    *net.UDPConn
	dst     net.IP
	dstPort int
}

// socks5UDPAssociate asks the proxy at proxyAddr to relay UDP for dst. RFC 1928
// wants the client to name the address it will send from; the relay does not
// know it before the first datagram, so it names 0.0.0.0:0 and lets the proxy
// learn it from the datagram, which every real implementation does.
func socks5UDPAssociate(proxyAddr string, dst net.IP, dstPort int) (*udpAssoc, error) {
	c, err := net.DialTimeout("tcp", proxyAddr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			c.Close()
		}
	}()
	if err := c.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return nil, err
	}
	if _, err := c.Write([]byte{5, 1, 0}); err != nil {
		return nil, err
	}
	var greeting [2]byte
	if _, err := io.ReadFull(c, greeting[:]); err != nil {
		return nil, err
	}
	if greeting[0] != 5 || greeting[1] != 0 {
		return nil, fmt.Errorf("the class inbound at %s did not accept a no-authentication greeting: %v", proxyAddr, greeting)
	}
	// CMD 3 is UDP ASSOCIATE; the address is the one the client will send
	// from, which it cannot know before the first datagram.
	if _, err := c.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return nil, err
	}
	var head [4]byte
	if _, err := io.ReadFull(c, head[:]); err != nil {
		return nil, err
	}
	if head[0] != 5 || head[1] != 0 {
		return nil, fmt.Errorf("the class inbound at %s refused the association: version %d, status %d", proxyAddr, head[0], head[1])
	}
	host, port, err := readSocksAddress(c, head[3])
	if err != nil {
		return nil, err
	}
	// A proxy may answer 0.0.0.0:<port>, meaning "the address you already
	// reached me at".
	if host == "0.0.0.0" || host == "::" {
		if h, _, err := net.SplitHostPort(proxyAddr); err == nil {
			host = h
		}
	}
	bnd, err := net.ResolveUDPAddr("udp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	uc, err := net.DialUDP("udp", nil, bnd)
	if err != nil {
		return nil, err
	}
	if err := c.SetDeadline(time.Time{}); err != nil {
		uc.Close()
		return nil, err
	}
	ok = true
	return &udpAssoc{ctrl: c, conn: uc, dst: dst, dstPort: dstPort}, nil
}

// readSocksAddress reads the address type, address and port that follow a
// SOCKS5 request or reply header.
func readSocksAddress(c io.Reader, atyp byte) (string, int, error) {
	var host string
	switch atyp {
	case 1:
		b := make([]byte, 4)
		if _, err := io.ReadFull(c, b); err != nil {
			return "", 0, err
		}
		host = net.IP(b).String()
	case 4:
		b := make([]byte, 16)
		if _, err := io.ReadFull(c, b); err != nil {
			return "", 0, err
		}
		host = net.IP(b).String()
	case 3:
		var n [1]byte
		if _, err := io.ReadFull(c, n[:]); err != nil {
			return "", 0, err
		}
		b := make([]byte, int(n[0]))
		if _, err := io.ReadFull(c, b); err != nil {
			return "", 0, err
		}
		host = string(b)
	default:
		return "", 0, fmt.Errorf("the class inbound answered with address type %d", atyp)
	}
	var portBytes [2]byte
	if _, err := io.ReadFull(c, portBytes[:]); err != nil {
		return "", 0, err
	}
	return host, int(binary.BigEndian.Uint16(portBytes[:])), nil
}

// send frames one datagram the way RFC 1928 carries it inside an association.
func (a *udpAssoc) send(payload []byte) error {
	_, err := a.conn.Write(wrapUDP(a.dst, a.dstPort, payload))
	return err
}

// receive returns one payload from the association, unwrapped. A datagram that
// cannot be parsed is not an answer and is skipped.
func (a *udpAssoc) receive(buf []byte) (int, error) {
	for {
		n, err := a.conn.Read(buf)
		if err != nil {
			return 0, err
		}
		payload, err := unwrapUDP(buf[:n])
		if err != nil {
			continue
		}
		copy(buf, payload)
		return len(payload), nil
	}
}

func (a *udpAssoc) close() {
	a.conn.Close()
	a.ctrl.Close()
}

// wrapUDP builds the header RFC 1928 puts in front of a datagram: two reserved
// bytes, a fragment number (0: the relay never fragments), then the
// destination.
func wrapUDP(dst net.IP, port int, payload []byte) []byte {
	buf := []byte{0, 0, 0}
	if v4 := dst.To4(); v4 != nil {
		buf = append(buf, 1)
		buf = append(buf, v4...)
	} else {
		buf = append(buf, 4)
		buf = append(buf, dst.To16()...)
	}
	buf = binary.BigEndian.AppendUint16(buf, uint16(port))
	return append(buf, payload...)
}

// unwrapUDP returns the payload of a datagram read from an association. A
// fragment other than 0 is refused: the relay never asks for fragmentation, so
// such a datagram cannot be an answer to what it sent.
func unwrapUDP(datagram []byte) ([]byte, error) {
	if len(datagram) < 4 || datagram[2] != 0 {
		return nil, fmt.Errorf("not one unfragmented datagram")
	}
	offset := 4
	switch datagram[3] {
	case 1:
		offset += 4
	case 4:
		offset += 16
	case 3:
		if len(datagram) < offset+1 {
			return nil, fmt.Errorf("truncated domain length")
		}
		offset += 1 + int(datagram[offset])
	default:
		return nil, fmt.Errorf("address type %d", datagram[3])
	}
	offset += 2 // port
	if len(datagram) < offset {
		return nil, fmt.Errorf("truncated datagram")
	}
	return datagram[offset:], nil
}