package service

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/tidwall/gjson"
	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/conf"
)

func CheckUpdate() (foundNew bool, remoteVersion string, err error) {
	resp, err := http.Get("https://api.github.com/repos/AnotherNot/v2rayA/releases/latest")
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, "", fmt.Errorf("the release feed answered HTTP %s", resp.Status)
	}
	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return
	}
	tag := gjson.GetBytes(buf.Bytes(), "tag_name").String()
	if tag == "" {
		return false, "", fmt.Errorf("the release feed names no tag")
	}
	// Remote version obtained
	ge, err := common.VersionGreaterEqual(conf.Version, tag)
	return !ge, tag, err
}
