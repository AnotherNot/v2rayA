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