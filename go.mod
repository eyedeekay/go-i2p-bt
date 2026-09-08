module github.com/go-i2p/go-i2p-bt

// Minimum Go version requirement - enforced by Go toolchain
go 1.26.0

require (
	github.com/go-i2p/i2pkeys v0.33.92
	github.com/go-i2p/sam3 v0.33.9
	github.com/gorilla/websocket v1.5.3
	go.etcd.io/bbolt v1.5.0
)

require (
	github.com/sirupsen/logrus v1.10.2 // indirect
	golang.org/x/sys v0.48.0 // indirect
)

retract (
	v0.1.59999
	v0.1.5999
	v0.1.599
)
