package ilink

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"wcfLink/internal/model"
)

func NormalizeMessageItems(msg WeixinMessage) []model.EventItem {
	items := make([]model.EventItem, 0, len(msg.ItemList))
	for _, item := range msg.ItemList {
		normalized, ok := normalizeMessageItem(item)
		if !ok {
			continue
		}
		items = append(items, normalized)
	}
	return items
}

func ParseNormalizedMessageItemsFromRaw(raw string) []model.EventItem {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var msg WeixinMessage
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		return nil
	}
	return NormalizeMessageItems(msg)
}

func DetectEventType(msg WeixinMessage) string {
	items := NormalizeMessageItems(msg)
	if len(items) == 0 {
		return "unknown"
	}
	return items[0].Kind
}

func ExtractBodyText(msg WeixinMessage) string {
	return SummarizeEventItems(NormalizeMessageItems(msg))
}

func SummarizeEventItems(items []model.EventItem) string {
	if len(items) == 0 {
		return ""
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		summary := summarizeEventItem(item)
		if summary == "" {
			continue
		}
		parts = append(parts, summary)
	}
	return strings.Join(parts, " | ")
}

func normalizeMessageItem(item MessageItem) (model.EventItem, bool) {
	switch item.Type {
	case 1:
		if item.TextItem == nil {
			return model.EventItem{}, false
		}
		return model.EventItem{
			Kind: "text",
			Text: item.TextItem.Text,
		}, true
	case 2:
		if item.ImageItem == nil {
			return model.EventItem{Kind: "image"}, true
		}
		aesKey := item.ImageItem.Media.AESKey
		if strings.TrimSpace(aesKey) == "" && strings.TrimSpace(item.ImageItem.AESKey) != "" {
			aesKey = base64.StdEncoding.EncodeToString([]byte(strings.TrimSpace(item.ImageItem.AESKey)))
		}
		return model.EventItem{
			Kind:                   "image",
			MediaEncryptQueryParam: item.ImageItem.Media.EncryptQueryParam,
			MediaAESKey:            aesKey,
		}, true
	case 3:
		if item.VoiceItem == nil {
			return model.EventItem{Kind: "voice"}, true
		}
		return model.EventItem{
			Kind:                   "voice",
			Text:                   item.VoiceItem.Text,
			EncodeType:             item.VoiceItem.EncodeType,
			MediaEncryptQueryParam: item.VoiceItem.Media.EncryptQueryParam,
			MediaAESKey:            item.VoiceItem.Media.AESKey,
		}, true
	case 4:
		if item.FileItem == nil {
			return model.EventItem{Kind: "file"}, true
		}
		return model.EventItem{
			Kind:                   "file",
			FileName:               item.FileItem.FileName,
			FileLen:                item.FileItem.Len,
			MediaEncryptQueryParam: item.FileItem.Media.EncryptQueryParam,
			MediaAESKey:            item.FileItem.Media.AESKey,
		}, true
	case 5:
		if item.VideoItem == nil {
			return model.EventItem{Kind: "video"}, true
		}
		return model.EventItem{
			Kind:                   "video",
			MediaEncryptQueryParam: item.VideoItem.Media.EncryptQueryParam,
			MediaAESKey:            item.VideoItem.Media.AESKey,
		}, true
	default:
		return model.EventItem{}, false
	}
}

func summarizeEventItem(item model.EventItem) string {
	switch item.Kind {
	case "text":
		return item.Text
	case "voice":
		if strings.TrimSpace(item.Text) != "" {
			return "[voice] " + item.Text
		}
		return "[voice]"
	case "image":
		return "[image]"
	case "file":
		if strings.TrimSpace(item.FileName) != "" {
			return "[file] " + item.FileName
		}
		return "[file]"
	case "video":
		return "[video]"
	default:
		return ""
	}
}
