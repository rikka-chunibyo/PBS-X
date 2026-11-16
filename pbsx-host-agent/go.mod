module github.com/pbs-x/pbsx-host-agent

go 1.27.0

require (
	fyne.io/fyne/v2 v2.8.2
	github.com/BurntSushi/toml v1.6.0
	github.com/Masterminds/semver v1.5.0
	github.com/billgraziano/dpapi v0.5.0
	github.com/containers/winquit v1.1.0
	github.com/fxamacker/cbor/v2 v2.9.3
	github.com/gobwas/glob v1.0.0
	github.com/gofrs/flock v0.13.1
	github.com/kardianos/service v1.3.0
	github.com/klauspost/compress v1.19.2
	github.com/minio/sha256-simd v1.0.1
	github.com/mxk/go-vss v1.2.1
	github.com/pbnjay/memory v0.0.0-20210728143218-7b4eea64cf58
	github.com/pbs-plus/pxar v0.45.0
	github.com/puzpuzpuz/xsync/v4 v4.5.0
	github.com/quic-go/quic-go v0.61.0
	github.com/stretchr/testify v1.12.1
	github.com/xtaci/smux v1.5.57
	go.uber.org/goleak v1.3.0
	golang.org/x/crypto v0.55.0
	golang.org/x/sys v0.47.0
	golang.org/x/term v0.45.0
	golang.org/x/time v0.15.0
	gopkg.in/natefinch/lumberjack.v2 v2.2.1
)

require (
	fyne.io/systray v1.12.3-0.20260810170012-af4e8e793ec4 // indirect
	github.com/FyshOS/fancyfs v0.0.1 // indirect
	github.com/anthonynsimon/bild v0.14.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/fsnotify/fsnotify v1.10.1 // indirect
	github.com/fyne-io/gl-js v0.2.1-0.20260315212741-029c47fd27e8 // indirect
	github.com/fyne-io/glfw-js v0.4.0 // indirect
	github.com/fyne-io/image v0.1.1 // indirect
	github.com/fyne-io/oksvg v0.2.1-0.20260918172519-a9af55fa95d9 // indirect
	github.com/go-gl/gl v0.0.0-20260331235117-4566fea9a276 // indirect
	github.com/go-gl/glfw/v3.4/glfw v0.1.0-pre.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-text/render v0.2.1 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/hack-pad/go-indexeddb v0.3.2 // indirect
	github.com/hack-pad/safejs v0.1.0 // indirect
	github.com/jeandeaual/go-locale v0.0.0-20250612000132-0ef82f21eade // indirect
	github.com/jsummers/gobmp v0.0.0-20230614200233-a9de23ed2e25 // indirect
	github.com/klauspost/cpuid/v2 v2.4.0 // indirect
	github.com/mattn/go-runewidth v0.0.24 // indirect
	github.com/nfnt/resize v0.0.0-20180221191011-83c6a9932646 // indirect
	github.com/nicksnyder/go-i18n/v2 v2.5.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/rymdport/portal v0.4.2 // indirect
	github.com/sirupsen/logrus v1.10.2 // indirect
	github.com/srwiley/oksvg v0.0.0-20221011165216-be6e8873101c // indirect
	github.com/srwiley/rasterx v0.0.0-20220730225603-2ab79fcdd4ef // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/yuin/goldmark v1.8.2 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/image v0.44.0 // indirect
	golang.org/x/mod v0.39.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/telemetry v0.0.0-20260811182544-a038080d80e5 // indirect
	golang.org/x/text v0.41.0 // indirect
	golang.org/x/tools v0.49.0 // indirect
)

tool golang.org/x/tools/cmd/deadcode

replace github.com/pbs-plus/pxar => ./third_party/pxar
