package service

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/common/httpClient"
	"github.com/v2rayA/v2rayA/db/configure"
	"github.com/v2rayA/v2rayA/kernel/serverObj"
	"github.com/v2rayA/v2rayA/kernel/v2ray"
	"github.com/v2rayA/v2rayA/pkg/util/log"
)

// Draft throughput probe: one download per node through a temporary core,
// reported back and dropped. It mirrors TestHttpLatency's setup (a no-op
// merge of the two is future work) but measures bytes per second instead of
// time to first byte, and never persists the result.
const (
	// HttpSpeedTestURL answers arbitrary byte counts, so the download size
	// does not depend on a fixed fixture file.
	HttpSpeedTestURL = "https://speed.cloudflare.com/__down?bytes=10000000"
	// HttpSpeedMaxBytes caps one probe; the URL above is asked for 10 MiB.
	HttpSpeedMaxBytes = 10 << 20
)

// TestHttpSpeed downloads testURL (at most maxBytes) through each node and
// reports the throughput in which.Speed ("12.4 Mbit/s" or a latency-style
// error token). An empty testURL takes the default; non-positive maxBytes
// takes the default cap.
func TestHttpSpeed(which []*configure.Which, timeout time.Duration, maxParallel int, testURL string, maxBytes int64) ([]*configure.Which, error) {
	if testURL == "" {
		testURL = HttpSpeedTestURL
	} else if u, err := url.Parse(testURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, common.Coded("INVALID_TEST_URL", fmt.Errorf("test URL %q must be an HTTP or HTTPS URL with a host", testURL), map[string]interface{}{"testUrl": testURL})
	}
	if maxBytes <= 0 {
		maxBytes = HttpSpeedMaxBytes
	}
	var whiches = configure.NewWhiches(which)
	which = whiches.Get()
	for i := len(which) - 1; i >= 0; i-- {
		if which[i].TYPE == configure.SubscriptionType {
			which = append(which[:i], which[i+1:]...)
		}
	}
	if len(which) == 0 {
		return which, nil
	}
	v2rayRunning := v2ray.ProcessManager.Running()
	wg := new(sync.WaitGroup)
	vms := make([]serverObj.ServerObj, len(which))
	loc := configure.NewLocator()
	for i := range which {
		which[i].Speed = ""
		sr, err := loc.Locate(&which[i].NodeRef)
		if err != nil {
			which[i].Speed = err.Error()
			continue
		}
		vms[i] = sr.ServerObj
	}
	var (
		tmpl *v2ray.Template
		err  error
	)
	if v2rayRunning {
		tmpl, err = v2ray.NewTemplateFromConnectedServers(nil)
		if err != nil {
			if !errors.Is(err, v2ray.NoConnectedServerErr) {
				log.Warn("NewTemplateFromConnectedServers: %v", err)
			}
		}
	}
	if tmpl == nil {
		tmpl = v2ray.NewEmptyTemplate(&configure.Setting{
			RulePortMode: configure.WhitelistMode,
			TcpFastOpen:  configure.Default,
			MuxOn:        configure.No,
			Transparent:  configure.TransparentClose,
		})
		tmpl.SetAPI(nil)
	}
	handedOver := false
	defer func() {
		if !handedOver {
			_ = tmpl.Close()
		}
	}()
	inboundPortMap := make([]string, len(vms))
	pluginPortMap := make(map[int]int)
	listenAddr := "127.0.0.1"
	if tmpl.Setting != nil && tmpl.Setting.PortSharing {
		listenAddr = "0.0.0.0"
	}
	var toClose []io.Closer
	defer func() {
		for _, l := range toClose {
			_ = l.Close()
		}
	}()
	for i, v := range vms {
		if which[i].Speed != "" {
			continue
		}
		t := time.Now()
		var port int
		for {
			l, err := net.Listen("tcp", listenAddr+":0")
			if err == nil {
				port = l.Addr().(*net.TCPAddr).Port
				toClose = append(toClose, l)
				l2, err2 := net.ListenPacket("udp", listenAddr+":"+strconv.Itoa(port))
				if err2 == nil {
					toClose = append(toClose, l2)
					break
				}
			}
			if time.Since(t) > 3*time.Second {
				return nil, fmt.Errorf("could not find a free local port for the speed test within 3 s")
			}
		}
		v2rayInboundPort := strconv.Itoa(port)
		pluginPort := 0
		if v.NeedPluginPort() {
			for {
				l, err := net.Listen("tcp", "127.0.0.1:0")
				if err == nil {
					toClose = append(toClose, l)
					port = l.Addr().(*net.TCPAddr).Port
					l2, err2 := net.ListenPacket("udp", "127.0.0.1:"+strconv.Itoa(port))
					if err2 == nil {
						toClose = append(toClose, l2)
						break
					}
				}
				if time.Since(t) > 3*time.Second {
					return nil, fmt.Errorf("could not find a free local port for the speed test within 3 s")
				}
			}
			pluginPort = port
			pluginPortMap[i] = port
		}
		err := tmpl.InsertMappingOutbound(v, v2rayInboundPort, false, pluginPort, "socks")
		if err != nil {
			if strings.Contains(err.Error(), "unsupported") {
				which[i].Speed = "UNSUPPORTED PROTOCOL"
				continue
			}
			return nil, err
		}
		tmpl.Inbounds[len(tmpl.Inbounds)-1].Listen = listenAddr
		inboundPortMap[i] = v2rayInboundPort
	}
	for _, l := range toClose {
		_ = l.Close()
	}
	toClose = nil
	time.Sleep(30 * time.Millisecond)
	tmpl.Routing.DomainStrategy = "AsIs"
	addHosts(tmpl, vms)
	tmpl.SetOutboundSockopt()
	v2ray.ProcessManager.SetLatencyTesting(true)
	handedOver = true
	if err := v2ray.ProcessManager.Start(tmpl); err != nil {
		v2ray.ProcessManager.SetLatencyTesting(false)
		if v2rayRunning && configure.GetConnectedServers() != nil {
			if restoreErr := v2ray.UpdateV2RayConfig(); restoreErr != nil {
				return nil, fmt.Errorf("%v; cannot restart v2ray-core: %w", err, restoreErr)
			}
		}
		return nil, err
	}
	wg = new(sync.WaitGroup)
	cc := make(chan interface{}, maxParallel)
	for i := range which {
		if which[i].Speed != "" {
			log.Warn("Error[%v]%v: %v", i+1, which[i].Speed, which[i].Link)
			continue
		}
		wg.Add(1)
		go func(i int) {
			cc <- nil
			defer func() { <-cc; wg.Done() }()
			httpSpeed(which[i], inboundPortMap[i], timeout, testURL, maxBytes)
			log.Info("Speed done[%v]%v: %v", i+1, which[i].Speed, which[i].Link)
		}(i)
	}
	wg.Wait()
	v2ray.ProcessManager.SetLatencyTesting(false)
	if v2rayRunning && configure.GetConnectedServers() != nil {
		if err := v2ray.UpdateV2RayConfig(); err != nil {
			return which, fmt.Errorf("cannot restart v2ray-core: %w", err)
		}
	} else {
		v2ray.ProcessManager.Stop(true)
	}
	if err := configure.NewWhiches(which).SaveSpeeds(); err != nil {
		return nil, fmt.Errorf("failed to save the speed test result: %v", err)
	}
	return which, nil
}

func httpSpeed(which *configure.Which, port string, timeout time.Duration, testURL string, maxBytes int64) {
	c, err := httpClient.GetHttpClientWithProxy("socks5://127.0.0.1:" + port)
	if err != nil {
		which.Speed = "SYSTEM ERROR"
		return
	}
	defer c.CloseIdleConnections()
	c.Timeout = timeout
	t := time.Now()
	req, err := http.NewRequest("GET", testURL, nil)
	if err != nil {
		which.Speed = "SYSTEM ERROR"
		return
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "close")
	req.Header.Set("User-Agent", "curl/7.70.0")
	resp, err := c.Do(req)
	if err != nil {
		which.Speed = speedErrorToken(err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		which.Speed = "BAD RESPONSE"
		return
	}
	n, err := io.Copy(io.Discard, io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		which.Speed = speedErrorToken(err)
		return
	}
	secs := time.Since(t).Seconds()
	if secs <= 0 {
		which.Speed = "NOT STABLE"
		return
	}
	mbit := float64(n) * 8 / secs / 1e6
	which.Speed = fmt.Sprintf("%.1f Mbit/s", mbit)
}

func speedErrorToken(err error) string {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "TIMEOUT"
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return "NOT STABLE"
	}
	return "PROXY ERROR"
}
