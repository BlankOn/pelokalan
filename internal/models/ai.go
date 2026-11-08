package models

import "time"

// AISettings configures the LLM used for pre-translation in a project
type AISettings struct {
	ProjectID     int64
	BaseURL       string
	Model         string
	APIKeyEnc     string
	APIKeyHint    string  // last characters of the key, for display
	PriceInput    float64 // USD per 1M input tokens
	PriceOutput   float64 // USD per 1M output tokens
	MonthlyBudget float64 // USD, 0 = unlimited
	ExtraContext  string  // project-wide guidance for every AI request
	UpdatedBy     *int64
	UpdatedAt     time.Time
}

// Cost returns the USD cost of a token usage
func (s *AISettings) Cost(promptTokens, completionTokens int64) float64 {
	return float64(promptTokens)*s.PriceInput/1e6 + float64(completionTokens)*s.PriceOutput/1e6
}

// AI job statuses
const (
	AIJobRunning   = "running"
	AIJobDone      = "done"
	AIJobFailed    = "failed"
	AIJobCancelled = "cancelled"
)

// AIJob is one AI pre-translation run
type AIJob struct {
	ID               int64
	ProjectID        int64
	CatalogID        int64
	UserID           *int64
	Status           string
	Model            string
	Total            int
	Processed        int
	FromMemory       int
	FromGlossary     int
	Translated       int
	Flagged          int
	Skipped          int
	PromptTokens     int64
	CompletionTokens int64
	Cost             float64
	EstimatedCost    float64
	CancelRequested  bool
	Error            string
	CreatedAt        time.Time
	FinishedAt       *time.Time
}

// IsRunning reports whether the job is still working
func (j *AIJob) IsRunning() bool { return j.Status == AIJobRunning }

// Percent returns the progress in percent
func (j *AIJob) Percent() int {
	if j.Total == 0 {
		return 100
	}
	return j.Processed * 100 / j.Total
}
