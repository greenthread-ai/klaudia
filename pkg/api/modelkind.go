package api

import (
	"slices"
	"strings"
)

// What a listed model is for.
//
// GET /v1/models was a list of chat models as far as Klaudia knew, and on an
// endpoint that also serves speech, transcription and image models that was
// wrong in a way the user paid for: /model offered a TTS model beside the chat
// ones, and picking it sent the next turn to Chat Completions, which the
// server refused (measured on one such gateway: 400 model_not_supported, "does
// not support chat_completions").
//
// The OpenAI spec puts nothing but an id on a model, but servers that host
// more than chat say more, each in its own words. Klaudia reads what is said,
// most authoritative first: the endpoints the server will route the model to
// (the one it enforces), its declared type, its declared modalities, and only
// then the id. A model none of those place is ModelKindUnknown and still
// counts as chat: hiding a model the user may need because we could not read
// its listing is worse than offering one that fails with a clear error.

// ModelKind is the job a model does.
type ModelKind string

const (
	ModelKindUnknown    ModelKind = ""
	ModelKindChat       ModelKind = "chat"
	ModelKindImage      ModelKind = "image" // text → image
	ModelKindSpeech     ModelKind = "tts"   // text → speech
	ModelKindTranscribe ModelKind = "stt"   // audio → text
	ModelKindMusic      ModelKind = "music" // lyrics and a style prompt → song
	ModelKindEmbedding  ModelKind = "embedding"
)

// Chat reports whether a model can drive the agent loop. Unknown counts: see
// the note at the top of this file.
func (m ModelInfo) Chat() bool {
	return m.Kind == ModelKindChat || m.Kind == ModelKindUnknown
}

// ChatModels is models without the ones that cannot hold a conversation.
func ChatModels(models []ModelInfo) []ModelInfo {
	out := make([]ModelInfo, 0, len(models))
	for _, m := range models {
		if m.Chat() {
			out = append(out, m)
		}
	}
	return out
}

// declaredKinds maps the type names servers use to a kind. The keys are what
// has been seen in the wild ("asr" is one gateway's word for transcription);
// a type not here falls through to the other signals rather than guessing.
var declaredKinds = map[string]ModelKind{
	"chat": ModelKindChat, "llm": ModelKindChat, "language": ModelKindChat,
	"image": ModelKindImage, "image_generation": ModelKindImage,
	"tts": ModelKindSpeech, "speech": ModelKindSpeech,
	"asr": ModelKindTranscribe, "stt": ModelKindTranscribe, "transcription": ModelKindTranscribe,
	"music":     ModelKindMusic,
	"embedding": ModelKindEmbedding, "embeddings": ModelKindEmbedding,
}

// classifyModel places a model from what its listing says. endpoints is the
// server's supported_endpoints, declared its model_type (or type), in and out
// its architecture modalities; any may be empty.
func classifyModel(id string, endpoints []string, declared string, in, out []string) ModelKind {
	// The declared type first when it is one we know: it is the only signal
	// that tells music from speech, which share /audio/speech.
	if k, ok := declaredKinds[strings.ToLower(strings.TrimSpace(declared))]; ok {
		return k
	}
	switch {
	case slices.Contains(endpoints, "chat_completions"):
		return ModelKindChat
	case slices.Contains(endpoints, "images_generations"):
		return ModelKindImage
	case slices.Contains(endpoints, "audio_transcriptions"):
		return ModelKindTranscribe
	case slices.Contains(endpoints, "audio_speech"):
		return ModelKindSpeech
	case slices.Contains(endpoints, "embeddings"):
		return ModelKindEmbedding
	}
	switch {
	case slices.Contains(out, "image"):
		return ModelKindImage
	case slices.Contains(out, "audio"):
		return ModelKindSpeech
	case slices.Contains(in, "audio") && slices.Contains(out, "text"):
		return ModelKindTranscribe
	case slices.Contains(out, "text"):
		return ModelKindChat
	}
	return classifyID(id)
}

// classifyID is the last resort, for servers that list bare ids: OpenAI's own
// does, and serves whisper, tts and image models from the same list as chat.
// Only families whose names cannot be a chat model are matched.
func classifyID(id string) ModelKind {
	id = strings.ToLower(id)
	if i := strings.LastIndex(id, "/"); i >= 0 {
		id = id[i+1:]
	}
	switch {
	case strings.HasPrefix(id, "whisper"), strings.Contains(id, "-transcribe"):
		return ModelKindTranscribe
	case strings.HasPrefix(id, "tts-"), strings.HasSuffix(id, "-tts"):
		return ModelKindSpeech
	case strings.HasPrefix(id, "dall-e"), strings.HasPrefix(id, "gpt-image"):
		return ModelKindImage
	case strings.Contains(id, "embedding"):
		return ModelKindEmbedding
	}
	return ModelKindUnknown
}
