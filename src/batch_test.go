package sarvam

import (
	"context"
	"encoding/json"
	"github.com/meetkool/SarvamAI-Go-SDK/src/models"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestBatchFlow(t *testing.T) {
	var (
		srv        *httptest.Server
		uploaded   []byte
		blobType   string
		statusHits atomic.Int32
	)

	mux := http.NewServeMux()

	mux.HandleFunc("POST /speech-to-text/job/v1", func(w http.ResponseWriter, r *http.Request) {
		var body initJobRequest
		json.NewDecoder(r.Body).Decode(&body)
		if !body.JobParameters.WithDiarization {
			t.Error("with_diarization was not sent")
		}
		if body.JobParameters.NumSpeakers != 2 {
			t.Errorf("num_speakers = %d, want 2", body.JobParameters.NumSpeakers)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"job_id": "job-1", "job_state": "Accepted", "storage_container_type": "Azure",
		})
	})

	mux.HandleFunc("POST /speech-to-text/job/v1/upload-files", func(w http.ResponseWriter, r *http.Request) {
		var body fileURLsRequest
		json.NewDecoder(r.Body).Decode(&body)
		urls := map[string]fileURL{}
		for _, name := range body.Files {
			urls[name] = fileURL{FileURL: srv.URL + "/blob/" + name}
		}
		json.NewEncoder(w).Encode(map[string]any{
			"job_id": body.JobID, "job_state": "Accepted",
			"upload_urls": urls, "storage_container_type": "Azure",
		})
	})

	mux.HandleFunc("PUT /blob/{name}", func(w http.ResponseWriter, r *http.Request) {
		blobType = r.Header.Get("x-ms-blob-type")
		uploaded, _ = io.ReadAll(r.Body)
		if r.Header.Get("api-subscription-key") != "" {
			t.Error("the API key must not be sent to storage: the URL is already signed")
		}
		w.WriteHeader(http.StatusCreated)
	})

	mux.HandleFunc("POST /speech-to-text/job/v1/job-1/start", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"job_id": "job-1", "job_state": "Running", "total_files": 1,
		})
	})

	mux.HandleFunc("GET /speech-to-text/job/v1/job-1/status", func(w http.ResponseWriter, r *http.Request) {
		if statusHits.Add(1) == 1 {
			json.NewEncoder(w).Encode(map[string]any{"job_id": "job-1", "job_state": "Running"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"job_id": "job-1", "job_state": "Completed",
			"total_files": 1, "successful_files_count": 1,
			"job_details": []any{map[string]any{
				"inputs":  []any{map[string]string{"file_name": "clip.wav"}},
				"outputs": []any{map[string]string{"file_name": "0.json"}},
				"state":   "Success",
			}},
		})
	})

	mux.HandleFunc("POST /speech-to-text/job/v1/download-files", func(w http.ResponseWriter, r *http.Request) {
		var body fileURLsRequest
		json.NewDecoder(r.Body).Decode(&body)
		urls := map[string]fileURL{}
		for _, name := range body.Files {
			urls[name] = fileURL{FileURL: srv.URL + "/out/" + name}
		}
		json.NewEncoder(w).Encode(map[string]any{
			"job_id": body.JobID, "job_state": "Completed",
			"download_urls": urls, "storage_container_type": "Azure",
		})
	})

	mux.HandleFunc("GET /out/0.json", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"transcript":"hello there hi","diarized_transcript":{"entries":[
			{"transcript":"hello","start_time_seconds":0,"end_time_seconds":1,"speaker_id":"0"},
			{"transcript":"hi","start_time_seconds":1,"end_time_seconds":2,"speaker_id":"1"}]}}`))
	})

	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := New(WithAPIKey("test-key"), WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	audio := testWAV(16000, time.Second)

	job, err := client.Batch.Create(ctx, &models.BatchRequest{
		Files:       []models.Input{models.BytesInput(audio, models.WAV(16000))},
		Model:       models.STTSaarasV4,
		Diarize:     true,
		NumSpeakers: 2,
		Timestamps:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != "job-1" {
		t.Fatalf("job ID = %q", job.ID)
	}
	if blobType != "BlockBlob" {
		t.Errorf("x-ms-blob-type = %q, want BlockBlob for Azure storage", blobType)
	}
	if len(uploaded) != len(audio) {
		t.Errorf("uploaded %d bytes, want %d", len(uploaded), len(audio))
	}

	job, err = client.Batch.Wait(ctx, job.ID, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if job.State != models.JobCompleted {
		t.Fatalf("state = %q, want Completed", job.State)
	}
	if statusHits.Load() < 2 {
		t.Error("Wait should keep polling while the job is still running")
	}

	results, err := client.Batch.Results(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	result, ok := results["clip.wav"]
	if !ok {
		t.Fatalf("results are keyed by input name, got %v", results)
	}
	turns := result.SpeakerTurns()
	if len(turns) != 2 || turns[0].Speaker != "0" || turns[1].Text != "hi" {
		t.Fatalf("turns = %+v", turns)
	}
}

func TestBatchChecksItsLimits(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, err := client.Batch.Create(ctx, &models.BatchRequest{}); err == nil {
		t.Error("a job with no files should be refused")
	}

	many := make([]models.Input, batchMaxFiles+1)
	for i := range many {
		many[i] = models.BytesInput([]byte("x"), models.WAV(16000))
	}
	if _, err := client.Batch.Create(ctx, &models.BatchRequest{Files: many}); err == nil {
		t.Errorf("more than %d files should be refused", batchMaxFiles)
	}
}

func TestUniqueNameAvoidsClashes(t *testing.T) {
	taken := map[string]models.Input{"clip.wav": nil, "clip-2.wav": nil}
	if got := uniqueName("clip.wav", taken); got != "clip-3.wav" {
		t.Fatalf("uniqueName = %q, want clip-3.wav", got)
	}
}
