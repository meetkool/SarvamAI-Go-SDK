package main

import (
	"context"
	"fmt"
	"log"
	"os"

	sarvam "github.com/meetkool/SarvamAI-Go-SDK/src"
	"github.com/meetkool/SarvamAI-Go-SDK/src/models"
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

	result, err := client.Transcription.Create(context.Background(), &models.TranscriptionRequest{
		Model:      models.STTSaarasV4,
		Audio:      models.FileInput(path),
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
