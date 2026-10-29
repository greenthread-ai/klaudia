package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The request shapes below are the ones a live gateway accepted (measured):
// multipart for transcription, JSON for speech and images, and b64_json
// because it refused response_format "url".
func TestMediaRequests(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 16)...)
	var seen map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("%s: no auth", r.URL.Path)
		}
		switch r.URL.Path {
		case "/v1/audio/transcriptions":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatal(err)
			}
			f, h, err := r.FormFile("file")
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(f)
			if h.Filename != "call.mp3" || string(body) != "AUDIO" || r.FormValue("model") != "asr" || r.FormValue("language") != "en" {
				t.Errorf("multipart = %q %q %q %q", h.Filename, body, r.FormValue("model"), r.FormValue("language"))
			}
			_, _ = w.Write([]byte(`{"text":"hello","usage":{"type":"duration","seconds":5}}`))
		case "/v1/audio/speech":
			_ = json.NewDecoder(r.Body).Decode(&seen)
			w.Header().Set("Content-Type", "audio/wav")
			_, _ = w.Write([]byte("RIFF....WAVE"))
		case "/v1/images/generations":
			_ = json.NewDecoder(r.Body).Decode(&seen)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"b64_json": base64.StdEncoding.EncodeToString(png), "url": nil}}})
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	p := NewOpenAIProvider(srv.URL+"/v1", "k", nil, Tuning{})
	ctx := context.Background()

	text, err := p.Transcribe(ctx, TranscribeRequest{Model: "asr", Filename: "call.mp3", Audio: []byte("AUDIO"), Language: "en"})
	if err != nil || text != "hello" {
		t.Errorf("Transcribe = %q, %v", text, err)
	}

	audio, err := p.Speech(ctx, SpeechRequest{Model: "music", Input: "lyrics", Instructions: "lo-fi"})
	if err != nil || audio.ContentType != "audio/wav" || string(audio.Data) != "RIFF....WAVE" {
		t.Errorf("Speech = %+v, %v", audio, err)
	}
	if seen["input"] != "lyrics" || seen["instructions"] != "lo-fi" {
		t.Errorf("speech body = %v", seen)
	}
	if _, ok := seen["voice"]; ok {
		t.Error("an unset voice must not be sent: some servers reject unknown values")
	}

	imgs, err := p.GenerateImage(ctx, ImageRequest{Model: "flux", Prompt: "fox", Size: "512x512"})
	if err != nil || len(imgs) != 1 || imgs[0].ContentType != "image/png" {
		t.Fatalf("GenerateImage = %+v, %v", imgs, err)
	}
	if seen["response_format"] != "b64_json" || seen["size"] != "512x512" {
		t.Errorf("image body = %v", seen)
	}
}

func TestMediaErrorKeepsTheServersMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"model_not_found","message":"unknown model: nope"}}`))
	}))
	defer srv.Close()
	_, err := NewOpenAIProvider(srv.URL+"/v1", "k", nil, Tuning{}).Speech(context.Background(), SpeechRequest{Model: "nope", Input: "x"})
	var oe *OpenAIError
	if !errors.As(err, &oe) || oe.StatusCode != 404 || !strings.Contains(oe.Payload().Message, "unknown model: nope") {
		t.Errorf("err = %v", err)
	}
}

func TestOnlyTheOpenAIProviderServesMedia(t *testing.T) {
	var _ MediaClient = (*OpenAIProvider)(nil)
	var p Provider = &Client{}
	if _, ok := p.(MediaClient); ok {
		t.Error("the Anthropic client serves no media routes and must not claim to")
	}
}
