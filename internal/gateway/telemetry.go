package gateway

import "github.com/murongg/SubLane/internal/metrics"

func (s *Service) SetTelemetry(collector *metrics.Collector) { s.telemetry = collector }

func (s *Service) Pressure() (inFlight, queued int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, state := range s.health {
		inFlight += state.InFlight
	}
	return inFlight, queued
}
