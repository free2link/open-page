package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (a *Service) mcp() http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "OpenPage", Version: appVersion}, nil)
	for _, tool := range []struct{ name, description, fields string }{
		{"list_pages", "List pages and published URLs", ""},
		{"create_page", "Create a page and an empty draft", "id title"},
		{"get_page", "Read page settings and current version", "id"},
		{"update_page", "Change a page title or restrict public access", "id title? private?"},
		{"delete_page", "Delete a non-default page and its history; confirm must be true", "id confirm"},
		{"list_files", "List files and obtain a draft revision; pass version to inspect history", "id version?"},
		{"read_file", "Read a file and its ETag; pass version to inspect history", "id path version?"},
		{"write_file", "Write a draft file; expected is the file ETag or new for creation", "id path expected content encoding?"},
		{"delete_file", "Delete a draft file using its current ETag as expected", "id path expected"},
		{"preview_page", "Get a private draft preview URL valid for 30 minutes", "id"},
		{"publish_page", "Publish a draft; expected is the draft revision returned by list_files", "id expected description?"},
		{"list_versions", "List release history", "id"},
		{"protect_version", "Set whether a release is protected from automatic pruning", "id version protected"},
		{"restore_version", "Publish a historical release again", "id version description?"},
		{"reset_draft", "Replace the draft from a release; expected is the draft revision returned by list_files", "id version expected"},
		{"get_page_url", "Get the stable public URL", "id"},
		{"get_settings", "Read service settings", ""},
		{"update_settings", "Set or clear the default page, or rotate and revoke the current API token", "defaultPage? rotateToken?"},
	} {
		properties := map[string]map[string]string{}
		required := []string{}
		for _, field := range strings.Fields(tool.fields) {
			name := strings.TrimSuffix(field, "?")
			kind := "string"
			switch name {
			case "confirm", "protected", "rotateToken", "private":
				kind = "boolean"
			}
			properties[name] = map[string]string{"type": kind}
			if name == field {
				required = append(required, name)
			}
		}
		server.AddTool(&mcp.Tool{Name: tool.name, Description: tool.description, InputSchema: map[string]any{
			"type": "object", "properties": properties, "required": required, "additionalProperties": false,
		}}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args := req.Params.Arguments
			if len(args) == 0 {
				args = json.RawMessage("{}")
			}
			var input map[string]json.RawMessage
			if err := json.Unmarshal(args, &input); err != nil || input == nil {
				return toolFailure("arguments must be an object"), nil
			}
			for name, value := range input {
				prop, exists := properties[name]
				if !exists || bytes.Equal(value, []byte("null")) {
					return toolFailure("unexpected or null argument: " + name), nil
				}
				var target any = new(string)
				if prop["type"] == "boolean" {
					target = new(bool)
				}
				if err := json.Unmarshal(value, target); err != nil {
					return toolFailure("invalid argument type: " + name), nil
				}
			}
			for _, name := range required {
				if _, exists := input[name]; !exists {
					return toolFailure("required argument: " + name), nil
				}
			}
			var c Command
			if err := decodeCommand(bytes.NewReader(args), &c); err != nil {
				return toolFailure(err.Error()), nil
			}
			out, err := a.execute(tool.name, c)
			if err != nil {
				if errors.Is(err, errMissing) || errors.Is(err, errConflict) || errors.Is(err, errInvalid) || errors.Is(err, errQuota) {
					return toolFailure(err.Error()), nil
				}
				log.Printf("MCP %s: %v", tool.name, err)
				return toolFailure("internal server error"), nil
			}
			structured := map[string]any{"result": out}
			data, err := json.Marshal(structured)
			if err != nil {
				return nil, fmt.Errorf("encode tool result: %w", err)
			}
			return &mcp.CallToolResult{StructuredContent: structured, Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil
		})
	}
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	return a.authorized(handler)
}

func toolFailure(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}
}
