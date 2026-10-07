package rtmp

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"
)

func TestFlowControlAndConcurrentPingReplies(t *testing.T) {
	l := listener(t)
	body := make([]byte, 256<<10)
	copy(body, []byte{0x17, 1, 0, 0, 0})
	for i := 5; i < len(body); i++ {
		body[i] = byte(i)
	}
	done := make(chan error, 1)
	go func() {
		n, err := l.Accept()
		if err != nil {
			done <- err
			return
		}
		defer n.Close()
		c, err := Accept(n, func(_, _ string) bool { return true })
		if err != nil {
			done <- err
			return
		}
		_ = n.SetReadDeadline(time.Now().Add(5 * time.Second))
		// 5 MiB crosses the negotiated 2.5 MB acknowledgement window twice.
		for i := range 20 {
			if err := c.control(4, append([]byte{0, 6}, u32(uint32(i))...)); err != nil {
				done <- err
				return
			}
			m, err := c.Read()
			if err != nil {
				done <- err
				return
			}
			if m.Type != Video || !bytes.Equal(m.Body, body) {
				done <- fmt.Errorf("packet %d was corrupted", i)
				return
			}
		}
		done <- nil
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, stream, err := Dial(ctx, "rtmp://"+l.Addr().String()+"/app", "key")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Net.Close()
	readDone := make(chan struct{})
	go func() { defer close(readDone); _, _ = c.Read() }()
	defer func() { c.Net.Close(); <-readDone }()
	for i := range 20 {
		if err := c.Write(&Message{ChunkStreamID: 6, MessageStreamID: stream, Type: Video, Timestamp: time.Duration(i) * time.Second, Body: body}); err != nil {
			t.Fatal(err)
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
