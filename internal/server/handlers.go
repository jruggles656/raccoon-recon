package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/jamesruggles/reconsuite/internal/database"
	"github.com/jamesruggles/reconsuite/internal/scanner"
	"github.com/jamesruggles/reconsuite/internal/tools"
)

type pageData struct {
	ActivePage string
}

func (s *Server) renderPage(w http.ResponseWriter, page string, data pageData) {
	tmpl, ok := s.pages[page]
	if !ok {
		http.Error(w, "page not found", http.StatusInternalServerError)
		return
	}
	if err := tmpl.ExecuteTemplate(w, "layout.html", data); err != nil {
		slog.Error("template render error", "page", page, "error", err)
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

func (s *Server) handleWelcome(w http.ResponseWriter, r *http.Request) {
	// If already accepted, redirect to dashboard
	if cookie, err := r.Cookie("disclaimer_accepted"); err == nil && cookie.Value == "true" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if err := s.welcomeTmpl.Execute(w, nil); err != nil {
		slog.Error("template render error", "page", "welcome", "error", err)
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

func (s *Server) handleWelcomeAccept(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "disclaimer_accepted",
		Value:    "true",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	s.renderPage(w, "dashboard.html", pageData{ActivePage: "dashboard"})
}

func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, "projects.html", pageData{ActivePage: "projects"})
}

func (s *Server) handlePassive(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, "passive.html", pageData{ActivePage: "passive"})
}

func (s *Server) handleActive(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, "active.html", pageData{ActivePage: "active"})
}

func (s *Server) handleWeb(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, "web.html", pageData{ActivePage: "web"})
}

func (s *Server) handleResults(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, "results.html", pageData{ActivePage: "results"})
}

func (s *Server) handleReports(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, "reports.html", pageData{ActivePage: "reports"})
}

// --- API Handlers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// handleAPIProjects handles /api/projects (collection)
func (s *Server) handleAPIProjects(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		projects, err := s.db.ListProjects()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if projects == nil {
			projects = []database.Project{}
		}
		writeJSON(w, http.StatusOK, projects)

	case http.MethodPost:
		var p database.Project
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if p.Name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		if err := s.db.CreateProject(&p); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, p)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAPIProject handles /api/projects/{id} (single resource)
func (s *Server) handleAPIProject(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/projects/")
	if idStr == "" {
		writeError(w, http.StatusBadRequest, "missing project id")
		return
	}

	// Check for sub-routes like /api/projects/{id}/scans
	parts := strings.SplitN(idStr, "/", 2)
	idStr = parts[0]

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid project id")
		return
	}

	if len(parts) > 1 {
		switch parts[1] {
		case "scans":
			s.handleAPIProjectScans(w, r, id)
		case "results":
			s.handleAPIProjectResults(w, r, id)
		default:
			http.NotFound(w, r)
		}
		return
	}

	switch r.Method {
	case http.MethodGet:
		p, err := s.db.GetProject(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if p == nil {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
		writeJSON(w, http.StatusOK, p)

	case http.MethodPut:
		var p database.Project
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		p.ID = id
		if err := s.db.UpdateProject(&p); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, p)

	case http.MethodDelete:
		if err := s.db.DeleteProject(id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPIProjectScans(w http.ResponseWriter, r *http.Request, projectID int64) {
	scans, err := s.db.ListScansByProject(projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if scans == nil {
		scans = []database.Scan{}
	}
	writeJSON(w, http.StatusOK, scans)
}

func (s *Server) handleAPIProjectResults(w http.ResponseWriter, r *http.Request, projectID int64) {
	results, err := s.db.GetResultsByProject(projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if results == nil {
		results = []database.Result{}
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) handleAPIStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.db.GetStats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// --- Scan API ---

func (s *Server) handleAPIScans(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var scan database.Scan
		if err := json.NewDecoder(r.Body).Decode(&scan); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if scan.Target == "" || scan.Tool == "" || scan.ScanType == "" {
			writeError(w, http.StatusBadRequest, "target, tool, and scan_type are required")
			return
		}
		if err := s.executor.StartScan(&scan); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, scan)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPIScan(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/scans/")
	if idStr == "" {
		writeError(w, http.StatusBadRequest, "missing scan id")
		return
	}

	// Handle /api/scans/recent
	if idStr == "recent" {
		scans, err := s.db.ListRecentScans(10)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if scans == nil {
			scans = []database.Scan{}
		}
		writeJSON(w, http.StatusOK, scans)
		return
	}

	parts := strings.SplitN(idStr, "/", 2)
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid scan id")
		return
	}

	if len(parts) > 1 && parts[1] == "results" {
		results, err := s.db.GetResultsByScan(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if results == nil {
			results = []database.Result{}
		}
		writeJSON(w, http.StatusOK, results)
		return
	}

	switch r.Method {
	case http.MethodGet:
		scan, err := s.db.GetScan(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if scan == nil {
			writeError(w, http.StatusNotFound, "scan not found")
			return
		}
		writeJSON(w, http.StatusOK, scan)

	case http.MethodDelete:
		s.executor.CancelScan(id)
		writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Report API ---

func (s *Server) handleAPIReports(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req struct {
			ProjectID int64  `json:"project_id"`
			Format    string `json:"format"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if req.ProjectID == 0 {
			writeError(w, http.StatusBadRequest, "project_id is required")
			return
		}
		if req.Format == "" {
			req.Format = "markdown"
		}

		var rpt *database.Report
		var err error

		switch req.Format {
		case "markdown":
			_, rpt, err = s.reportGen.SaveMarkdown(req.ProjectID)
		case "pdf":
			_, rpt, err = s.reportGen.SavePDF(req.ProjectID)
		default:
			writeError(w, http.StatusBadRequest, "format must be 'markdown' or 'pdf'")
			return
		}

		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, rpt)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPIReport(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/reports/")
	if idStr == "" {
		writeError(w, http.StatusBadRequest, "missing report id")
		return
	}

	// Handle /api/reports/by-project/{id} or /api/reports/by-project/all
	if strings.HasPrefix(idStr, "by-project/") {
		projIDStr := strings.TrimPrefix(idStr, "by-project/")

		var reports []database.Report
		var err error

		if projIDStr == "all" {
			reports, err = s.db.ListAllReports()
		} else {
			projID, parseErr := strconv.ParseInt(projIDStr, 10, 64)
			if parseErr != nil {
				writeError(w, http.StatusBadRequest, "invalid project id")
				return
			}
			reports, err = s.db.ListReportsByProject(projID)
		}

		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if reports == nil {
			reports = []database.Report{}
		}
		writeJSON(w, http.StatusOK, reports)
		return
	}

	parts := strings.SplitN(idStr, "/", 2)
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid report id")
		return
	}

	// Handle /api/reports/{id}/download
	if len(parts) > 1 && parts[1] == "download" {
		rpt, err := s.db.GetReport(id)
		if err != nil || rpt == nil {
			writeError(w, http.StatusNotFound, "report not found")
			return
		}
		if rpt.FilePath != "" {
			http.ServeFile(w, r, rpt.FilePath)
			return
		}
		if rpt.Format == "markdown" && rpt.Content != "" {
			w.Header().Set("Content-Type", "text/markdown")
			w.Header().Set("Content-Disposition", "attachment; filename=report.md")
			w.Write([]byte(rpt.Content))
			return
		}
		writeError(w, http.StatusNotFound, "report file not found")
		return
	}

	// GET /api/reports/{id}
	rpt, err := s.db.GetReport(id)
	if err != nil || rpt == nil {
		writeError(w, http.StatusNotFound, "report not found")
		return
	}
	writeJSON(w, http.StatusOK, rpt)
}

// --- Tool Status API ---

func (s *Server) handleAPIToolStatus(w http.ResponseWriter, r *http.Request) {
	statuses := tools.DetectAll()
	writeJSON(w, http.StatusOK, statuses)
}

// --- File Metadata Upload API ---

func (s *Server) handleAPIFileMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseMultipartForm(10 << 20); err != nil { // 10MB limit
		writeError(w, http.StatusBadRequest, "file too large or invalid form data")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "no file uploaded")
		return
	}
	defer file.Close()

	data := make([]byte, header.Size)
	if _, err := file.Read(data); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read file")
		return
	}

	results, err := scanner.ExtractFileMetadata(header.Filename, data)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"filename":  header.Filename,
		"size":      header.Size,
		"mime_type": http.DetectContentType(data),
		"results":   results,
	})
}
