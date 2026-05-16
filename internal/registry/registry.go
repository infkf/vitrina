package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"vitrina/internal/config"
)

type App struct {
	Subdomain string `json:"subdomain"`
	FQDN      string `json:"fqdn"`
	Port      int    `json:"port"`
	CreatedAt string `json:"created_at"`
	Scaffold  string `json:"scaffold,omitempty"`
}

type Store struct {
	mu   sync.Mutex
	path string
}

type data struct {
	Apps map[string]*App `json:"apps"`
}

func New() *Store {
	return &Store{
		path: filepath.Join(config.DefaultConfigDir, config.DefaultAppsFile),
	}
}

func (s *Store) load() (*data, error) {
	d := &data{Apps: make(map[string]*App)}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return d, nil
		}
		return nil, fmt.Errorf("failed to read apps registry at %s: %w", s.path, err)
	}
	if len(raw) == 0 {
		return d, nil
	}
	if err := json.Unmarshal(raw, d); err != nil {
		return nil, fmt.Errorf("failed to parse apps registry at %s: %w", s.path, err)
	}
	if d.Apps == nil {
		d.Apps = make(map[string]*App)
	}
	return d, nil
}

func (s *Store) save(d *data) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", filepath.Dir(s.path), err)
	}
	raw, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal registry: %w", err)
	}
	if err := os.WriteFile(s.path, raw, 0644); err != nil {
		return fmt.Errorf("failed to write registry to %s: %w", s.path, err)
	}
	return nil
}

func (s *Store) Add(app *App) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	d, err := s.load()
	if err != nil {
		return err
	}

	if _, exists := d.Apps[app.Subdomain]; exists {
		return fmt.Errorf("subdomain %q is already registered in vitrina", app.Subdomain)
	}

	for _, a := range d.Apps {
		if a.Port == app.Port {
			return fmt.Errorf("port %d is already assigned to subdomain %q", app.Port, a.Subdomain)
		}
	}

	if app.CreatedAt == "" {
		app.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	d.Apps[app.Subdomain] = app
	return s.save(d)
}

func (s *Store) Remove(subdomain string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	d, err := s.load()
	if err != nil {
		return err
	}

	if _, exists := d.Apps[subdomain]; !exists {
		return fmt.Errorf("subdomain %q is not registered in vitrina", subdomain)
	}

	delete(d.Apps, subdomain)
	return s.save(d)
}

func (s *Store) Get(subdomain string) (*App, error) {
	d, err := s.load()
	if err != nil {
		return nil, err
	}
	app, exists := d.Apps[subdomain]
	if !exists {
		return nil, fmt.Errorf("subdomain %q not found in registry", subdomain)
	}
	return app, nil
}

func (s *Store) List() ([]*App, error) {
	d, err := s.load()
	if err != nil {
		return nil, err
	}
	apps := make([]*App, 0, len(d.Apps))
	for _, app := range d.Apps {
		apps = append(apps, app)
	}
	sort.Slice(apps, func(i, j int) bool {
		return apps[i].Subdomain < apps[j].Subdomain
	})
	return apps, nil
}
