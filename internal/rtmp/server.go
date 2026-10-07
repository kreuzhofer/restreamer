package rtmp

import (
	"errors"
	"net"
	"time"

	"github.com/bluenviron/gortmplib/pkg/amf0"
	"github.com/bluenviron/gortmplib/pkg/bytecounter"
	"github.com/bluenviron/gortmplib/pkg/handshake"
)

// Accept authenticates and reserves a publisher before acknowledging publish.
// authorize must return false if another input is already active.
func Accept(n net.Conn, authorize func(app, key string) bool) (_ *Conn, retErr error) {
	stage := "handshake"
	defer func() {
		if retErr != nil {
			retErr = acceptFailure(stage, retErr)
		}
	}()
	if err := n.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return nil, err
	}
	bc := bytecounter.NewReadWriter(n)
	key, _, err := handshake.DoServer(bc, false)
	if err != nil {
		return nil, err
	}
	if key != nil {
		return nil, errors.New("RTMPE is not supported")
	}
	c := New(n, bc)
	stage = "connect_command"
	a, err := c.readCommand()
	if err != nil {
		return nil, err
	}
	if a[0] != "connect" {
		return nil, errors.New("expected connect")
	}
	app, ok := object(a[2]).GetString("app")
	if !ok {
		return nil, errors.New("missing application")
	}
	stage = "connect_reply"
	if err := c.control(5, u32(2500000)); err != nil {
		return nil, err
	}
	if err := c.control(6, append(u32(2500000), 2)); err != nil {
		return nil, err
	}
	if err := c.control(1, u32(65536)); err != nil {
		return nil, err
	}
	if err := c.command(0, "_result", a[1].(float64),
		amf0.Object{{Key: "fmsVer", Value: "FMS/3,5,7,7009"}, {Key: "capabilities", Value: float64(31)}},
		amf0.Object{{Key: "level", Value: "status"}, {Key: "code", Value: "NetConnection.Connect.Success"}, {Key: "objectEncoding", Value: float64(0)}}); err != nil {
		return nil, err
	}
	for {
		stage = "publish_command"
		a, err = c.readCommand()
		if err != nil {
			return nil, err
		}
		switch a[0] {
		case "createStream":
			stage = "create_stream_reply"
			if err := c.command(0, "_result", a[1].(float64), nil, float64(1)); err != nil {
				return nil, err
			}
		case "releaseStream", "FCPublish":
			stage = "prepare_publish_reply"
			if err := c.command(0, "_result", a[1].(float64), nil, nil); err != nil {
				return nil, err
			}
		case "publish":
			stage = "publish_authorization"
			if len(a) < 4 {
				return nil, errors.New("missing stream key")
			}
			streamKey, ok := a[3].(string)
			if !ok || !authorize(app, streamKey) {
				_ = c.status("error", "NetStream.Publish.BadName")
				return nil, errors.New("publisher rejected")
			}
			stage = "publish_reply"
			if err := c.control(4, append([]byte{0, 0}, u32(1)...)); err != nil {
				return nil, err
			}
			if err := c.status("status", "NetStream.Publish.Start"); err != nil {
				return nil, err
			}
			if err := n.SetDeadline(time.Time{}); err != nil {
				return nil, err
			}
			return c, nil
		case "play":
			stage = "playback_rejected"
			_ = c.status("error", "NetStream.Play.Failed")
			return nil, errors.New("playback is not supported")
		}
	}
}

func (c *Conn) status(level, code string) error {
	return c.command(StreamID, "onStatus", 0, nil, amf0.Object{{Key: "level", Value: level}, {Key: "code", Value: code}})
}
