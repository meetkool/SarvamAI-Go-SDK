# Browser voice assistant

A complete voice assistant built with this Go SDK: a Go server and an embedded
HTML interface. Talk through your browser microphone, read live captions, and
hear streamed replies in the language you spoke. Speak during a reply to interrupt.

## Quick start

You need Go 1.25 or newer, a Sarvam API key, and a browser with microphone access.
No frontend build or Node.js installation is needed to run the app.

From the SDK repository root:

```sh
cd examples/voice_agent
```

Copy `.env.example` to `.env` and replace `your-key-here` with your key.
An existing `SARVAM_API_KEY` environment variable takes precedence over `.env`.

```sh
go run . -addr :8137
```

Open [localhost:8137](http://localhost:8137), select **Start talking**, and allow
microphone access. Headphones help avoid speaker audio triggering interruptions.
Select **Stop** to end the conversation; Ctrl+C stops the server.

If your key is already set in the environment, you can also run directly from
the SDK root:

```sh
go run ./examples/voice_agent -addr :8137
```

The `.env` file is loaded from the directory where you run the command.
The API key stays on the server; the browser only connects to the local app.

## Included

- Streaming microphone transcription and partial captions.
- Streamed chat responses and 24 kHz PCM speech playback.
- Conversation history, language selection, and speech interruptions.
- Replacement of failed speech connections on subsequent turns.
- A quiet continuous ambience that stays on through pauses.
- Optional HTTPS for testing from another device.

The background is synthesized room noise. Office chatter and typing recordings
are not bundled. The HTML is served by the Go app; opening it directly as a file
does not connect the voice pipeline.

## Customize

| File | Purpose |
| --- | --- |
| `session.go` | Assistant prompt, chat and speech models, turn handling |
| `web/index.html` | Interface, microphone capture, playback, ambience volume |
| `main.go` | Server routes and command-line flags |
| `serve.go` | Optional development HTTPS certificates |

Choose a speaker with `-voice shubh`. Tune speech detection using the optional
settings in `.env.example`. This example uses the parent SDK module directly;
there is no separate module or sibling-project dependency.

For HTTPS testing, run `go run . -tls -addr :8137`. The server creates a
self-signed certificate under `certs/` and prints local network URLs. Another
device needs HTTPS and a certificate it trusts before its browser can grant
microphone access. This is a local demo; add authentication before exposing it
publicly.

## Build and checks

From this directory:

```sh
go build -o voice-agent .
go test .
```

On Windows, use `go build -o voice-agent.exe .`. The HTML is embedded in the
executable, so rebuild after changing the interface.

The tests use a local fake Sarvam service and require no API key. Optional
playback logic checks need Node.js:

```sh
node web_audio_test.cjs
```
