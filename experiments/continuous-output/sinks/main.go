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
	"net/url"
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
	disconnectAfter    time.Duration
	listener           net.Listener
	mu                 sync.Mutex
	result             result
	directory          string
	start              time.Time
	proxyURL, proxyKey string
	controlPath        string
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
			c, err := rtmp.Accept(n, func(app, key string) bool {
				if s.proxyURL != "" {
					return app == "live" && key == "benchmark-input-key"
				}
				return app == "app" && key == "benchmark-local-key"
			})
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
			var forward func(*rtmp.Message) error
			if s.proxyURL != "" {
				upstream, _, dialErr := rtmp.Dial(ctx, s.proxyURL, s.proxyKey)
				if dialErr != nil {
					s.mu.Lock()
					s.result.WriteFailures++
					s.mu.Unlock()
					return
				}
				upstreamDone := make(chan struct{})
				go func() {
					defer close(upstreamDone)
					for {
						message, err := upstream.Read()
						if err != nil || rtmp.CheckControl(message) != nil {
							upstream.Net.Close()
							n.Close()
							return
						}
					}
				}()
				defer func() { upstream.Net.Close(); <-upstreamDone }()
				stopUpstream := context.AfterFunc(ctx, func() { upstream.Net.Close() })
				defer stopUpstream()
				var control struct {
					DropAudio bool `json:"drop_audio"`
				}
				var checked time.Time
				forward = func(m *rtmp.Message) error {
					if time.Since(checked) >= 100*time.Millisecond {
						data, err := os.ReadFile(s.controlPath)
						if err != nil || json.Unmarshal(data, &control) != nil {
							return fmt.Errorf("invalid ingress fault control")
						}
						checked = time.Now()
					}
					if control.DropAudio && m.Type == rtmp.Audio {
						return nil
					}
					copy := *m
					copy.MessageStreamID = rtmp.StreamID
					copy.ChunkStreamID = 6
					if m.Type == rtmp.Audio {
						copy.ChunkStreamID = 4
					}
					return upstream.Write(&copy)
				}
			}
			if err := s.capture(c, number, forward); err != nil {
				s.mu.Lock()
				s.result.WriteFailures++
				s.mu.Unlock()
			}
		}()
	}
}

func (s *sink) capture(c *rtmp.Conn, number int, forward func(*rtmp.Message) error) (err error) {
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
		if forward != nil {
			if err := forward(m); err != nil {
				return fmt.Errorf("ingress forwarding failed")
			}
		}
	}
}

func run() error {
	directory := flag.String("output", "", "New capture directory")
	seconds := flag.Int("seconds", 14500, "Bounded lifetime")
	disconnect := flag.Duration("disconnect-after", 0, "Disconnect only the first destination once after accepting its first connection")
	proxyURL := flag.String("proxy-upstream", "", "Optional loopback-only upstream for ingress capture")
	proxyKey := flag.String("proxy-key", "", "Dummy upstream stream key for ingress capture")
	flag.Parse()
	if *proxyURL != "" {
		u, err := url.Parse(*proxyURL)
		if err != nil || u.Scheme != "rtmp" || u.User != nil || !net.ParseIP(u.Hostname()).IsLoopback() || *proxyKey == "" {
			return fmt.Errorf("ingress upstream must be loopback RTMP with a dummy key")
		}
	}
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
	count := 2
	if *proxyURL != "" {
		count = 1
	}
	for index := 1; index <= count; index++ {
		dir := filepath.Join(*directory, fmt.Sprintf("sink-%d", index))
		if *proxyURL != "" {
			dir = filepath.Join(*directory, "source")
		}
		if err := os.Mkdir(dir, 0700); err != nil {
			return err
		}
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return err
		}
		s := &sink{listener: listener, directory: dir, start: time.Now(), proxyURL: *proxyURL, proxyKey: *proxyKey}
		if *proxyURL != "" {
			s.controlPath = filepath.Join(*directory, "control.json")
			if err := os.WriteFile(s.controlPath, []byte(`{"drop_audio":false}`), 0600); err != nil {
				listener.Close()
				return err
			}
		}
		if index == 1 {
			s.disconnectAfter = *disconnect
		}
		sinks = append(sinks, s)
		targets = append(targets, map[string]string{"name": fmt.Sprintf("sink-%d", index), "url": "rtmp://" + listener.Addr().String() + "/app", "stream_key": "benchmark-local-key"})
		workers.Add(1)
		go s.serve(ctx, &workers)
	}
	ready := map[string]any{"targets": targets}
	if *proxyURL != "" {
		ready["url"] = "rtmp://" + sinks[0].listener.Addr().String() + "/live/benchmark-input-key"
	}
	if err := json.NewEncoder(os.Stdout).Encode(ready); err != nil {
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
