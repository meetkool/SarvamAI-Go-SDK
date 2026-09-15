package sarvam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/meetkool/SarvamAI-Go-SDK/internal/multipart"
	"github.com/meetkool/SarvamAI-Go-SDK/src/models"
)

type Client struct {
	Speech *SpeechService

	Transcription *TranscriptionService

	Batch *BatchService

	Chat *ChatService

	apiKey        string
	baseURL       string
	userAgent     string
	http          *http.Client
	timeout       time.Duration
	retry         retryPolicy
	defaultFormat models.Format
	slots         chan struct{}
	log           *slog.Logger
}

func New(opts ...Option) (*Client, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	if cfg.apiKey == "" {
		return nil, ErrMissingAPIKey
	}
	baseURL := strings.TrimRight(cfg.baseURL, "/")
	if u, err := url.Parse(baseURL); err != nil || u.Scheme == "" || u.Host == "" {
		return nil, invalidRequest("base URL %q is not a URL", cfg.baseURL)
	}

	httpClient := cfg.httpClient
	if httpClient == nil {

		httpClient = &http.Client{}
	}
	userAgent := "sarvam-go-sdk/" + Version
	if cfg.userAgent != "" {
		userAgent += " " + cfg.userAgent
	}
	logger := cfg.logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	c := &Client{
		apiKey:    cfg.apiKey,
		baseURL:   baseURL,
		userAgent: userAgent,
		http:      httpClient,
		timeout:   cfg.timeout,
		retry: retryPolicy{
			maxRetries: cfg.maxRetries,
			baseDelay:  cfg.retryBaseDelay,
			maxDelay:   cfg.retryMaxDelay,
			jitter:     0.25,
		},
		defaultFormat: cfg.defaultFormat,
		slots:         make(chan struct{}, max(cfg.maxConcurrency, 1)),
		log:           logger,
	}
	c.Speech = &SpeechService{client: c}
	c.Transcription = &TranscriptionService{client: c}
	c.Batch = &BatchService{client: c}
	c.Chat = &ChatService{client: c}
	return c, nil
}

func NewFromEnv(opts ...Option) (*Client, error) {
	return New(append(envOptions(), opts...)...)
}

func (c *Client) BaseURL() string { return c.baseURL }

func (c *Client) format(f models.Format) models.Format {
	if f.IsZero() {
		return c.defaultFormat
	}
	return f
}

func (c *Client) doJSON(ctx context.Context, method, path string, in, out any) error {
	payload, err := encodeJSON(in)
	if err != nil {
		return err
	}

	return withRetry(ctx, c.retry, func() error {
		attemptCtx, cancel := c.withTimeout(ctx)
		defer cancel()

		var body io.Reader
		var contentType string
		if payload != nil {
			body, contentType = bytes.NewReader(payload), "application/json"
		}
		resp, err := c.send(attemptCtx, method, c.baseURL+path, body, contentType)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		return decodeJSON(resp.Body, out)
	})
}

func (c *Client) doUpload(ctx context.Context, path string, fields []multipart.Field, in models.Input, out any) error {
	policy := c.retry
	if !in.Replayable() {
		policy.maxRetries = 0
	}

	return withRetry(ctx, policy, func() error {
		file, err := in.Open()
		if err != nil {
			return err
		}

		body, contentType := multipart.Body(fields, multipart.File{
			FieldName:   "file",
			FileName:    in.Filename(),
			ContentType: in.ContentType(),
			Body:        file,
		})

		attemptCtx, cancel := c.withTimeout(ctx)
		defer cancel()

		resp, err := c.send(attemptCtx, http.MethodPost, c.baseURL+path, body, contentType)
		if err != nil {
			body.Close()
			return err
		}
		defer resp.Body.Close()
		return decodeJSON(resp.Body, out)
	})
}

func (c *Client) openStream(ctx context.Context, path string, in any) (*http.Response, error) {
	payload, err := encodeJSON(in)
	if err != nil {
		return nil, err
	}
	return c.send(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload), "application/json")
}

func (c *Client) send(ctx context.Context, method, endpoint string, body io.Reader, contentType string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("sarvam: building request: %w", err)
	}
	req.Header.Set("api-subscription-key", c.apiKey)
	req.Header.Set("User-Agent", c.userAgent)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	release, err := c.acquire(ctx)
	if err != nil {
		return nil, err
	}

	started := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		release()
		return nil, transportError(err)
	}
	c.log.Debug("sarvam request",
		"method", method, "url", endpoint, "status", resp.StatusCode, "took", time.Since(started))

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		defer resp.Body.Close()
		release()
		return nil, parseAPIError(resp)
	}
	resp.Body = &releasingBody{ReadCloser: resp.Body, release: release}
	return resp, nil
}

func (c *Client) acquire(ctx context.Context) (func(), error) {
	select {
	case c.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	var once sync.Once
	return func() { once.Do(func() { <-c.slots }) }, nil
}

type releasingBody struct {
	io.ReadCloser
	release func()
}

func (b *releasingBody) Close() error {
	defer b.release()
	return b.ReadCloser.Close()
}

func (c *Client) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, c.timeout)
}

func transportError(err error) error {
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return fmt.Errorf("%w: %w", ErrTimeout, err)
	}
	return err
}

func encodeJSON(in any) ([]byte, error) {
	if in == nil {
		return nil, nil
	}
	payload, err := json.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("sarvam: encoding request: %w", err)
	}
	return payload, nil
}

func decodeJSON(r io.Reader, out any) error {
	if out == nil {
		_, _ = io.Copy(io.Discard, r)
		return nil
	}
	if err := json.NewDecoder(r).Decode(out); err != nil {
		return fmt.Errorf("%w: %w", ErrDecode, err)
	}
	return nil
}
