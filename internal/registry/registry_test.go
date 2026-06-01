package registry_test

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"vitrina/internal/registry"
)

func newStore(t *testing.T) *registry.Store {
	t.Helper()
	return registry.NewWithPath(filepath.Join(t.TempDir(), "apps.json"))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	return string(raw)
}

func TestAdd(t *testing.T) {
	s := newStore(t)

	app := &registry.App{
		Subdomain: "myapp",
		FQDN:      "myapp.example.com",
		Port:      3000,
	}
	if err := s.Add(app); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if app.CreatedAt == "" {
		t.Error("CreatedAt should be set by Add")
	}

	// Verify duplicate subdomain is rejected
	if err := s.Add(app); err == nil {
		t.Error("expected error for duplicate subdomain")
	} else if !strings.Contains(err.Error(), "already registered") {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify duplicate port is rejected
	app2 := &registry.App{
		Subdomain: "other",
		FQDN:      "other.example.com",
		Port:      3000,
	}
	if err := s.Add(app2); err == nil {
		t.Error("expected error for duplicate port")
	} else if !strings.Contains(err.Error(), "already assigned") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAddPreservesCreatedAt(t *testing.T) {
	s := newStore(t)
	app := &registry.App{
		Subdomain: "myapp",
		FQDN:      "myapp.example.com",
		Port:      3000,
		CreatedAt: "2024-01-01T00:00:00Z",
	}
	if err := s.Add(app); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if app.CreatedAt != "2024-01-01T00:00:00Z" {
		t.Errorf("CreatedAt was overwritten: %s", app.CreatedAt)
	}
}

func TestUpdate(t *testing.T) {
	s := newStore(t)

	_ = s.Add(&registry.App{Subdomain: "app1", FQDN: "app1.example.com", Port: 3000})
	_ = s.Add(&registry.App{Subdomain: "app2", FQDN: "app2.example.com", Port: 3001})

	app := &registry.App{Subdomain: "app1", FQDN: "app1.example.com", Port: 3000}
	if err := s.Update(app); err != nil {
		t.Fatalf("Update should succeed for same port: %v", err)
	}

	app2 := &registry.App{Subdomain: "app1", FQDN: "app1.example.com", Port: 4000}
	if err := s.Update(app2); err != nil {
		t.Fatalf("Update should succeed for non-conflicting port: %v", err)
	}

	app3 := &registry.App{Subdomain: "app1", FQDN: "app1.example.com", Port: 3001}
	if err := s.Update(app3); err == nil {
		t.Error("Update should reject port that collides with another app")
	}

	if err := s.Update(&registry.App{Subdomain: "nonexistent", Port: 5000}); err == nil {
		t.Error("Update should reject nonexistent subdomain")
	}

	got, _ := s.Get("app1")
	if got.Port != 4000 {
		t.Errorf("app1 port should be 4000 after successful update, got %d", got.Port)
	}
}

func TestRemove(t *testing.T) {
	s := newStore(t)

	app := &registry.App{Subdomain: "myapp", FQDN: "myapp.example.com", Port: 3000}
	if err := s.Add(app); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	if err := s.Remove("myapp"); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	if _, err := s.Get("myapp"); err == nil {
		t.Error("expected not-found error after Remove")
	}

	if err := s.Remove("nonexistent"); err == nil {
		t.Error("expected error for nonexistent subdomain")
	}
}

func TestGet(t *testing.T) {
	s := newStore(t)

	app := &registry.App{Subdomain: "myapp", FQDN: "myapp.example.com", Port: 3000}
	if err := s.Add(app); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	got, err := s.Get("myapp")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Subdomain != "myapp" || got.Port != 3000 {
		t.Errorf("unexpected app: %+v", got)
	}

	_, err = s.Get("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent subdomain")
	}
}

func TestList(t *testing.T) {
	s := newStore(t)

	apps := []*registry.App{
		{Subdomain: "zulu", FQDN: "zulu.example.com", Port: 3002},
		{Subdomain: "alpha", FQDN: "alpha.example.com", Port: 3000},
		{Subdomain: "mike", FQDN: "mike.example.com", Port: 3001},
	}
	for _, a := range apps {
		if err := s.Add(a); err != nil {
			t.Fatalf("Add %s failed: %v", a.Subdomain, err)
		}
	}

	list, err := s.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 apps, got %d", len(list))
	}

	// List should be sorted by subdomain
	if list[0].Subdomain != "alpha" || list[1].Subdomain != "mike" || list[2].Subdomain != "zulu" {
		t.Errorf("List not sorted: %v", list)
	}
}

func TestNextFreePort(t *testing.T) {
	s := newStore(t)

	// Empty registry: first port >= 3000 is 3000
	port, err := s.NextFreePort(3000)
	if err != nil {
		t.Fatalf("NextFreePort failed: %v", err)
	}
	if port != 3000 {
		t.Errorf("expected 3000, got %d", port)
	}

	// Add 3000, next should be 3001
	app := &registry.App{Subdomain: "app1", FQDN: "app1.example.com", Port: 3000}
	if err := s.Add(app); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	port, err = s.NextFreePort(3000)
	if err != nil {
		t.Fatalf("NextFreePort failed: %v", err)
	}
	if port != 3001 {
		t.Errorf("expected 3001, got %d", port)
	}

	// Tests skip 80 and 443 explicitly
	// Set up situation where 80/443 are in range
	s2 := newStore(t)
	port, err = s2.NextFreePort(80)
	if err != nil {
		t.Fatalf("NextFreePort failed: %v", err)
	}
	if port == 80 || port == 443 {
		t.Errorf("should not return system port %d", port)
	}
}

func TestNextFreePortStartAt(t *testing.T) {
	s := newStore(t)

	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	freePort := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	port, err := s.NextFreePort(freePort)
	if err != nil {
		t.Fatalf("NextFreePort failed: %v", err)
	}
	if port != freePort {
		t.Errorf("expected %d, got %d", freePort, port)
	}
}

func TestPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apps.json")
	s := registry.NewWithPath(path)

	app := &registry.App{Subdomain: "myapp", FQDN: "myapp.example.com", Port: 3000}
	if err := s.Add(app); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Verify JSON is well-formed
	data := readFile(t, path)
	if !strings.Contains(data, "\"subdomain\"") {
		t.Error("JSON output missing subdomain field")
	}
	if !strings.Contains(data, "\"port\"") {
		t.Error("JSON output missing port field")
	}

	// New store should read the same data
	s2 := registry.NewWithPath(path)
	got, err := s2.Get("myapp")
	if err != nil {
		t.Fatalf("Get from new store failed: %v", err)
	}
	if got.Port != 3000 {
		t.Errorf("expected port 3000, got %d", got.Port)
	}
}

func TestEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apps.json")
	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	s := registry.NewWithPath(path)
	list, err := s.List()
	if err != nil {
		t.Fatalf("List on empty file failed: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d apps", len(list))
	}

	port, err := s.NextFreePort(3000)
	if err != nil {
		t.Fatalf("NextFreePort failed: %v", err)
	}
	if port != 3000 {
		t.Errorf("expected 3000, got %d", port)
	}
}

func TestCorruptJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apps.json")
	if err := os.WriteFile(path, []byte("{invalid"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	s := registry.NewWithPath(path)
	if _, err := s.List(); err == nil {
		t.Error("expected error for corrupt JSON")
	}
	if _, err := s.Get("foo"); err == nil {
		t.Error("expected error for corrupt JSON")
	}
}

func TestJSONRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apps.json")
	s := registry.NewWithPath(path)

	app := &registry.App{
		Subdomain: "hello",
		FQDN:      "hello.example.com",
		Port:      8080,
		Scaffold:  "docker",
	}
	if err := s.Add(app); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	raw := readFile(t, path)
	var data struct {
		Apps map[string]json.RawMessage `json:"apps"`
	}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if _, ok := data.Apps["hello"]; !ok {
		t.Fatal("missing 'hello' key in JSON")
	}
}

func TestConcurrentAddDifferentSubdomains(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apps.json")
	s := registry.NewWithPath(path)

	var wg sync.WaitGroup
	errs := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sub := "app" + string(rune('a'+idx))
			errs <- s.Add(&registry.App{
				Subdomain: sub,
				FQDN:      sub + ".example.com",
				Port:      3000 + idx,
			})
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Errorf("concurrent Add failed: %v", err)
		}
	}

	list, err := s.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 10 {
		t.Errorf("expected 10 apps, got %d", len(list))
	}
}
