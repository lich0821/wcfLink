package ilink

import "testing"

func TestNormalizeMessageItems(t *testing.T) {
	msg := WeixinMessage{
		ItemList: []MessageItem{
			{Type: 1, TextItem: &TextItem{Text: "hello"}},
			{Type: 3, VoiceItem: &VoiceItem{Text: "voice text", EncodeType: 7}},
			{Type: 4, FileItem: &FileItem{FileName: "report.pdf", Len: "12345"}},
			{Type: 2, ImageItem: &ImageItem{Media: CDNMedia{EncryptQueryParam: "enc=image", AESKey: "aes-image"}}},
			{Type: 5, VideoItem: &VideoItem{Media: CDNMedia{EncryptQueryParam: "enc=video", AESKey: "aes-video"}}},
		},
	}

	items := NormalizeMessageItems(msg)
	if len(items) != 5 {
		t.Fatalf("expected 5 normalized items, got %d", len(items))
	}
	if items[0].Kind != "text" || items[0].Text != "hello" {
		t.Fatalf("unexpected text item: %#v", items[0])
	}
	if items[1].Kind != "voice" || items[1].EncodeType != 7 || items[1].Text != "voice text" {
		t.Fatalf("unexpected voice item: %#v", items[1])
	}
	if items[2].Kind != "file" || items[2].FileName != "report.pdf" || items[2].FileLen != "12345" {
		t.Fatalf("unexpected file item: %#v", items[2])
	}
	if items[3].Kind != "image" || items[3].MediaEncryptQueryParam != "enc=image" || items[3].MediaAESKey != "aes-image" {
		t.Fatalf("unexpected image item: %#v", items[3])
	}
	if items[4].Kind != "video" || items[4].MediaEncryptQueryParam != "enc=video" || items[4].MediaAESKey != "aes-video" {
		t.Fatalf("unexpected video item: %#v", items[4])
	}
}

func TestParseNormalizedMessageItemsFromRaw(t *testing.T) {
	raw := `{"message_id":1,"item_list":[{"type":4,"file_item":{"file_name":"invoice.zip","len":"888"}}]}`
	items := ParseNormalizedMessageItemsFromRaw(raw)
	if len(items) != 1 {
		t.Fatalf("expected 1 parsed item, got %d", len(items))
	}
	if items[0].Kind != "file" || items[0].FileName != "invoice.zip" {
		t.Fatalf("unexpected parsed item: %#v", items[0])
	}
}

func TestSummariesAndEventType(t *testing.T) {
	msg := WeixinMessage{
		ItemList: []MessageItem{
			{Type: 3, VoiceItem: &VoiceItem{Text: "语音转写"}},
			{Type: 4, FileItem: &FileItem{FileName: "evidence.txt"}},
		},
	}

	if got := DetectEventType(msg); got != "voice" {
		t.Fatalf("expected event type voice, got %q", got)
	}
	if got := ExtractBodyText(msg); got != "[voice] 语音转写 | [file] evidence.txt" {
		t.Fatalf("unexpected body text: %q", got)
	}
}
