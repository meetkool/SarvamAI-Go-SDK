package sarvam

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"strconv"
	"sync/atomic"

	"github.com/crynta/sarvam-go-sdk/internal/wav"
	"github.com/crynta/sarvam-go-sdk/src/models"
)

const streamChunkSize = 16 << 10

type SpeechService struct{ client *Client }

type speechBody struct {
	Text                string          `json:"text"`
	LanguageCode        models.Language `json:"language_code"`
	Speaker             string          `json:"speaker,omitempty"`
	Model               models.Model    `json:"model,omitempty"`
	Pace                *float64        `json:"pace,omitempty"`
	Pitch               *float64        `json:"pitch,omitempty"`
	Loudness            *float64        `json:"loudness,omitempty"`
	Temperature         *float64        `json:"temperature,omitempty"`
	EnablePreprocessing *bool           `json:"enable_preprocessing,omitempty"`
	SpeechSampleRate    int             `json:"speech_sample_rate,omitempty"`
	OutputAudioCodec    models.Codec    `json:"output_audio_codec,omitempty"`
	OutputAudioBitrate  string          `json:"output_audio_bitrate,omitempty"`
	DictID              string          `json:"dict_id,omitempty"`
}

func (s *SpeechService) newBody(req *models.SpeechRequest, withBitrate bool) (*speechBody, models.Format, error) {
	if req == nil {
		return nil, models.Format{}, invalidRequest("request is nil")
	}
	if req.Text == "" {
		return nil, models.Format{}, invalidRequest("Text is required")
	}
	if req.Language == "" {
		return nil, models.Format{}, invalidRequest("models.Language is required, e.g. sarvam.LangHindi")
	}

	format := s.client.format(req.Format)
	body := &speechBody{
		Text:                req.Text,
		LanguageCode:        req.Language,
		Speaker:             req.Voice,
		Model:               req.Model,
		Pace:                req.Speed,
		Pitch:               req.Pitch,
		Loudness:            req.Loudness,
		Temperature:         req.Temperature,
		EnablePreprocessing: req.Preprocess,
		SpeechSampleRate:    format.SampleRate,
		OutputAudioCodec:    format.Codec,
		DictID:              req.Dictionary,
	}
	if withBitrate {
		body.OutputAudioBitrate = format.BitrateParam()
	}
	return body, format, nil
}

func (s *SpeechService) Create(ctx context.Context, req *models.SpeechRequest) (*models.Audio, error) {
	body, format, err := s.newBody(req, false)
	if err != nil {
		return nil, err
	}

	var out struct {
		RequestID string   `json:"request_id"`
		Audios    []string `json:"audios"`
	}
	if err := s.client.doJSON(ctx, http.MethodPost, "/text-to-speech", body, &out); err != nil {
		return nil, err
	}
	if len(out.Audios) == 0 {
		return nil, fmt.Errorf("%w: the reply carried no audio", ErrDecode)
	}

	parts := make([][]byte, 0, len(out.Audios))
	for _, encoded := range out.Audios {
		raw, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrDecode, err)
		}
		parts = append(parts, raw)
	}
	data, err := joinAudio(parts, format)
	if err != nil {
		return nil, err
	}
	return models.NewAudio(data, format), nil
}

func joinAudio(parts [][]byte, f models.Format) ([]byte, error) {
	if len(parts) == 1 {
		return parts[0], nil
	}
	if f.Codec == models.CodecWAV {
		data, err := wav.Join(parts)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrDecode, err)
		}
		return data, nil
	}
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out, nil
}

func (s *SpeechService) Stream(ctx context.Context, req *models.SpeechRequest) (*SpeechStream, error) {
	body, format, err := s.newBody(req, true)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.openStream(ctx, "/text-to-speech/stream", body)
	if err != nil {
		return nil, err
	}
	return &SpeechStream{body: resp.Body, format: format}, nil
}

type SpeechStream struct {
	body    io.ReadCloser
	format  models.Format
	chunk   models.AudioChunk
	index   int
	err     error
	pending error
	closed  bool
}

func (s *SpeechStream) Format() models.Format { return s.format }

func (s *SpeechStream) Next() bool {
	if s.closed {
		return false
	}
	if s.pending != nil {
		err := s.pending
		s.pending = nil
		s.finish(err)
		return false
	}
	for {
		buf := make([]byte, streamChunkSize)
		n, err := s.body.Read(buf)
		if n > 0 {
			s.chunk = models.AudioChunk{Bytes: buf[:n], Index: s.index}
			s.index++
			s.pending = err
			return true
		}
		if err != nil {
			s.finish(err)
			return false
		}

	}
}

func (s *SpeechStream) finish(err error) {
	if err != nil && err != io.EOF {
		s.err = &StreamError{ChunkIndex: s.index, Underlying: err}
	}
	s.Close()
}

func (s *SpeechStream) Chunk() models.AudioChunk { return s.chunk }

func (s *SpeechStream) Err() error { return s.err }

func (s *SpeechStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.body.Close()
}

func (s *SpeechStream) All() iter.Seq2[models.AudioChunk, error] {
	return func(yield func(models.AudioChunk, error) bool) {
		for s.Next() {
			if !yield(s.Chunk(), nil) {
				return
			}
		}
		if err := s.Err(); err != nil {
			yield(models.AudioChunk{}, err)
		}
	}
}

type ttsClientMessage struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

type ttsConfig struct {
	LanguageCode        models.Language `json:"language_code"`
	Speaker             string          `json:"speaker"`
	Model               models.Model    `json:"model,omitempty"`
	Pace                *float64        `json:"pace,omitempty"`
	Pitch               *float64        `json:"pitch,omitempty"`
	Loudness            *float64        `json:"loudness,omitempty"`
	Temperature         *float64        `json:"temperature,omitempty"`
	EnablePreprocessing *bool           `json:"enable_preprocessing,omitempty"`
	SpeechSampleRate    string          `json:"speech_sample_rate,omitempty"`
	OutputAudioCodec    models.Codec    `json:"output_audio_codec,omitempty"`
	OutputAudioBitrate  string          `json:"output_audio_bitrate,omitempty"`
	DictID              string          `json:"dict_id,omitempty"`
	MinBufferSize       int             `json:"min_buffer_size,omitempty"`
	MaxChunkLength      int             `json:"max_chunk_length,omitempty"`
}

type ttsServerMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type ttsAudioData struct {
	Audio       string `json:"audio"`
	ContentType string `json:"content_type"`
	RequestID   string `json:"request_id"`
}

type ttsEventData struct {
	EventType string `json:"event_type"`
	Message   string `json:"message"`
}

type ttsErrorData struct {
	Message   string          `json:"message"`
	Code      json.RawMessage `json:"code"`
	RequestID string          `json:"request_id"`
}

func (s *SpeechService) Duplex(ctx context.Context, req *models.SpeechRequest) (*SpeechDuplex, error) {
	if req == nil {
		return nil, invalidRequest("request is nil")
	}
	if req.Language == "" {
		return nil, invalidRequest("models.Language is required, e.g. sarvam.LangHindi")
	}
	if req.Voice == "" {
		return nil, invalidRequest("Voice is required for a Duplex session")
	}

	format := s.client.format(req.Format)
	query := url.Values{"send_completion_event": {"true"}}
	if req.Model != "" {
		query.Set("model", string(req.Model))
	}

	ws, err := s.client.dialWS(ctx, "/text-to-speech/ws", query)
	if err != nil {
		return nil, err
	}

	config := ttsConfig{
		LanguageCode:        req.Language,
		Speaker:             req.Voice,
		Model:               req.Model,
		Pace:                req.Speed,
		Pitch:               req.Pitch,
		Loudness:            req.Loudness,
		Temperature:         req.Temperature,
		EnablePreprocessing: req.Preprocess,
		OutputAudioCodec:    format.Codec,
		OutputAudioBitrate:  format.BitrateParam(),
		DictID:              req.Dictionary,
		MinBufferSize:       req.MinBufferSize,
		MaxChunkLength:      req.MaxChunkLength,
	}
	if format.SampleRate > 0 {
		config.SpeechSampleRate = strconv.Itoa(format.SampleRate)
	}
	if err := ws.writeJSON(ctx, ttsClientMessage{Type: "config", Data: config}); err != nil {
		ws.close()
		return nil, err
	}

	d := &SpeechDuplex{ws: ws, ctx: ctx, format: format}
	if req.Text != "" {
		if err := d.SendText(ctx, req.Text); err != nil {
			d.Close()
			return nil, err
		}
	}
	return d, nil
}

type SpeechDuplex struct {
	ws     *wsConn
	ctx    context.Context
	format models.Format

	chunk      models.AudioChunk
	index      int
	err        error
	sendClosed atomic.Bool
	closed     bool
}

func (d *SpeechDuplex) Format() models.Format { return d.format }

func (d *SpeechDuplex) SendText(ctx context.Context, text string) error {
	if text == "" {
		return nil
	}
	return d.ws.writeJSON(ctx, ttsClientMessage{
		Type: "text",
		Data: struct {
			Text string `json:"text"`
		}{Text: text},
	})
}

func (d *SpeechDuplex) Flush(ctx context.Context) error {
	return d.ws.writeJSON(ctx, ttsClientMessage{Type: "flush"})
}

func (d *SpeechDuplex) CloseSend(ctx context.Context) error {
	d.sendClosed.Store(true)
	return d.Flush(ctx)
}

func (d *SpeechDuplex) Next() bool {
	if d.closed {
		return false
	}
	for {
		payload, err := d.ws.read(d.ctx)
		if err != nil {
			if err != io.EOF {
				d.err = &StreamError{ChunkIndex: d.index, Underlying: err}
			}
			d.Close()
			return false
		}

		var msg ttsServerMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			d.fail(fmt.Errorf("%w: %w", ErrDecode, err))
			return false
		}

		switch msg.Type {
		case "audio":
			var data ttsAudioData
			if err := json.Unmarshal(msg.Data, &data); err != nil {
				d.fail(fmt.Errorf("%w: %w", ErrDecode, err))
				return false
			}
			raw, err := base64.StdEncoding.DecodeString(data.Audio)
			if err != nil {
				d.fail(fmt.Errorf("%w: %w", ErrDecode, err))
				return false
			}
			if len(raw) == 0 {
				continue
			}
			d.chunk = models.AudioChunk{Bytes: raw, Index: d.index}
			d.index++
			return true

		case "event":
			var data ttsEventData
			if err := json.Unmarshal(msg.Data, &data); err != nil {
				continue
			}

			if data.EventType == "final" && d.sendClosed.Load() {
				d.Close()
				return false
			}

		case "error":
			d.fail(ttsError(msg.Data))
			return false
		}

	}
}

func (d *SpeechDuplex) fail(err error) {
	d.err = err
	d.Close()
}

func ttsError(raw json.RawMessage) error {
	var data ttsErrorData
	if err := json.Unmarshal(raw, &data); err != nil {
		return fmt.Errorf("%w: %w", ErrDecode, err)
	}
	apiErr := &APIError{Message: data.Message, RequestID: data.RequestID, Raw: raw}
	if status, err := strconv.Atoi(string(data.Code)); err == nil {
		apiErr.StatusCode = status
	} else {
		var code string
		if err := json.Unmarshal(data.Code, &code); err == nil {
			apiErr.Code = code
		}
	}
	return apiErr
}

func (d *SpeechDuplex) Chunk() models.AudioChunk { return d.chunk }

func (d *SpeechDuplex) Err() error { return d.err }

func (d *SpeechDuplex) Close() error {
	if d.closed {
		return nil
	}
	d.closed = true
	return d.ws.close()
}

func (d *SpeechDuplex) All() iter.Seq2[models.AudioChunk, error] {
	return func(yield func(models.AudioChunk, error) bool) {
		for d.Next() {
			if !yield(d.Chunk(), nil) {
				return
			}
		}
		if err := d.Err(); err != nil {
			yield(models.AudioChunk{}, err)
		}
	}
}
