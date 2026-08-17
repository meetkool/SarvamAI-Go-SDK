package sarvam

import (
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"
)

const (
	DefaultBaseURL = "https://api.sarvam.ai"

	DefaultTimeout        = 5 * time.Minute
	DefaultMaxRetries     = 3
	DefaultMaxConcurrency = 8
)

const (
	envAPIKey  = "SARVAM_API_KEY"
	envBaseURL = "SARVAM_BASE_URL"
	envTimeout = "SARVAM_TIMEOUT_SECS"
)

type config struct {
	apiKey         string
	baseURL        string
	timeout        time.Duration
	maxRetries     int
	retryBaseDelay time.Duration
	retryMaxDelay  time.Duration
	defaultFormat  Format
	maxConcurrency int
	userAgent      string
	httpClient     *http.Client
	logger         *slog.Logger
}

type Option func(*config)

func defaultConfig() config {
	return config{
		baseURL:        DefaultBaseURL,
		timeout:        DefaultTimeout,
		maxRetries:     DefaultMaxRetries,
		retryBaseDelay: time.Second,
		retryMaxDelay:  60 * time.Second,
		defaultFormat:  WAV(24000),
		maxConcurrency: DefaultMaxConcurrency,
	}
}

func envOptions() []Option {
	var opts []Option
	if v := os.Getenv(envAPIKey); v != "" {
		opts = append(opts, WithAPIKey(v))
	}
	if v := os.Getenv(envBaseURL); v != "" {
		opts = append(opts, WithBaseURL(v))
	}
	if secs, err := strconv.Atoi(os.Getenv(envTimeout)); err == nil && secs > 0 {
		opts = append(opts, WithTimeout(time.Duration(secs)*time.Second))
	}
	return opts
}

func WithAPIKey(key string) Option {
	return func(c *config) { c.apiKey = key }
}

func WithBaseURL(baseURL string) Option {
	return func(c *config) { c.baseURL = baseURL }
}

func WithHTTPClient(hc *http.Client) Option {
	return func(c *config) { c.httpClient = hc }
}

func WithTimeout(d time.Duration) Option {
	return func(c *config) { c.timeout = d }
}

func WithMaxRetries(n int) Option {
	return func(c *config) { c.maxRetries = n }
}

func WithRetryDelays(base, maxDelay time.Duration) Option {
	return func(c *config) {
		c.retryBaseDelay = base
		c.retryMaxDelay = maxDelay
	}
}

func WithDefaultFormat(f Format) Option {
	return func(c *config) { c.defaultFormat = f }
}

func WithMaxConcurrency(n int) Option {
	return func(c *config) { c.maxConcurrency = n }
}

func WithUserAgent(ua string) Option {
	return func(c *config) { c.userAgent = ua }
}

func WithLogger(l *slog.Logger) Option {
	return func(c *config) { c.logger = l }
}
