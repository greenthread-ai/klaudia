package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// gatewayListing is a /v1/models response in the shape a multi-modal gateway
// returned (trimmed to the fields Klaudia reads, plus one it must ignore). The
// music model shares audio_speech with the TTS one; only model_type tells them
// apart.
const gatewayListing = `{"object":"list","data":[
 {"id":"MiniMaxAI/MiniMax-Music3","model_type":"music","supported_endpoints":["audio_speech"],
  "supported_parameters":["instructions","seed"],"architecture":{"input_modalities":["text"],"output_modalities":["audio"]}},
 {"id":"Qwen/Qwen3-ASR-1.7B","model_type":"asr","supported_endpoints":["audio_transcriptions","audio_translations"],
  "architecture":{"input_modalities":["audio"],"output_modalities":["text"]}},
 {"id":"Qwen/Qwen3-TTS-12Hz-1.7B-Base","model_type":"tts","supported_endpoints":["audio_speech","voices"],
  "supported_parameters":["voice","speed","response_format"],"architecture":{"input_modalities":["text"],"output_modalities":["audio"]}},
 {"id":"black-forest-labs/FLUX.2-klein-4B","model_type":"image","supported_endpoints":["images_generations"],
  "architecture":{"input_modalities":["text"],"output_modalities":["image"]}},
 {"id":"cyankiwi/Qwen3.6-35B-A3B-AWQ-4bit","model_type":"chat","description":"General-purpose chat model.",
  "supported_endpoints":["chat_completions","completions","responses"],"supported_parameters":["max_tokens","tools"],
  "context_length":65536,"top_provider":{"context_length":8192},
  "architecture":{"input_modalities":["text","image"],"output_modalities":["text"]}}
]}`

func TestOpenAIListModelsReadsGatewayMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(gatewayListing))
	}))
	defer srv.Close()

	got, err := NewOpenAIProvider(srv.URL+"/v1", "k", nil, Tuning{}).ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]ModelKind{}
	byID := map[string]ModelInfo{}
	for _, m := range got {
		kinds[m.ID] = m.Kind
		byID[m.ID] = m
	}
	want := map[string]ModelKind{
		"MiniMaxAI/MiniMax-Music3":          ModelKindMusic,
		"Qwen/Qwen3-ASR-1.7B":               ModelKindTranscribe,
		"Qwen/Qwen3-TTS-12Hz-1.7B-Base":     ModelKindSpeech,
		"black-forest-labs/FLUX.2-klein-4B": ModelKindImage,
		"cyankiwi/Qwen3.6-35B-A3B-AWQ-4bit": ModelKindChat,
	}
	for id, k := range want {
		if kinds[id] != k {
			t.Errorf("%s: kind %q, want %q", id, kinds[id], k)
		}
	}
	chat := byID["cyankiwi/Qwen3.6-35B-A3B-AWQ-4bit"]
	// context_length, not top_provider.context_length: the gateway enforced
	// neither 8192 on input nor on output.
	if chat.ContextWindow != 65536 {
		t.Errorf("context window = %d, want 65536 from context_length", chat.ContextWindow)
	}
	if chat.Description == "" || len(chat.Params) != 2 || len(chat.Inputs) != 2 {
		t.Errorf("metadata dropped: %+v", chat)
	}
	if c := ChatModels(got); len(c) != 1 || c[0].ID != "cyankiwi/Qwen3.6-35B-A3B-AWQ-4bit" {
		t.Errorf("ChatModels = %+v, want only the chat model", c)
	}
}

func TestClassifyModel(t *testing.T) {
	for _, tc := range []struct {
		name      string
		id        string
		endpoints []string
		declared  string
		in, out   []string
		want      ModelKind
	}{
		{name: "endpoint over modalities", id: "x", endpoints: []string{"audio_transcriptions"}, out: []string{"text"}, want: ModelKindTranscribe},
		{name: "unknown declared type falls through", id: "x", declared: "reranker", endpoints: []string{"chat_completions"}, want: ModelKindChat},
		{name: "Together-style type", id: "x", declared: "image", want: ModelKindImage},
		{name: "OpenRouter-style modalities only", id: "x", in: []string{"text"}, out: []string{"image"}, want: ModelKindImage},
		{name: "audio in, text out", id: "x", in: []string{"audio"}, out: []string{"text"}, want: ModelKindTranscribe},
		{name: "vision chat", id: "x", in: []string{"text", "image"}, out: []string{"text"}, want: ModelKindChat},
		// OpenAI's own list: bare ids.
		{name: "whisper", id: "whisper-1", want: ModelKindTranscribe},
		{name: "transcribe", id: "gpt-4o-transcribe", want: ModelKindTranscribe},
		{name: "tts prefix", id: "tts-1-hd", want: ModelKindSpeech},
		{name: "tts suffix", id: "gpt-4o-mini-tts", want: ModelKindSpeech},
		{name: "image", id: "openai/gpt-image-1", want: ModelKindImage},
		{name: "embedding", id: "text-embedding-3-small", want: ModelKindEmbedding},
		{name: "a bare chat id stays unknown, which counts as chat", id: "gpt-5.5", want: ModelKindUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyModel(tc.id, tc.endpoints, tc.declared, tc.in, tc.out); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
	if !(ModelInfo{}).Chat() {
		t.Error("an unclassified model must still be offered as chat")
	}
}
