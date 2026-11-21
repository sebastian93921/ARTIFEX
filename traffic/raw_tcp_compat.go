package traffic

import "time"

// The raw-TCP recorder in raw_tcp.go arrived from upstream together with its
// blob-permission fix, but the SOCKS5 listener file that normally declares
// these two symbols has not been ported to this tree. Declaring them here keeps
// raw_tcp.go byte-identical to upstream; delete this file when socks5.go lands.

// DefaultSOCKS5PreviewBytes is the inline preview kept per direction before the
// captured bytes spill to the content-addressed blob store.
const DefaultSOCKS5PreviewBytes = blobPreview

// RawTCPCapture is the completed metadata and bounded directional payloads of
// one relayed raw TCP connection.
type RawTCPCapture struct {
	Host      string
	Port      int
	StartedAt time.Time
	EndedAt   time.Time
	C2SBytes  int64
	S2CBytes  int64
	C2SData   []byte
	S2CData   []byte
	Truncated bool
	Paced     bool
}
