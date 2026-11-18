package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"path"
	"strings"
	"time"
)

const maxRequest = maxFile*2 + 4096

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("write JSON response: %v", err)
	}
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	message := err.Error()
	switch {
	case errors.Is(err, errMissing):
		status = http.StatusNotFound
	case errors.Is(err, errConflict):
		status = http.StatusConflict
	case errors.Is(err, errInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, errQuota):
		status = http.StatusRequestEntityTooLarge
	default:
		log.Printf("request failed: %v", err)
		message = "internal server error"
	}
	writeJSON(w, status, map[string]string{"error": message})
}

func decodeCommand(body io.Reader, c *Command) error {
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(c); err != nil {
		return errInvalid
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errInvalid
	}
	return nil
}

type authenticatedKey struct{}

type accessMode uint8

const (
	adminAccess accessMode = iota
	apiAccess
	mcpAccess
)

func (a *Service) authorized(next http.Handler, mode accessMode) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		origin := r.Header.Get("Origin")
		if (origin != "" && origin != a.origin) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			writeJSON(w, 403, map[string]string{"error": "origin is not allowed"})
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		a.store.mu.RLock()
		hash := a.store.settings.TokenHash
		if mode == adminAccess {
			hash = a.store.settings.AdminHash
		}
		valid := ok && subtle.ConstantTimeCompare([]byte(digest([]byte(token))), []byte(hash)) == 1
		optional := (mode == apiAccess && a.store.settings.APIAuthDisabled) ||
			(mode == mcpAccess && a.store.settings.MCPAuthDisabled)
		a.store.mu.RUnlock()
		if !valid && !optional {
			requirePassword(w)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxRequest)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authenticatedKey{}, valid)))
	})
}

func requirePassword(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="OpenPage"`)
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "valid access token required"})
}

func (a *Service) endpoint(op string, body bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if (op == "get_settings" || op == "update_settings") && r.Context().Value(authenticatedKey{}) != true {
			requirePassword(w)
			return
		}
		var c Command
		if body {
			if err := decodeCommand(r.Body, &c); err != nil {
				writeError(w, err)
				return
			}
		}
		for key, target := range map[string]*string{"id": &c.ID, "file": &c.Path, "version": &c.Version} {
			if value := r.PathValue(key); value != "" {
				*target = value
			}
		}
		if op == "write_file" || op == "delete_file" {
			if r.Header.Get("If-None-Match") == "*" {
				c.Expected = "new"
			} else {
				c.Expected = strings.Trim(r.Header.Get("If-Match"), `"`)
			}
		}
		if op == "write_file" {
			data, err := io.ReadAll(io.LimitReader(r.Body, maxFile+1))
			if err != nil {
				writeError(w, err)
				return
			}
			if len(data) > maxFile {
				writeError(w, errQuota)
				return
			}
			content := string(data)
			c.Content = &content
		}
		if op == "delete_page" {
			c.Confirm = r.URL.Query().Get("confirm") == c.ID
		}
		out, err := a.execute(op, c)
		if err != nil {
			writeError(w, err)
			return
		}
		if file, ok := out.(FileResult); ok {
			w.Header().Set("ETag", `"`+file.ETag+`"`)
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", "attachment")
			http.ServeContent(w, r, path.Base(c.Path), time.Time{}, bytes.NewReader(file.data))
			return
		}
		status := http.StatusOK
		if op == "create_page" || op == "publish_page" || op == "restore_version" {
			status = http.StatusCreated
		}
		writeJSON(w, status, out)
	}
}

func (a *Service) api() http.Handler {
	mux := http.NewServeMux()
	for _, route := range []struct {
		pattern, op string
		body        bool
	}{
		{"GET /api/pages", "list_pages", false},
		{"POST /api/pages", "create_page", true},
		{"GET /api/pages/{id}", "get_page", false},
		{"PUT /api/pages/{id}", "update_page", true},
		{"DELETE /api/pages/{id}", "delete_page", false},
		{"GET /api/pages/{id}/files", "list_files", false},
		{"GET /api/pages/{id}/files/{file...}", "read_file", false},
		{"PUT /api/pages/{id}/files/{file...}", "write_file", false},
		{"DELETE /api/pages/{id}/files/{file...}", "delete_file", false},
		{"POST /api/pages/{id}/preview", "preview_page", false},
		{"GET /api/pages/{id}/versions", "list_versions", false},
		{"GET /api/pages/{id}/versions/{version}/files", "list_files", false},
		{"GET /api/pages/{id}/versions/{version}/files/{file...}", "read_file", false},
		{"PUT /api/pages/{id}/versions/{version}", "protect_version", true},
		{"POST /api/pages/{id}/publish", "publish_page", true},
		{"POST /api/pages/{id}/restore", "restore_version", true},
		{"POST /api/pages/{id}/reset", "reset_draft", true},
		{"GET /api/settings", "get_settings", false},
		{"PUT /api/settings", "update_settings", true},
	} {
		mux.HandleFunc(route.pattern, a.endpoint(route.op, route.body))
	}
	return mux
}
