module github.com/pbs-x/pbsx-android/core

go 1.27.0

require github.com/pbs-plus/pxar v0.45.0

require (
	github.com/klauspost/compress v1.18.5 // indirect
	golang.org/x/crypto v0.58.0 // indirect
	golang.org/x/mobile v0.0.0-20261011034528-197770444867 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/net v0.61.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	golang.org/x/tools v0.52.0 // indirect
)

replace github.com/pbs-plus/pxar => ../../pbsx-host-agent/third_party/pxar

tool golang.org/x/mobile/cmd/gobind
