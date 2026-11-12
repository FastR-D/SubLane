// Package pricing loads LiteLLM-compatible model prices for allowance plans.
package pricing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const DefaultURL = "https://raw.githubusercontent.com/Wei-Shaw/model-price-repo/main/model_prices_and_context_window.json"
const DefaultHashURL = "https://raw.githubusercontent.com/Wei-Shaw/model-price-repo/main/model_prices_and_context_window.sha256"

// Price uses micro-USD per million tokens, matching allocations.Rate.
type Price struct {
	Input  int64 `json:"input"`
	Cached int64 `json:"cached"`
	Output int64 `json:"output"`
	// ImageInput prices image input tokens for image generation models; zero means the text input rate.
	ImageInput int64  `json:"image_input,omitempty"`
	Source     string `json:"source"`
}

type ModelPrice struct {
	Model string `json:"model"`
	Price
}

type Config struct {
	URL          string
	HashURL      string
	CachePath    string
	HashPath     string
	FallbackPath string
	OverridePath string
	Interval     time.Duration
	HTTPClient   *http.Client
}

type Service struct {
	mu        sync.RWMutex
	prices    map[string]Price
	config    Config
	client    *http.Client
	stop      context.CancelFunc
	done      chan struct{}
	cacheHash string
}

func New(cfg Config) (*Service, error) {
	if cfg.URL == "" {
		cfg.URL = DefaultURL
		if cfg.HashURL == "" {
			cfg.HashURL = DefaultHashURL
		}
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 6 * time.Hour
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	s := &Service{config: cfg, client: cfg.HTTPClient, prices: map[string]Price{}, done: make(chan struct{})}
	var cacheRaw []byte
	if cfg.CachePath != "" {
		if raw, err := os.ReadFile(cfg.CachePath); err == nil {
			cacheRaw = raw
			if parsed, e := parsePrices(raw, "cache"); e == nil {
				s.prices = parsed
			}
		}
	}
	if cfg.HashPath != "" {
		if raw, err := os.ReadFile(cfg.HashPath); err == nil {
			s.cacheHash = normalizeHash(string(raw))
		}
	}
	if cfg.HashURL != "" && (s.cacheHash == "" || hashDocument(cacheRaw) != s.cacheHash) {
		// A cache and its saved digest are a pair; a partial write must trigger a fresh download.
		s.prices = map[string]Price{}
		s.cacheHash = ""
	}
	if err := s.mergeFile(cfg.FallbackPath, "fallback"); err != nil {
		return nil, err
	}
	if err := s.mergeFile(cfg.OverridePath, "override"); err != nil {
		return nil, err
	}
	return s, nil
}

// NewStatic creates a read-only catalog for embedded defaults and tests.
func NewStatic(values map[string]Price) *Service {
	prices := make(map[string]Price, len(values))
	for model, price := range values {
		prices[strings.ToLower(normalizeModel(model))] = price
	}
	return &Service{prices: prices, done: make(chan struct{})}
}

func (s *Service) mergeFile(path, source string) error {
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	parsed, err := parsePrices(raw, source)
	if err != nil {
		return fmt.Errorf("pricing %s: %w", source, err)
	}
	for model, price := range parsed {
		if source == "override" {
			base := s.prices[model]
			if price.Input > 0 {
				base.Input = price.Input
			}
			if price.Cached > 0 {
				base.Cached = price.Cached
			}
			if price.Output > 0 {
				base.Output = price.Output
			}
			base.Source = source
			if base.Input > 0 && base.Output > 0 {
				s.prices[model] = base
			}
			continue
		}
		s.prices[model] = price
	}
	return nil
}

func (s *Service) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	s.stop = cancel
	go func() {
		defer close(s.done)
		refresh := func() { _ = s.Refresh(ctx) }
		refresh()
		ticker := time.NewTicker(s.config.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				refresh()
			}
		}
	}()
}

func (s *Service) Close() {
	if s.stop != nil {
		s.stop()
		<-s.done
	}
}

func (s *Service) Lookup(model string) (Price, bool) {
	model = normalizeModel(model)
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, candidate := range []string{model, strings.TrimSuffix(model, "-high"), strings.TrimSuffix(model, "-xhigh")} {
		if price, ok := s.prices[strings.ToLower(candidate)]; ok {
			return price, true
		}
	}
	return Price{}, false
}

func (s *Service) Resolve(models []string) map[string]Price {
	result := make(map[string]Price, len(models))
	for _, model := range models {
		if price, ok := s.Lookup(model); ok {
			result[model] = price
		}
	}
	return result
}

func (s *Service) List() []ModelPrice {
	s.mu.RLock()
	prices := make([]ModelPrice, 0, len(s.prices))
	for model, price := range s.prices {
		prices = append(prices, ModelPrice{Model: model, Price: price})
	}
	s.mu.RUnlock()
	sort.Slice(prices, func(i, j int) bool { return prices[i].Model < prices[j].Model })
	return prices
}

func (s *Service) Refresh(ctx context.Context) error {
	if s.config.URL == "" {
		return nil
	}
	hash := ""
	if s.config.HashURL != "" {
		body, err := s.fetch(ctx, s.config.HashURL)
		if err != nil {
			return err
		}
		hash = normalizeHash(string(body))
		if hash == "" {
			return errors.New("empty pricing hash")
		}
		s.mu.RLock()
		unchanged := hash == s.cacheHash && len(s.prices) > 0
		s.mu.RUnlock()
		if unchanged {
			return nil
		}
	}
	body, err := s.fetch(ctx, s.config.URL)
	if err != nil {
		return err
	}
	if hash != "" && hashDocument(body) != hash {
		return errors.New("pricing document SHA-256 mismatch")
	}
	parsed, err := parsePrices(body, "remote")
	if err != nil {
		return err
	}
	if len(parsed) == 0 {
		return errors.New("pricing document has no usable models")
	}
	if s.config.FallbackPath != "" || s.config.OverridePath != "" {
		// Rebuild the merge from the freshly downloaded document so overrides never
		// mutate the remote snapshot in place.
		base := &Service{prices: parsed}
		if err := base.mergeFile(s.config.FallbackPath, "fallback"); err != nil {
			return err
		}
		if err := base.mergeFile(s.config.OverridePath, "override"); err != nil {
			return err
		}
		parsed = base.prices
	}
	if s.config.CachePath != "" {
		if err := atomicWrite(s.config.CachePath, body); err != nil {
			return err
		}
	}
	if hash != "" && s.config.HashPath != "" {
		if err := atomicWrite(s.config.HashPath, []byte(hash+"\n")); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.prices, s.cacheHash = parsed, hash
	s.mu.Unlock()
	return nil
}

func (s *Service) fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("pricing fetch %s: %s", url, resp.Status)
	}
	return readLimit(resp.Body, 32<<20)
}

func readLimit(reader io.Reader, limit int64) ([]byte, error) {
	value, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(value)) > limit {
		return nil, errors.New("pricing document is too large")
	}
	return value, nil
}

type litePrice struct {
	Input  *float64 `json:"input_cost_per_token"`
	Cached *float64 `json:"cache_read_input_token_cost"`
	Output *float64 `json:"output_cost_per_token"`
	// Image generation models price image tokens separately from text tokens.
	ImageInput  *float64 `json:"input_cost_per_image_token"`
	ImageOutput *float64 `json:"output_cost_per_image_token"`
	Mode        string   `json:"mode"`
}

func parsePrices(raw []byte, source string) (map[string]Price, error) {
	var document map[string]litePrice
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	result := make(map[string]Price, len(document))
	for model, value := range document {
		// Image models emit image tokens, so their output is always charged at the image rate.
		if value.Mode == "image_generation" && value.ImageOutput != nil {
			value.Output = value.ImageOutput
		}
		if source == "override" {
			price := Price{Source: source}
			if value.Input != nil && *value.Input > 0 {
				price.Input, _ = toMicroPerMillion(*value.Input)
			}
			if value.Cached != nil && *value.Cached >= 0 {
				price.Cached, _ = toMicroPerMillion(*value.Cached)
			}
			if value.Output != nil && *value.Output > 0 {
				price.Output, _ = toMicroPerMillion(*value.Output)
			}
			if price.Input > 0 || price.Cached > 0 || price.Output > 0 {
				result[strings.ToLower(normalizeModel(model))] = price
			}
			continue
		}
		if value.Input == nil || value.Output == nil || *value.Input <= 0 || *value.Output <= 0 || math.IsNaN(*value.Input) || math.IsNaN(*value.Output) {
			continue
		}
		cached := *value.Input
		if value.Cached != nil && *value.Cached >= 0 && !math.IsNaN(*value.Cached) {
			cached = *value.Cached
		}
		input, ok1 := toMicroPerMillion(*value.Input)
		cache, ok2 := toMicroPerMillion(cached)
		output, ok3 := toMicroPerMillion(*value.Output)
		if !ok1 || !ok2 || !ok3 {
			continue
		}
		price := Price{Input: input, Cached: cache, Output: output, Source: source}
		if value.Mode == "image_generation" && value.ImageInput != nil && *value.ImageInput > 0 && !math.IsNaN(*value.ImageInput) {
			if imageInput, ok := toMicroPerMillion(*value.ImageInput); ok {
				price.ImageInput = imageInput
			}
		}
		result[strings.ToLower(normalizeModel(model))] = price
	}
	return result, nil
}

func toMicroPerMillion(perToken float64) (int64, bool) {
	value := math.Round(perToken * 1_000_000_000_000)
	if value <= 0 || value > 1_000_000_000_000 || math.IsInf(value, 0) {
		return 0, false
	}
	return int64(value), true
}

func normalizeModel(model string) string {
	if provider, native, found := strings.Cut(strings.TrimSpace(model), "/"); found && provider != "" {
		model = native
	}
	if i := strings.IndexByte(model, '('); i > 0 {
		model = model[:i]
	}
	return strings.TrimSpace(model)
}

func normalizeHash(raw string) string {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return ""
	}
	hash := strings.ToLower(fields[0])
	if len(hash) != 64 {
		return ""
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return ""
	}
	return hash
}

func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pricing-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Chmod(0600)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}

func hashDocument(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
