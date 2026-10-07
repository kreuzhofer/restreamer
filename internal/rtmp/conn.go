// Package rtmp implements the publish-only RTMP session layer. Chunk framing,
// handshake and AMF encoding are provided by gortmplib. Media bodies stay opaque.
package rtmp

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/bluenviron/gortmplib/pkg/amf0"
	"github.com/bluenviron/gortmplib/pkg/bytecounter"
	"github.com/bluenviron/gortmplib/pkg/rawmessage"
)

const (
	Audio   = 8
	Video   = 9
	Data    = 18
	Command = 20
	// gortmplib exposes the RTMP little-endian stream ID as a big-endian uint32.
	StreamID = 0x01000000
)

type Message = rawmessage.Message

type Conn struct {
	Net          net.Conn
	reader       *rawmessage.Reader
	writer       *rawmessage.Writer
	mu           sync.Mutex // Includes automatic acknowledgements and ping responses.
	WriteTimeout time.Duration
}

func New(n net.Conn, counter *bytecounter.ReadWriter) *Conn {
	c := &Conn{Net: n, WriteTimeout: 10 * time.Second}
	c.writer = rawmessage.NewWriter(counter, counter.Writer, false)
	c.reader = rawmessage.NewReader(counter, counter.Reader, func(count uint32) error {
		return c.control(3, u32(count))
	})
	return c
}

func u32(v uint32) []byte { b := make([]byte, 4); binary.BigEndian.PutUint32(b, v); return b }

func (c *Conn) Write(m *Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.Net.SetWriteDeadline(time.Now().Add(c.WriteTimeout)); err != nil {
		return err
	}
	if err := c.writer.Write(m); err != nil {
		return err
	}
	if m.Type == 1 && len(m.Body) == 4 {
		c.writer.SetChunkSize(binary.BigEndian.Uint32(m.Body))
	}
	return nil
}

func (c *Conn) control(typ uint8, body []byte) error {
	return c.Write(&Message{ChunkStreamID: 2, Type: typ, Body: body})
}

func (c *Conn) Read() (*Message, error) {
	for {
		m, err := c.reader.Read()
		if err != nil {
			return nil, err
		}
		switch m.Type {
		case 1, 2, 3, 5:
			if len(m.Body) != 4 {
				return nil, errors.New("invalid RTMP control message")
			}
			value := binary.BigEndian.Uint32(m.Body)
			switch m.Type {
			case 1:
				if value == 0 {
					return nil, errors.New("invalid RTMP chunk size")
				}
				if err := c.reader.SetChunkSize(value); err != nil {
					return nil, err
				}
			case 2:
				c.reader.AbortChunkStream(value)
			case 3: // No outbound acknowledgement-window enforcement; writes have deadlines.
			case 5:
				c.reader.SetWindowAckSize(value)
			}
		case 4:
			if len(m.Body) < 2 {
				return nil, errors.New("invalid RTMP user control message")
			}
			if binary.BigEndian.Uint16(m.Body) == 6 {
				if len(m.Body) != 6 {
					return nil, errors.New("invalid RTMP ping")
				}
				body := append([]byte(nil), m.Body...)
				body[1] = 7
				if err := c.control(4, body); err != nil {
					return nil, err
				}
			}
		case 6:
			if len(m.Body) != 5 {
				return nil, errors.New("invalid peer bandwidth message")
			}
		default:
			// The raw reader reuses its Message struct. Give each caller an owned
			// body so packets can safely outlive the next Read and be shared.
			owned := *m
			owned.Body = append([]byte(nil), m.Body...)
			return &owned, nil
		}
	}
}

func (c *Conn) command(stream uint32, name string, id float64, args ...any) error {
	body, err := append(amf0.Data{name, id}, args...).Marshal()
	if err != nil {
		return err
	}
	return c.Write(&Message{ChunkStreamID: 3, MessageStreamID: stream, Type: Command, Body: body})
}

func decode(m *Message) (amf0.Data, error) {
	body := m.Body
	if m.Type == 17 && len(body) > 0 && body[0] == 0 {
		body = body[1:]
	} else if m.Type != Command {
		return nil, errors.New("not a command")
	}
	a, err := amf0.Unmarshal(body)
	if err != nil || len(a) < 3 {
		return nil, errors.New("invalid RTMP command")
	}
	if _, ok := a[0].(string); !ok {
		return nil, errors.New("invalid command name")
	}
	if _, ok := a[1].(float64); !ok {
		return nil, errors.New("invalid transaction ID")
	}
	return a, nil
}

func (c *Conn) readCommand() (amf0.Data, error) {
	for {
		m, err := c.Read()
		if err != nil {
			return nil, err
		}
		if m.Type == Command || m.Type == 17 {
			return decode(m)
		}
	}
}

func object(v any) amf0.Object {
	switch o := v.(type) {
	case amf0.Object:
		return o
	case amf0.ECMAArray:
		return amf0.Object(o)
	}
	return nil
}

// CheckControl detects downstream rejection and upstream unpublish commands.
// Error strings deliberately exclude peer-provided data, which can contain keys.
func CheckControl(m *Message) error {
	if m.Type != Command && m.Type != 17 {
		return nil
	}
	a, err := decode(m)
	if err != nil {
		return err
	}
	switch a[0] {
	case "deleteStream", "closeStream", "FCUnpublish":
		return io.EOF
	case "_error":
		return errors.New("peer rejected stream")
	case "onStatus":
		if len(a) > 3 {
			o := object(a[3])
			level, _ := o.GetString("level")
			code, _ := o.GetString("code")
			if level == "error" || code == "NetStream.Unpublish.Success" || code == "NetStream.Play.Stop" {
				return errors.New("peer stopped stream")
			}
		}
	}
	return nil
}
