package job

import "time"

type Status string

const (
	StatusQueued     Status = "queued"
	StatusProcessing Status = "processing"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
)

type Job struct {
	ID               string    `json:"job_id"`
	Status           Status    `json:"status"`
	Progress         int64     `json:"progress"`
	Attempts         int64     `json:"attempts"`
	OriginalFile     string    `json:"original_file,omitempty"`
	ProcessedFile    string    `json:"processed_file,omitempty"`
	ErrorMessage     string    `json:"error,omitempty"`
	OriginalFilename string    `json:"original_filename,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type Message struct {
	JobID           string `json:"job_id"`
	InputBucket     string `json:"input_bucket"`
	InputObject     string `json:"input_object"`
	OutputBucket    string `json:"output_bucket"`
	OutputObject    string `json:"output_object"`
	OriginalName    string `json:"original_name"`
	TargetCodec     string `json:"target_codec"`
	TargetContainer string `json:"target_container"`
}

