package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	sarvam "github.com/meetkool/SarvamAI-Go-SDK/src"
	"github.com/meetkool/SarvamAI-Go-SDK/src/models"
)

const systemPrompt = "You are a friendly voice assistant. Keep replies to one or two short sentences, " +
	"unless the user asks for a story, an essay, a list or more detail, and then give them the whole thing. " +
	"Always reply in the same language the user spoke to you in. " +
	"Never use markdown, lists, emoji or special characters, because your words are read out loud."

const (
	defaultVADSilence   = 300 * time.Millisecond
	defaultVADMinSpeech = 200 * time.Millisecond
	defaultVADThreshold = 0.4

	minVADDuration = time.Millisecond
	maxVADDuration = 10 * time.Second
)

type vadSettings struct {
	silence   time.Duration
	minSpeech time.Duration
	threshold float64
}

func loadVAD() vadSettings {
	return vadSettings{
		silence:   envDuration("SARVAM_VAD_SILENCE_MS", defaultVADSilence),
		minSpeech: envDuration("SARVAM_VAD_MIN_SPEECH_MS", defaultVADMinSpeech),
		threshold: envThreshold("SARVAM_VAD_THRESHOLD", defaultVADThreshold),
	}
}

func envDuration(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms < int(minVADDuration/time.Millisecond) || ms > int(maxVADDuration/time.Millisecond) {
		log.Printf("ignoring %s=%q, using %v", name, raw, fallback)
		return fallback
	}
	return time.Duration(ms) * time.Millisecond
}

func envThreshold(name string, fallback float64) float64 {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v <= 0 || v > 1 {
		log.Printf("ignoring %s=%q, using %v", name, raw, fallback)
		return fallback
	}
	return v
}

type event struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Language string `json:"language,omitempty"`
	State    string `json:"state,omitempty"`
}

type turnRequest struct {
	text string
	lang models.Language
}

type session struct {
	client *sarvam.Client
	conn   *websocket.Conn
	voice  string

	writeMu sync.Mutex
	turns   chan turnRequest

	mu         sync.Mutex
	cancel     context.CancelFunc
	history    []models.Message
	duplex     *sarvam.SpeechDuplex
	duplexLang models.Language

	speaking atomic.Bool
}

func (s *session) run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer s.dropSpeaker()

	vad := loadVAD()
	log.Printf("vad: silence=%v min_speech=%v threshold=%v", vad.silence, vad.minSpeech, vad.threshold)

	stt, err := s.client.Transcription.Stream(ctx, &models.TranscriptionStreamRequest{
		Model:             models.STTSaarasV3Realtime,
		InputFormat:       models.PCM16(16000),
		StreamType:        models.StreamFast,
		VADThreshold:      models.Float(vad.threshold),
		SilenceDuration:   vad.silence,
		MinSpeechDuration: vad.minSpeech,
	})
	if err != nil {
		s.send(event{Type: "error", Text: err.Error()})
		return err
	}
	defer stt.Close()

	s.history = []models.Message{models.SystemMessage(systemPrompt)}
	s.turns = make(chan turnRequest, 1)
	go s.work(ctx)

	s.send(event{Type: "state", State: "listening"})
	log.Println("session started")

	failed := make(chan error, 2)
	go func() { failed <- s.pumpMic(ctx, stt) }()
	go func() { failed <- s.pumpTranscripts(ctx, stt) }()
	return <-failed
}

func (s *session) work(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-s.turns:
			s.turn(ctx, req.text, req.lang)
		}
	}
}

func (s *session) queue(req turnRequest) {
	s.interrupt()

	select {
	case <-s.turns:
	default:
	}
	select {
	case s.turns <- req:
	default:
	}
}

func (s *session) pumpMic(ctx context.Context, stt *sarvam.TranscriptionStream) error {
	for {
		kind, data, err := s.conn.Read(ctx)
		if err != nil {
			return err
		}
		if kind != websocket.MessageBinary {
			continue
		}
		if err := stt.SendAudio(ctx, data); err != nil {
			return err
		}
	}
}

func (s *session) pumpTranscripts(ctx context.Context, stt *sarvam.TranscriptionStream) error {
	for {
		ev, err := stt.Next(ctx)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		switch e := ev.(type) {
		case *models.PartialTranscript:
			s.send(event{Type: "partial", Text: e.Text})

		case *models.SpeechStarted:
			if s.speaking.Load() {
				log.Println("barge-in")
				s.interrupt()
			}

		case *models.FinalTranscript:
			text := strings.TrimSpace(e.Text)
			if text == "" {
				continue
			}
			if s.speaking.Load() && len(strings.Fields(text)) < 2 {
				log.Printf("ignoring backchannel %q", text)
				continue
			}
			s.send(event{Type: "final", Text: text, Language: string(e.Language)})
			s.queue(turnRequest{text: text, lang: e.Language})
		}
	}
}

func (s *session) interrupt() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
		s.speaking.Store(false)
		s.send(event{Type: "interrupt"})
	}
}

func (s *session) speaker(ctx context.Context, lang models.Language) (*sarvam.SpeechDuplex, error) {
	s.mu.Lock()
	if s.duplex != nil && !s.duplex.IsClosed() && s.duplexLang == lang {
		duplex := s.duplex
		s.mu.Unlock()
		duplex.Reset()
		return duplex, nil
	}
	previous := s.duplex
	s.duplex = nil
	s.mu.Unlock()

	if previous != nil {
		previous.Close()
	}

	duplex, err := s.client.Speech.Duplex(ctx, &models.SpeechRequest{
		Model:    models.TTSBulbulV3,
		Voice:    s.voice,
		Language: lang,
		Format:   models.PCM16(24000),
	})
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.duplex, s.duplexLang = duplex, lang
	s.mu.Unlock()
	return duplex, nil
}

func (s *session) dropSpeaker() {
	s.mu.Lock()
	duplex := s.duplex
	s.duplex = nil
	s.mu.Unlock()

	if duplex != nil {
		duplex.Close()
	}
}

func (s *session) turn(parent context.Context, userText string, spoken models.Language) {
	turnCtx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	defer func() {
		s.mu.Lock()
		s.cancel = nil
		s.mu.Unlock()
		s.speaking.Store(false)
	}()

	s.mu.Lock()
	s.cancel = cancel
	s.history = append(s.history, models.UserMessage(userText))
	messages := append([]models.Message(nil), s.history...)
	s.mu.Unlock()

	s.send(event{Type: "interrupt"})
	s.send(event{Type: "state", State: "thinking"})

	duplex, err := s.speaker(parent, ttsLanguage(spoken))
	if err != nil {
		log.Println("tts open:", err)
		s.send(event{Type: "error", Text: err.Error()})
		s.send(event{Type: "state", State: "listening"})
		return
	}
	// Next reads with the connection's session context. Close it on turn
	// cancellation so a silent service cannot block all subsequent turns.
	closeDone := make(chan struct{})
	stopClose := context.AfterFunc(turnCtx, func() {
		duplex.Close()
		close(closeDone)
	})
	defer func() {
		if !stopClose() {
			<-closeDone
		}
	}()

	var audioBytes atomic.Int64
	audioDone := make(chan struct{})
	go func() {
		defer close(audioDone)
		for duplex.Next() {
			if turnCtx.Err() != nil {
				continue
			}
			chunk := duplex.Chunk().Bytes
			if audioBytes.Add(int64(len(chunk))) == int64(len(chunk)) {
				s.speaking.Store(true)
				s.send(event{Type: "state", State: "speaking"})
			}
			s.sendAudio(turnCtx, chunk)
		}
		if err := duplex.Err(); err != nil {
			log.Println("tts stream:", err)
		}
	}()

	stream, err := s.client.Chat.Stream(turnCtx, &models.ChatRequest{
		Model:           models.ChatSarvam105BConversations,
		Messages:        messages,
		ReasoningEffort: models.ReasoningOff,
		MaxTokens:       1500,
	})
	if err != nil {
		log.Println("chat open:", err)
		s.send(event{Type: "error", Text: err.Error()})
		duplex.Close()
		<-audioDone
		s.send(event{Type: "state", State: "listening"})
		return
	}

	var reply strings.Builder
	for stream.Next() {
		token := stream.Chunk().Content()
		if token == "" {
			continue
		}
		reply.WriteString(token)
		s.send(event{Type: "reply", Text: token})

		if err := duplex.SendToken(turnCtx, token); err != nil {
			log.Println("tts sendtoken:", err)
			duplex.Close()
			break
		}
	}
	stream.Close()
	if err := stream.Err(); err != nil {
		log.Println("chat stream:", err)
	}

	if err := duplex.CloseSend(turnCtx); err != nil {
		log.Println("tts close send:", err)
		duplex.Close()
	}
	<-audioDone
	s.speaking.Store(false)

	said := strings.TrimSpace(duplex.SpokenText())
	if said == "" {
		said = strings.TrimSpace(reply.String())
	}
	interrupted := turnCtx.Err() != nil
	log.Printf("turn: %q -> %q (%d audio bytes, interrupted=%v)", userText, said, audioBytes.Load(), interrupted)

	if interrupted || duplex.IsClosed() || audioBytes.Load() == 0 {
		s.dropSpeaker()
	}
	if !interrupted && said != "" && audioBytes.Load() == 0 {
		s.send(event{Type: "error", Text: "Speech audio was unavailable. Please try again; the next reply will use a new voice connection."})
	}
	if said != "" {
		s.mu.Lock()
		s.history = append(s.history, models.AssistantMessage(said))
		s.mu.Unlock()
	}
	if !interrupted {
		s.send(event{Type: "state", State: "listening"})
	}
}

func ttsLanguage(spoken models.Language) models.Language {
	switch spoken {
	case models.LangEnglish, models.LangHindi, models.LangBengali, models.LangGujarati,
		models.LangKannada, models.LangMalayalam, models.LangMarathi, models.LangOdia,
		models.LangPunjabi, models.LangTamil, models.LangTelugu:
		return spoken
	case "or-IN":
		return models.LangOdia
	}
	return models.LangEnglish
}

func (s *session) send(e event) {
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.conn.Write(ctx, websocket.MessageText, data)
}

func (s *session) sendAudio(ctx context.Context, pcm []byte) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.conn.Write(ctx, websocket.MessageBinary, pcm)
}
