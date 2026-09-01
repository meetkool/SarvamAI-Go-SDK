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
	"strings"
	"time"

	"github.com/crynta/sarvam-go-sdk/internal/multipart"
)

const restAudioLimit = 30 * time.Second

type TranscriptionService struct{ client *Client }

type TranscriptionRequest struct {
	Model      Model
	Audio      Input
	Language   Language
	Mode       Mode
	Timestamps bool
	Keyterms   []string
	InputCodec string
}

func (t *TranscriptionService) Create(ctx context.Context, req *TranscriptionRequest) (*TranscriptionResult, error) {
	if req == nil || req.Audio == nil {
		return nil, invalidRequest("Audio is required")
	}
	if d, ok := req.Audio.duration(); ok && d > restAudioLimit {
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

type TranscriptionResult struct {
	RequestID string
	Text      string
	Language  Language

	LanguageProbability float64

	Words []Word
}

type Word struct {
	Text    string
	Start   time.Duration
	End     time.Duration
	Speaker string
}

type SpeakerTurn struct {
	Speaker string
	Text    string
	Start   time.Duration
	End     time.Duration
}

func (r *TranscriptionResult) SpeakerTurns() []SpeakerTurn {
	var turns []SpeakerTurn
	for _, w := range r.Words {
		if n := len(turns); n > 0 && turns[n-1].Speaker == w.Speaker {
			turns[n-1].Text += " " + w.Text
			turns[n-1].End = w.End
			continue
		}
		turns = append(turns, SpeakerTurn{Speaker: w.Speaker, Text: w.Text, Start: w.Start, End: w.End})
	}
	return turns
}

func (r *TranscriptionResult) SRT() string {
	var b strings.Builder
	for i, w := range r.Words {
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n", i+1, timestamp(w.Start, ','), timestamp(w.End, ','), w.Text)
	}
	return b.String()
}

func (r *TranscriptionResult) VTT() string {
	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	for _, w := range r.Words {
		fmt.Fprintf(&b, "%s --> %s\n%s\n\n", timestamp(w.Start, '.'), timestamp(w.End, '.'), w.Text)
	}
	return b.String()
}

func timestamp(d time.Duration, sep byte) string {
	if d < 0 {
		d = 0
	}
	return fmt.Sprintf("%02d:%02d:%02d%c%03d",
		int(d/time.Hour), int(d/time.Minute)%60, int(d/time.Second)%60, sep, int(d/time.Millisecond)%1000)
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

func (r *sttResponse) result() *TranscriptionResult {
	out := &TranscriptionResult{
		RequestID:           r.RequestID,
		Text:                r.Transcript,
		Language:            Language(r.LanguageCode),
		LanguageProbability: r.LanguageProbability,
	}

	if d := r.DiarizedTranscript; d != nil && len(d.Entries) > 0 {
		for _, e := range d.Entries {
			out.Words = append(out.Words, Word{
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
			w := Word{Text: text}
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

var realtimeFormats = []Format{PCM16(16000), PCM16(8000), ULaw8000(), ALaw8000()}

type TranscriptionStreamRequest struct {
	Model       Model
	Language    Language
	Mode        Mode
	InputFormat Format
	StreamType  StreamType
	Prompt      string
	Timestamps  bool

	ManualEndpointing bool
	VADThreshold      *float64
	SilenceDuration   time.Duration
	MinSpeechDuration time.Duration
}

func (t *TranscriptionService) Stream(ctx context.Context, req *TranscriptionStreamRequest) (*TranscriptionStream, error) {
	if req == nil {
		req = &TranscriptionStreamRequest{}
	}
	format := req.InputFormat
	if format.IsZero() {
		format = PCM16(16000)
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

func supportedRealtimeFormat(f Format) bool {
	for _, supported := range realtimeFormats {
		if f == supported {
			return true
		}
	}
	return false
}

type TranscriptionStream struct {
	ws     *wsConn
	format Format
	log    *slog.Logger
	closed bool
}

func (s *TranscriptionStream) Format() Format { return s.format }

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

type TranscriptionEvent interface{ isTranscriptionEvent() }

type SessionStarted struct{ RequestID string }

type SpeechStarted struct {
	Utterance  int
	Confidence float64
}

type SpeechEnded struct {
	Utterance  int
	Confidence float64
}

type PartialTranscript struct {
	Utterance int
	Text      string
	Language  Language
}

type FinalTranscript struct {
	Utterance          int
	Text               string
	Language           Language
	LanguageConfidence float64
	Start              time.Duration
	End                time.Duration
}

type SessionEnded struct {
	AudioDuration time.Duration
	Utterances    int
}

func (*SessionStarted) isTranscriptionEvent()    {}
func (*SpeechStarted) isTranscriptionEvent()     {}
func (*SpeechEnded) isTranscriptionEvent()       {}
func (*PartialTranscript) isTranscriptionEvent() {}
func (*FinalTranscript) isTranscriptionEvent()   {}
func (*SessionEnded) isTranscriptionEvent()      {}

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

func (s *TranscriptionStream) Next(ctx context.Context) (TranscriptionEvent, error) {
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
			return &SessionStarted{RequestID: ev.RequestID}, nil
		case "vad.speech_start":
			return &SpeechStarted{Utterance: ev.UtteranceIdx, Confidence: ev.Confidence}, nil
		case "vad.speech_end":
			return &SpeechEnded{Utterance: ev.UtteranceIdx, Confidence: ev.Confidence}, nil
		case "transcript.partial":
			return &PartialTranscript{Utterance: ev.UtteranceIdx, Text: ev.Text, Language: Language(ev.Language)}, nil
		case "transcript.final":
			return &FinalTranscript{
				Utterance:          ev.UtteranceIdx,
				Text:               ev.Text,
				Language:           Language(ev.Language),
				LanguageConfidence: ev.LanguageConfidence,
				Start:              floatSeconds(ev.StartS),
				End:                floatSeconds(ev.EndS),
			}, nil
		case "session.end":
			return &SessionEnded{
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
