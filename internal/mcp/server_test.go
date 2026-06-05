package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iomz/brain-mcp/internal/brain"
)

func TestInitializedNotificationReturnsNoError(t *testing.T) {
	server := testServer(t)

	resp, err := server.HandleBytes([]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(resp) != 0 {
		t.Fatalf("got response %s, want no response", resp)
	}
}

func TestNotificationWithoutIDProducesNoErrorResponse(t *testing.T) {
	server := testServer(t)

	resp, err := server.HandleBytes([]byte(`{"jsonrpc":"2.0","method":"unknown/notification"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(resp) != 0 {
		t.Fatalf("got response %s, want no response", resp)
	}
}

func TestUnknownRequestMethodReturnsMethodNotFound(t *testing.T) {
	server := testServer(t)

	resp, err := server.HandleBytes([]byte(`{"jsonrpc":"2.0","id":1,"method":"unknown/request"}`))
	if err != nil {
		t.Fatal(err)
	}

	var got response
	if err := json.Unmarshal(resp, &got); err != nil {
		t.Fatal(err)
	}
	if got.Error == nil {
		t.Fatalf("got no error in %s", resp)
	}
	if got.Error.Code != -32601 || got.Error.Message != "method not found" {
		t.Fatalf("got error %+v, want method not found", got.Error)
	}
}

func TestToolsListIncludesSecuritySchemes(t *testing.T) {
	server := testServer(t)

	resp, err := server.HandleBytes([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if err != nil {
		t.Fatal(err)
	}

	var got struct {
		Result struct {
			Tools []map[string]any `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(resp, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Result.Tools) == 0 {
		t.Fatalf("got no tools in %s", resp)
	}
	for _, tool := range got.Result.Tools {
		name, _ := tool["name"].(string)
		if _, ok := tool["securitySchemes"]; !ok {
			t.Fatalf("tool %s missing securitySchemes", name)
		}
		meta, ok := tool["_meta"].(map[string]any)
		if !ok {
			t.Fatalf("tool %s missing _meta", name)
		}
		if _, ok := meta["securitySchemes"]; !ok {
			t.Fatalf("tool %s missing _meta.securitySchemes", name)
		}
		if _, ok := tool["outputSchema"]; !ok {
			t.Fatalf("tool %s missing outputSchema", name)
		}
	}
}

func TestToolCallReturnsStructuredContent(t *testing.T) {
	server := testServer(t)
	path := filepath.Join(server.vault.Root(), "Knowledge", "Self.md")
	if err := os.WriteFile(path, []byte("# Self\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := server.HandleBytes([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"brain_read_note","arguments":{"path":"Knowledge/Self.md"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Result map[string]any `json:"result"`
	}
	if err := json.Unmarshal(resp, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Result["structuredContent"]; !ok {
		t.Fatalf("response missing structuredContent: %s", resp)
	}
}

func TestSectionToolsUpdateExistingHeading(t *testing.T) {
	server := testServer(t)
	path := filepath.Join(server.vault.Root(), "Knowledge", "Self.md")
	if err := os.WriteFile(path, []byte("# Self\n\n## Notes\n\nOld.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := server.HandleBytes([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"brain_append_section","arguments":{"path":"Knowledge/Self.md","heading":"Notes","content":"New."}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(resp), "diff") {
		t.Fatalf("response missing diff: %s", resp)
	}

	_, content, err := server.vault.ReadNote("Knowledge/Self.md")
	if err != nil {
		t.Fatal(err)
	}
	if content != "# Self\n\n## Notes\n\nOld.\n\nNew.\n\n" {
		t.Fatalf("got content:\n%s", content)
	}
}

func TestCreateNoteToolCreatesNewMarkdownFile(t *testing.T) {
	server := testServer(t)

	resp, err := server.HandleBytes([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"brain_create_note","arguments":{"path":"Knowledge/Preferences.md","content":"# Preferences"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	text := string(resp)
	if !strings.Contains(text, `"created":true`) || !strings.Contains(text, `"bytes_written"`) {
		t.Fatalf("response missing create fields: %s", resp)
	}
	_, content, err := server.vault.ReadNote("Knowledge/Preferences.md")
	if err != nil {
		t.Fatal(err)
	}
	if content != "# Preferences\n" {
		t.Fatalf("got content %q", content)
	}
}

func TestOAuthReadScopeCanListJournalAndResolveToday(t *testing.T) {
	server := testServer(t)
	if err := os.WriteFile(filepath.Join(server.vault.Root(), "Journal", "2026-06-05.md"), []byte("# Today\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	auth := AuthContext{
		Kind:    AuthKindOAuth,
		Subject: "brain-mcp-user",
		Email:   "user@example.test",
		Scopes:  []string{"brain:read"},
	}
	resp, err := server.HandleBytesWithAuth([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"brain_list_notes","arguments":{"prefix":"Journal/"}}}`), auth)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(resp), "Journal/2026-06-05.md") {
		t.Fatalf("response missing journal note: %s", resp)
	}

	resp, err = server.HandleBytesWithAuth([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"brain_get_today_journal","arguments":{}}}`), auth)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(resp), "Journal/") || !strings.Contains(string(resp), ".md") {
		t.Fatalf("response missing today journal path: %s", resp)
	}
}

func TestOAuthWriteScopeUpdatesExistingJournal(t *testing.T) {
	server := testServer(t)
	if err := os.WriteFile(filepath.Join(server.vault.Root(), "Journal", "2026-06-05.md"), []byte("# Today\n\n## Notes\n\nOld.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	auth := AuthContext{
		Kind:    AuthKindOAuth,
		Subject: "brain-mcp-user",
		Email:   "user@example.test",
		Scopes:  []string{"brain:write"},
	}
	resp, err := server.HandleBytesWithAuth([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"brain_upsert_section","arguments":{"path":"Journal/2026-06-05.md","heading":"## Notes","content":"New."}}}`), auth)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(resp), `"success":true`) {
		t.Fatalf("response missing success: %s", resp)
	}
	_, content, err := server.vault.ReadNote("Journal/2026-06-05.md")
	if err != nil {
		t.Fatal(err)
	}
	if content != "# Today\n\n## Notes\n\nNew.\n\n" {
		t.Fatalf("got content:\n%s", content)
	}
}

func TestOAuthWriteScopeCreatesJournalWithUpsert(t *testing.T) {
	server := testServer(t)
	auth := AuthContext{Kind: AuthKindOAuth, Scopes: []string{"brain:write"}}

	resp, err := server.HandleBytesWithAuth([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"brain_upsert_section","arguments":{"path":"Journal/2026-06-05.md","heading":"## Notes","content":"New."}}}`), auth)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(resp), `"success":true`) {
		t.Fatalf("response missing success: %s", resp)
	}
}

func TestMissingScopeUsesExplicitReason(t *testing.T) {
	server := testServer(t)

	resp, err := server.HandleBytesWithAuth([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"brain_list_notes","arguments":{"prefix":"Journal/"}}}`), AuthContext{Kind: AuthKindOAuth, Scopes: []string{"brain:write"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(resp), brain.ReasonMissingScope) {
		t.Fatalf("response missing missing_scope reason: %s", resp)
	}
}

func TestGitCommitToolReturnsHash(t *testing.T) {
	server := testGitServer(t)
	path := filepath.Join(server.vault.Root(), "Knowledge", "Self.md")
	if err := os.WriteFile(path, []byte("# Self\n\nUpdated.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := server.HandleBytes([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"brain_git_commit","arguments":{"message":"update self"}}}`))
	if err != nil {
		t.Fatal(err)
	}

	text := string(resp)
	if !strings.Contains(text, `\"hash\"`) {
		t.Fatalf("response missing hash: %s", resp)
	}
	status, err := runGitOutput(t, server.vault.Root(), "status", "--short")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(status) != "" {
		t.Fatalf("got dirty status %q", status)
	}
}

func testServer(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Knowledge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "Journal"), 0o755); err != nil {
		t.Fatal(err)
	}
	vault, err := brain.NewVaultWithPolicy(root, brain.Policy{
		WritablePaths: brain.DefaultWritablePaths(),
		ReadonlyPaths: brain.DefaultReadonlyPaths(),
		RequireGit:    false,
	})
	if err != nil {
		t.Fatal(err)
	}
	return NewServer(vault)
}

func testGitServer(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Knowledge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Knowledge", "Self.md"), []byte("# Self\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "brain-mcp@example.test")
	runGit(t, root, "config", "user.name", "brain-mcp tests")
	runGit(t, root, "config", "commit.gpgsign", "false")
	runGit(t, root, "add", "--all", "--")
	runGit(t, root, "commit", "-m", "initial")
	vault, err := brain.NewVaultWithPolicy(root, brain.Policy{
		WritablePaths: brain.DefaultWritablePaths(),
		ReadonlyPaths: brain.DefaultReadonlyPaths(),
		RequireGit:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return NewServer(vault)
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	if _, err := runGitOutput(t, root, args...); err != nil {
		t.Fatal(err)
	}
}

func runGitOutput(t *testing.T, root string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %v failed: %w\n%s", args, err, out)
	}
	return string(out), nil
}
