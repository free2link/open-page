package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	maxFile     = 8 << 20
	maxDraft    = 64 << 20
	maxFiles    = 4096
	maxReleases = int64(2 << 30)
	retention   = 20
)

var (
	errMissing  = errors.New("not found")
	errConflict = errors.New("content changed; reload before retrying")
	errInvalid  = errors.New("invalid input")
	errQuota    = errors.New("storage limit exceeded")
	pageID      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	filePart    = regexp.MustCompile(`^[a-zA-Z0-9_@-][a-zA-Z0-9_.@ -]{0,127}$`)
)

type Version struct {
	ID           string    `json:"id"`
	Created      time.Time `json:"created"`
	Description  string    `json:"description"`
	RestoredFrom string    `json:"restoredFrom,omitempty"`
	Protected    bool      `json:"protected"`
	Bytes        int64     `json:"bytes"`
}

type Page struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Private  bool      `json:"private"`
	Draft    string    `json:"-"`
	Current  string    `json:"current"`
	Versions []Version `json:"-"`
	Instance string    `json:"-"`
}

type pageDisk struct {
	Page
	DraftDir   string    `json:"draft"`
	History    []Version `json:"versions"`
	InstanceID string    `json:"instance"`
}

type Settings struct {
	DefaultPage     string `json:"defaultPage"`
	TokenHash       string `json:"tokenHash"`
	PreviewKey      string `json:"previewKey"`
	APIAuthDisabled bool   `json:"apiAuthDisabled,omitempty"`
	MCPAuthDisabled bool   `json:"mcpAuthDisabled,omitempty"`
}

type Store struct {
	mu       sync.RWMutex
	root     *os.Root
	lock     *os.File
	settings Settings
	changed  chan struct{}
}

func randomID() string {
	var b [18]byte
	rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func openStore(dir, bootstrap string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	s := &Store{root: root, changed: make(chan struct{})}
	s.lock, err = root.OpenFile(".openpage.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		s.close()
		return nil, err
	}
	if err := syscall.Flock(int(s.lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		s.close()
		return nil, fmt.Errorf("lock data directory: %w", err)
	}
	if err := s.initialize(bootstrap); err != nil {
		s.close()
		return nil, err
	}
	if err := s.recoverStorage(); err != nil {
		s.close()
		return nil, err
	}
	return s, nil
}

func (s *Store) close() {
	if s.lock != nil {
		s.lock.Close()
	}
	s.root.Close()
}

func (s *Store) recoverStorage() error {
	entries, err := fs.ReadDir(s.root.FS(), "pages")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		id := entry.Name()
		base := pageDir(id)
		if strings.HasPrefix(id, ".new-") || strings.HasPrefix(id, ".deleted-") {
			s.cleanup(base)
			continue
		}
		if !entry.IsDir() || !pageID.MatchString(id) {
			continue
		}
		p, err := s.load(id)
		if err != nil {
			return err
		}
		entries, err := fs.ReadDir(s.root.FS(), base)
		if err != nil {
			return err
		}
		for _, item := range entries {
			name := item.Name()
			if (name == "draft" || strings.HasPrefix(name, "draft-")) && name != p.Draft {
				s.cleanup(base + "/" + name)
			}
		}
		releases, err := fs.ReadDir(s.root.FS(), base+"/versions")
		if err != nil {
			return err
		}
		for _, release := range releases {
			if _, err := s.directory(p, release.Name()); err != nil {
				s.cleanup(base + "/versions/" + release.Name())
			}
		}
	}
	return fs.WalkDir(s.root.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), ".tmp-") {
			return s.root.Remove(name)
		}
		return nil
	})
}

func (s *Store) initialize(token string) error {
	for _, dir := range []string{"config", "pages"} {
		if err := s.root.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	err := s.readJSON("config/settings.json", &s.settings)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if errors.Is(err, fs.ErrNotExist) {
		if token == "" {
			token = "admin"
		} else if token != "admin" && len(token) < 24 {
			return errors.New("OPENPAGE_TOKEN must have at least 24 characters or use the default admin password")
		}
		s.settings.TokenHash = digest([]byte(token))
	}
	fresh := errors.Is(err, fs.ErrNotExist)
	write := fresh
	if s.settings.PreviewKey == "" {
		s.settings.PreviewKey = randomID() + randomID()
		write = true
	}
	if fresh {
		if err := s.seedDefaultPage(); err != nil {
			return err
		}
	}
	if write {
		return s.writeJSON("config/settings.json", s.settings)
	}
	return nil
}

func (s *Store) readJSON(name string, value any) error {
	if _, err := s.fileInfo(path.Dir(name), path.Base(name)); err != nil {
		if errors.Is(err, errMissing) {
			return fs.ErrNotExist
		}
		return err
	}
	data, err := s.root.ReadFile(name)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}

func (s *Store) writeJSON(name string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return s.replace(name, data)
}

func (s *Store) replace(name string, data []byte) error {
	tmp := path.Join(path.Dir(name), ".tmp-"+randomID())
	f, err := s.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer s.root.Remove(tmp)
	_, writeErr := f.Write(data)
	err = errors.Join(writeErr, f.Sync(), f.Close())
	if err != nil {
		return err
	}
	return s.root.Rename(tmp, name)
}

func validFile(name string) bool {
	if name == "" || len(name) > 512 {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if !filePart.MatchString(part) || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return false
		}
	}
	return true
}

func pageDir(id string) string { return "pages/" + id }

func (s *Store) load(id string) (Page, error) {
	var record pageDisk
	if !pageID.MatchString(id) {
		return Page{}, errInvalid
	}
	err := s.readJSON(pageDir(id)+"/manifest.json", &record)
	if errors.Is(err, fs.ErrNotExist) {
		return Page{}, errMissing
	}
	record.Page.Draft = record.DraftDir
	record.Page.Versions = record.History
	record.Page.Instance = record.InstanceID
	return record.Page, err
}

func (s *Store) save(p Page) error {
	return s.writeJSON(pageDir(p.ID)+"/manifest.json", pageDisk{p, p.Draft, p.Versions, p.Instance})
}

func (s *Store) list() ([]Page, error) {
	f, err := s.root.Open("pages")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	pages := []Page{}
	for _, entry := range entries {
		if !entry.IsDir() || !pageID.MatchString(entry.Name()) {
			continue
		}
		p, err := s.load(entry.Name())
		if errors.Is(err, errMissing) {
			continue
		}
		if err != nil {
			return nil, err
		}
		pages = append(pages, p)
	}
	slices.SortFunc(pages, func(a, b Page) int { return strings.Compare(a.ID, b.ID) })
	return pages, nil
}

func (s *Store) create(id, title string) (Page, error) {
	p := Page{ID: id, Title: strings.TrimSpace(title), Draft: "draft", Versions: []Version{}, Instance: randomID()}
	if !pageID.MatchString(id) || p.Title == "" || len(p.Title) > 240 {
		return Page{}, errInvalid
	}
	staging := "pages/.new-" + randomID()
	if err := s.root.MkdirAll(staging+"/draft", 0700); err != nil {
		return Page{}, err
	}
	defer s.root.RemoveAll(staging)
	if err := s.root.Mkdir(staging+"/versions", 0700); err != nil {
		return Page{}, err
	}
	if err := s.writeJSON(staging+"/manifest.json", pageDisk{p, p.Draft, p.Versions, p.Instance}); err != nil {
		return Page{}, err
	}
	_, err := s.root.Lstat(pageDir(id))
	if err == nil {
		return Page{}, errConflict
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return Page{}, err
	}
	return p, s.root.Rename(staging, pageDir(id))
}

func (s *Store) notify() {
	close(s.changed)
	s.changed = make(chan struct{})
}

func (s *Store) directory(p Page, version string) (string, error) {
	if version == "" {
		return pageDir(p.ID) + "/" + p.Draft, nil
	}
	if slices.ContainsFunc(p.Versions, func(v Version) bool { return v.ID == version }) {
		return pageDir(p.ID) + "/versions/" + version, nil
	}
	return "", errMissing
}

func (s *Store) tree(dir string) ([]string, int64, error) {
	parts := strings.Split(dir, "/")
	for i := range parts {
		info, err := s.root.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil {
			return nil, 0, err
		}
		if !info.IsDir() {
			return nil, 0, errInvalid
		}
	}
	files := []string{}
	var size int64
	if err := fs.WalkDir(s.root.FS(), dir, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name == dir && !entry.IsDir() {
			return errInvalid
		}
		if name != dir && !validFile(strings.TrimPrefix(name, dir+"/")) {
			return errInvalid
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errInvalid
		}
		size += info.Size()
		files = append(files, strings.TrimPrefix(name, dir+"/"))
		return nil
	}); err != nil {
		return nil, 0, err
	}
	return files, size, nil
}

// The store root confines filesystem lookups, and each path component must
// also belong to the expected directory tree without traversing symlinks.
func (s *Store) fileInfo(dir, name string) (fs.FileInfo, error) {
	if !validFile(name) {
		return nil, errInvalid
	}
	parts := strings.Split(dir+"/"+name, "/")
	for i := range parts {
		info, err := s.root.Lstat(strings.Join(parts[:i+1], "/"))
		if errors.Is(err, fs.ErrNotExist) {
			return nil, errMissing
		}
		if err != nil {
			return nil, err
		}
		if i == len(parts)-1 {
			if info.Mode().IsRegular() {
				return info, nil
			}
			return nil, errInvalid
		}
		if !info.IsDir() {
			return nil, errInvalid
		}
	}
	return nil, errMissing
}

func (s *Store) readFile(dir, name string) ([]byte, error) {
	info, err := s.fileInfo(dir, name)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxFile {
		return nil, errQuota
	}
	f, err := s.root.Open(dir + "/" + name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxFile+1))
	if len(data) > maxFile {
		return nil, errQuota
	}
	return data, err
}

func (s *Store) editFile(p Page, name, expected string, data []byte, remove bool) (string, error) {
	dir, _ := s.directory(p, "")
	previous, err := s.readFile(dir, name)
	switch {
	case err == nil && expected != digest(previous):
		return "", errConflict
	case errors.Is(err, errMissing) && expected != "new":
		return "", errConflict
	case err != nil && (!errors.Is(err, errMissing) || remove):
		return "", err
	}
	fullName := dir + "/" + name
	if remove {
		if err := s.root.Remove(fullName); err != nil {
			return "", err
		}
		for parent := path.Dir(fullName); parent != dir; parent = path.Dir(parent) {
			if s.root.Remove(parent) != nil {
				break
			}
		}
		return "", nil
	}
	files, size, err := s.tree(dir)
	if err != nil {
		return "", err
	}
	if len(data) > maxFile || size-int64(len(previous))+int64(len(data)) > maxDraft ||
		(expected == "new" && len(files) >= maxFiles) {
		return "", errQuota
	}
	if err := s.root.MkdirAll(path.Dir(fullName), 0700); err != nil {
		return "", err
	}
	if err := s.replace(fullName, data); err != nil {
		return "", err
	}
	return digest(data), nil
}

func (s *Store) copyTree(src, target string) error {
	files, size, err := s.tree(src)
	if err != nil {
		return err
	}
	if size > maxDraft || len(files) > maxFiles {
		return errQuota
	}
	if err := s.root.Mkdir(target, 0700); err != nil {
		return err
	}
	for _, name := range files {
		data, err := s.readFile(src, name)
		if err != nil {
			return err
		}
		fullName := target + "/" + name
		if err := s.root.MkdirAll(path.Dir(fullName), 0700); err != nil {
			return err
		}
		if err := s.replace(fullName, data); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) draftRevision(p Page) (string, error) {
	dir, _ := s.directory(p, "")
	files, _, err := s.tree(dir)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	io.WriteString(hash, p.Draft+"\n")
	for _, name := range files {
		data, err := s.readFile(dir, name)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(hash, "%s\n%s\n", name, digest(data))
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (s *Store) changeVersion(p Page, action, version, description, expected string) (any, error) {
	dir, err := s.directory(p, version)
	if err != nil {
		return nil, err
	}
	if action == "publish" || action == "reset" {
		revision, err := s.draftRevision(p)
		if err != nil {
			return nil, err
		}
		if expected != revision {
			return nil, errConflict
		}
	}
	if action == "reset" {
		old := p.Draft
		p.Draft = "draft-" + randomID()
		target := pageDir(p.ID) + "/" + p.Draft
		if err := s.copyTree(dir, target); err != nil {
			s.cleanup(target)
			return nil, err
		}
		if err := s.save(p); err != nil {
			s.cleanup(target)
			return nil, err
		}
		s.cleanup(pageDir(p.ID) + "/" + old)
		return p, nil
	}
	if _, err := s.readFile(dir, "index.html"); err != nil {
		return nil, fmt.Errorf("%w: index.html is required: %v", errInvalid, err)
	}
	_, size, err := s.tree(dir)
	if err != nil {
		return nil, err
	}
	v := Version{ID: randomID(), Created: time.Now().UTC(), Description: description, RestoredFrom: version, Bytes: size}
	p.Current = v.ID
	p.Versions = append([]Version{v}, p.Versions...)
	kept := p.Versions[:0]
	dropped := []Version{}
	var total int64
	for i, item := range p.Versions {
		if i < retention || item.Protected {
			kept = append(kept, item)
			total += item.Bytes
		} else {
			dropped = append(dropped, item)
		}
	}
	if total > maxReleases || len(kept) > 100 {
		return nil, errQuota
	}
	target := pageDir(p.ID) + "/versions/" + v.ID
	if err := s.copyTree(dir, target); err != nil {
		s.cleanup(target)
		return nil, err
	}
	p.Versions = kept
	if err := s.save(p); err != nil {
		s.cleanup(target)
		return nil, err
	}
	for _, item := range dropped {
		s.cleanup(pageDir(p.ID) + "/versions/" + item.ID)
	}
	s.notify()
	return v, nil
}

func (s *Store) cleanup(name string) {
	if err := s.root.RemoveAll(name); err != nil {
		log.Printf("cleanup %s: %v", name, err)
	}
}
