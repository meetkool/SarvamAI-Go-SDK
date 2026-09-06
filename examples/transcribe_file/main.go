package main

import (
	"context"
	"fmt"
	"log"
	"os"

	sarvam "github.com/crynta/sarvam-go-sdk"
)

func main() {
	path := "meeting.wav"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}

	client, err := sarvam.NewFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	result, err := client.Transcription.Create(context.Background(), &sarvam.TranscriptionRequest{
		Model:      sarvam.STTSaarasV4,
		Audio:      sarvam.FileInput(path),
		Timestamps: true,
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result.Text)
	fmt.Println("language:", result.Language)
	for _, w := range result.Words {
		fmt.Printf("%6.2f - %6.2fs  %s\n", w.Start.Seconds(), w.End.Seconds(), w.Text)
	}

	if err := os.WriteFile("transcript.srt", []byte(result.SRT()), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Println("saved transcript.srt")
}
