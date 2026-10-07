package relay

import (
	"context"
	"sync"
	"time"
)

const historyWindow = 15 * time.Minute
const historyCapacity = 900

type bitrateSample struct {
	Time      int64              `json:"time"`
	Input     float64            `json:"input"`
	Outputs   map[string]float64 `json:"outputs"`
	InputFPS  float64            `json:"input_fps"`
	OutputFPS map[string]float64 `json:"output_fps"`
}

type metrics struct {
	mu           sync.Mutex
	previous     time.Time
	input        uint64
	outputs      map[string]uint64
	inputFrames  uint64
	outputFrames map[string]uint64
	samples      []bitrateSample
}

func (s *Server) sampleLoop(ctx context.Context) {
	s.sample(time.Now())
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.sample(time.Now())
		}
	}
}

func (s *Server) sample(now time.Time) {
	m := &s.metrics
	m.mu.Lock()
	defer m.mu.Unlock()
	input := s.inputBytes.Load()
	inputFrames := s.inputFrames.Load()
	outputs := make(map[string]uint64, len(s.outputs))
	outputFrames := make(map[string]uint64, len(s.outputs))
	for _, o := range s.outputs {
		status := o.snapshot()
		outputs[o.config.Name] = status.Bytes
		outputFrames[o.config.Name] = status.Frames
	}
	if !m.previous.IsZero() && now.After(m.previous) {
		seconds := now.Sub(m.previous).Seconds()
		point := bitrateSample{Time: now.UnixMilli(), Input: float64(input-m.input) * 8 / seconds, Outputs: make(map[string]float64, len(outputs))}
		point.InputFPS = float64(inputFrames-m.inputFrames) / seconds
		point.OutputFPS = make(map[string]float64, len(outputs))
		for name, bytes := range outputs {
			point.Outputs[name] = float64(bytes-m.outputs[name]) * 8 / seconds
			point.OutputFPS[name] = float64(outputFrames[name]-m.outputFrames[name]) / seconds
		}
		// A delayed sampler represents an average across the elapsed interval.
		m.samples = append(m.samples, point)
		cutoff := now.Add(-historyWindow).UnixMilli()
		start := 0
		for start < len(m.samples) && (m.samples[start].Time <= cutoff || len(m.samples)-start > historyCapacity) {
			start++
		}
		if start > 0 {
			copy(m.samples, m.samples[start:])
			m.samples = m.samples[:len(m.samples)-start]
		}
	}
	m.previous, m.input, m.outputs = now, input, outputs
	m.inputFrames, m.outputFrames = inputFrames, outputFrames
}

func (s *Server) history(now time.Time) []bitrateSample {
	s.metrics.mu.Lock()
	defer s.metrics.mu.Unlock()
	result := make([]bitrateSample, 0, len(s.metrics.samples))
	for _, sample := range s.metrics.samples {
		if sample.Time > now.Add(-historyWindow).UnixMilli() {
			result = append(result, sample)
		}
	}
	return result // Sample maps are immutable after construction.
}
