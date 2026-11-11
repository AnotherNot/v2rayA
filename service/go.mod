module github.com/v2rayA/v2rayA

go 1.26.10

require (
	github.com/adrg/xdg v0.5.3
	github.com/beevik/ntp v0.3.0
	github.com/cilium/ebpf v0.22.0
	github.com/devfeel/mapper v0.7.14
	github.com/gin-contrib/cors v1.7.11
	github.com/gin-gonic/gin v1.12.0
	github.com/go-leo/slicex v1.0.14
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/gorilla/websocket v1.5.3
	github.com/json-iterator/go v1.1.12
	github.com/mattn/go-sqlite3 v1.14.53
	github.com/miekg/dns v1.1.73
	github.com/mohae/deepcopy v0.0.0-20170929034955-c48cc78d4826
	github.com/muhammadmuzzammil1998/jsonc v0.0.0-20201229145248-615b0916ca38
	github.com/pkg/errors v0.9.1
	github.com/shirou/gopsutil/v3 v3.24.5
	github.com/stevenroose/gonfig v0.1.5
	github.com/tidwall/gjson v1.20.1
	github.com/v2fly/v2ray-core/v5 v5.7.0
	github.com/v2rayA/RoutingA v1.0.2
	github.com/v2rayA/beego/v2 v2.0.7
	github.com/v2rayA/v2rayA-lib4 v0.0.0-20230812094818-595f87cb2a49
	github.com/vearutop/statigz v1.5.0
	go.etcd.io/bbolt v1.5.0
	golang.org/x/crypto v0.58.0
	golang.org/x/net v0.62.0
	golang.org/x/sys v0.50.0
	golang.zx2c4.com/wireguard/windows v1.1.1
	google.golang.org/grpc v1.85.0
	google.golang.org/protobuf v1.36.12
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.62.0
)

require (
	github.com/bytedance/gopkg v0.1.4 // indirect
	github.com/bytedance/sonic/loader v0.5.1 // indirect
	github.com/cloudwego/base64x v0.1.7 // indirect
	github.com/goccy/go-yaml v1.19.2 // indirect
	github.com/quic-go/qpack v0.6.0 // indirect
	github.com/quic-go/quic-go v0.60.0 // indirect
	github.com/tklauser/go-sysconf v0.3.12 // indirect
	github.com/tklauser/numcpus v0.6.1 // indirect
	go.mongodb.org/mongo-driver/v2 v2.8.0 // indirect
)

require (
	github.com/bytedance/sonic v1.15.2 // indirect
	github.com/djherbis/times v1.5.0 // indirect
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/gabriel-vasile/mimetype v1.4.13 // indirect
	github.com/gin-contrib/sse v1.1.1 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.30.3 // indirect
	github.com/gocarina/gocsv v0.0.0-20210408192840-02d7211d929d // indirect
	github.com/goccy/go-json v0.10.6 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7
	github.com/klauspost/cpuid/v2 v2.4.0 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/lufia/plan9stats v0.0.0-20211012122336-39d0f177ccd0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/ncruces/go-strftime v1.1.0 // indirect
	github.com/pelletier/go-toml v1.9.5 // indirect
	github.com/pelletier/go-toml/v2 v2.4.3 // indirect
	github.com/pires/go-proxyproto v0.7.0 // indirect
	github.com/power-devops/perfstat v0.0.0-20210106213030-5aafc221ea8c // indirect
	github.com/refraction-networking/utls v1.8.2 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/shiena/ansicolor v0.0.0-20200904210342-c7312218db18 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.0 // indirect
	github.com/tidwall/sjson v1.2.3 // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	github.com/ugorji/go/codec v1.3.1 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	go4.org/netipx v0.0.0-20231129151722-fdeea329fbba // indirect
	golang.org/x/arch v0.29.0 // indirect
	golang.org/x/exp v0.0.0-20250305212735-054e65f0b394 // indirect
	golang.org/x/sync v0.25.0
	golang.org/x/text v0.43.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260817212433-ac3dfec99bb1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	modernc.org/libc v1.80.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)

// Replace dependency modules with local developing copy
// use `go list -m all` to confirm the final module used
//replace github.com/v2rayA/shadowsocksR => ../../shadowsocksR
//replace github.com/mzz2017/go-engine => ../../go-engine
//replace github.com/v2rayA/beego/v2 => D:\beego

// replace github.com/daeuniverse/softwind => ../../softwind

replace github.com/v2fly/v2ray-core/v5 => github.com/v2rayA/v2ray-core/v5 v5.0.0-20230812170925-960565fa0686
