package where

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/conf"
	"golang.org/x/sync/singleflight"
)

// Variant identifies the core binary type.
// Since v2rayA now only supports v2raya_core, this is always V2rayaCore.
type Variant string

const (
	// V2rayaCore is the merged v2raya-core binary (xray-core + MultiObservatory).
	// Binary name: v2raya_core
	V2rayaCore Variant = "V2rayaCore"
)

var NotFoundErr = fmt.Errorf("not found")
var ServiceNameList = []string{"v2raya_core"}
var v2rayVersion struct {
	version    string
	binPath    string
	lastUpdate time.Time
	lastErr    error
	mu         sync.Mutex
	flight     singleflight.Group
}

// cachedVersion reports the last result while it is younger than 10 s. A
// failure is cached for the same window: a missing or unresponsive core must
// not turn every request into another exec, which is what made the
// unauthenticated /api/version a queue of processes.
func cachedVersion() (string, error, bool) {
	v2rayVersion.mu.Lock()
	defer v2rayVersion.mu.Unlock()
	if time.Since(v2rayVersion.lastUpdate) < 10*time.Second {
		return v2rayVersion.version, v2rayVersion.lastErr, true
	}
	return "", nil, false
}

// GetV2rayServiceVersion returns the version string of the v2raya_core binary.
func GetV2rayServiceVersion() (variant Variant, ver string, err error) {
	if v, cachedErr, ok := cachedVersion(); ok {
		return V2rayaCore, v, cachedErr
	}
	// Concurrent cache misses share one exec instead of queueing on the
	// cache mutex and running the binary again one by one.
	value, err, _ := v2rayVersion.flight.Do("version", func() (interface{}, error) {
		if v, cachedErr, ok := cachedVersion(); ok {
			return v, cachedErr
		}
		version, binPath, err := readV2rayServiceVersion()
		v2rayVersion.mu.Lock()
		v2rayVersion.lastUpdate = time.Now()
		if err != nil {
			v2rayVersion.version = ""
			v2rayVersion.lastErr = err
		} else {
			v2rayVersion.version = version
			v2rayVersion.binPath = binPath
			v2rayVersion.lastErr = nil
		}
		v2rayVersion.mu.Unlock()
		return version, err
	})
	if err != nil {
		return V2rayaCore, "", err
	}
	ver, _ = value.(string)
	return V2rayaCore, ver, nil
}

func readV2rayServiceVersion() (ver string, binPath string, err error) {
	v2rayPath, err := GetV2rayBinPath()
	if err != nil {
		return "", "", err
	}
	if len(v2rayPath) <= 0 {
		return "", "", common.Coded("CORE_NOT_FOUND", fmt.Errorf("%w: v2raya_core executable not found next to v2rayA or in PATH; set --v2ray-bin", NotFoundErr), nil)
	}

	// Get version from binary. The deadline is enforced by the context rather
	// than by a watchdog goroutine: a watchdog started before Start can find
	// cmd.Process still nil, and it is never cancelled, so a slow core both
	// escaped the kill and leaked the goroutine on every call.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, v2rayPath, "version")
	cmd.WaitDelay = 100 * time.Millisecond
	output := bytes.NewBuffer(nil)
	cmd.Stdout = output
	cmd.Stderr = output
	runErr := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", "", fmt.Errorf("%v did not report a version within 5 s", v2rayPath)
	}

	fields := strings.Fields(strings.TrimSpace(output.String()))
	if len(fields) < 2 {
		if runErr != nil {
			return "", "", fmt.Errorf("%v version failed: %w", v2rayPath, runErr)
		}
		return "", "", fmt.Errorf("cannot parse version from output: %q", output.String())
	}
	return fields[1], v2rayPath, nil
}

func GetV2rayBinPath() (string, error) {
	v2rayBinPath := conf.GetEnvironmentConfig().V2rayBin
	if v2rayBinPath == "" {
		return getV2rayBinPathAnyway()
	}
	return v2rayBinPath, nil
}

func getV2rayBinPathAnyway() (path string, err error) {
	for _, target := range ServiceNameList {
		if path, err = getV2rayBinPath(target); err == nil {
			return
		}
	}
	return "", common.Coded("CORE_NOT_FOUND", fmt.Errorf("%w: v2raya_core executable not found next to v2rayA or in PATH; set --v2ray-bin", NotFoundErr), nil)
}

func getV2rayBinPath(target string) (string, error) {
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(target), ".exe") {
		target += ".exe"
	}
	// 先从可执行文件同目录下查找
	if exe, err := os.Executable(); err == nil {
		pa := filepath.Join(filepath.Dir(exe), target)
		if _, err := os.Stat(pa); err == nil {
			return pa, nil
		}
	}
	// 再从 PATH 环境变量里查找
	if pa, err := exec.LookPath(target); err == nil {
		return pa, nil
	}
	return "", NotFoundErr
}
