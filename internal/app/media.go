package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"wcfLink/internal/ilink"
	"wcfLink/internal/model"
)

var invalidFileNameChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]+`)

func (s *service) downloadInboundMedia(ctx context.Context, account model.Account, event model.Event, msg ilink.WeixinMessage) []model.EventItem {
	items := ilink.NormalizeMessageItems(msg)
	if len(items) == 0 {
		return items
	}
	for idx := range items {
		if !canDownloadMedia(items[idx]) {
			continue
		}
		existingPath := s.resolveLocalMediaPath(account.AccountID, event.ID, idx)
		if existingPath != "" {
			items[idx].LocalPath = existingPath
			continue
		}
		data, err := s.client.DownloadAndDecryptMedia(ctx, items[idx].MediaEncryptQueryParam, items[idx].MediaAESKey)
		if err != nil {
			s.logger.Warn("download inbound media failed", "event_id", event.ID, "kind", items[idx].Kind, "err", err)
			_ = s.store.AddLog(context.Background(), "WARN", "download inbound media failed", "media", fmt.Sprintf(`{"event_id":%d,"kind":%q,"err":%q}`, event.ID, items[idx].Kind, err.Error()))
			continue
		}
		targetPath, err := s.writeInboundMedia(account.AccountID, event.ID, idx, items[idx], data)
		if err != nil {
			s.logger.Warn("persist inbound media failed", "event_id", event.ID, "kind", items[idx].Kind, "err", err)
			_ = s.store.AddLog(context.Background(), "WARN", "persist inbound media failed", "media", fmt.Sprintf(`{"event_id":%d,"kind":%q,"err":%q}`, event.ID, items[idx].Kind, err.Error()))
			continue
		}
		items[idx].LocalPath = targetPath
	}
	return items
}

func (s *service) enrichEventItemsWithLocalPaths(accountID string, eventID int64, items []model.EventItem) []model.EventItem {
	if len(items) == 0 {
		return items
	}
	out := make([]model.EventItem, 0, len(items))
	for idx := range items {
		item := items[idx]
		if canDownloadMedia(item) {
			item.LocalPath = s.resolveLocalMediaPath(accountID, eventID, idx)
			if item.LocalPath == "" {
				localPath, err := s.downloadEventItem(context.Background(), accountID, eventID, idx, item)
				if err != nil {
					s.logger.Warn("lazy download media failed", "event_id", eventID, "kind", item.Kind, "err", err)
				} else {
					item.LocalPath = localPath
				}
			}
		}
		out = append(out, item)
	}
	return out
}

func (s *service) downloadEventItem(ctx context.Context, accountID string, eventID int64, itemIndex int, item model.EventItem) (string, error) {
	data, err := s.client.DownloadAndDecryptMedia(ctx, item.MediaEncryptQueryParam, item.MediaAESKey)
	if err != nil {
		return "", err
	}
	return s.writeInboundMedia(accountID, eventID, itemIndex, item, data)
}

func (s *service) writeInboundMedia(accountID string, eventID int64, itemIndex int, item model.EventItem, data []byte) (string, error) {
	accountDir := filepath.Join(s.cfg.MediaDir, sanitizeFileName(accountID))
	if err := os.MkdirAll(accountDir, 0o755); err != nil {
		return "", err
	}
	targetPath := filepath.Join(accountDir, s.localMediaFileName(eventID, itemIndex, item, data))
	if err := os.WriteFile(targetPath, data, 0o644); err != nil {
		return "", err
	}
	return targetPath, nil
}

func (s *service) resolveLocalMediaPath(accountID string, eventID int64, itemIndex int) string {
	pattern := filepath.Join(s.cfg.MediaDir, sanitizeFileName(accountID), localMediaPrefix(eventID, itemIndex)+"*")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return ""
	}
	return matches[0]
}

func (s *service) localMediaFileName(eventID int64, itemIndex int, item model.EventItem, data []byte) string {
	prefix := localMediaPrefix(eventID, itemIndex)
	switch item.Kind {
	case "file":
		name := sanitizeFileName(item.FileName)
		if name == "" {
			name = "file.bin"
		}
		return prefix + "_" + name
	case "voice":
		return prefix + ".silk"
	case "video":
		return prefix + ".mp4"
	case "image":
		return prefix + detectImageExtension(data)
	default:
		return prefix + ".bin"
	}
}

func localMediaPrefix(eventID int64, itemIndex int) string {
	return fmt.Sprintf("%09d_%02d", eventID, itemIndex)
}

func canDownloadMedia(item model.EventItem) bool {
	if strings.TrimSpace(item.MediaEncryptQueryParam) == "" {
		return false
	}
	switch item.Kind {
	case "image", "voice", "file", "video":
		return true
	default:
		return false
	}
}

func detectImageExtension(data []byte) string {
	switch http.DetectContentType(data) {
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/bmp":
		return ".bmp"
	default:
		return ".jpg"
	}
}

func sanitizeFileName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = invalidFileNameChars.ReplaceAllString(name, "_")
	name = strings.ReplaceAll(name, "..", "_")
	name = strings.Trim(name, ". ")
	return name
}

func logMediaPath(logger *slog.Logger, path string) {
	if logger == nil || path == "" {
		return
	}
	logger.Debug("media file ready", "path", path)
}
