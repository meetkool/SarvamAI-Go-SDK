package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	sarvam "github.com/crynta/sarvam-go-sdk/src"
	"github.com/crynta/sarvam-go-sdk/src/models"
)

func main() {
	path := "interview.wav"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}

	client, err := sarvam.NewFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()

	job, err := client.Batch.Create(ctx, &models.BatchRequest{
		Files:      []models.Input{models.FileInput(path)},
		Model:      models.STTSaarasV4,
		Diarize:    true,
		Timestamps: true,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("job", job.ID, "started")

	job, err = client.Batch.Wait(ctx, job.ID, 10*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("job %s: %s (%d ok, %d failed)\n", job.ID, job.State, job.Succeeded, job.Failed)

	results, err := client.Batch.Results(ctx, job)
	if err != nil {
		log.Fatal(err)
	}
	for name, result := range results {
		fmt.Printf("\n== %s ==\n", name)
		for _, turn := range result.SpeakerTurns() {
			fmt.Printf("[speaker %s] %s\n", turn.Speaker, turn.Text)
		}
	}
	for _, f := range job.Files {
		if f.Error != "" {
			fmt.Printf("%s failed: %s\n", f.Input, f.Error)
		}
	}
}
