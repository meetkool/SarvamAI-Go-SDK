# Sarvam Go SDK

A Go SDK for the [Sarvam AI](https://www.sarvam.ai) audio APIs: text to speech, streaming speech, transcription, live transcription, and batch jobs with speaker labels.

## Features

- **Chat** — Sarvam-105B, one-shot or streamed token by token, with tool calling
- **Text to speech** — 30+ voices across 11 Indian languages
- **Streaming speech** — audio arrives piece by piece, so playback starts early
- **Two-way speech** — push text in as an LLM writes it, get audio straight back
- **Transcription** — clips up to 30 seconds, with phrase-level timings
- **Live transcription** — WebSocket session with partial and final transcripts
- **Batch jobs** — files up to two hours, with speaker labels (diarization)
- **Subtitles** — SRT and WebVTT straight off a result
- **Formats** — MP3, WAV, FLAC, Opus, AAC, PCM16, mu-law and A-law, with sample rate control
- **Playback and microphone** — optional sub-packages, kept out of the core
- **Context aware** — every call takes a `context.Context`, and cancelling stops the request, the upload, or the stream
- **Automatic retries** — with backoff, on network errors, 429 and 5xx
- **No dependencies in the core** — the WebSocket library is only pulled in for streaming sessions

## Requirements

- Go 1.25 or newer
- A Sarvam API subscription key
- Microphone capture (the `mic` package only) needs cgo and a C compiler

## Installation

```bash
go get github.com/meetkool/SarvamAI-Go-SDK
```

## Quick Start

```go
package main

import (
	"context"
	"fmt"
	"log"

	sarvam "github.com/meetkool/SarvamAI-Go-SDK/src"
	"github.com/meetkool/SarvamAI-Go-SDK/src/models"
)

func main() {
	// Reads SARVAM_API_KEY from the environment
	client, err := sarvam.NewFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	audio, err := client.Speech.Create(context.Background(), &models.SpeechRequest{
		Model:    models.TTSBulbulV3,
		Voice:    "shubh",
		Language: models.LangHindi,
		Text:     "Namaste, yah Sarvam Go SDK hai.",
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := audio.Save("hello.wav"); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%d bytes, %.2fs\n", audio.Len(), audio.Duration().Seconds())
}
```

## Authentication

```bash
export SARVAM_API_KEY="your-api-key"
```

```powershell
$env:SARVAM_API_KEY = "your-api-key"
```

Or pass the key in:

```go
client, err := sarvam.New(sarvam.WithAPIKey("your-api-key"))
```

## Chat

The brain, if you are building an agent — Sarvam's own LLM.

```go
resp, err := client.Chat.Create(ctx, &models.ChatRequest{
	Model: models.ChatSarvam105B,
	Messages: []models.Message{
		models.SystemMessage("You are helpful. Answer in one short sentence."),
		models.UserMessage("Bharat me sabse lambi nadi kaunsi hai?"),
	},
	Temperature: models.Float(0.5),
})

fmt.Println(resp.Content())
fmt.Println(resp.Usage.TotalTokens)
```

Token by token, which is what you feed into `Speech.Duplex` for a voice agent:

```go
stream, err := client.Chat.Stream(ctx, &models.ChatRequest{...})
defer stream.Close()

for stream.Next() {
	fmt.Print(stream.Chunk().Content())
}
return stream.Err()
```

Tool calling works the usual way:

```go
Tools: []models.Tool{
	models.FunctionTool("get_weather", "current weather", schema),
}

if resp.HasToolCalls() {
	call := resp.ToolCalls()[0]
	var args WeatherArgs
	call.Function.ParseArguments(&args)
}
```

**Thinking is on by default.** Those tokens are billed as output and they arrive before any answer, which is seconds of silence in a voice agent. Turn it off and use the conversational model when latency matters:

```go
Model:           models.ChatSarvam105BConversations,
ReasoningEffort: models.ReasoningOff,
```

When it is on, read it with `resp.Reasoning()` or `chunk.Reasoning()`.

## Text to Speech

```go
audio, err := client.Speech.Create(ctx, &models.SpeechRequest{
	Model:    models.TTSBulbulV3,
	Voice:    "shubh",
	Language: models.LangEnglish,
	Text:     "The quick brown fox jumps over the lazy dog.",
	Format:   models.MP3(24000, 128),

	// Optional fields are pointers: nil means "let the server decide"
	Speed:       models.Float(1.05),
	Temperature: models.Float(0.6),
})

audio.Save("fox.mp3")
data := audio.Bytes()
io.Copy(w, audio) // Audio is an io.Reader and an io.WriterTo
```

`Pitch` and `Loudness` work on `bulbul:v2`; `Temperature` and `Dictionary` work on `bulbul:v3`.

### Streaming

```go
stream, err := client.Speech.Stream(ctx, &models.SpeechRequest{
	Model:    models.TTSBulbulV3,
	Voice:    "shubh",
	Language: models.LangEnglish,
	Text:     "Audio arrives as it is made.",
	Format:   models.MP3(24000, 128),
})
if err != nil {
	return err
}
defer stream.Close()

for stream.Next() {
	file.Write(stream.Chunk().Bytes)
}
return stream.Err()
```

Range-over-func works too:

```go
for chunk, err := range stream.All() {
	if err != nil {
		return err
	}
	file.Write(chunk.Bytes)
}
```

### Text in, audio out

For piping the tokens of a language model into speech without waiting for whole sentences.

```go
duplex, err := client.Speech.Duplex(ctx, &models.SpeechRequest{
	Model: models.TTSBulbulV3, Voice: "shubh", Language: models.LangEnglish,
})
defer duplex.Close()

go func() {
	for token := range llmTokens {
		duplex.SendText(ctx, token)
	}
	duplex.CloseSend(ctx) // no more text is coming
}()

for duplex.Next() {
	speaker.Write(duplex.Chunk().Bytes)
}
return duplex.Err()
```

`SendText` from one goroutine while another runs `Next` is safe: writes are serialized inside.

Text with no letters in it — a stray newline, or a lone `!` arriving as its own
token — is skipped instead of sent. The API rejects such messages and closes the
whole connection, which would otherwise end the reply mid-sentence.

## Transcription

```go
result, err := client.Transcription.Create(ctx, &models.TranscriptionRequest{
	Model:      models.STTSaarasV4,
	Audio:      models.FileInput("meeting.wav"),
	Language:   models.LangHindi, // leave empty to auto-detect
	Timestamps: true,
})

fmt.Println(result.Text, result.Language)
for _, w := range result.Words {
	fmt.Printf("%.2f-%.2fs %s\n", w.Start.Seconds(), w.End.Seconds(), w.Text)
}

os.WriteFile("meeting.srt", []byte(result.SRT()), 0644)
os.WriteFile("meeting.vtt", []byte(result.VTT()), 0644)
```

This endpoint takes clips of up to 30 seconds. Longer audio is refused before the upload starts, with an `*AudioTooLongError` pointing you at `client.Batch`. Sarvam returns phrase-level chunks rather than one entry per word.

Audio can come from anywhere:

```go
models.FileInput("clip.mp3")                              // from disk, streamed
models.BytesInput(buf, models.WAV(16000))                 // from memory
models.ReaderInput(r, "recording.wav", "audio/wav")       // any io.Reader
```

`FileInput` and `ReaderInput` stream into the multipart body, so a 200MB file never lands in RAM. A plain reader cannot be rewound, so those requests are not retried.

### Live transcription

```go
stream, err := client.Transcription.Stream(ctx, &models.TranscriptionStreamRequest{
	Model:       models.STTSaarasV3Realtime,
	InputFormat: models.PCM16(16000),
	StreamType:  models.StreamFast,
})
defer stream.Close()

go func() {
	for frame := range microphone.Frames() {
		stream.SendAudio(ctx, frame)
	}
	stream.CloseSend(ctx)
}()

for {
	event, err := stream.Next(ctx)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	switch e := event.(type) {
	case *models.PartialTranscript:
		fmt.Printf("\r%s", e.Text)
	case *models.FinalTranscript:
		fmt.Printf("\r%s\n", e.Text)
	case *models.SpeechStarted:
	case *models.SpeechEnded:
	case *models.SessionEnded:
		fmt.Printf("billed %.1fs\n", e.AudioDuration.Seconds())
	}
}
```

Live sessions take PCM16, mu-law or A-law at 8 or 16 kHz. Anything else is refused with a `*FormatError` that lists what works.

## Batch Jobs

Up to 20 files per job, two hours each. This is the only Sarvam API that labels speakers.

```go
job, err := client.Batch.Create(ctx, &models.BatchRequest{
	Files:       []models.Input{models.FileInput("interview.wav")},
	Model:       models.STTSaarasV4,
	Diarize:     true,
	NumSpeakers: 2, // 0 lets the model work it out
	Timestamps:  true,
})

job, err = client.Batch.Wait(ctx, job.ID, 10*time.Second)

results, err := client.Batch.Results(ctx, job)
for name, result := range results {
	for _, turn := range result.SpeakerTurns() {
		fmt.Printf("[%s] %s: %s\n", name, turn.Speaker, turn.Text)
	}
}
```

`Create` registers the job, uploads the files to storage, and starts processing. `Wait` polls until it finishes, and `Results` downloads the transcripts, keyed by the file name they were uploaded under.

## Audio Formats

```go
models.MP3(44100, 128)
models.WAV(24000)
models.FLAC(24000)
models.Opus(24000, 64)
models.AAC(24000, 128)
models.PCM16(24000)  // raw, for realtime playback
models.ULaw8000()    // telephony
models.ALaw8000()
```

`Audio` carries the format with the bytes, so nothing has to remember what was asked for:

```go
audio.Bytes()      // encoded audio
audio.Len()        // size in bytes
audio.Format()     // codec, sample rate, bitrate
audio.SampleRate()
audio.Duration()   // exact for WAV and PCM, estimated for MP3, Opus and AAC
audio.Save(path)
audio.Read(p)      // io.Reader
audio.WriteTo(w)   // io.WriterTo
```

## Playback and Microphone

These live apart, so the core SDK never pulls in an audio library.

```go
import "github.com/meetkool/SarvamAI-Go-SDK/playback"

playback.Play(ctx, audio) // WAV, PCM16 or MP3

speaker, err := playback.NewSpeaker(24000) // stream PCM16 as it arrives
speaker.Write(chunk.Bytes)
speaker.Clear() // drop what is queued when the user interrupts
```

```go
import "github.com/meetkool/SarvamAI-Go-SDK/mic"

microphone, err := mic.Open(16000, 0) // mono PCM16 frames of 100ms
defer microphone.Close()

for frame := range microphone.Frames() {
	stream.SendAudio(ctx, frame)
}
```

Capture needs cgo. Built without it, `mic.Open` returns `mic.ErrNoCapture` and nothing else changes.

## Configuration

```go
client, err := sarvam.New(
	sarvam.WithAPIKey("your-api-key"),
	sarvam.WithBaseURL("https://api.sarvam.ai"),
	sarvam.WithHTTPClient(myClient),
	sarvam.WithTimeout(5*time.Minute),
	sarvam.WithMaxRetries(3),
	sarvam.WithRetryDelays(time.Second, 60*time.Second),
	sarvam.WithDefaultFormat(models.WAV(24000)),
	sarvam.WithMaxConcurrency(8),
	sarvam.WithUserAgent("MyApp/1.0"),
	sarvam.WithLogger(slog.Default()),
)
```

| Option | Default | Environment variable |
| --- | --- | --- |
| `WithAPIKey` | required | `SARVAM_API_KEY` |
| `WithBaseURL` | `https://api.sarvam.ai` | `SARVAM_BASE_URL` |
| `WithTimeout` | 5m, per attempt | `SARVAM_TIMEOUT_SECS` |
| `WithMaxRetries` | 3 | |
| `WithRetryDelays` | 1s first, 60s longest | |
| `WithDefaultFormat` | `WAV(24000)` | |
| `WithMaxConcurrency` | 8 requests at once | |
| `WithHTTPClient` | one with no timeout of its own | |

**Streams and realtime sessions are never retried** once bytes have reached you, because replaying them would repeat audio. `WithTimeout` does not apply to them either: bound those with `context.WithTimeout`.

## Error Handling

```go
switch {
case errors.Is(err, sarvam.ErrUnauthorized):   // 401, 403
case errors.Is(err, sarvam.ErrQuotaExceeded):  // 429, out of credits
case errors.Is(err, sarvam.ErrRateLimited):    // 429, too fast
case errors.Is(err, sarvam.ErrInvalidRequest): // 400, 422
case errors.Is(err, sarvam.ErrServer):         // 5xx
case errors.Is(err, sarvam.ErrTimeout):
}

var rateErr *sarvam.RateLimitError
if errors.As(err, &rateErr) {
	time.Sleep(rateErr.RetryAfter)
}

var apiErr *sarvam.APIError
if errors.As(err, &apiErr) {
	log.Printf("HTTP %d: %s (code %s, request %s)",
		apiErr.StatusCode, apiErr.Message, apiErr.Code, apiErr.RequestID)
}
```

Sentinels: `ErrMissingAPIKey`, `ErrUnauthorized`, `ErrInvalidRequest`, `ErrRateLimited`, `ErrQuotaExceeded`, `ErrServer`, `ErrTimeout`, `ErrMaxRetriesExceeded`, `ErrUnsupportedFormat`, `ErrAudioTooLong`, `ErrDecode`, `ErrStreamClosed`.

Typed errors, each wrapping its sentinel: `*APIError` (`StatusCode`, `Code`, `Message`, `RequestID`, `Raw`), `*RateLimitError` (`RetryAfter`), `*AudioTooLongError` (`Duration`, `MaxDuration`), `*FormatError` (`Format`, `Endpoint`, `Supported`), `*StreamError` (`ChunkIndex`, `Underlying`), `*MaxRetriesError` (`Attempts`).

Network errors, 429 and 5xx (except 501) are retried with growing delays, and a `Retry-After` header is honoured. Running out of credits is not retried, because waiting will not refill them.

## What Sarvam Does Not Offer

Sarvam has no API for voice cloning (it is done in their dashboard), listing voices, voice changing, sound effects, audio isolation, or realtime voice agents, so this SDK does not pretend to. Speakers are chosen by name from the Bulbul voice list.

## Project Structure

The layout follows the Rust SDK: the library lives in `src/`, and the request
and response types live in `src/models/`. So you import two packages — `src`
for the client, `src/models` for the types you fill in.

```
sarvam-go-sdk/
├── go.mod  go.sum  README.md
├── src/
│   ├── sarvam.go          version
│   ├── client.go          Client, New, NewFromEnv, HTTP plumbing and retries
│   ├── options.go         With* options and defaults
│   ├── errors.go          sentinels and typed errors
│   ├── retry.go           backoff
│   ├── ws.go              WebSocket connection
│   ├── stream.go          Server-Sent Events reader
│   ├── chat.go            ChatService: Create, Stream
│   ├── speech.go          SpeechService: Create, Stream, Duplex
│   ├── transcription.go   TranscriptionService: Create, Stream
│   ├── batch.go           BatchService: Create, Get, Wait, Results
│   └── models/
│       ├── chat.go           messages, tools, chat request and response
│       ├── speech.go         SpeechRequest, AudioChunk
│       ├── transcription.go  TranscriptionRequest, results, live events
│       ├── batch.go          BatchRequest, Job, JobState
│       ├── audio.go          Audio
│       ├── format.go         Format, Codec
│       ├── input.go          Input: FileInput, BytesInput, ReaderInput
│       ├── models.go         model IDs, languages, modes
│       └── ptr.go            Float, Int, Bool, String helpers
├── internal/
│   ├── wav/                WAV header reading and writing
│   └── multipart/          streaming form encoder
├── playback/              optional: play audio through the speakers
├── mic/                   optional: capture from the microphone
└── examples/              one folder per runnable example
```

## Adding an Endpoint

1. Request and response types go next to the service that uses them.
2. Add the method to the service, using `client.doJSON`, `client.doUpload`, `client.openStream` or `client.dialWS`.
3. Add a test against an `httptest` server, as in `speech_test.go`.
4. Add a runnable example under `examples/`.

## Examples

```bash
go run ./examples/chat_simple
go run ./examples/chat_streaming
go run ./examples/tts_simple
go run ./examples/tts_streaming
go run ./examples/tts_duplex
go run ./examples/transcribe_file meeting.wav
go run ./examples/transcribe_realtime
go run ./examples/batch_diarize interview.wav
go run ./examples/play_speech
go run ./examples/error_handling
```

## Development

```bash
go build ./...
go vet ./...
go test ./...
```

The tests run offline against local servers, so they need no API key.

## License

MIT. See [LICENSE](LICENSE).
