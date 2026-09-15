package main

import (
	"context"
	"fmt"
	"log"

	sarvam "github.com/meetkool/SarvamAI-Go-SDK/src"
	"github.com/meetkool/SarvamAI-Go-SDK/src/models"
)

func main() {
	c, err := sarvam.NewFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	a, err := c.Speech.Create(context.Background(), &models.SpeechRequest{
		Model:    models.TTSBulbulV3,
		Voice:    "shubh",
		Language: models.LangHindi,
		Text:     "Namaste! Yah Sarvam Go SDK ka test hai.",
		Format:   models.WAV(24000),
		Speed:    models.Float(1.05),
	})
	if err != nil {
		log.Fatal(err)
	}
	a.Save("hello.wav")
	fmt.Printf("saved hello.wav: %d bytes, %.2fs\n", a.Len(), a.Duration().Seconds())
}
