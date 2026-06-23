package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseAliasesAndContactName(t *testing.T) {
	cfg := bridgeConfig{
		ContactAliases: parseAliases("filehelper=文件传输助手,wxid_a=张三,bad"),
	}

	for wxid, want := range map[string]string{
		"filehelper": "文件传输助手",
		"wxid_a":     "张三",
		"wxid_b":     "wxid_b",
	} {
		if got := contactName(cfg, wxid); got != want {
			t.Fatalf("contactName(%q)=%q, want %q", wxid, got, want)
		}
	}
}

func TestEnsureLoopbackAddr(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:3021", "localhost:3021", "[::1]:3021"} {
		if err := ensureLoopbackAddr(addr); err != nil {
			t.Fatalf("expected %s to be accepted: %v", addr, err)
		}
	}
	if err := ensureLoopbackAddr("0.0.0.0:3021"); err == nil {
		t.Fatal("expected non-loopback address to be rejected")
	}
}

func TestSplitComma(t *testing.T) {
	got := splitComma("a, b,,c ")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("len=%d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%q, want %q", i, got[i], want[i])
		}
	}
}

func TestPauseWindows(t *testing.T) {
	loc := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 6, 24, 10, 30, 0, 0, loc)
	state := pauseStateFromWindows("test", "09:00-11:00,14:00-15:00", now)
	if !state.Paused {
		t.Fatal("expected current time to be paused")
	}
	if state.Until != "2026-06-24T11:00:00+08:00" {
		t.Fatalf("until=%q", state.Until)
	}

	now = time.Date(2026, 6, 24, 12, 0, 0, 0, loc)
	if state := pauseStateFromWindows("test", "09:00-11:00", now); state.Paused {
		t.Fatalf("did not expect pause: %#v", state)
	}
}

func TestPauseWindowAcrossMidnight(t *testing.T) {
	loc := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 6, 24, 23, 30, 0, 0, loc)
	state := pauseStateFromWindows("test", "22:00-01:00", now)
	if !state.Paused {
		t.Fatal("expected overnight pause")
	}
	if state.Until != "2026-06-25T01:00:00+08:00" {
		t.Fatalf("until=%q", state.Until)
	}

	now = time.Date(2026, 6, 24, 0, 30, 0, 0, loc)
	if state := pauseStateFromWindows("test", "22:00-01:00", now); !state.Paused {
		t.Fatal("expected after-midnight pause")
	}
}

func TestPauseFileUntil(t *testing.T) {
	loc := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 6, 24, 10, 30, 0, 0, loc)
	path := filepath.Join(t.TempDir(), "pause.json")
	if err := os.WriteFile(path, []byte("\ufeff"+`{"pause_until":"2026-06-24T11:00:00+08:00","reason":"handoff"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	state := pauseStateFromFile(path, now)
	if !state.Paused || state.Reason != "handoff" {
		t.Fatalf("state=%#v", state)
	}

	now = time.Date(2026, 6, 24, 11, 1, 0, 0, loc)
	if state := pauseStateFromFile(path, now); state.Paused {
		t.Fatalf("pause should expire: %#v", state)
	}
}

func TestBuildPauseFileConfigMinutes(t *testing.T) {
	loc := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 6, 24, 10, 30, 0, 0, loc)
	cfg, err := buildPauseFileConfig(operatorPauseRequest{
		Minutes: 30,
		Reason:  "manual test",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PauseUntil != "2026-06-24T11:00:00+08:00" {
		t.Fatalf("pause_until=%q", cfg.PauseUntil)
	}
	if cfg.Reason != "manual test" {
		t.Fatalf("reason=%q", cfg.Reason)
	}
}

func TestWritePauseFileRoundTrip(t *testing.T) {
	loc := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 6, 24, 10, 30, 0, 0, loc)
	path := filepath.Join(t.TempDir(), "nested", "pause.json")
	if err := writePauseFile(path, pauseFileConfig{
		PauseUntil: "2026-06-24T11:00:00+08:00",
		Reason:     "manual takeover",
	}); err != nil {
		t.Fatal(err)
	}
	state := pauseStateFromFile(path, now)
	if !state.Paused || state.Reason != "manual takeover" || state.Until != "2026-06-24T11:00:00+08:00" {
		t.Fatalf("state=%#v", state)
	}
}
