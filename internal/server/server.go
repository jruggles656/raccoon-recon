package server

import (
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/jamesruggles/reconsuite/internal/config"
	"github.com/jamesruggles/reconsuite/internal/database"
	"github.com/jamesruggles/reconsuite/internal/report"
	"github.com/jamesruggles/reconsuite/internal/scanner"
	"github.com/jamesruggles/reconsuite/web"
)

type Server struct {
	cfg       *config.Config
	db        *database.DB
	hub       *Hub
	executor  *scanner.Executor
	reportGen *report.Generator
	mux       *http.ServeMux
	pages     map[string]*template.Template
}

func New(cfg *config.Config, db *database.DB) (*Server, error) {
	hub := NewHub()

	s := &Server{
		cfg:       cfg,
		db:        db,
		hub:       hub,
		executor:  scanner.NewExecutor(db, hub),
		reportGen: report.NewGenerator(db, cfg.Reports.Directory),
		mux:       http.NewServeMux(),
		pages:     make(map[string]*template.Template),
	}

	if err := s.loadTemplates(); err != nil {
		return nil, fmt.Errorf("loading templates: %w", err)
	}

	s.registerRoutes()
	return s, nil
}

func (s *Server) loadTemplates() error {
	pageFiles := []string{
		"dashboard.html",
		"projects.html",
		"passive.html",
		"active.html",
		"web.html",
		"results.html",
		"reports.html",
	}

	for _, page := range pageFiles {
		tmpl, err := template.ParseFS(web.Templates, "templates/layout.html", "templates/"+page)
		if err != nil {
			return fmt.Errorf("parsing %s: %w", page, err)
		}
		s.pages[page] = tmpl
	}

	return nil
}

func (s *Server) ListenAndServe() error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Server.Host, s.cfg.Server.Port)
	slog.Info("starting server", "addr", addr)

	handler := recoveryMiddleware(securityHeaders(loggingMiddleware(s.mux)))
	return http.ListenAndServe(addr, handler)
}

func (s *Server) registerRoutes() {
	staticFS, _ := fs.Sub(web.Static, "static")
	s.mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Pages
	s.mux.HandleFunc("/", s.handleDashboard)
	s.mux.HandleFunc("/projects", s.handleProjects)
	s.mux.HandleFunc("/passive", s.handlePassive)
	s.mux.HandleFunc("/active", s.handleActive)
	s.mux.HandleFunc("/web", s.handleWeb)
	s.mux.HandleFunc("/results", s.handleResults)
	s.mux.HandleFunc("/reports", s.handleReports)

	// API
	s.mux.HandleFunc("/api/projects", s.handleAPIProjects)
	s.mux.HandleFunc("/api/projects/", s.handleAPIProject)
	s.mux.HandleFunc("/api/stats", s.handleAPIStats)
	s.mux.HandleFunc("/api/scans", s.handleAPIScans)
	s.mux.HandleFunc("/api/scans/", s.handleAPIScan)
	s.mux.HandleFunc("/api/reports", s.handleAPIReports)
	s.mux.HandleFunc("/api/reports/", s.handleAPIReport)
	s.mux.HandleFunc("/api/tools/status", s.handleAPIToolStatus)

	// WebSocket
	s.mux.HandleFunc("/ws", s.handleWebSocket)
}
