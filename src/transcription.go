package sarvam

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strconv"
	"time"

	"github.com/crynta/sarvam-go-sdk/internal/multipart"
	"github.com/crynta/sarvam-go-sdk/src/models"
)

const restAudioLimit = 30 * time.Second

type TranscriptionService struct{ client *Client }

func (t *TranscriptionService) Create(ctx context.Context, req *models.TranscriptionRequest) (*models.TranscriptionResult, error) {
	if req == nil || req.Audio == nil {
		return nil, invalidRequest("models.Audio is required")
	}
	if d, ok := req.Audio.Duration(); ok && d > restAudioLimit {
		return nil, &AudioTooLongError{Duration: d, MaxDuration: restAudioLimit, Endpoint: "/speech-to-text"}
	}

	var fields []multipart.Field
	add := func(name, value string) {
		if value != "" {
			fields = append(fields, multipart.Field{Name: name, Value: value})
		}
	}
	add("model", string(req.Model))
	add("mode", string(req.Mode))
	add("language_code", string(req.Language))
	add("input_audio_codec", req.InputCodec)
	if req.Timestamps {
		add("with_timestamps", "true")
	}
	if len(req.Keyterms) > 0 {
		encoded, err := json.Marshal(req.Keyterms)
		if err != nil {
			return nil, fmt.Errorf("sarvam: encoding keyterms: %w", err)
		}
		add("keyterms", string(encoded))
	}

	var out sttResponse
	if err := t.client.doUpload(ctx, "/speech-to-text", fields, req.Audio, &out); err != nil {
		return nil, err
	}
	return out.result(), nil
}

type sttResponse struct {
	RequestID           string  `json:"request_id"`
	Transcript          string  `json:"transcript"`
	LanguageCode        string  `json:"language_code"`
	LanguageProbability float64 `json:"language_probability"`
	Timestamps          *struct {
		Words  []string  `json:"words"`
		Chunks []string  `json:"chunks"`
		Start  []float64 `json:"start_time_seconds"`
		End    []float64 `json:"end_time_seconds"`
	} `json:"timestamps"`
	DiarizedTranscript *struct {
		Entries []struct {
			Transcript string     `json:"transcript"`
			Start      float64    `json:"start_time_seconds"`
			End        float64    `json:"end_time_seconds"`
			SpeakerID  flexString `json:"speaker_id"`
		} `json:"entries"`
	} `json:"diarized_transcript"`
}

func (r *sttResponse) result() *models.TranscriptionResult {
	out := &models.TranscriptionResult{
		RequestID:           r.RequestID,
		Text:                r.Transcript,
		Language:            models.Language(r.LanguageCode),
		LanguageProbability: r.LanguageProbability,
	}

	if d := r.DiarizedTranscript; d != nil && len(d.Entries) > 0 {
		for _, e := range d.Entries {
			out.Words = append(out.Words, models.Word{
				Text:    e.Transcript,
				Start:   floatSeconds(e.Start),
				End:     floatSeconds(e.End),
				Speaker: string(e.SpeakerID),
			})
		}
		return out
	}

	if t := r.Timestamps; t != nil {
		texts := t.Words
		if len(texts) == 0 {
			texts = t.Chunks
		}
		for i, text := range texts {
			w := models.Word{Text: text}
			if i < len(t.Start) {
				w.Start = floatSeconds(t.Start[i])
			}
			if i < len(t.End) {
				w.End = floatSeconds(t.End[i])
			}
			out.Words = append(out.Words, w)
		}
	}
	return out
}

func floatSeconds(s float64) time.Duration {
	return time.Duration(s * float64(time.Second))
}

type flexString string

func (f *flexString) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*f = flexString(s)
		return nil
	}
	*f = flexString(data)
	return nil
}

var realtimeFormats = []models.Format{models.PCM16(16000), models.PCM16(8000), models.ULaw8000(), models.ALaw8000()}

func (t *TranscriptionService) Stream(ctx context.Context, req *models.TranscriptionStreamRequest) (*TranscriptionStream, error) {
	if req == nil {
		req = &models.TranscriptionStreamRequest{}
	}
	format := req.InputFormat
	if format.IsZero() {
		format = models.PCM16(16000)
	}
	if !supportedRealtimeFormat(format) {
		return nil, &FormatError{Format: format, Endpoint: "realtime transcription", Supported: realtimeFormats}
	}

	language := string(req.Language)
	if language == "" {
		language = "auto"
	}
	query := url.Values{
		"language_code": {language},
		"encoding":      {string(format.Codec)},
		"sample_rate":   {strconv.Itoa(format.SampleRate)},
	}
	if req.Model != "" {
		query.Set("model", string(req.Model))
	}
	if req.Mode != "" {
		query.Set("mode", string(req.Mode))
	}
	if req.StreamType != "" {
		query.Set("stream_type", string(req.StreamType))
	}
	if req.Prompt != "" {
		query.Set("prompt", req.Prompt)
	}
	if req.Timestamps {
		query.Set("return_timestamps", "true")
	}
	if req.ManualEndpointing {
		query.Set("endpointing", "manual")
	}
	if req.VADThreshold != nil {
		query.Set("threshold", strconv.FormatFloat(*req.VADThreshold, 'g', -1, 64))
	}
	if req.SilenceDuration > 0 {
		query.Set("silence_duration_ms", strconv.Itoa(int(req.SilenceDuration.Milliseconds())))
	}
	if req.MinSpeechDuration > 0 {
		query.Set("min_speech_duration_ms", strconv.Itoa(int(req.MinSpeechDuration.Milliseconds())))
	}

	ws, err := t.client.dialWS(ctx, "/speech-to-text-realtime/ws", query)
	if err != nil {
		return nil, err
	}
	return &TranscriptionStream{ws: ws, format: format, log: t.client.log}, nil
}

func supportedRealtimeFormat(f models.Format) bool {
	for _, supported := range realtimeFormats {
		if f == supported {
			return true
		}
	}
	return false
}

type TranscriptionStream struct {
	ws     *wsConn
	format models.Format
	log    *slog.Logger
	closed bool
}

func (s *TranscriptionStream) Format() models.Format { return s.format }

func (s *TranscriptionStream) SendAudio(ctx context.Context, frame []byte) error {
	if len(frame) == 0 {
		return nil
	}
	return s.ws.writeJSON(ctx, map[string]string{
		"event": "audio_input",
		"audio": base64.StdEncoding.EncodeToString(frame),
	})
}

func (s *TranscriptionStream) SpeechStart(ctx context.Context) error {
	return s.ws.writeJSON(ctx, map[string]string{"event": "speech_start"})
}

func (s *TranscriptionStream) SpeechEnd(ctx context.Context) error {
	return s.ws.writeJSON(ctx, map[string]string{"event": "speech_end"})
}

func (s *TranscriptionStream) Flush(ctx context.Context) error {
	return s.ws.writeJSON(ctx, map[string]string{"event": "flush"})
}

func (s *TranscriptionStream) CloseSend(ctx context.Context) error {
	return s.ws.writeJSON(ctx, map[string]string{"event": "end"})
}

func (s *TranscriptionStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.ws.close()
}

type realtimeEvent struct {
	Event              string  `json:"event"`
	RequestID          string  `json:"request_id"`
	UtteranceIdx       int     `json:"utterance_idx"`
	Text               string  `json:"text"`
	Language           string  `json:"language"`
	LanguageConfidence float64 `json:"language_confidence"`
	Confidence         float64 `json:"confidence"`
	StartS             float64 `json:"start_s"`
	EndS               float64 `json:"end_s"`
	AudioDurationS     float64 `json:"audio_duration_s"`
	TotalUtterances    int     `json:"total_utterances"`
	Code               string  `json:"code"`
	Message            string  `json:"message"`
	IsFatal            bool    `json:"is_fatal"`
	StatusCode         int     `json:"status_code"`
}

func (s *TranscriptionStream) Next(ctx context.Context) (models.TranscriptionEvent, error) {
	if s.closed {
		return nil, io.EOF
	}
	for {
		payload, err := s.ws.read(ctx)
		if err != nil {
			return nil, err
		}

		var ev realtimeEvent
		if err := json.Unmarshal(payload, &ev); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrDecode, err)
		}

		switch ev.Event {
		case "session.begin":
			return &models.SessionStarted{RequestID: ev.RequestID}, nil
		case "vad.speech_start":
			return &models.SpeechStarted{Utterance: ev.UtteranceIdx, Confidence: ev.Confidence}, nil
		case "vad.speech_end":
			return &models.SpeechEnded{Utterance: ev.UtteranceIdx, Confidence: ev.Confidence}, nil
		case "transcript.partial":
			return &models.PartialTranscript{Utterance: ev.UtteranceIdx, Text: ev.Text, Language: models.Language(ev.Language)}, nil
		case "transcript.final":
			return &models.FinalTranscript{
				Utterance:          ev.UtteranceIdx,
				Text:               ev.Text,
				Language:           models.Language(ev.Language),
				LanguageConfidence: ev.LanguageConfidence,
				Start:              floatSeconds(ev.StartS),
				End:                floatSeconds(ev.EndS),
			}, nil
		case "session.end":
			return &models.SessionEnded{
				AudioDuration: floatSeconds(ev.AudioDurationS),
				Utterances:    ev.TotalUtterances,
			}, nil
		case "error":
			apiErr := &APIError{
				StatusCode: ev.StatusCode,
				Code:       ev.Code,
				Message:    ev.Message,
				RequestID:  ev.RequestID,
				Raw:        payload,
			}
			if ev.IsFatal {
				return nil, apiErr
			}

			s.log.Warn("sarvam realtime warning", "code", ev.Code, "message", ev.Message)
		}

	}
}
