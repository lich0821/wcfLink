package ilink

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestParseMediaAESKey(t *testing.T) {
	raw := "0123456789abcdeffedcba9876543210"
	base64Hex := base64.StdEncoding.EncodeToString([]byte(raw))
	key, err := parseMediaAESKey(base64Hex)
	if err != nil {
		t.Fatalf("parse base64 hex key failed: %v", err)
	}
	if got := len(key); got != 16 {
		t.Fatalf("expected 16-byte key, got %d", got)
	}

	rawKey := []byte("1234567890abcdef")
	base64Raw := base64.StdEncoding.EncodeToString(rawKey)
	key, err = parseMediaAESKey(base64Raw)
	if err != nil {
		t.Fatalf("parse base64 raw key failed: %v", err)
	}
	if string(key) != string(rawKey) {
		t.Fatalf("unexpected raw key parse result")
	}
}

func TestAESECBRoundTrip(t *testing.T) {
	key := []byte("1234567890abcdef")
	plain := []byte("hello media payload")
	ciphertext, err := encryptAESECB(plain, key)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}
	if len(ciphertext)%16 != 0 {
		t.Fatalf("ciphertext size must align to block size")
	}
	decoded, err := decryptAESECB(ciphertext, key)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}
	if string(decoded) != string(plain) {
		t.Fatalf("round trip mismatch: %q != %q", decoded, plain)
	}
}

func TestDetectOutboundMediaType(t *testing.T) {
	cases := map[string]string{
		"demo.jpg":  "image",
		"demo.png":  "image",
		"demo.mp4":  "video",
		"demo.pdf":  "file",
		"demo.silk": "file",
	}
	for name, want := range cases {
		if got, _ := detectOutboundMediaType(name); got != want {
			t.Fatalf("detectOutboundMediaType(%q)=%q want %q", name, got, want)
		}
	}
}

func TestMediaBodyText(t *testing.T) {
	if got := MediaBodyText(UploadedMedia{ItemType: "image", FileName: "x.png"}); got != "[image] x.png" {
		t.Fatalf("unexpected image body text: %q", got)
	}
	if got := MediaBodyText(UploadedMedia{ItemType: "video", FileName: "x.mp4"}); got != "[video] x.mp4" {
		t.Fatalf("unexpected video body text: %q", got)
	}
	if got := MediaBodyText(UploadedMedia{ItemType: "file", FileName: "x.pdf"}); got != "[file] x.pdf" {
		t.Fatalf("unexpected file body text: %q", got)
	}
}

func TestUploadMediaRejectsEmptyFile(t *testing.T) {
	client := NewClient("2.0.1", "https://novac2c.cdn.weixin.qq.com/c2c", 0)
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.bin")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
	if _, err := client.UploadMedia(t.Context(), "https://ilinkai.weixin.qq.com", "token", "to", path); err == nil {
		t.Fatalf("expected empty file upload to fail")
	}
}
