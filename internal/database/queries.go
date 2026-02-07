package database

import (
	"database/sql"
	"fmt"
	"time"
)

// --- Projects ---

func (db *DB) CreateProject(p *Project) error {
	res, err := db.Exec(
		`INSERT INTO projects (name, description, scope) VALUES (?, ?, ?)`,
		p.Name, p.Description, p.Scope,
	)
	if err != nil {
		return fmt.Errorf("insert project: %w", err)
	}
	p.ID, _ = res.LastInsertId()
	return nil
}

func (db *DB) GetProject(id int64) (*Project, error) {
	p := &Project{}
	err := db.QueryRow(
		`SELECT id, name, description, scope, created_at, updated_at FROM projects WHERE id = ?`, id,
	).Scan(&p.ID, &p.Name, &p.Description, &p.Scope, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}
	return p, nil
}

func (db *DB) ListProjects() ([]Project, error) {
	rows, err := db.Query(`SELECT id, name, description, scope, created_at, updated_at FROM projects ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Scope, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func (db *DB) UpdateProject(p *Project) error {
	_, err := db.Exec(
		`UPDATE projects SET name = ?, description = ?, scope = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		p.Name, p.Description, p.Scope, p.ID,
	)
	if err != nil {
		return fmt.Errorf("update project: %w", err)
	}
	return nil
}

func (db *DB) DeleteProject(id int64) error {
	_, err := db.Exec(`DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	return nil
}

// --- Scans ---

func (db *DB) CreateScan(s *Scan) error {
	res, err := db.Exec(
		`INSERT INTO scans (project_id, scan_type, tool, target, parameters, status) VALUES (?, ?, ?, ?, ?, ?)`,
		s.ProjectID, s.ScanType, s.Tool, s.Target, s.Parameters, s.Status,
	)
	if err != nil {
		return fmt.Errorf("insert scan: %w", err)
	}
	s.ID, _ = res.LastInsertId()
	return nil
}

func (db *DB) GetScan(id int64) (*Scan, error) {
	s := &Scan{}
	err := db.QueryRow(
		`SELECT id, project_id, scan_type, tool, target, parameters, status, raw_output, started_at, completed_at, created_at
		 FROM scans WHERE id = ?`, id,
	).Scan(&s.ID, &s.ProjectID, &s.ScanType, &s.Tool, &s.Target, &s.Parameters, &s.Status, &s.RawOutput, &s.StartedAt, &s.CompletedAt, &s.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get scan: %w", err)
	}
	return s, nil
}

func (db *DB) ListScansByProject(projectID int64) ([]Scan, error) {
	rows, err := db.Query(
		`SELECT id, project_id, scan_type, tool, target, parameters, status, raw_output, started_at, completed_at, created_at
		 FROM scans WHERE project_id = ? ORDER BY created_at DESC`, projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("list scans: %w", err)
	}
	defer rows.Close()

	var scans []Scan
	for rows.Next() {
		var s Scan
		if err := rows.Scan(&s.ID, &s.ProjectID, &s.ScanType, &s.Tool, &s.Target, &s.Parameters, &s.Status, &s.RawOutput, &s.StartedAt, &s.CompletedAt, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		scans = append(scans, s)
	}
	return scans, rows.Err()
}

func (db *DB) UpdateScanStatus(id int64, status string) error {
	now := time.Now()
	switch status {
	case "running":
		_, err := db.Exec(`UPDATE scans SET status = ?, started_at = ? WHERE id = ?`, status, now, id)
		return err
	case "completed", "failed":
		_, err := db.Exec(`UPDATE scans SET status = ?, completed_at = ? WHERE id = ?`, status, now, id)
		return err
	default:
		_, err := db.Exec(`UPDATE scans SET status = ? WHERE id = ?`, status, id)
		return err
	}
}

func (db *DB) UpdateScanRawOutput(id int64, output string) error {
	_, err := db.Exec(`UPDATE scans SET raw_output = ? WHERE id = ?`, output, id)
	return err
}

// --- Results ---

func (db *DB) CreateResult(r *Result) error {
	res, err := db.Exec(
		`INSERT INTO results (scan_id, result_type, key, value, details) VALUES (?, ?, ?, ?, ?)`,
		r.ScanID, r.ResultType, r.Key, r.Value, r.Details,
	)
	if err != nil {
		return fmt.Errorf("insert result: %w", err)
	}
	r.ID, _ = res.LastInsertId()
	return nil
}

func (db *DB) CreateResults(results []Result) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO results (scan_id, result_type, key, value, details) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for _, r := range results {
		if _, err := stmt.Exec(r.ScanID, r.ResultType, r.Key, r.Value, r.Details); err != nil {
			return fmt.Errorf("exec: %w", err)
		}
	}
	return tx.Commit()
}

func (db *DB) GetResultsByScan(scanID int64) ([]Result, error) {
	rows, err := db.Query(
		`SELECT id, scan_id, result_type, key, value, details, created_at
		 FROM results WHERE scan_id = ? ORDER BY id`, scanID,
	)
	if err != nil {
		return nil, fmt.Errorf("list results by scan: %w", err)
	}
	defer rows.Close()

	var results []Result
	for rows.Next() {
		var r Result
		if err := rows.Scan(&r.ID, &r.ScanID, &r.ResultType, &r.Key, &r.Value, &r.Details, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan result: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func (db *DB) GetResultsByProject(projectID int64) ([]Result, error) {
	rows, err := db.Query(
		`SELECT r.id, r.scan_id, r.result_type, r.key, r.value, r.details, r.created_at
		 FROM results r JOIN scans s ON r.scan_id = s.id
		 WHERE s.project_id = ? ORDER BY r.id`, projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("list results by project: %w", err)
	}
	defer rows.Close()

	var results []Result
	for rows.Next() {
		var r Result
		if err := rows.Scan(&r.ID, &r.ScanID, &r.ResultType, &r.Key, &r.Value, &r.Details, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan result: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// --- Reports ---

func (db *DB) CreateReport(r *Report) error {
	res, err := db.Exec(
		`INSERT INTO reports (project_id, title, format, content, file_path) VALUES (?, ?, ?, ?, ?)`,
		r.ProjectID, r.Title, r.Format, r.Content, r.FilePath,
	)
	if err != nil {
		return fmt.Errorf("insert report: %w", err)
	}
	r.ID, _ = res.LastInsertId()
	return nil
}

func (db *DB) GetReport(id int64) (*Report, error) {
	r := &Report{}
	err := db.QueryRow(
		`SELECT id, project_id, title, format, content, file_path, created_at FROM reports WHERE id = ?`, id,
	).Scan(&r.ID, &r.ProjectID, &r.Title, &r.Format, &r.Content, &r.FilePath, &r.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get report: %w", err)
	}
	return r, nil
}

func (db *DB) ListReportsByProject(projectID int64) ([]Report, error) {
	rows, err := db.Query(
		`SELECT id, project_id, title, format, file_path, created_at
		 FROM reports WHERE project_id = ? ORDER BY created_at DESC`, projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("list reports: %w", err)
	}
	defer rows.Close()

	var reports []Report
	for rows.Next() {
		var r Report
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Title, &r.Format, &r.FilePath, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan report: %w", err)
		}
		reports = append(reports, r)
	}
	return reports, rows.Err()
}

// --- Stats ---

type DashboardStats struct {
	ProjectCount int `json:"project_count"`
	ScanCount    int `json:"scan_count"`
	ResultCount  int `json:"result_count"`
}

func (db *DB) GetStats() (*DashboardStats, error) {
	stats := &DashboardStats{}
	db.QueryRow(`SELECT COUNT(*) FROM projects`).Scan(&stats.ProjectCount)
	db.QueryRow(`SELECT COUNT(*) FROM scans`).Scan(&stats.ScanCount)
	db.QueryRow(`SELECT COUNT(*) FROM results`).Scan(&stats.ResultCount)
	return stats, nil
}

func (db *DB) ListRecentScans(limit int) ([]Scan, error) {
	rows, err := db.Query(
		`SELECT id, project_id, scan_type, tool, target, parameters, status, '', started_at, completed_at, created_at
		 FROM scans ORDER BY created_at DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list recent scans: %w", err)
	}
	defer rows.Close()

	var scans []Scan
	for rows.Next() {
		var s Scan
		if err := rows.Scan(&s.ID, &s.ProjectID, &s.ScanType, &s.Tool, &s.Target, &s.Parameters, &s.Status, &s.RawOutput, &s.StartedAt, &s.CompletedAt, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		scans = append(scans, s)
	}
	return scans, rows.Err()
}
