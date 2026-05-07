package workerpool

type Status string

const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusSuccess    Status = "success"
	StatusFailed     Status = "failed"
	StatusSkipped    Status = "skipped"
)

type Job struct {
	ID         int
	InputPath  string
	OutputPath string
	RecipePath string
	Status     Status
	Error      error
}
