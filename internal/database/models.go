package database

import "time"

type Project struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Scope       string    `json:"scope"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Scan struct {
	ID          int64      `json:"id"`
	ProjectID   int64      `json:"project_id"`
	ScanType    string     `json:"scan_type"`
	Tool        string     `json:"tool"`
	Target      string     `json:"target"`
	Parameters  string     `json:"parameters"`
	Status      string     `json:"status"`
	RawOutput   string     `json:"raw_output,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type Result struct {
	ID         int64     `json:"id"`
	ScanID     int64     `json:"scan_id"`
	ResultType string    `json:"result_type"`
	Key        string    `json:"key"`
	Value      string    `json:"value"`
	Details    string    `json:"details,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type Report struct {
	ID        int64     `json:"id"`
	ProjectID int64     `json:"project_id"`
	Title     string    `json:"title"`
	Format    string    `json:"format"`
	Content   string    `json:"content,omitempty"`
	FilePath  string    `json:"file_path"`
	CreatedAt time.Time `json:"created_at"`
}
