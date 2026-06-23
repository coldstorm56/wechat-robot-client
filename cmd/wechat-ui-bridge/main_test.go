package main

import "testing"

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
