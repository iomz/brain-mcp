package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
		resp := s.handle(req)
		if resp != nil {
			if err := enc.Encode(resp); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

func (s *Server) HandleBytes(data []byte) ([]byte, error) {
	var req request
	if err := json.Unmarshal(data, &req); err != nil {
		log.Printf("mcp_parse_error transport=http bytes=%d error=%q", len(data), err)
		resp := response{JSONRPC: "2.0", Error: &responseError{Code: -32700, Message: err.Error()}}
		return json.Marshal(resp)
	}
	resp := s.handle(req)
	if resp == nil {
		return nil, nil
	}
	return json.Marshal(resp)
}

func (s *Server) handle(req request) *response {
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
		result, err := s.callTool(req.Params)
		if err != nil {
			log.Printf("mcp_tool_error tool=%s error=%q", logValue(toolName), err)
			resp.Error = &responseError{Code: -32000, Message: err.Error()}
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

func (s *Server) callTool(raw json.RawMessage) (any, error) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
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

func toolResult(v any) map[string]any {
	data, _ := json.Marshal(v)
	return map[string]any{
		"content": []map[string]string{
			{"type": "text", "text": string(data)},
		},
	}
}

func tools() []map[string]any {
	stringSchema := map[string]string{"type": "string"}
	return []map[string]any{
		{"name": "brain_read_note", "description": "Read a Markdown note by relative path.", "inputSchema": objectSchema(map[string]any{"path": stringSchema}, []string{"path"})},
		{"name": "brain_list_notes", "description": "List Markdown notes under a directory prefix.", "inputSchema": objectSchema(map[string]any{"prefix": stringSchema}, []string{})},
		{"name": "brain_show_diff", "description": "Show unified diff between current and proposed content.", "inputSchema": objectSchema(map[string]any{"path": stringSchema, "new_content": stringSchema}, []string{"path", "new_content"})},
		{"name": "brain_apply_patch", "description": "Apply proposed Markdown content after producing a diff.", "inputSchema": objectSchema(map[string]any{"path": stringSchema, "proposed_content": stringSchema}, []string{"path", "proposed_content"})},
		{"name": "brain_append_section", "description": "Append Markdown content to an existing heading section.", "inputSchema": objectSchema(map[string]any{"path": stringSchema, "heading": stringSchema, "content": stringSchema}, []string{"path", "heading", "content"})},
		{"name": "brain_get_section", "description": "Read one Markdown section by exact heading line.", "inputSchema": objectSchema(map[string]any{"path": stringSchema, "heading": stringSchema}, []string{"path", "heading"})},
		{"name": "brain_replace_section", "description": "Replace one Markdown section by exact heading line.", "inputSchema": objectSchema(map[string]any{"path": stringSchema, "heading": stringSchema, "content": stringSchema}, []string{"path", "heading", "content"})},
		{"name": "brain_upsert_section", "description": "Replace an exact heading section, or insert it under an optional parent heading.", "inputSchema": objectSchema(map[string]any{"path": stringSchema, "heading": stringSchema, "content": stringSchema, "parent_heading": stringSchema}, []string{"path", "heading", "content"})},
		{"name": "brain_delete_duplicate_section", "description": "Delete duplicate exact heading sections while keeping first or last.", "inputSchema": objectSchema(map[string]any{"path": stringSchema, "heading": stringSchema, "keep": stringSchema}, []string{"path", "heading", "keep"})},
		{"name": "brain_replace_text", "description": "Replace one exact text occurrence in a Markdown note.", "inputSchema": objectSchema(map[string]any{"path": stringSchema, "old_text": stringSchema, "new_text": stringSchema}, []string{"path", "old_text", "new_text"})},
		{"name": "brain_git_status", "description": "Return git status for the Brain repository.", "inputSchema": objectSchema(map[string]any{}, []string{})},
		{"name": "brain_git_diff", "description": "Return git diff for the Brain repository.", "inputSchema": objectSchema(map[string]any{}, []string{})},
		{"name": "brain_git_commit", "description": "Commit current Brain repository changes and return the commit hash.", "inputSchema": objectSchema(map[string]any{"message": stringSchema}, []string{"message"})},
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
