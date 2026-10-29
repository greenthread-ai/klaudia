package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
)

// The OpenAI routes for models that are not chat models: speech in, speech
// out, images out. Klaudia does not talk to these models as conversations —
// the chat model calls them through tools (internal/tools/media.go) — so they
// are plain request/response calls, not turns.
//
// Only the OpenAI-compatible provider implements MediaClient. The Anthropic
// API serves none of these, and a backend that does not is detected by type
// assertion, as ModelLister is.

// MediaClient is a provider that can call the non-chat model routes.
type MediaClient interface {
	Transcribe(ctx context.Context, req TranscribeRequest) (string, error)
	Speech(ctx context.Context, req SpeechRequest) (Media, error)
	GenerateImage(ctx context.Context, req ImageRequest) ([]Media, error)
}

// Media is a generated file's bytes and type.
type Media struct {
	Data        []byte
	ContentType string // "audio/wav", "image/png"; from the response, or sniffed
}

// TranscribeRequest is POST /audio/transcriptions.
type TranscribeRequest struct {
	Model    string
	Filename string // sent as the multipart file name; servers infer the format from it
	Audio    []byte
	Language string // ISO-639-1, optional
	Prompt   string // optional: spelling hints, prior context
}

// SpeechRequest is POST /audio/speech. Music models share the route: Input is
// the lyrics and Instructions the style prompt.
type SpeechRequest struct {
	Model          string
	Input          string
	Voice          string
	Instructions   string
	ResponseFormat string // "wav", "mp3", ...; optional
	Speed          float64
}

// ImageRequest is POST /images/generations.
type ImageRequest struct {
	Model  string
	Prompt string
	Size   string // "1024x1024"; optional
	N      int
}

// maxMediaBytes bounds a response read into memory. A minute of 32 kHz stereo
// WAV is ~7.7 MB; a 2048² PNG rarely passes 10 MB.
const maxMediaBytes = 64 << 20

// Transcribe returns the text of an audio file.
func (p *OpenAIProvider) Transcribe(ctx context.Context, r TranscribeRequest) (string, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fields := [][2]string{{"model", r.Model}, {"language", r.Language}, {"prompt", r.Prompt}, {"response_format", "json"}}
	for _, f := range fields {
		if f[1] != "" {
			_ = w.WriteField(f[0], f[1])
		}
	}
	fw, err := w.CreateFormFile("file", r.Filename)
	if err != nil {
		return "", err
	}
	if _, err := fw.Write(r.Audio); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	data, _, err := p.mediaPost(ctx, "/audio/transcriptions", w.FormDataContentType(), body.Bytes())
	if err != nil {
		return "", err
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("transcription response: %w", err)
	}
	return out.Text, nil
}

// Speech returns synthesised audio.
func (p *OpenAIProvider) Speech(ctx context.Context, r SpeechRequest) (Media, error) {
	req := map[string]any{"model": r.Model, "input": r.Input}
	setIf(req, "voice", r.Voice)
	setIf(req, "instructions", r.Instructions)
	setIf(req, "response_format", r.ResponseFormat)
	if r.Speed > 0 {
		req["speed"] = r.Speed
	}
	raw, _ := json.Marshal(req)
	data, ctype, err := p.mediaPost(ctx, "/audio/speech", "application/json", raw)
	if err != nil {
		return Media{}, err
	}
	return Media{Data: data, ContentType: mediaType(ctype, data)}, nil
}

// GenerateImage returns the generated images. b64_json is asked for, not a
// URL: one gateway refuses "url" outright, OpenAI's newer image models only
// return base64, and a URL would be a second fetch from a host the user never
// configured.
func (p *OpenAIProvider) GenerateImage(ctx context.Context, r ImageRequest) ([]Media, error) {
	req := map[string]any{"model": r.Model, "prompt": r.Prompt, "response_format": "b64_json"}
	setIf(req, "size", r.Size)
	if r.N > 0 {
		req["n"] = r.N
	}
	raw, _ := json.Marshal(req)
	data, _, err := p.mediaPost(ctx, "/images/generations", "application/json", raw)
	if err != nil {
		return nil, err
	}
	var out struct {
		Data []struct {
			B64 string `json:"b64_json"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("image response: %w", err)
	}
	var images []Media
	for _, d := range out.Data {
		if d.B64 == "" {
			continue
		}
		b, err := base64.StdEncoding.DecodeString(d.B64)
		if err != nil {
			return nil, fmt.Errorf("image response: %w", err)
		}
		images = append(images, Media{Data: b, ContentType: http.DetectContentType(b)})
	}
	if len(images) == 0 {
		return nil, fmt.Errorf("the endpoint returned no images")
	}
	return images, nil
}

// mediaPost sends one request with the provider's auth and retry policy and
// returns the body. A non-2xx is an *OpenAIError carrying the body, so the
// server's own message ("unknown model: x") reaches the model.
func (p *OpenAIProvider) mediaPost(ctx context.Context, path, contentType string, body []byte) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+path, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", contentType)
	p.setAuth(req)
	resp, err := p.doWithRetry(req, body)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxMediaBytes+1))
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", &OpenAIError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(data))}
	}
	if len(data) > maxMediaBytes {
		return nil, "", fmt.Errorf("%s: response larger than %d MB", path, maxMediaBytes>>20)
	}
	return data, resp.Header.Get("Content-Type"), nil
}

// mediaType prefers the server's Content-Type and sniffs when it is missing
// or generic. Go's sniffer knows WAV, MP3 and the common image types.
func mediaType(header string, data []byte) string {
	if t, _, _ := strings.Cut(header, ";"); t != "" && t != "application/octet-stream" {
		return strings.TrimSpace(t)
	}
	return http.DetectContentType(data)
}

func setIf(m map[string]any, k, v string) {
	if v != "" {
		m[k] = v
	}
}
