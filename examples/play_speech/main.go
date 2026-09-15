package main

import (
	"context"
	"fmt"
	"log"

	"github.com/crynta/sarvam-go-sdk/playback"
	sarvam "github.com/crynta/sarvam-go-sdk/src"
	"github.com/crynta/sarvam-go-sdk/src/models"
)

func main() {
	client, err := sarvam.NewFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()

	audio, err := client.Speech.Create(ctx, &models.SpeechRequest{
		Model:    models.TTSBulbulV3,
		Voice:    "shubh",
		Language: models.LangEnglish,
		Text:     "This clip is playing straight from the SDK.",
		Format:   models.WAV(24000),
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("playing %.2fs of audio\n", audio.Duration().Seconds())
	if err := playback.Play(ctx, audio); err != nil {
		log.Fatal(err)
	}
}
