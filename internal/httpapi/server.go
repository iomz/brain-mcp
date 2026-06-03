package httpapi

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/iomz/brain-mcp/internal/brain"
	braingit "github.com/iomz/brain-mcp/internal/git"
	"github.com/iomz/brain-mcp/internal/mcp"
)

type Server struct {
	token string
	vault *brain.Vault
	mcp   *mcp.Server
}

func NewServer(vault *brain.Vault, token string) *Server {
	return &Server{
		token: token,
		vault: vault,
		mcp:   mcp.NewServer(vault),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.withAuth(s.healthz))
	mux.HandleFunc("GET /info", s.withAuth(s.info))
	mux.HandleFunc("POST /mcp", s.withAuth(s.mcpPost))
	return mux
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.token == "" {
			writeError(w, http.StatusInternalServerError, "BRAIN_MCP_TOKEN is required")
			return
		}
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") || strings.TrimPrefix(header, "Bearer ") != s.token {
			writeError(w, http.StatusUnauthorized, "missing or invalid bearer token")
			return
		}
		next(w, r)
	}
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) info(w http.ResponseWriter, _ *http.Request) {
	status, err := braingit.Status(s.vault.Root())
	summary := map[string]any{"available": err == nil}
	if err == nil {
		lines := strings.Split(strings.TrimSpace(status), "\n")
		entries := 0
		if strings.TrimSpace(status) != "" {
			entries = len(lines)
		}
		summary["clean"] = entries == 0
		summary["entries"] = entries
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"vault":          filepath.Base(s.vault.Root()),
		"writable_paths": s.vault.WritablePaths(),
		"readonly_paths": s.vault.ReadonlyPaths(),
		"git_status":     summary,
	})
}

func (s *Server) mcpPost(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024))
	if err != nil {
		log.Printf("http_mcp_error remote=%s bytes=unknown status=%d error=%q", r.RemoteAddr, http.StatusBadRequest, err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	log.Printf("http_mcp_request remote=%s bytes=%d", r.RemoteAddr, len(body))
	resp, err := s.mcp.HandleBytes(body)
	if err != nil {
		log.Printf("http_mcp_error remote=%s bytes=%d status=%d duration_ms=%d error=%q", r.RemoteAddr, len(body), http.StatusInternalServerError, time.Since(start).Milliseconds(), err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp)
	log.Printf("http_mcp_response remote=%s request_bytes=%d response_bytes=%d status=%d duration_ms=%d", r.RemoteAddr, len(body), len(resp), http.StatusOK, time.Since(start).Milliseconds())
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
