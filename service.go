package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type Service struct {
	store  *Store
	origin string
}

type Command struct {
	ID             string  `json:"id"`
	Title          string  `json:"title"`
	Private        *bool   `json:"private"`
	Path           string  `json:"path"`
	Version        string  `json:"version"`
	Expected       string  `json:"expected"`
	Description    string  `json:"description"`
	Content        *string `json:"content"`
	Encoding       string  `json:"encoding"`
	Confirm        bool    `json:"confirm"`
	Protected      bool    `json:"protected"`
	DefaultPage    *string `json:"defaultPage"`
	NewPassword    *string `json:"newPassword"`
	NewToken       *string `json:"newToken"`
	RequireAPIAuth *bool   `json:"requireApiAuth"`
	RequireMCPAuth *bool   `json:"requireMcpAuth"`
}

type PageView struct {
	Page
	URL    string `json:"url"`
	Status string `json:"status"`
}

type FileResult struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
	ETag     string `json:"etag"`
	data     []byte
}

func (a *Service) view(p Page) PageView {
	status := "draft"
	if p.Current != "" {
		status = "published"
	}
	return PageView{p, a.origin + "/p/" + p.ID + "/", status}
}

func (a *Service) execute(op string, c Command) (any, error) {
	s := a.store
	switch op {
	case "list_pages", "get_settings", "get_page", "list_files", "read_file", "list_versions", "preview_page", "get_page_url":
		s.mu.RLock()
		defer s.mu.RUnlock()
	default:
		s.mu.Lock()
		defer s.mu.Unlock()
	}
	switch op {
	case "list_pages":
		pages, err := s.list()
		out := make([]PageView, 0, len(pages))
		for _, p := range pages {
			out = append(out, a.view(p))
		}
		return out, err
	case "create_page":
		p, err := s.create(c.ID, c.Title)
		return a.view(p), err
	case "get_settings", "update_settings":
		out := map[string]any{"publicUrl": a.origin, "previewUrl": a.origin, "adminUrl": a.origin, "version": appVersion, "retention": retention, "maxFileBytes": maxFile, "maxDraftBytes": maxDraft, "apiToken": s.settings.APIToken, "starterHtml": seedPageHTML}
		if op == "update_settings" {
			next := s.settings
			if c.DefaultPage != nil {
				id := *c.DefaultPage
				if id != "" {
					p, err := s.load(id)
					if err != nil {
						return nil, err
					}
					if p.Current == "" || p.Private {
						return nil, fmt.Errorf("%w: default page must be published and public", errInvalid)
					}
				}
				next.DefaultPage = id
			}
			if c.RequireAPIAuth != nil {
				next.APIAuthDisabled = !*c.RequireAPIAuth
			}
			if c.RequireMCPAuth != nil {
				next.MCPAuthDisabled = !*c.RequireMCPAuth
			}
			if c.NewPassword != nil {
				password := *c.NewPassword
				if len(password) < 12 || len(password) > 256 ||
					strings.TrimSpace(password) != password || strings.IndexFunc(password, func(char rune) bool { return char < 32 || char > 126 }) >= 0 {
					return nil, fmt.Errorf("%w: password requires 12–256 printable ASCII characters with no surrounding spaces", errInvalid)
				}
				next.AdminHash = digest([]byte(password))
				if next.AdminHash != s.settings.AdminHash {
					next.PreviewKey = randomID() + randomID()
				}
			}
			if c.NewToken != nil {
				token := *c.NewToken
				if len(token) < 24 || len(token) > 256 || strings.TrimSpace(token) != token ||
					strings.IndexFunc(token, func(char rune) bool { return char < 32 || char > 126 }) >= 0 {
					return nil, fmt.Errorf("%w: token requires 24–256 printable ASCII characters with no surrounding spaces", errInvalid)
				}
				next.APIToken = token
				next.TokenHash = digest([]byte(token))
			}
			if err := s.writeJSON("config/settings.json", next); err != nil {
				return nil, err
			}
			defaultChanged := s.settings.DefaultPage != next.DefaultPage
			s.settings = next
			if defaultChanged {
				s.notify()
			}
		}
		out["defaultPage"] = s.settings.DefaultPage
		out["requireApiAuth"] = !s.settings.APIAuthDisabled
		out["requireMcpAuth"] = !s.settings.MCPAuthDisabled
		return out, nil
	}

	p, err := s.load(c.ID)
	if err != nil {
		return nil, err
	}
	switch op {
	case "get_page":
		return a.view(p), nil
	case "get_page_url":
		return map[string]string{"url": a.view(p).URL}, nil
	case "update_page":
		title := strings.TrimSpace(c.Title)
		if len(title) > 240 || (title == "" && c.Private == nil) {
			return nil, errInvalid
		}
		if title != "" {
			p.Title = title
		}
		visibilityChanged := c.Private != nil && p.Private != *c.Private
		if visibilityChanged {
			if *c.Private && s.settings.DefaultPage == p.ID {
				return nil, fmt.Errorf("%w: clear the default page before restricting access", errInvalid)
			}
			p.Private = *c.Private
		}
		if err := s.save(p); err != nil {
			return nil, err
		}
		if visibilityChanged {
			s.notify()
		}
		return a.view(p), nil
	case "delete_page":
		if !c.Confirm {
			return nil, fmt.Errorf("%w: confirmation required", errInvalid)
		}
		if s.settings.DefaultPage == p.ID {
			return nil, fmt.Errorf("%w: clear the default page before deleting it", errInvalid)
		}
		deleted := "pages/.deleted-" + randomID()
		if err := s.root.Rename(pageDir(p.ID), deleted); err != nil {
			return nil, err
		}
		s.notify()
		s.cleanup(deleted)
		return map[string]bool{"deleted": true}, nil
	case "preview_page":
		expiry := time.Now().Add(30 * time.Minute).Unix()
		ticket := strconv.FormatInt(expiry, 10)
		ticket += "." + signPreview(s.settings.PreviewKey, p, ticket)
		return map[string]any{"url": a.origin + "/d/" + p.ID + "/" + ticket + "/", "expiresAt": time.Unix(expiry, 0).UTC()}, nil
	case "list_versions":
		return p.Versions, nil
	case "protect_version":
		for i, v := range p.Versions {
			if v.ID == c.Version {
				p.Versions[i].Protected = c.Protected
				return p.Versions[i], s.save(p)
			}
		}
		return nil, errMissing
	case "publish_page", "restore_version", "reset_draft":
		action := strings.SplitN(op, "_", 2)[0]
		if action != "publish" && c.Version == "" {
			return nil, errInvalid
		}
		if action == "publish" || action == "reset" {
			if c.Expected == "" {
				return nil, fmt.Errorf("%w: expected draft revision is required", errInvalid)
			}
		}
		if len(c.Description) > 1000 {
			return nil, errInvalid
		}
		if action == "publish" {
			c.Version = ""
		}
		return s.changeVersion(p, action, c.Version, c.Description, c.Expected)
	case "list_files", "read_file":
		dir, err := s.directory(p, c.Version)
		if err != nil {
			return nil, err
		}
		if op == "list_files" {
			files, _, err := s.tree(dir)
			if err != nil {
				return nil, err
			}
			result := map[string]any{"files": files}
			if c.Version == "" {
				result["revision"], err = s.draftRevision(p)
			}
			return result, err
		}
		data, err := s.readFile(dir, c.Path)
		if err != nil {
			return nil, err
		}
		encoding, content := "utf8", string(data)
		if !utf8.Valid(data) || strings.ContainsRune(content, 0) {
			encoding, content = "base64", base64.StdEncoding.EncodeToString(data)
		}
		return FileResult{Content: content, Encoding: encoding, ETag: digest(data), data: data}, nil
	case "write_file", "delete_file":
		remove := op == "delete_file"
		if c.Expected == "" || (!remove && c.Content == nil) {
			return nil, fmt.Errorf("%w: expected file ETag (or new) and write content are required", errInvalid)
		}
		var data []byte
		if !remove {
			switch c.Encoding {
			case "", "utf8":
				data = []byte(*c.Content)
			case "base64":
				data, err = base64.StdEncoding.DecodeString(*c.Content)
			default:
				err = errInvalid
			}
			if err != nil {
				return nil, errInvalid
			}
		}
		tag, err := s.editFile(p, c.Path, c.Expected, data, remove)
		return map[string]string{"etag": tag}, err
	default:
		return nil, errMissing
	}
}

func signPreview(secret string, page Page, expiry string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(page.ID + "\n" + page.Instance + "\n" + expiry))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func validOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return "", fmt.Errorf("invalid origin %q", raw)
	}
	u.Host = strings.ToLower(u.Host)
	if (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80") {
		u.Host = strings.TrimSuffix(u.Host, ":"+u.Port())
	}
	return u.String(), nil
}
