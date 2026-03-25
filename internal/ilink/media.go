package ilink

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	UploadMediaImage = 1
	UploadMediaVideo = 2
	UploadMediaFile  = 3
)

var (
	imageExts = map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".bmp": true, ".webp": true, ".tiff": true, ".ico": true, ".svg": true,
	}
	videoExts = map[string]bool{
		".mp4": true, ".avi": true, ".mov": true, ".mkv": true, ".webm": true, ".flv": true,
	}
)

type UploadURLResponse struct {
	UploadParam string `json:"upload_param"`
}

type UploadedMedia struct {
	EncryptQueryParam string
	AESKeyB64         string
	CiphertextSize    int
	RawSize           int
	FileName          string
	ItemType          string
}

func (c *Client) DownloadAndDecryptMedia(ctx context.Context, encryptQueryParam, aesKey string) ([]byte, error) {
	if strings.TrimSpace(c.cdnBaseURL) == "" {
		return nil, fmt.Errorf("cdn base url is empty")
	}
	downloadURL := fmt.Sprintf("%s/download?encrypted_query_param=%s", c.cdnBaseURL, url.QueryEscape(strings.TrimSpace(encryptQueryParam)))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("cdn download http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	key, err := parseMediaAESKey(aesKey)
	if err != nil {
		return nil, err
	}
	if len(key) == 0 {
		return data, nil
	}
	return decryptAESECB(data, key)
}

func (c *Client) UploadMedia(ctx context.Context, baseURL, token, toUserID, filePath string) (UploadedMedia, error) {
	itemType, uploadType := detectOutboundMediaType(filePath)
	rawData, err := os.ReadFile(filePath)
	if err != nil {
		return UploadedMedia{}, err
	}
	rawSize := len(rawData)
	if rawSize == 0 {
		return UploadedMedia{}, fmt.Errorf("file is empty")
	}

	aesKeyRaw := make([]byte, 16)
	if _, err := rand.Read(aesKeyRaw); err != nil {
		return UploadedMedia{}, err
	}
	aesKeyHex := hex.EncodeToString(aesKeyRaw)
	ciphertextSize := aesECBEncryptedSize(rawSize)
	fileKey, err := randomHex(16)
	if err != nil {
		return UploadedMedia{}, err
	}
	sum := md5.Sum(rawData)

	body := map[string]any{
		"filekey":       fileKey,
		"media_type":    uploadType,
		"to_user_id":    toUserID,
		"rawsize":       rawSize,
		"rawfilemd5":    hex.EncodeToString(sum[:]),
		"filesize":      ciphertextSize,
		"no_need_thumb": true,
		"aeskey":        aesKeyHex,
		"base_info": map[string]any{
			"channel_version": c.channelVersion,
		},
	}

	var uploadResp UploadURLResponse
	if err := c.postJSON(ctx, strings.TrimRight(baseURL, "/")+"/ilink/bot/getuploadurl", token, body, &uploadResp); err != nil {
		return UploadedMedia{}, err
	}
	if strings.TrimSpace(uploadResp.UploadParam) == "" {
		return UploadedMedia{}, fmt.Errorf("getuploadurl returned empty upload_param")
	}

	ciphertext, err := encryptAESECB(rawData, aesKeyRaw)
	if err != nil {
		return UploadedMedia{}, err
	}
	uploadURL := fmt.Sprintf("%s/upload?encrypted_query_param=%s&filekey=%s", c.cdnBaseURL, url.QueryEscape(uploadResp.UploadParam), url.QueryEscape(fileKey))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, bytes.NewReader(ciphertext))
	if err != nil {
		return UploadedMedia{}, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return UploadedMedia{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return UploadedMedia{}, fmt.Errorf("cdn upload http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	downloadParam := strings.TrimSpace(resp.Header.Get("x-encrypted-param"))
	if downloadParam == "" {
		return UploadedMedia{}, fmt.Errorf("cdn upload response missing x-encrypted-param header")
	}

	return UploadedMedia{
		EncryptQueryParam: downloadParam,
		AESKeyB64:         base64.StdEncoding.EncodeToString([]byte(aesKeyHex)),
		CiphertextSize:    ciphertextSize,
		RawSize:           rawSize,
		FileName:          filepath.Base(filePath),
		ItemType:          itemType,
	}, nil
}

func (c *Client) SendPreparedMessage(ctx context.Context, baseURL, token string, msg map[string]any) error {
	body := map[string]any{
		"msg": msg,
		"base_info": map[string]any{
			"channel_version": c.channelVersion,
		},
	}
	var out SendMessageResponse
	if err := c.postJSON(ctx, strings.TrimRight(baseURL, "/")+"/ilink/bot/sendmessage", token, body, &out); err != nil {
		return err
	}
	if out.ErrCode != 0 || out.Ret != 0 {
		errText := out.ErrMsg
		if strings.TrimSpace(errText) == "" {
			errText = "sendmessage returned non-zero status"
		}
		return fmt.Errorf("%s (ret=%d errcode=%d)", errText, out.Ret, out.ErrCode)
	}
	return nil
}

func (c *Client) SendMediaMessage(ctx context.Context, baseURL, token, toUserID, contextToken, filePath, text string) (UploadedMedia, map[string]any, error) {
	uploaded, err := c.UploadMedia(ctx, baseURL, token, toUserID, filePath)
	if err != nil {
		return UploadedMedia{}, nil, err
	}
	msg := BuildOutboundMediaMessage(toUserID, contextToken, text, uploaded)
	if err := c.SendPreparedMessage(ctx, baseURL, token, msg); err != nil {
		return UploadedMedia{}, nil, err
	}
	return uploaded, msg, nil
}

func BuildOutboundMediaMessage(toUserID, contextToken, text string, uploaded UploadedMedia) map[string]any {
	itemList := make([]map[string]any, 0, 2)
	if strings.TrimSpace(text) != "" {
		itemList = append(itemList, map[string]any{
			"type": 1,
			"text_item": map[string]any{
				"text": text,
			},
		})
	}
	itemList = append(itemList, buildOutboundMediaItem(uploaded))
	msg := map[string]any{
		"from_user_id":  "",
		"to_user_id":    toUserID,
		"client_id":     fmt.Sprintf("wcfLink-media-%d", time.Now().UnixNano()),
		"message_type":  2,
		"message_state": 2,
		"item_list":     itemList,
	}
	if strings.TrimSpace(contextToken) != "" {
		msg["context_token"] = contextToken
	}
	return msg
}

func MediaBodyText(uploaded UploadedMedia) string {
	switch uploaded.ItemType {
	case "image":
		return "[image] " + uploaded.FileName
	case "video":
		return "[video] " + uploaded.FileName
	default:
		return "[file] " + uploaded.FileName
	}
}

func buildOutboundMediaItem(uploaded UploadedMedia) map[string]any {
	media := map[string]any{
		"encrypt_query_param": uploaded.EncryptQueryParam,
		"aes_key":             uploaded.AESKeyB64,
		"encrypt_type":        1,
	}
	switch uploaded.ItemType {
	case "image":
		return map[string]any{
			"type": 2,
			"image_item": map[string]any{
				"media":    media,
				"mid_size": uploaded.CiphertextSize,
			},
		}
	case "video":
		return map[string]any{
			"type": 5,
			"video_item": map[string]any{
				"media":      media,
				"video_size": uploaded.CiphertextSize,
			},
		}
	default:
		return map[string]any{
			"type": 4,
			"file_item": map[string]any{
				"media":     media,
				"file_name": uploaded.FileName,
				"len":       fmt.Sprintf("%d", uploaded.RawSize),
			},
		}
	}
}

func detectOutboundMediaType(filePath string) (string, int) {
	ext := strings.ToLower(filepath.Ext(filePath))
	if imageExts[ext] {
		return "image", UploadMediaImage
	}
	if videoExts[ext] {
		return "video", UploadMediaVideo
	}
	return "file", UploadMediaFile
}

func parseMediaAESKey(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	if decodedHex, err := hex.DecodeString(value); err == nil && len(decodedHex) == 16 {
		return decodedHex, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, err
	}
	if len(decoded) == 16 {
		return decoded, nil
	}
	if len(decoded) == 32 {
		if decodedHex, err := hex.DecodeString(string(decoded)); err == nil && len(decodedHex) == 16 {
			return decodedHex, nil
		}
	}
	return nil, fmt.Errorf("unsupported aes key format")
}

func aesECBEncryptedSize(rawSize int) int {
	return ((rawSize + 1 + aes.BlockSize - 1) / aes.BlockSize) * aes.BlockSize
}

func encryptAESECB(data, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	padded := pkcs7Pad(data, aes.BlockSize)
	out := make([]byte, len(padded))
	for i := 0; i < len(padded); i += aes.BlockSize {
		block.Encrypt(out[i:i+aes.BlockSize], padded[i:i+aes.BlockSize])
	}
	return out, nil
}

func decryptAESECB(data, key []byte) ([]byte, error) {
	if len(data)%aes.BlockSize != 0 {
		return data, nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += aes.BlockSize {
		block.Decrypt(out[i:i+aes.BlockSize], data[i:i+aes.BlockSize])
	}
	unpadded, err := pkcs7Unpad(out, aes.BlockSize)
	if err != nil {
		return out, nil
	}
	return unpadded, nil
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padLen := blockSize - (len(data) % blockSize)
	if padLen == 0 {
		padLen = blockSize
	}
	padding := bytes.Repeat([]byte{byte(padLen)}, padLen)
	return append(append([]byte(nil), data...), padding...)
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, fmt.Errorf("invalid padded data length")
	}
	padLen := int(data[len(data)-1])
	if padLen <= 0 || padLen > blockSize || padLen > len(data) {
		return nil, fmt.Errorf("invalid pad length")
	}
	for i := len(data) - padLen; i < len(data); i++ {
		if int(data[i]) != padLen {
			return nil, fmt.Errorf("invalid pad bytes")
		}
	}
	return data[:len(data)-padLen], nil
}

func randomHex(byteLen int) (string, error) {
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
