package sarvam

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/meetkool/SarvamAI-Go-SDK/src/models"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var (
	ErrMissingAPIKey      = errors.New("sarvam: missing API key (set SARVAM_API_KEY or use WithAPIKey)")
	ErrUnauthorized       = errors.New("sarvam: unauthorized")
	ErrInvalidRequest     = errors.New("sarvam: invalid request")
	ErrRateLimited        = errors.New("sarvam: rate limited")
	ErrQuotaExceeded      = errors.New("sarvam: credits exhausted")
	ErrServer             = errors.New("sarvam: server error")
	ErrTimeout            = errors.New("sarvam: request timed out")
	ErrMaxRetriesExceeded = errors.New("sarvam: all retries failed")
	ErrUnsupportedFormat  = errors.New("sarvam: unsupported audio format")
	ErrAudioTooLong       = errors.New("sarvam: audio is too long for this endpoint")
	ErrDecode             = errors.New("sarvam: could not decode the response")
	ErrStreamClosed       = errors.New("sarvam: stream is closed")
)

const (
	codeQuotaExceeded = "insufficient_quota_error"
	codeRateLimited   = "rate_limit_exceeded_error"
)

type errorKind int

const (
	kindUnknown errorKind = iota
	kindQuota
	kindRate
	kindAuth
	kindBadRequest
	kindServer
)

func (k errorKind) String() string {
	switch k {
	case kindQuota:
		return "quota"
	case kindRate:
		return "rate_limit"
	case kindAuth:
		return "auth"
	case kindBadRequest:
		return "bad_request"
	case kindServer:
		return "server"
	}
	return "unknown"
}

func (k errorKind) sentinel() error {
	switch k {
	case kindQuota:
		return ErrQuotaExceeded
	case kindRate:
		return ErrRateLimited
	case kindAuth:
		return ErrUnauthorized
	case kindBadRequest:
		return ErrInvalidRequest
	case kindServer:
		return ErrServer
	}
	return nil
}

func (k errorKind) retryable() bool { return k == kindRate || k == kindServer }

type classifyRule struct {
	kind       errorKind
	code       string
	statuses   []int
	statusFrom int
}

var classifyRules = []classifyRule{
	{kind: kindQuota, code: codeQuotaExceeded},
	{kind: kindRate, code: codeRateLimited},
	{kind: kindRate, statuses: []int{http.StatusTooManyRequests}},
	{kind: kindAuth, statuses: []int{http.StatusUnauthorized, http.StatusForbidden}},
	{kind: kindBadRequest, statuses: []int{http.StatusBadRequest, http.StatusUnprocessableEntity}},
	{kind: kindServer, statusFrom: 500},
}

func classify(status int, code string) errorKind {
	for _, r := range classifyRules {
		if r.code != "" && r.code == code {
			return r.kind
		}
		for _, s := range r.statuses {
			if s == status {
				return r.kind
			}
		}
		if r.statusFrom > 0 && status >= r.statusFrom {
			return r.kind
		}
	}
	return kindUnknown
}

type APIError struct {
	StatusCode int
	Code       string
	Message    string
	RequestID  string
	Raw        []byte
}

func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.StatusCode)
	}
	s := fmt.Sprintf("sarvam: %s (status %d", msg, e.StatusCode)
	if e.Code != "" {
		s += ", code " + e.Code
	}
	if e.RequestID != "" {
		s += ", request " + e.RequestID
	}
	return s + ")"
}

func (e *APIError) Kind() string { return classify(e.StatusCode, e.Code).String() }

func (e *APIError) Unwrap() error { return classify(e.StatusCode, e.Code).sentinel() }

func (e *APIError) retryable() bool {
	if e.StatusCode == http.StatusNotImplemented {
		return false
	}
	return classify(e.StatusCode, e.Code).retryable()
}

type RateLimitError struct {
	*APIError
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("%s; retry after %s", e.APIError.Error(), e.RetryAfter)
	}
	return e.APIError.Error()
}

func (e *RateLimitError) Unwrap() error { return e.APIError }

type AudioTooLongError struct {
	Duration    time.Duration
	MaxDuration time.Duration
	Endpoint    string
}

func (e *AudioTooLongError) Error() string {
	return fmt.Sprintf("sarvam: audio is %s but %s accepts at most %s; use client.Batch for long recordings",
		e.Duration.Round(time.Millisecond), e.Endpoint, e.MaxDuration)
}
func (e *AudioTooLongError) Unwrap() error { return ErrAudioTooLong }

type FormatError struct {
	Format    models.Format
	Endpoint  string
	Supported []models.Format
}

func (e *FormatError) Error() string {
	supported := make([]string, len(e.Supported))
	for i, f := range e.Supported {
		supported[i] = f.String()
	}
	return fmt.Sprintf("sarvam: %s cannot use %s; supported: %s",
		e.Endpoint, e.Format, strings.Join(supported, ", "))
}
func (e *FormatError) Unwrap() error { return ErrUnsupportedFormat }

type StreamError struct {
	ChunkIndex int
	Underlying error
}

func (e *StreamError) Error() string {
	return fmt.Sprintf("sarvam: stream failed after %d chunks: %v", e.ChunkIndex, e.Underlying)
}
func (e *StreamError) Unwrap() error { return e.Underlying }

type MaxRetriesError struct {
	Attempts int
	Err      error
}

func (e *MaxRetriesError) Error() string {
	return fmt.Sprintf("sarvam: gave up after %d attempts: %v", e.Attempts, e.Err)
}
func (e *MaxRetriesError) Unwrap() []error { return []error{ErrMaxRetriesExceeded, e.Err} }

func invalidRequest(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidRequest, fmt.Sprintf(format, args...))
}

type apiErrorBody struct {
	Error struct {
		Message   string `json:"message"`
		Code      string `json:"code"`
		RequestID string `json:"request_id"`
	} `json:"error"`
	Message string `json:"message"`
}

func parseAPIError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	apiErr := newAPIError(resp, raw)

	if resp.StatusCode == http.StatusTooManyRequests {
		return &RateLimitError{APIError: apiErr, RetryAfter: retryAfter(resp.Header)}
	}
	return apiErr
}

func newAPIError(resp *http.Response, raw []byte) *APIError {
	e := &APIError{
		StatusCode: resp.StatusCode,
		Raw:        raw,
		RequestID:  resp.Header.Get("X-Request-Id"),
	}

	var body apiErrorBody
	if err := json.Unmarshal(raw, &body); err == nil {
		e.Message = body.Error.Message
		e.Code = body.Error.Code
		if body.Error.RequestID != "" {
			e.RequestID = body.Error.RequestID
		}
		if e.Message == "" {
			e.Message = body.Message
		}
	}
	if e.Message == "" {
		e.Message = strings.TrimSpace(string(raw))
	}
	return e
}

func retryAfter(h http.Header) time.Duration {
	v := h.Get("Retry-After")
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
