package models

import (
	"fmt"
	"strings"
	"time"
)

type TranscriptionRequest struct {
	Model      Model
	Audio      Input
	Language   Language
	Mode       Mode
	Timestamps bool
	Keyterms   []string
	InputCodec string
}

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

type TranscriptionResult struct {
	RequestID           string
	Text                string
	Language            Language
	LanguageProbability float64
	Words               []Word
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
