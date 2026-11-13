package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	d := Defaults()
	if d.Preset != PresetBalanceado || d.Thresholds.MaxLines != 400 || d.Thresholds.MaxFiles != 15 ||
		d.Thresholds.MaxMinorDetails != 5 || d.Concurrency != 4 || d.AgentTimeout != 10*time.Minute ||
		d.CloneURL != "git@bitbucket.org:{workspace}/{repo}.git" ||
		d.TaskIDPattern != `^(CU-[0-9a-z]+|[A-Z][A-Z0-9]{1,9}-\d+)` {
		t.Fatalf("unexpected defaults: %+v", d)
	}
	if !reflect.DeepEqual(d.Models, PresetAssignments(PresetBalanceado)) {
		t.Fatal("default models should equal balanceado")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.yaml")
	cfg := Defaults()
	cfg.Workspace = "acme"
	cfg.Preset = PresetPersonalizado
	cfg.AgentTimeout = 90 * time.Second
	cfg.Models[RoleBugs] = Assignment{ModelSonnet, EffortLow}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm %v", info.Mode().Perm())
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "1m30s") {
		t.Fatalf("timeout not a duration string:\n%s", raw)
	}
	got, found, err := Load(path)
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if !reflect.DeepEqual(got, cfg) {
		t.Fatalf("got %+v\nwant %+v", got, cfg)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("leftover temp files: %v", entries)
	}
}

func TestLoadMissingReturnsDefaults(t *testing.T) {
	got, found, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil || found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if !reflect.DeepEqual(got, Defaults()) {
		t.Fatal("want defaults")
	}
}

func TestLoadPartialKeepsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(path, []byte("workspace: acme\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, found, err := Load(path)
	if err != nil || !found {
		t.Fatal(found, err)
	}
	if got.Workspace != "acme" || got.AgentTimeout != 10*time.Minute || got.Concurrency != 4 {
		t.Fatalf("%+v", got)
	}
}

func TestLoadInvalidDuration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(path, []byte("timeout_agente: banana\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path); err == nil {
		t.Fatal("want error")
	}
}

func TestAssignmentForFallsBack(t *testing.T) {
	cfg := Defaults()
	cfg.Models = map[Role]Assignment{
		RoleBugs:  {ModelHaiku, EffortHigh}, // invalid
		RoleTests: {ModelOpus, EffortMax},   // valid override
	}
	preset := PresetAssignments(cfg.Preset)
	if got := cfg.AssignmentFor(RoleBugs); got != preset[RoleBugs] {
		t.Errorf("invalid: got %+v", got)
	}
	if got := cfg.AssignmentFor(RoleJuezA); got != preset[RoleJuezA] {
		t.Errorf("missing: got %+v", got)
	}
	if got := cfg.AssignmentFor(RoleTests); got != (Assignment{ModelOpus, EffortMax}) {
		t.Errorf("override: got %+v", got)
	}
}

func TestLoadRejectsInvalidTaskIDPattern(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte("patron_id_tarea: \"([unclosed\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := Load(p)
	if err == nil || !strings.Contains(err.Error(), "patron_id_tarea") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadRejectsInvalidLimits(t *testing.T) {
	for doc, key := range map[string]string{
		"umbrales:\n  max_lineas: 0\n":            "max_lineas",
		"umbrales:\n  max_archivos: -1\n":         "max_archivos",
		"umbrales:\n  max_detalles_menores: -1\n": "max_detalles_menores",
		"concurrencia: 0\n":                       "concurrencia",
		"timeout_agente: 0s\n":                    "timeout_agente",
		"timeout_agente: -5m\n":                   "timeout_agente",
	} {
		p := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(p, []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Load(p); err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("%q: err = %v, want one naming %s", doc, err, key)
		}
	}
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte("umbrales:\n  max_detalles_menores: 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(p)
	if err != nil || cfg.Thresholds.MaxMinorDetails != 0 || cfg.Thresholds.MaxLines != 400 {
		t.Errorf("zero minor details is valid and keeps the other limits: %+v, %v", cfg.Thresholds, err)
	}
}
