package mcp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/iomz/brain-mcp/internal/brain"
	braingit "github.com/iomz/brain-mcp/internal/git"
)

type Server struct {
	vault *brain.Vault
}

func NewServer(vault *brain.Vault) *Server {
	return &Server{vault: vault}
}

type AuthKind string

const (
	AuthKindBearerStatic AuthKind = "bearer_static"
	AuthKindOAuth        AuthKind = "oauth"
	AuthKindAnonymous    AuthKind = "anonymous"
)

type AuthContext struct {
	Subject   string
	Email     string
	Groups    []string
	Scopes    []string
	Kind      AuthKind
	Issuer    string
	Audience  []string
	ExpiresAt int64
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	HasID   bool            `json:"-"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func (r *request) UnmarshalJSON(data []byte) error {
	type alias request
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	_, a.HasID = raw["id"]
	*r = request(a)
	return nil
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *responseError  `json:"error,omitempty"`
}

type responseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) Run(r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	enc := json.NewEncoder(w)
	for scanner.Scan() {
		var req request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			log.Printf("mcp_parse_error transport=stdio error=%q", err)
			if err := enc.Encode(response{JSONRPC: "2.0", Error: &responseError{Code: -32700, Message: err.Error()}}); err != nil {
				return err
			}
			continue
		}
		resp := s.handle(req, AuthContext{
			Kind:   AuthKindBearerStatic,
			Scopes: []string{"brain:read", "brain:write", "brain:git", "brain:admin"},
		})
		if resp != nil {
			if err := enc.Encode(resp); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

func (s *Server) HandleBytes(data []byte) ([]byte, error) {
	return s.HandleBytesWithAuth(data, AuthContext{
		Kind:   AuthKindBearerStatic,
		Scopes: []string{"brain:read", "brain:write", "brain:git", "brain:admin"},
	})
}

func (s *Server) HandleBytesWithAuth(data []byte, auth AuthContext) ([]byte, error) {
	var req request
	if err := json.Unmarshal(data, &req); err != nil {
		log.Printf("mcp_parse_error transport=http bytes=%d error=%q", len(data), err)
		resp := response{JSONRPC: "2.0", Error: &responseError{Code: -32700, Message: err.Error()}}
		return json.Marshal(resp)
	}
	resp := s.handle(req, auth)
	if resp == nil {
		return nil, nil
	}
	return json.Marshal(resp)
}

func (s *Server) handle(req request, auth AuthContext) *response {
	start := time.Now()
	toolName := requestToolName(req)
	log.Printf("mcp_request method=%s tool=%s has_id=%t", req.Method, logValue(toolName), req.HasID)
	defer func() {
		log.Printf("mcp_request_done method=%s tool=%s has_id=%t duration_ms=%d", req.Method, logValue(toolName), req.HasID, time.Since(start).Milliseconds())
	}()

	if !req.HasID {
		return nil
	}

	resp := &response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": "2024-11-05",
			"serverInfo": map[string]string{
				"name":    "brain-mcp",
				"version": "0.0.1",
			},
			"capabilities": map[string]any{"tools": map[string]any{}},
		}
	case "tools/list":
		resp.Result = map[string]any{"tools": tools()}
	case "tools/call":
		result, err := s.callTool(req.Params, auth)
		if err != nil {
			logToolError(toolName, err, auth)
			code := -32000
			if isInsufficientScope(err) {
				code = -32003
			}
			resp.Error = &responseError{Code: code, Message: err.Error()}
			return resp
		}
		log.Printf("mcp_tool_result tool=%s status=ok", logValue(toolName))
		resp.Result = result
	default:
		resp.Error = &responseError{Code: -32601, Message: "method not found"}
	}
	return resp
}

func requestToolName(req request) string {
	if req.Method != "tools/call" || len(req.Params) == 0 {
		return ""
	}
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return ""
	}
	return params.Name
}

func logValue(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func (s *Server) callTool(raw json.RawMessage, auth AuthContext) (any, error) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}
	if scope := RequiredScope(params.Name); scope != "" && !HasScope(auth.Scopes, scope) {
		return nil, insufficientScopeError{scope: scope}
	}
	switch params.Name {
	case "brain_read_note":
		var args struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, err
		}
		path, content, err := s.vault.ReadNote(args.Path)
		if err != nil {
			return nil, err
		}
		return toolResult(map[string]any{"path": path, "content": content}), nil
	case "brain_list_notes":
		var args struct {
			Dir    string `json:"dir"`
			Prefix string `json:"prefix"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, err
		}
		dir := args.Prefix
		if dir == "" {
			dir = args.Dir
		}
		notes, err := s.vault.ListNotes(dir)
		if err != nil {
			return nil, err
		}
		return toolResult(map[string]any{"notes": notes}), nil
	case "brain_get_journal_config":
		return toolResult(map[string]any{"journal": s.vault.JournalConfig()}), nil
	case "brain_get_today_journal":
		journal, err := s.vault.TodayJournal(time.Now())
		if err != nil {
			return nil, err
		}
		return toolResult(map[string]any{"journal": journal}), nil
	case "brain_find_recent_journals":
		var args struct {
			Limit int `json:"limit"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, err
		}
		journals, err := s.vault.RecentJournals(args.Limit)
		if err != nil {
			return nil, err
		}
		return toolResult(map[string]any{"journals": journals}), nil
	case "brain_show_diff":
		var args struct {
			Path       string `json:"path"`
			NewContent string `json:"new_content"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, err
		}
		path, patch, err := s.vault.ShowDiff(args.Path, args.NewContent)
		if err != nil {
			return nil, err
		}
		return toolResult(map[string]any{"path": path, "diff": patch}), nil
	case "brain_apply_patch", "brain_write_note":
		var args struct {
			Path            string `json:"path"`
			Content         string `json:"content"`
			ProposedContent string `json:"proposed_content"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, err
		}
		content := args.ProposedContent
		if content == "" {
			content = args.Content
		}
		path, patch, err := s.vault.ApplyPatch(args.Path, content)
		if err != nil {
			return nil, err
		}
		return toolResult(map[string]any{"success": true, "path": path, "diff": patch}), nil
	case "brain_create_note":
		var args struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, err
		}
		path, bytesWritten, err := s.vault.CreateNote(args.Path, args.Content)
		if err != nil {
			return nil, err
		}
		return toolResult(map[string]any{
			"path":          path,
			"created":       true,
			"bytes_written": bytesWritten,
			"message":       "created note " + path,
		}), nil
	case "brain_append_section":
		var args struct {
			Path    string `json:"path"`
			Heading string `json:"heading"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, err
		}
		path, patch, err := s.vault.AppendSection(args.Path, args.Heading, args.Content)
		if err != nil {
			return nil, err
		}
		return toolResult(map[string]any{"success": true, "path": path, "diff": patch}), nil
	case "brain_get_section":
		var args struct {
			Path    string `json:"path"`
			Heading string `json:"heading"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, err
		}
		path, content, err := s.vault.GetSection(args.Path, args.Heading)
		if err != nil {
			return nil, err
		}
		return toolResult(map[string]any{"path": path, "content": content}), nil
	case "brain_replace_section":
		var args struct {
			Path    string `json:"path"`
			Heading string `json:"heading"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, err
		}
		path, patch, err := s.vault.ReplaceSection(args.Path, args.Heading, args.Content)
		if err != nil {
			return nil, err
		}
		return toolResult(map[string]any{"success": true, "path": path, "diff": patch}), nil
	case "brain_upsert_section":
		var args struct {
			Path          string `json:"path"`
			Heading       string `json:"heading"`
			Content       string `json:"content"`
			ParentHeading string `json:"parent_heading"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, err
		}
		path, patch, err := s.vault.UpsertSection(args.Path, args.Heading, args.Content, args.ParentHeading)
		if err != nil {
			return nil, err
		}
		return toolResult(map[string]any{"success": true, "path": path, "diff": patch}), nil
	case "brain_delete_duplicate_section":
		var args struct {
			Path    string `json:"path"`
			Heading string `json:"heading"`
			Keep    string `json:"keep"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, err
		}
		path, patch, err := s.vault.DeleteDuplicateSection(args.Path, args.Heading, args.Keep)
		if err != nil {
			return nil, err
		}
		return toolResult(map[string]any{"success": true, "path": path, "diff": patch}), nil
	case "brain_replace_text":
		var args struct {
			Path    string `json:"path"`
			OldText string `json:"old_text"`
			NewText string `json:"new_text"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, err
		}
		path, patch, err := s.vault.ReplaceText(args.Path, args.OldText, args.NewText)
		if err != nil {
			return nil, err
		}
		return toolResult(map[string]any{"success": true, "path": path, "diff": patch}), nil
	case "brain_git_status":
		status, err := braingit.Status(s.vault.Root())
		if err != nil {
			return nil, err
		}
		return toolResult(map[string]any{"status": status}), nil
	case "brain_git_diff":
		patch, err := braingit.Diff(s.vault.Root())
		if err != nil {
			return nil, err
		}
		return toolResult(map[string]any{"diff": patch}), nil
	case "brain_git_commit":
		var args struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, err
		}
		hash, err := braingit.Commit(s.vault.Root(), args.Message)
		if err != nil {
			return nil, err
		}
		return toolResult(map[string]any{"hash": hash}), nil
	default:
		return nil, fmt.Errorf("unknown tool: %s", params.Name)
	}
}

type insufficientScopeError struct {
	scope string
}

func (e insufficientScopeError) Error() string {
	return brain.ReasonMissingScope + ": insufficient scope: " + e.scope
}

func isInsufficientScope(err error) bool {
	_, ok := err.(insufficientScopeError)
	return ok
}

func logToolError(toolName string, err error, auth AuthContext) {
	var pathErr brain.PathError
	if errors.As(err, &pathErr) {
		log.Printf(
			"mcp_tool_error tool=%s error=%q reason=%s requested_path=%q normalized_path=%q resolved_path=%q file_exists=%t parent_exists=%t auth_kind=%s subject=%q email=%q scopes=%q issuer=%q audience=%q",
			logValue(toolName),
			err,
			pathErr.Code,
			pathErr.RequestedPath,
			pathErr.NormalizedPath,
			pathErr.ResolvedPath,
			pathErr.FileExists,
			pathErr.ParentExists,
			auth.Kind,
			auth.Subject,
			auth.Email,
			strings.Join(auth.Scopes, ","),
			auth.Issuer,
			strings.Join(auth.Audience, ","),
		)
		return
	}
	log.Printf("mcp_tool_error tool=%s error=%q auth_kind=%s subject=%q email=%q scopes=%q issuer=%q audience=%q", logValue(toolName), err, auth.Kind, auth.Subject, auth.Email, strings.Join(auth.Scopes, ","), auth.Issuer, strings.Join(auth.Audience, ","))
}

func RequiredScope(toolName string) string {
	switch toolName {
	case "brain_read_note", "brain_list_notes", "brain_get_section", "brain_show_diff", "brain_get_journal_config", "brain_get_today_journal", "brain_find_recent_journals":
		return "brain:read"
	case "brain_apply_patch", "brain_write_note", "brain_create_note", "brain_append_section", "brain_replace_section", "brain_upsert_section", "brain_delete_duplicate_section", "brain_replace_text":
		return "brain:write"
	case "brain_git_status", "brain_git_diff", "brain_git_commit":
		return "brain:git"
	default:
		return ""
	}
}

func HasScope(scopes []string, want string) bool {
	for _, scope := range scopes {
		if scope == want || scope == "brain:admin" {
			return true
		}
	}
	return false
}

func toolResult(v any) map[string]any {
	data, _ := json.Marshal(v)
	return map[string]any{
		"content": []map[string]string{
			{"type": "text", "text": string(data)},
		},
		"structuredContent": v,
	}
}

func tools() []map[string]any {
	stringSchema := map[string]string{"type": "string"}
	boolSchema := map[string]string{"type": "boolean"}
	stringArraySchema := map[string]any{"type": "array", "items": stringSchema}
	readNoteOutput := objectSchema(map[string]any{"path": stringSchema, "content": stringSchema}, []string{"path", "content"})
	listNotesOutput := objectSchema(map[string]any{"notes": stringArraySchema}, []string{"notes"})
	journalConfigOutput := objectSchema(map[string]any{"journal": objectSchema(map[string]any{"root": stringSchema, "daily_pattern": stringSchema, "monthly_pattern": stringSchema, "yearly_pattern": stringSchema}, []string{"root", "daily_pattern", "monthly_pattern", "yearly_pattern"})}, []string{"journal"})
	journalNoteOutput := objectSchema(map[string]any{"journal": objectSchema(map[string]any{"path": stringSchema, "exists": boolSchema}, []string{"path", "exists"})}, []string{"journal"})
	recentJournalsOutput := objectSchema(map[string]any{"journals": map[string]any{"type": "array", "items": objectSchema(map[string]any{"path": stringSchema, "exists": boolSchema}, []string{"path", "exists"})}}, []string{"journals"})
	diffOutput := objectSchema(map[string]any{"path": stringSchema, "diff": stringSchema}, []string{"path", "diff"})
	writeOutput := objectSchema(map[string]any{"success": boolSchema, "path": stringSchema, "diff": stringSchema}, []string{"success", "path", "diff"})
	createOutput := objectSchema(map[string]any{"path": stringSchema, "created": boolSchema, "bytes_written": map[string]string{"type": "integer"}, "message": stringSchema}, []string{"path", "created", "bytes_written", "message"})
	statusOutput := objectSchema(map[string]any{"status": stringSchema}, []string{"status"})
	gitDiffOutput := objectSchema(map[string]any{"diff": stringSchema}, []string{"diff"})
	commitOutput := objectSchema(map[string]any{"hash": stringSchema}, []string{"hash"})
	return []map[string]any{
		tool("brain_read_note", "Read a Markdown note by relative path.", objectSchema(map[string]any{"path": stringSchema}, []string{"path"}), readNoteOutput, true, "brain:read"),
		tool("brain_list_notes", "List Markdown notes under a directory prefix.", objectSchema(map[string]any{"prefix": stringSchema}, []string{}), listNotesOutput, true, "brain:read"),
		tool("brain_get_journal_config", "Return journal root and date patterns.", objectSchema(map[string]any{}, []string{}), journalConfigOutput, true, "brain:read"),
		tool("brain_get_today_journal", "Resolve today's daily journal note path and whether it exists.", objectSchema(map[string]any{}, []string{}), journalNoteOutput, true, "brain:read"),
		tool("brain_find_recent_journals", "List recent journal notes newest first.", objectSchema(map[string]any{"limit": map[string]string{"type": "integer"}}, []string{}), recentJournalsOutput, true, "brain:read"),
		tool("brain_show_diff", "Show unified diff between current and proposed content.", objectSchema(map[string]any{"path": stringSchema, "new_content": stringSchema}, []string{"path", "new_content"}), diffOutput, true, "brain:read"),
		tool("brain_apply_patch", "Apply proposed Markdown content after producing a diff.", objectSchema(map[string]any{"path": stringSchema, "proposed_content": stringSchema}, []string{"path", "proposed_content"}), writeOutput, false, "brain:write"),
		tool("brain_create_note", "Create a new Markdown note without overwriting existing files.", objectSchema(map[string]any{"path": stringSchema, "content": stringSchema}, []string{"path", "content"}), createOutput, false, "brain:write"),
		tool("brain_append_section", "Append Markdown content to an existing heading section.", objectSchema(map[string]any{"path": stringSchema, "heading": stringSchema, "content": stringSchema}, []string{"path", "heading", "content"}), writeOutput, false, "brain:write"),
		tool("brain_get_section", "Read one Markdown section by exact heading line.", objectSchema(map[string]any{"path": stringSchema, "heading": stringSchema}, []string{"path", "heading"}), readNoteOutput, true, "brain:read"),
		tool("brain_replace_section", "Replace one Markdown section by exact heading line.", objectSchema(map[string]any{"path": stringSchema, "heading": stringSchema, "content": stringSchema}, []string{"path", "heading", "content"}), writeOutput, false, "brain:write"),
		tool("brain_upsert_section", "Replace an exact heading section, or insert it under an optional parent heading.", objectSchema(map[string]any{"path": stringSchema, "heading": stringSchema, "content": stringSchema, "parent_heading": stringSchema}, []string{"path", "heading", "content"}), writeOutput, false, "brain:write"),
		tool("brain_delete_duplicate_section", "Delete duplicate exact heading sections while keeping first or last.", objectSchema(map[string]any{"path": stringSchema, "heading": stringSchema, "keep": stringSchema}, []string{"path", "heading", "keep"}), writeOutput, false, "brain:write"),
		tool("brain_replace_text", "Replace one exact text occurrence in a Markdown note.", objectSchema(map[string]any{"path": stringSchema, "old_text": stringSchema, "new_text": stringSchema}, []string{"path", "old_text", "new_text"}), writeOutput, false, "brain:write"),
		tool("brain_git_status", "Return git status for the Brain repository.", objectSchema(map[string]any{}, []string{}), statusOutput, true, "brain:git"),
		tool("brain_git_diff", "Return git diff for the Brain repository.", objectSchema(map[string]any{}, []string{}), gitDiffOutput, true, "brain:git"),
		tool("brain_git_commit", "Commit current Brain repository changes and return the commit hash. Use only after explicit user approval.", objectSchema(map[string]any{"message": stringSchema}, []string{"message"}), commitOutput, false, "brain:git"),
	}
}

func tool(name, description string, inputSchema, outputSchema map[string]any, readOnly bool, scopes ...string) map[string]any {
	securitySchemes := []map[string]any{{"type": "oauth2", "scopes": scopes}}
	annotations := map[string]any{"readOnlyHint": readOnly}
	return map[string]any{
		"name":            name,
		"title":           name,
		"description":     description,
		"inputSchema":     inputSchema,
		"outputSchema":    outputSchema,
		"annotations":     annotations,
		"securitySchemes": securitySchemes,
		"_meta": map[string]any{
			"securitySchemes":                securitySchemes,
			"openai/toolInvocation/invoking": "Running " + name,
			"openai/toolInvocation/invoked":  "Completed " + name,
		},
	}
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}
