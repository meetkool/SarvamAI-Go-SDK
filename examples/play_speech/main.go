package main

import (
	"context"
	"fmt"
	"log"

	sarvam "github.com/crynta/sarvam-go-sdk"
	"github.com/crynta/sarvam-go-sdk/playback"
)

func main() {
	client, err := sarvam.NewFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()

	audio, err := client.Speech.Create(ctx, &sarvam.SpeechRequest{
		Model:    sarvam.TTSBulbulV3,
		Voice:    "shubh",
		Language: sarvam.LangEnglish,
		Text:     "This clip is playing straight from the SDK.",
		Format:   sarvam.WAV(24000),
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("playing %.2fs of audio\n", audio.Duration().Seconds())
	if err := playback.Play(ctx, audio); err != nil {
		log.Fatal(err)
	}
}
