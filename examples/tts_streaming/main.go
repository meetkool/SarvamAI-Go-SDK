package main

import (
	"context"
	"fmt"
	"log"
	"os"

	sarvam "github.com/crynta/sarvam-go-sdk"
)

func main() {
	client, err := sarvam.NewFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	stream, err := client.Speech.Stream(context.Background(), &sarvam.SpeechRequest{
		Model:    sarvam.TTSBulbulV3,
		Voice:    "shubh",
		Language: sarvam.LangEnglish,
		Text:     "Streaming audio arrives piece by piece, so you can play it as it is made.",
		Format:   sarvam.MP3(24000, 128),
	})
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()

	file, err := os.Create("stream.mp3")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	total := 0
	for stream.Next() {
		chunk := stream.Chunk()
		if _, err := file.Write(chunk.Bytes); err != nil {
			log.Fatal(err)
		}
		total += len(chunk.Bytes)
		fmt.Printf("\rchunk %d, %d bytes", chunk.Index, total)
	}
	if err := stream.Err(); err != nil {
		log.Fatal(err)
	}
	fmt.Println("\nsaved stream.mp3")
}
