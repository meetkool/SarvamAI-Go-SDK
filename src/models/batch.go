package models

type BatchRequest struct {
	Files       []Input
	Model       Model
	Mode        Mode
	Language    Language
	Timestamps  bool
	Diarize     bool
	NumSpeakers int
	Keyterms    []string
	InputCodec  string

	CallbackURL   string
	CallbackToken string
}

type JobState string

const (
	JobAccepted           JobState = "Accepted"
	JobPending            JobState = "Pending"
	JobRunning            JobState = "Running"
	JobCompleted          JobState = "Completed"
	JobPartiallyCompleted JobState = "PartiallyCompleted"
	JobFailed             JobState = "Failed"
)

func (s JobState) Done() bool {
	switch s {
	case JobCompleted, JobPartiallyCompleted, JobFailed:
		return true
	}
	return false
}

type Job struct {
	ID        string
	State     JobState
	Total     int
	Succeeded int
	Failed    int
	Error     string
	Files     []JobFile
}

type JobFile struct {
	Input  string
	Output string
	State  string
	Error  string
}
