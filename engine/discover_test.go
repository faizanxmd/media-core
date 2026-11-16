package main

import (
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

func TestServeDiscoverLoopback(t *testing.T) {
	t.Setenv("MEDIA_CORE_NAME", "TestBox")
	t.Setenv("MEDIA_CORE_PUBLIC_HOST", "box.example.com")

	pc := serveDiscover("127.0.0.1:0")
	if pc == nil {
		t.Fatal("serveDiscover returned nil")
	}
	defer pc.Close()

	// Binding the same port again logs once and returns nil instead of failing fatally.
	if dup := serveDiscover(pc.LocalAddr().String()); dup != nil {
		dup.Close()
		t.Fatal("expected nil when port already bound")
	}

	c, err := net.Dial("udp4", pc.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	// Non-matching payload must be ignored.
	if _, err := c.Write([]byte("wrong-probe")); err != nil {
		t.Fatal(err)
	}
	c.SetReadDeadline(time.Now().Add(60 * time.Millisecond))
	buf := make([]byte, 2048)
	if n, err := c.Read(buf); err == nil {
		t.Fatalf("expected no reply to wrong payload, got %q", buf[:n])
	}

	// Exact "media-core?" probe receives helloInfo JSON under 1400 bytes.
	if _, err := c.Write([]byte("media-core?")); err != nil {
		t.Fatal(err)
	}
	c.SetReadDeadline(time.Now().Add(time.Second))
	n, err := c.Read(buf)
	if err != nil {
		t.Fatalf("read reply: %v", err)
	}
	if n >= 1400 {
		t.Fatalf("reply size = %d, want < 1400", n)
	}
	var got map[string]any
	if err := json.Unmarshal(buf[:n], &got); err != nil {
		t.Fatalf("invalid json reply %q: %v", buf[:n], err)
	}
	if got["name"] != "TestBox" {
		t.Fatalf("reply name = %v, want TestBox", got["name"])
	}
	if got["magicdns"] == nil {
		t.Fatalf("expected magicdns in reply: %v", got)
	}
}

func TestDiscoverReplyDropsAppWhenOversized(t *testing.T) {
	info := map[string]any{
		"name": "Box",
		"lan":  []string{"http://192.168.1.10:8790"},
		"app":  strings.Repeat("x", 1500),
	}
	b := discoverReply(info)
	if len(b) == 0 || len(b) >= 1400 {
		t.Fatalf("discoverReply len = %d, want (0, 1400)", len(b))
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if _, has := got["app"]; has {
		t.Fatal("expected app to be dropped when reply exceeded 1400 bytes")
	}
	if got["name"] != "Box" {
		t.Fatalf("name = %v, want Box", got["name"])
	}
}

func TestIsLocalUDP(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", true},
		{"192.168.1.50", true},
		{"10.0.0.5", true},
		{"169.254.1.10", true},
		{"100.64.0.10", false},
		{"8.8.8.8", false},
		{"::1", false},
	}
	for _, tc := range cases {
		addr := &net.UDPAddr{IP: net.ParseIP(tc.ip), Port: 12345}
		if got := isLocalUDP(addr); got != tc.want {
			t.Errorf("isLocalUDP(%s) = %v, want %v", tc.ip, got, tc.want)
		}
	}
}
