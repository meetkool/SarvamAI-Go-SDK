package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"

	sarvam "github.com/crynta/sarvam-go-sdk"
	"github.com/crynta/sarvam-go-sdk/mic"
)

func main() {
	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stopSignals()

	client, err := sarvam.NewFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	microphone, err := mic.Open(16000, 0)
	if errors.Is(err, mic.ErrNoCapture) {
		log.Fatal("microphone capture needs cgo: install a C compiler and set CGO_ENABLED=1")
	}
	if err != nil {
		log.Fatal(err)
	}
	defer microphone.Close()

	stream, err := client.Transcription.Stream(ctx, &sarvam.TranscriptionStreamRequest{
		Model:       sarvam.STTSaarasV3Realtime,
		InputFormat: microphone.Format(),
		StreamType:  sarvam.StreamFast,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()

	go func() {
		for frame := range microphone.Frames() {
			if err := stream.SendAudio(ctx, frame); err != nil {
				return
			}
		}
		_ = stream.CloseSend(ctx)
	}()

	fmt.Println("listening, press Ctrl+C to stop")
	for {
		event, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
			return
		}
		if err != nil {
			log.Fatal(err)
		}

		switch e := event.(type) {
		case *sarvam.PartialTranscript:
			fmt.Printf("\r%s", e.Text)
		case *sarvam.FinalTranscript:
			fmt.Printf("\r%s\n", e.Text)
		case *sarvam.SpeechStarted:

		case *sarvam.SessionEnded:
			fmt.Printf("billed %.1fs of audio\n", e.AudioDuration.Seconds())
			return
		}
	}
}
