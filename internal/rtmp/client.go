package rtmp

import (
	"context"
	"crypto/tls"
	"errors"
	"math"
	"math/bits"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/bluenviron/gortmplib/pkg/amf0"
	"github.com/bluenviron/gortmplib/pkg/bytecounter"
	"github.com/bluenviron/gortmplib/pkg/handshake"
)

// Dial publishes using an OBS-style server URL and separate, literal stream key.
// TLS verification is always enabled. The caller owns the returned connection.
func Dial(ctx context.Context, serverURL, streamKey string) (*Conn, uint32, error) {
	u, err := url.Parse(serverURL)
	if err != nil || u == nil || (u.Scheme != "rtmp" && u.Scheme != "rtmps") {
		return nil, 0, errors.New("invalid target URL")
	}
	port := u.Port()
	if port == "" {
		port = "1935"
		if u.Scheme == "rtmps" {
			port = "443"
		}
	}
	n, err := (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(u.Hostname(), port))
	if err != nil {
		return nil, 0, err
	}
	success := false
	defer func() {
		if !success {
			n.Close()
		}
	}()
	rawConn := n
	stop := context.AfterFunc(ctx, func() { rawConn.Close() })
	defer stop()
	if err := n.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return nil, 0, err
	}
	if u.Scheme == "rtmps" {
		t := tls.Client(n, &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12})
		if err := t.HandshakeContext(ctx); err != nil {
			return nil, 0, err
		}
		n = t
	}
	bc := bytecounter.NewReadWriter(n)
	if _, _, err := handshake.DoClient(bc, false, false); err != nil {
		return nil, 0, err
	}
	c := New(n, bc)
	app := strings.Trim(u.Path, "/")
	if u.RawQuery != "" {
		app += "?" + u.RawQuery
	}
	if err := c.control(1, u32(65536)); err != nil {
		return nil, 0, err
	}
	if err := c.control(5, u32(2500000)); err != nil {
		return nil, 0, err
	}
	if err := c.command(0, "connect", 1, amf0.Object{
		{Key: "app", Value: app}, {Key: "tcUrl", Value: serverURL},
		{Key: "flashVer", Value: "FMLE/3.0 (compatible; restreamer)"}, {Key: "objectEncoding", Value: float64(0)},
	}); err != nil {
		return nil, 0, err
	}
	if _, err := c.result(1); err != nil {
		return nil, 0, err
	}
	if err := c.command(0, "releaseStream", 2, nil, streamKey); err != nil {
		return nil, 0, err
	}
	if err := c.command(0, "FCPublish", 3, nil, streamKey); err != nil {
		return nil, 0, err
	}
	if err := c.command(0, "createStream", 4, nil); err != nil {
		return nil, 0, err
	}
	res, err := c.result(4)
	if err != nil {
		return nil, 0, err
	}
	if len(res) < 4 {
		return nil, 0, errors.New("missing output stream ID")
	}
	id, ok := res[3].(float64)
	if !ok || math.IsNaN(id) || id < 1 || id > math.MaxUint32 || math.Trunc(id) != id {
		return nil, 0, errors.New("invalid output stream ID")
	}
	stream := bits.ReverseBytes32(uint32(id))
	if err := c.command(stream, "publish", 5, nil, streamKey, "live"); err != nil {
		return nil, 0, err
	}
	for {
		a, err := c.readCommand()
		if err != nil {
			return nil, 0, err
		}
		if a[0] == "_error" {
			return nil, 0, errors.New("target rejected publish")
		}
		if a[0] == "onStatus" && len(a) > 3 {
			o := object(a[3])
			code, _ := o.GetString("code")
			level, _ := o.GetString("level")
			if level == "error" {
				return nil, 0, errors.New("target rejected publish")
			}
			if code == "NetStream.Publish.Start" {
				break
			}
		}
	}
	if err := n.SetDeadline(time.Time{}); err != nil {
		return nil, 0, err
	}
	success = true
	return c, stream, nil
}

func (c *Conn) result(id float64) (amf0.Data, error) {
	for {
		a, err := c.readCommand()
		if err != nil {
			return nil, err
		}
		if a[1] == id {
			if a[0] != "_result" {
				return nil, errors.New("target rejected command")
			}
			return a, nil
		}
	}
}
