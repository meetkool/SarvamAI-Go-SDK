package sarvam

import "time"

const (
	MetricRequest         = "http.request"
	MetricRetry           = "http.retry"
	MetricFirstToken      = "chat.first_token"
	MetricFirstAudio      = "tts.first_audio"
	MetricFirstTranscript = "stt.first_transcript"
)

type Metric struct {
	Name     string
	Endpoint string
	Duration time.Duration
	Status   int
	Attempt  int
	Err      error
}

func (c *Client) emit(m Metric) {
	if c.metrics != nil {
		c.metrics(m)
	}
}
