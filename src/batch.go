package sarvam

import (
	"bytes"
	"context"
	"fmt"
	"github.com/meetkool/SarvamAI-Go-SDK/src/models"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

const (
	batchPath        = "/speech-to-text/job/v1"
	batchMaxFiles    = 20
	batchMaxSpeakers = 20
	batchPollWait    = 5 * time.Second
)

type BatchService struct{ client *Client }

type jobParameters struct {
	Model           models.Model    `json:"model,omitempty"`
	Mode            models.Mode     `json:"mode,omitempty"`
	LanguageCode    models.Language `json:"language_code,omitempty"`
	WithTimestamps  bool            `json:"with_timestamps,omitempty"`
	WithDiarization bool            `json:"with_diarization,omitempty"`
	NumSpeakers     int             `json:"num_speakers,omitempty"`
	Keyterms        []string        `json:"keyterms,omitempty"`
	InputAudioCodec string          `json:"input_audio_codec,omitempty"`
}

type jobCallback struct {
	URL       string `json:"url"`
	AuthToken string `json:"auth_token,omitempty"`
}

type initJobRequest struct {
	JobParameters jobParameters `json:"job_parameters"`
	Callback      *jobCallback  `json:"callback,omitempty"`
}

type jobFileRef struct {
	FileName string `json:"file_name"`
	FileID   string `json:"file_id"`
}

type jobStatus struct {
	JobID                string          `json:"job_id"`
	JobState             models.JobState `json:"job_state"`
	StorageContainerType string          `json:"storage_container_type"`
	TotalFiles           int             `json:"total_files"`
	SuccessfulFilesCount int             `json:"successful_files_count"`
	FailedFilesCount     int             `json:"failed_files_count"`
	ErrorMessage         string          `json:"error_message"`
	JobDetails           []struct {
		Inputs       []jobFileRef `json:"inputs"`
		Outputs      []jobFileRef `json:"outputs"`
		State        string       `json:"state"`
		ErrorMessage string       `json:"error_message"`
	} `json:"job_details"`
}

func (s *jobStatus) job() *models.Job {
	job := &models.Job{
		ID:        s.JobID,
		State:     s.JobState,
		Total:     s.TotalFiles,
		Succeeded: s.SuccessfulFilesCount,
		Failed:    s.FailedFilesCount,
		Error:     s.ErrorMessage,
	}
	for _, d := range s.JobDetails {
		f := models.JobFile{State: d.State, Error: d.ErrorMessage}
		if len(d.Inputs) > 0 {
			f.Input = d.Inputs[0].FileName
		}
		if len(d.Outputs) > 0 {
			f.Output = d.Outputs[0].FileName
		}
		job.Files = append(job.Files, f)
	}
	return job
}

type fileURLsRequest struct {
	JobID string   `json:"job_id"`
	Files []string `json:"files"`
}

type fileURL struct {
	FileURL string `json:"file_url"`
}

type fileURLsResponse struct {
	JobID                string             `json:"job_id"`
	JobState             models.JobState    `json:"job_state"`
	UploadURLs           map[string]fileURL `json:"upload_urls"`
	DownloadURLs         map[string]fileURL `json:"download_urls"`
	StorageContainerType string             `json:"storage_container_type"`
}

func (b *BatchService) Create(ctx context.Context, req *models.BatchRequest) (*models.Job, error) {
	if req == nil || len(req.Files) == 0 {
		return nil, invalidRequest("at least one file is required")
	}
	if len(req.Files) > batchMaxFiles {
		return nil, invalidRequest("a job takes at most %d files, got %d", batchMaxFiles, len(req.Files))
	}
	if req.NumSpeakers < 0 || req.NumSpeakers > batchMaxSpeakers {
		return nil, invalidRequest("NumSpeakers must be between 1 and %d", batchMaxSpeakers)
	}

	body := initJobRequest{
		JobParameters: jobParameters{
			Model:           req.Model,
			Mode:            req.Mode,
			LanguageCode:    req.Language,
			WithTimestamps:  req.Timestamps,
			WithDiarization: req.Diarize,
			NumSpeakers:     req.NumSpeakers,
			Keyterms:        req.Keyterms,
			InputAudioCodec: req.InputCodec,
		},
	}
	if req.CallbackURL != "" {
		body.Callback = &jobCallback{URL: req.CallbackURL, AuthToken: req.CallbackToken}
	}
	var created jobStatus
	if err := b.client.doJSON(ctx, http.MethodPost, batchPath, body, &created); err != nil {
		return nil, err
	}
	if created.JobID == "" {
		return nil, fmt.Errorf("%w: the API returned no job ID", ErrDecode)
	}

	names := make([]string, 0, len(req.Files))
	byName := make(map[string]models.Input, len(req.Files))
	for _, in := range req.Files {
		name := uniqueName(in.Filename(), byName)
		names = append(names, name)
		byName[name] = in
	}
	var urls fileURLsResponse
	if err := b.client.doJSON(ctx, http.MethodPost, batchPath+"/upload-files",
		fileURLsRequest{JobID: created.JobID, Files: names}, &urls); err != nil {
		return nil, err
	}

	for _, name := range names {
		target, ok := urls.UploadURLs[name]
		if !ok || target.FileURL == "" {
			return nil, fmt.Errorf("sarvam: no upload URL came back for %q", name)
		}
		if err := b.upload(ctx, target.FileURL, byName[name], urls.StorageContainerType); err != nil {
			return nil, err
		}
	}

	var started jobStatus
	if err := b.client.doJSON(ctx, http.MethodPost, fmt.Sprintf("%s/%s/start", batchPath, created.JobID), nil, &started); err != nil {
		return nil, err
	}
	if started.JobID == "" {
		started.JobID = created.JobID
	}
	return started.job(), nil
}

func uniqueName(name string, taken map[string]models.Input) string {
	if name == "" {
		name = "audio"
	}
	if _, clash := taken[name]; !clash {
		return name
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d%s", base, i, ext)
		if _, clash := taken[candidate]; !clash {
			return candidate
		}
	}
}

func (b *BatchService) upload(ctx context.Context, target string, in models.Input, storage string) error {
	body, size, err := uploadBody(in)
	if err != nil {
		return err
	}
	defer body.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, target, body)
	if err != nil {
		return fmt.Errorf("sarvam: building upload request: %w", err)
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", in.ContentType())
	if strings.HasPrefix(storage, "Azure") {
		req.Header.Set("x-ms-blob-type", "BlockBlob")
	}

	resp, err := b.client.http.Do(req)
	if err != nil {
		return transportError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return parseAPIError(resp)
	}
	return nil
}

func uploadBody(in models.Input) (io.ReadCloser, int64, error) {
	if size := in.Size(); size >= 0 {
		body, err := in.Open()
		if err != nil {
			return nil, 0, err
		}
		return body, size, nil
	}
	data, err := models.ReadAll(in)
	if err != nil {
		return nil, 0, err
	}
	return io.NopCloser(bytes.NewReader(data)), int64(len(data)), nil
}

func (b *BatchService) Get(ctx context.Context, jobID string) (*models.Job, error) {
	if jobID == "" {
		return nil, invalidRequest("job ID is required")
	}
	var status jobStatus
	if err := b.client.doJSON(ctx, http.MethodGet, fmt.Sprintf("%s/%s/status", batchPath, jobID), nil, &status); err != nil {
		return nil, err
	}
	if status.JobID == "" {
		status.JobID = jobID
	}
	return status.job(), nil
}

func (b *BatchService) Wait(ctx context.Context, jobID string, poll time.Duration) (*models.Job, error) {
	if poll <= 0 {
		poll = batchPollWait
	}
	for {
		job, err := b.Get(ctx, jobID)
		if err != nil {
			return nil, err
		}
		if job.State.Done() {
			return job, nil
		}
		if err := sleep(ctx, poll); err != nil {
			return nil, err
		}
	}
}

func (b *BatchService) Results(ctx context.Context, job *models.Job) (map[string]*models.TranscriptionResult, error) {
	if job == nil || job.ID == "" {
		return nil, invalidRequest("a job with an ID is required")
	}

	var outputs []string
	inputFor := make(map[string]string)
	for _, f := range job.Files {
		if f.Output == "" {
			continue
		}
		outputs = append(outputs, f.Output)
		inputFor[f.Output] = f.Input
	}
	results := make(map[string]*models.TranscriptionResult, len(outputs))
	if len(outputs) == 0 {
		return results, nil
	}

	var urls fileURLsResponse
	if err := b.client.doJSON(ctx, http.MethodPost, batchPath+"/download-files",
		fileURLsRequest{JobID: job.ID, Files: outputs}, &urls); err != nil {
		return nil, err
	}

	for _, name := range outputs {
		target, ok := urls.DownloadURLs[name]
		if !ok || target.FileURL == "" {
			continue
		}
		result, err := b.download(ctx, target.FileURL)
		if err != nil {
			return nil, err
		}
		key := inputFor[name]
		if key == "" {
			key = name
		}
		results[key] = result
	}
	return results, nil
}

func (b *BatchService) download(ctx context.Context, target string) (*models.TranscriptionResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("sarvam: building download request: %w", err)
	}
	resp, err := b.client.http.Do(req)
	if err != nil {
		return nil, transportError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, parseAPIError(resp)
	}

	var out sttResponse
	if err := decodeJSON(resp.Body, &out); err != nil {
		return nil, err
	}
	return out.result(), nil
}
