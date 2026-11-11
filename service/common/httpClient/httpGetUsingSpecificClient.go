package httpClient

import (
	"errors"
	"fmt"
	"net/http"
	neturl "net/url"

	"github.com/v2rayA/v2rayA/common/netguard"
	"github.com/v2rayA/v2rayA/common/redact"
	"github.com/v2rayA/v2rayA/conf"
)

func HttpGetUsingSpecificClient(c *http.Client, url string) (resp *http.Response, err error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return
	}
	//shadowrocket会有可能不清楚alterid的情况，影响aead是否启用的问题
	req.Header.Set("User-Agent", fmt.Sprintf("v2rayA/%v WebRequestHelper", conf.Version))
	// netguard.WrapClient resolves the target and refuses loopback, private,
	// link-local and reserved addresses before the transport dials; the dialer
	// then connects to the address that was checked. CodeQL cannot follow the
	// wrapper, so the alert is closed with this guard named in its reason.
	// codeql[go/request-forgery]
	if resp, err = netguard.WrapClient(c).Do(req); err != nil {
		return nil, fmt.Errorf("could not fetch through the selected proxy mode (%w); set the mode to direct or start the core", redact.Error(err))
	}
	if resp == nil {
		cause := err
		var urlErr *neturl.Error
		if errors.As(cause, &urlErr) {
			cause = urlErr.Err
		}
		if cause == nil {
			cause = errors.New("request returned no response")
		}
		return nil, fmt.Errorf("could not reach %s: %w", req.URL.Hostname(), cause)
	}
	return
}
