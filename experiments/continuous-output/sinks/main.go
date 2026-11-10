// Command sinks captures two loopback-only RTMP destinations for the experiment.
package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/kreuzhofer/restreamer/internal/rtmp"
)

type result struct {
	InjectedDisconnects int `json:"injected_disconnects"`
	Connections         int `json:"connections"`
	Rejected            int `json:"rejected"`
	WriteFailures       int `json:"write_failures"`
}

type sink struct {
	disconnectAfter time.Duration
	listener        net.Listener
	mu              sync.Mutex
	result          result
	directory       string
	start           time.Time
}

func (s *sink) serve(ctx context.Context, workers *sync.WaitGroup) {
	defer workers.Done()
	stop := context.AfterFunc(ctx, func() { s.listener.Close() })
	defer stop()
	slots := make(chan struct{}, 16)
	for {
		n, err := s.listener.Accept()
		if err != nil {
			return
		}
		select {
		case slots <- struct{}{}:
		default:
			n.Close()
			continue
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer func() { <-slots }()
			defer n.Close()
			stop := context.AfterFunc(ctx, func() { n.Close() })
			defer stop()
			c, err := rtmp.Accept(n, func(app, key string) bool { return app == "app" && key == "benchmark-local-key" })
			if err != nil {
				s.mu.Lock()
				s.result.Rejected++
				s.mu.Unlock()
				return
			}
			s.mu.Lock()
			s.result.Connections++
			number := s.result.Connections
			s.mu.Unlock()
			if number == 1 && s.disconnectAfter > 0 {
				timer := time.AfterFunc(s.disconnectAfter, func() {
					s.mu.Lock()
					s.result.InjectedDisconnects++
					s.mu.Unlock()
					n.Close()
				})
				defer timer.Stop()
			}
			if err := s.capture(c, number); err != nil {
				s.mu.Lock()
				s.result.WriteFailures++
				s.mu.Unlock()
			}
		}()
	}
}

func (s *sink) capture(c *rtmp.Conn, number int) (err error) {
	path := filepath.Join(s.directory, fmt.Sprintf("connection-%03d", number))
	media, err := os.OpenFile(path+".flv", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer media.Close()
	arrivals, err := os.OpenFile(path+".jsonl", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer arrivals.Close()
	video, events := bufio.NewWriter(media), bufio.NewWriter(arrivals)
	defer func() {
		if e := video.Flush(); err == nil {
			err = e
		}
		if e := events.Flush(); err == nil {
			err = e
		}
	}()
	if _, err := video.Write([]byte{'F', 'L', 'V', 1, 5, 0, 0, 0, 9, 0, 0, 0, 0}); err != nil {
		return err
	}
	encoder := json.NewEncoder(events)
	for {
		m, err := c.Read()
		if err != nil {
			return nil
		} // Connection endings are counted; no raw peer errors are logged.
		arrival := s.start.UnixNano() + time.Since(s.start).Nanoseconds()
		if m.Type != rtmp.Video && m.Type != rtmp.Audio && m.Type != rtmp.Data {
			continue
		}
		n := len(m.Body)
		ts := uint32(m.Timestamp / time.Millisecond)
		if _, err := video.Write([]byte{m.Type, byte(n >> 16), byte(n >> 8), byte(n), byte(ts >> 16), byte(ts >> 8), byte(ts), byte(ts >> 24), 0, 0, 0}); err != nil {
			return err
		}
		if _, err := video.Write(m.Body); err != nil {
			return err
		}
		var tail [4]byte
		binary.BigEndian.PutUint32(tail[:], uint32(n+11))
		if _, err := video.Write(tail[:]); err != nil {
			return err
		}
		if m.Type == rtmp.Video && len(m.Body) >= 5 && m.Body[1] == 1 {
			cts := int32(m.Body[2])<<16 | int32(m.Body[3])<<8 | int32(m.Body[4])
			if cts&0x800000 != 0 {
				cts -= 1 << 24
			}
			event := struct {
				PTSMS     int64 `json:"pts_ms"`
				ArrivalNS int64 `json:"arrival_ns"`
			}{int64(ts) + int64(cts), arrival}
			if err := encoder.Encode(event); err != nil {
				return err
			}
		}
	}
}

func run() error {
	directory := flag.String("output", "", "New capture directory")
	seconds := flag.Int("seconds", 14500, "Bounded lifetime")
	disconnect := flag.Duration("disconnect-after", 0, "Disconnect only the first destination once after accepting its first connection")
	flag.Parse()
	if *directory == "" || *seconds < 1 || *seconds > 14500 || *disconnect < 0 || *disconnect > 10*time.Minute {
		return fmt.Errorf("output and lifetime are required")
	}
	if err := os.Mkdir(*directory, 0700); err != nil {
		return fmt.Errorf("capture directory must be new")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	ctx, deadline := context.WithTimeout(ctx, time.Duration(*seconds)*time.Second)
	defer deadline()
	var sinks []*sink
	var targets []map[string]string
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	for index := 1; index <= 2; index++ {
		dir := filepath.Join(*directory, fmt.Sprintf("sink-%d", index))
		if err := os.Mkdir(dir, 0700); err != nil {
			return err
		}
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return err
		}
		s := &sink{listener: listener, directory: dir, start: time.Now()}
		if index == 1 {
			s.disconnectAfter = *disconnect
		}
		sinks = append(sinks, s)
		targets = append(targets, map[string]string{"name": fmt.Sprintf("sink-%d", index), "url": "rtmp://" + listener.Addr().String() + "/app", "stream_key": "benchmark-local-key"})
		workers.Add(1)
		go s.serve(ctx, &workers)
	}
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"targets": targets}); err != nil {
		return err
	}
	<-ctx.Done()
	workers.Wait()
	results := make([]result, 0, len(sinks))
	for _, s := range sinks {
		s.mu.Lock()
		results = append(results, s.result)
		s.mu.Unlock()
	}
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(*directory, "summary.json"), append(data, '\n'), 0600)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "capture helper:", err)
		os.Exit(1)
	}
}
