// Package bridge adapts Proton Mail Bridge's local IMAP and SMTP endpoints to
// the mail.Store and mail.Sender ports.
package bridge

import (
	"crypto/tls"
	"net"
	"time"
)

const dialTimeout = 15 * time.Second

// TLSConfig builds the STARTTLS client config for a bridge endpoint. Bridge
// presents a self-signed certificate, so skipVerify is normally true.
func TLSConfig(addr string, skipVerify bool) *tls.Config {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	return &tls.Config{ServerName: host, InsecureSkipVerify: skipVerify} //nolint:gosec // bridge cert is self-signed on loopback
}
