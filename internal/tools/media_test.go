package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/pkg/api"
	"github.com/greenthread-ai/klaudia/pkg/permission"
)

// fakePNG is the 8-byte PNG signature plus enough to sniff as image/png.
var fakePNG = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)

type fakeMedia struct {
	transcribe api.TranscribeRequest
	speech     api.SpeechRequest
	image      api.ImageRequest
	images     int
	err        error
}

func (f *fakeMedia) Transcribe(_ context.Context, r api.TranscribeRequest) (string, error) {
	f.transcribe = r
	return "hello there", f.err
}

func (f *fakeMedia) Speech(_ context.Context, r api.SpeechRequest) (api.Media, error) {
	f.speech = r
	return api.Media{Data: []byte("RIFF....WAVEfmt "), ContentType: "audio/wav"}, f.err
}

func (f *fakeMedia) GenerateImage(_ context.Context, r api.ImageRequest) ([]api.Media, error) {
	f.image = r
	n := f.images
	if n == 0 {
		n = 1
	}
	out := make([]api.Media, n)
	for i := range out {
		out[i] = api.Media{Data: fakePNG, ContentType: "image/png"}
	}
	return out, f.err
}

var gatewayMedia = MediaModels{
	{ID: "qwen/asr", Kind: api.ModelKindTranscribe},
	{ID: "qwen/tts", Kind: api.ModelKindSpeech, Description: "Text-to-speech.", Params: []string{"voice", "speed"}},
	{ID: "flux", Kind: api.ModelKindImage},
	{ID: "minimax/music", Kind: api.ModelKindMusic},
}

func runMedia(t *testing.T, tool Tool, dir string, input any) Result {
	t.Helper()
	raw, _ := json.Marshal(input)
	if err := tool.ValidateInput(raw); err != nil {
		t.Fatalf("ValidateInput: %v", err)
	}
	res, err := tool.Execute(context.Background(), Context{WorkingDir: dir}, raw)
	if err != nil || len(res) != 1 {
		t.Fatalf("Execute = %v, %v", res, err)
	}
	return res[0]
}

// A tool exists only where a model can back it.
func TestMediaToolsRegisterPerKind(t *testing.T) {
	reg, err := DefaultRegistry(nil, WithMedia(&fakeMedia{}, gatewayMedia[:2]))
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{"Transcribe": true, "Speak": true, "GenerateImage": false, "GenerateMusic": false} {
		if _, ok := reg.Lookup(name); ok != want {
			t.Errorf("%s registered = %v, want %v", name, ok, want)
		}
	}
	reg, _ = DefaultRegistry(nil)
	if _, ok := reg.Lookup("Speak"); ok {
		t.Error("no WithMedia must mean no media tools")
	}
	// Loaded, not deferred: a model that cannot see them reaches for pip and
	// a third-party API instead (media.go).
	for _, n := range []string{"Transcribe", "Speak", "GenerateImage", "GenerateMusic"} {
		if DeferredBuiltins[n] {
			t.Errorf("%s must not be deferred behind ToolSearch", n)
		}
	}
}

func TestSpeakSavesWhereTheModelSays(t *testing.T) {
	dir := t.TempDir()
	f := &fakeMedia{}
	speak, _ := NewSpeak(f, gatewayMedia.of(api.ModelKindSpeech))

	// No extension: the one the bytes call for is added.
	res := runMedia(t, speak, dir, SpeakInput{Text: "Hello", FilePath: "media/greeting", Voice: "alto"})
	want := filepath.Join(dir, "media", "greeting.wav")
	if _, err := os.Stat(want); err != nil || !strings.Contains(res.Content, want) {
		t.Errorf("want %s written and reported; got %q (%v)", want, res.Content, err)
	}
	if f.speech.Model != "qwen/tts" || f.speech.Voice != "alto" {
		t.Errorf("request = %+v; the only model should be picked and the voice passed", f.speech)
	}

	// Omitted: the working directory, named from the text, never overwriting.
	runMedia(t, speak, dir, SpeakInput{Text: "Good morning, team!"})
	runMedia(t, speak, dir, SpeakInput{Text: "Good morning, team!"})
	for _, name := range []string{"good-morning-team-speech.wav", "good-morning-team-speech-2.wav"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s not written: %v", name, err)
		}
	}
}

func TestGenerateImageShowsAndNumbers(t *testing.T) {
	dir := t.TempDir()
	f := &fakeMedia{images: 2}
	gen, _ := NewGenerateImage(f, gatewayMedia.of(api.ModelKindImage))
	res := runMedia(t, gen, dir, GenerateImageInput{Prompt: "a fox", FilePath: "assets/fox.png", Count: 2})
	for _, name := range []string{"fox-1.png", "fox-2.png"} {
		if _, err := os.Stat(filepath.Join(dir, "assets", name)); err != nil {
			t.Errorf("%s not written: %v", name, err)
		}
	}
	if len(res.Images) != 2 || res.Images[0].MediaType != "image/png" {
		t.Errorf("images shown = %+v, want both, so the model can check them", res.Images)
	}
	if f.image.N != 2 || f.image.Prompt != "a fox" {
		t.Errorf("request = %+v", f.image)
	}
}

func TestGenerateMusicSendsStyleAsInstructions(t *testing.T) {
	f := &fakeMedia{}
	music, _ := NewGenerateMusic(f, gatewayMedia.of(api.ModelKindMusic))
	runMedia(t, music, t.TempDir(), GenerateMusicInput{Lyrics: "[verse]\nla la", Style: "lo-fi, 80 bpm"})
	if f.speech.Input != "[verse]\nla la" || f.speech.Instructions != "lo-fi, 80 bpm" {
		t.Errorf("request = %+v; lyrics are the input, style the instructions", f.speech)
	}
}

func TestTranscribeSendsTheFile(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "call.wav"), []byte("RIFFdata"), 0o644)
	f := &fakeMedia{}
	tr, _ := NewTranscribe(f, gatewayMedia.of(api.ModelKindTranscribe))
	res := runMedia(t, tr, dir, TranscribeInput{FilePath: "call.wav", Language: "en"})
	if res.Content != "hello there" || res.IsError {
		t.Errorf("result = %+v", res)
	}
	if f.transcribe.Filename != "call.wav" || string(f.transcribe.Audio) != "RIFFdata" || f.transcribe.Language != "en" {
		t.Errorf("request = %+v", f.transcribe)
	}
}

// With a choice of models the model must make it; a model not in the catalog
// (denied, or of another kind) is refused before any request.
func TestMediaModelChoice(t *testing.T) {
	two := []api.ModelInfo{{ID: "a", Kind: api.ModelKindImage}, {ID: "b", Kind: api.ModelKindImage}}
	gen, _ := NewGenerateImage(&fakeMedia{}, two)
	raw, _ := json.Marshal(GenerateImageInput{Prompt: "x"})
	if err := gen.ValidateInput(raw); err == nil || !strings.Contains(err.Error(), "a, b") {
		t.Errorf("want an error naming the choices, got %v", err)
	}
	raw, _ = json.Marshal(GenerateImageInput{Prompt: "x", Model: "qwen/tts"})
	if err := gen.ValidateInput(raw); err == nil {
		t.Error("a model outside the catalog must be refused")
	}
	desc, _ := gen.Description(context.Background())
	if !strings.Contains(desc, "- a") || !strings.Contains(desc, "- b") {
		t.Errorf("description should list the models:\n%s", desc)
	}
}

func TestMediaErrorsCarryTheServersWords(t *testing.T) {
	f := &fakeMedia{err: &api.OpenAIError{StatusCode: 400, Body: `{"error":{"code":"model_not_supported","message":"model x does not support audio_speech"}}`}}
	speak, _ := NewSpeak(f, gatewayMedia.of(api.ModelKindSpeech))
	res := runMedia(t, speak, t.TempDir(), SpeakInput{Text: "hi"})
	if !res.IsError || !strings.Contains(res.Content, "does not support audio_speech (HTTP 400)") {
		t.Errorf("result = %+v", res)
	}
}

func TestMediaToolsRefusedInPlanMode(t *testing.T) {
	plan := permission.Context{Mode: permission.StaticMode(permission.ModePlan)}
	auto := permission.Context{}
	tr, _ := NewTranscribe(nil, nil)
	sp, _ := NewSpeak(nil, nil)
	im, _ := NewGenerateImage(nil, nil)
	mu, _ := NewGenerateMusic(nil, nil)
	for _, tool := range []Tool{tr, sp, im, mu} {
		if d := tool.CheckPermissions(plan, permission.PermissionRequest{}); d.Behavior != permission.Deny {
			t.Errorf("%s in plan mode = %v, want deny", tool.Name(), d.Behavior)
		}
		// Generation does not ask: there is no standing "always", so a prompt
		// would come on every call and refuse every unattended run.
		if d := tool.CheckPermissions(auto, permission.PermissionRequest{}); d.Behavior != permission.Allow {
			t.Errorf("%s in autonomous = %v, want allow", tool.Name(), d.Behavior)
		}
	}
}

// A model that names the file .mp3 gets an MP3 asked for; if the server sends
// something else anyway, the extension follows the bytes and the result says
// so, rather than leaving a WAV called .mp3 (seen live before this).
func TestMediaExtensionFollowsTheBytes(t *testing.T) {
	dir := t.TempDir()
	f := &fakeMedia{} // always answers audio/wav
	speak, _ := NewSpeak(f, gatewayMedia.of(api.ModelKindSpeech))
	res := runMedia(t, speak, dir, SpeakInput{Text: "hi", FilePath: "welcome.mp3"})
	if f.speech.ResponseFormat != "mp3" {
		t.Errorf("response_format = %q, want mp3 from the file name", f.speech.ResponseFormat)
	}
	if _, err := os.Stat(filepath.Join(dir, "welcome.wav")); err != nil {
		t.Errorf("want welcome.wav: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "welcome.mp3")); err == nil {
		t.Error("a WAV was saved under .mp3")
	}
	if !strings.Contains(res.Content, "saved as welcome.wav rather than welcome.mp3") {
		t.Errorf("the rename should be reported: %q", res.Content)
	}
	// An explicit format wins over the name.
	runMedia(t, speak, dir, SpeakInput{Text: "hi", FilePath: "a.mp3", Format: "wav"})
	if f.speech.ResponseFormat != "wav" {
		t.Errorf("response_format = %q, want the explicit wav", f.speech.ResponseFormat)
	}
}
