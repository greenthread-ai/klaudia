package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/greenthread-ai/klaudia/pkg/api"
	"github.com/greenthread-ai/klaudia/pkg/permission"
	"github.com/greenthread-ai/klaudia/pkg/schema"
)

// Tools over the endpoint's non-chat models: Transcribe (audio → text), Speak
// (text → speech), GenerateImage and GenerateMusic.
//
// They exist only where a model can back them. DefaultRegistry registers one
// when WithMedia's catalog lists a model of its kind, so a gateway that serves
// no image model offers no GenerateImage, and the Anthropic backend — which
// serves none of these — offers none.
//
// They are not deferred behind ToolSearch, though they began that way. Asked
// for a header image, a spoken welcome and its transcript, a model that could
// not see them never searched: it pip-installed Pillow, gTTS and
// SpeechRecognition with --break-system-packages, drew the "image" with PIL,
// and sent the audio to Google's free speech API (measured, one run in two).
// ToolSearch does not name what it holds, so a capability the model has no
// reason to expect is one it does not look for. The definitions cost ~1.2k
// tokens a request (4.9 KB measured, with one model of each kind), paid only
// on an endpoint that serves these models.
//
// What a tool produces is a file. Audio cannot be put back into the
// conversation and an image the user cannot keep is no use to them, so the
// output is written to disk and the path reported; an image is also shown to
// the model so it can check the result. Where it goes is the model's call —
// file_path, with the description suggesting it be put where the work that
// uses it lives. Omitted, it lands in the working directory under a name made
// from the request, never over an existing file.
//
// Permission is the same as Write's: the host gate classifies file_path
// (trust.ClassifyToolCall), so a path outside the project asks, and plan mode
// refuses all four, since each either writes a file or sends one off the
// machine. Generation does not otherwise ask. Klaudia has no standing "always
// allow", so a per-call prompt would ask on every image and refuse every
// unattended run; a model the user does not want used is kept out of the
// catalog by config instead.

// MediaModels is the catalog the media tools choose from: the endpoint's
// listed models that are not chat models.
type MediaModels []api.ModelInfo

func (ms MediaModels) of(kind api.ModelKind) []api.ModelInfo {
	var out []api.ModelInfo
	for _, m := range ms {
		if m.Kind == kind {
			out = append(out, m)
		}
	}
	return out
}

// mediaTimeout bounds one generation call. Measured on one gateway: a 512²
// image in ~6s, a 17-second song in ~19s. Five minutes leaves room for long
// audio without letting a wedged server hold the turn indefinitely.
const mediaTimeout = 5 * time.Minute

// maxTranscribeBytes is OpenAI's documented upload limit for transcription.
const maxTranscribeBytes = 25 << 20

// mediaTool is what the four tools share: the client, the models of their
// kind, and how to pick one.
type mediaTool struct {
	client api.MediaClient
	models []api.ModelInfo
	kind   api.ModelKind
}

// pick resolves the model a call asked for. Empty means the only one, and is
// an error when there is a choice to make — picking for the model would hide
// that the choice existed.
func (t mediaTool) pick(id string) (api.ModelInfo, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		if len(t.models) == 1 {
			return t.models[0], nil
		}
		return api.ModelInfo{}, fmt.Errorf("model is required: more than one %s model is available (%s)", t.kind, t.ids())
	}
	for _, m := range t.models {
		if m.ID == id {
			return m, nil
		}
	}
	return api.ModelInfo{}, fmt.Errorf("%s is not an available %s model; available: %s", id, t.kind, t.ids())
}

func (t mediaTool) ids() string {
	ids := make([]string, len(t.models))
	for i, m := range t.models {
		ids[i] = m.ID
	}
	return strings.Join(ids, ", ")
}

// catalog renders the available models for a description: id, what the
// endpoint says it is, and the parameters it accepts.
func (t mediaTool) catalog() string {
	var b strings.Builder
	b.WriteString("\n\nAvailable models:")
	for _, m := range t.models {
		b.WriteString("\n- " + m.ID)
		if m.Description != "" {
			b.WriteString(": " + m.Description)
		}
		if len(m.Params) > 0 {
			b.WriteString(" (accepts: " + strings.Join(m.Params, ", ") + ")")
		}
	}
	if len(t.models) == 1 {
		b.WriteString("\nmodel may be omitted: there is only one.")
	}
	return b.String()
}

// savePathHint steers file_path without deciding it. Live, a model told only
// that it could choose a path omitted it and then moved the file with mv, two
// runs out of two; saying when to set it is what the hint is for.
const savePathHint = "Set file_path (absolute, or relative to the working directory) whenever the file will be used " +
	"somewhere: put it where that work lives, beside the document or page that will reference it, or in the project's " +
	"existing assets/media directory if it has one, rather than saving it and moving it afterwards. Omitted, it is saved " +
	"in the working directory under a name made from the request."

// --- saving ---

var slugRun = regexp.MustCompile(`[^a-z0-9]+`)

// defaultMediaPath is where an output goes when the model named no path: the
// working directory, a name from the request, and a suffix if that is taken.
// It never overwrites: an omitted path is not a request to replace anything.
func defaultMediaPath(dir, from, kind, ext string) string {
	slug := strings.Trim(slugRun.ReplaceAllString(strings.ToLower(from), "-"), "-")
	if len(slug) > 40 {
		slug = strings.TrimRight(slug[:40], "-")
	}
	if slug == "" {
		slug = kind
	} else {
		slug += "-" + kind
	}
	p := filepath.Join(dir, slug+ext)
	for i := 2; ; i++ {
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return p
		}
		p = filepath.Join(dir, fmt.Sprintf("%s-%d%s", slug, i, ext))
	}
}

// extFor maps a returned media type to a file extension.
func extFor(mediaType string) string {
	switch mediaType {
	case "audio/wav", "audio/wave", "audio/x-wav":
		return ".wav"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/flac":
		return ".flac"
	case "audio/ogg", "audio/opus":
		return ".ogg"
	case "audio/aac":
		return ".aac"
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	}
	return ".bin"
}

// audioFormatFor is the response_format that produces a file named path, so
// a model that asks for welcome.mp3 gets an MP3 rather than WAV bytes under
// an .mp3 name (measured: with no format, one gateway returned WAV for a call
// the model had pointed at welcome.mp3). "" leaves the server's default.
func audioFormatFor(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		return "mp3"
	case ".wav":
		return "wav"
	case ".flac":
		return "flac"
	case ".opus":
		return "opus"
	case ".aac":
		return "aac"
	}
	return ""
}

// savePath resolves where one output goes, and a note when it is not where
// the model said. A path without an extension gets the one the bytes call
// for; one whose extension names a different type than the server returned
// has it corrected, because a player trusts the name and a WAV called .mp3
// will not play in some of them.
func savePath(tctx Context, chosen, from, kind, mediaType string) (path, note string) {
	ext := extFor(mediaType)
	if strings.TrimSpace(chosen) == "" {
		return defaultMediaPath(tctx.WorkingDir, from, kind, ext), ""
	}
	p := resolvePath(tctx, chosen)
	have := strings.ToLower(filepath.Ext(p))
	switch {
	case have == "":
		p += ext
	case ext != ".bin" && have != ext && !(have == ".jpeg" && ext == ".jpg"):
		fixed := strings.TrimSuffix(p, filepath.Ext(p)) + ext
		note = fmt.Sprintf(" (the endpoint returned %s, so it was saved as %s rather than %s; update any references)", mediaType, filepath.Base(fixed), filepath.Base(p))
		p = fixed
	}
	return p, note
}

func firstNonBlank(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func saveMedia(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o644)
}

func mediaErr(what string, err error) []Result {
	return []Result{{Content: what + ": " + friendlyMediaError(err), IsError: true}}
}

// friendlyMediaError surfaces the endpoint's own message: its words
// ("model X does not support chat_completions") are more use to the model than
// a status code.
func friendlyMediaError(err error) string {
	if oe, ok := err.(*api.OpenAIError); ok {
		if p := oe.Payload(); p != nil && p.Message != "" {
			return fmt.Sprintf("%s (HTTP %d)", p.Message, oe.StatusCode)
		}
	}
	return err.Error()
}

// writeMediaDecision is the intrinsic decision for the tools that write a
// file: plan mode refuses, as it does Write.
func writeMediaDecision(pctx permission.Context) permission.Decision {
	if permission.CurrentMode(pctx) == permission.ModePlan {
		return permission.Decision{Behavior: permission.Deny, Message: "plan mode is read-only; generating files is not allowed"}
	}
	return permission.Decision{Behavior: permission.Allow}
}

// --- Transcribe ---

type TranscribeInput struct {
	FilePath string `json:"file_path" jsonschema:"description=The audio file to transcribe (absolute, or relative to the working directory)"`
	Model    string `json:"model,omitempty" jsonschema:"description=Transcription model id; see the tool description"`
	Language string `json:"language,omitempty" jsonschema:"description=The spoken language as an ISO-639-1 code (e.g. en), if known"`
	Prompt   string `json:"prompt,omitempty" jsonschema:"description=Optional text to guide spelling: names, jargon, or the preceding transcript"`
}

type Transcribe struct {
	mediaTool
	schema *schema.Schema
}

func NewTranscribe(client api.MediaClient, models []api.ModelInfo) (*Transcribe, error) {
	s, err := schema.For[TranscribeInput]()
	if err != nil {
		return nil, fmt.Errorf("transcribe: build schema: %w", err)
	}
	return &Transcribe{mediaTool{client, models, api.ModelKindTranscribe}, s}, nil
}

func (t *Transcribe) Name() string { return "Transcribe" }

func (t *Transcribe) Description(context.Context) (string, error) {
	return "Transcribes an audio file (wav, mp3, m4a, flac, ogg, webm; up to 25 MB) to text using a speech-recognition " +
		"model on the configured endpoint, and returns the text." + t.catalog(), nil
}

func (t *Transcribe) InputSchema() json.RawMessage { return t.schema.Raw }

func (t *Transcribe) ValidateInput(raw json.RawMessage) error {
	if err := t.schema.Validate(raw); err != nil {
		return err
	}
	var in TranscribeInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.FilePath) == "" {
		return fmt.Errorf("file_path is required")
	}
	_, err := t.pick(in.Model)
	return err
}

func (t *Transcribe) PermissionRequest(raw json.RawMessage) permission.PermissionRequest {
	var in TranscribeInput
	_ = json.Unmarshal(raw, &in)
	return permission.PermissionRequest{Specifier: in.FilePath}
}

// CheckPermissions: the file leaves the machine, so plan mode refuses it as it
// refuses the web tools.
func (t *Transcribe) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return networkClassDecision(pctx)
}

func (t *Transcribe) Execute(ctx context.Context, tctx Context, raw json.RawMessage) ([]Result, error) {
	var in TranscribeInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	m, err := t.pick(in.Model)
	if err != nil {
		return []Result{{Content: err.Error(), IsError: true}}, nil
	}
	path := resolvePath(tctx, in.FilePath)
	info, err := os.Stat(path)
	if err != nil {
		return []Result{{Content: fmt.Sprintf("Error reading audio: %v", err), IsError: true}}, nil
	}
	if info.Size() > maxTranscribeBytes {
		return []Result{{Content: fmt.Sprintf("%s is %d MB, over the 25 MB upload limit; split or compress it first (e.g. ffmpeg -i in.wav -b:a 64k out.mp3).", path, info.Size()>>20), IsError: true}}, nil
	}
	audio, err := os.ReadFile(path)
	if err != nil {
		return []Result{{Content: fmt.Sprintf("Error reading audio: %v", err), IsError: true}}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, mediaTimeout)
	defer cancel()
	text, err := t.client.Transcribe(ctx, api.TranscribeRequest{
		Model: m.ID, Filename: filepath.Base(path), Audio: audio, Language: in.Language, Prompt: in.Prompt,
	})
	if err != nil {
		return mediaErr("Transcription failed", err), nil
	}
	if strings.TrimSpace(text) == "" {
		return []Result{{Content: fmt.Sprintf("%s transcribed to no text (silence, or audio the model could not follow).", path)}}, nil
	}
	return []Result{{Content: text}}, nil
}

// --- Speak ---

type SpeakInput struct {
	Text     string  `json:"text" jsonschema:"description=The text to speak"`
	FilePath string  `json:"file_path,omitempty" jsonschema:"description=Where to save the audio; see the tool description"`
	Model    string  `json:"model,omitempty" jsonschema:"description=Speech model id; see the tool description"`
	Voice    string  `json:"voice,omitempty" jsonschema:"description=Voice name, if the model offers a choice"`
	Speed    float64 `json:"speed,omitempty" jsonschema:"description=Playback speed multiplier (1.0 is normal), if the model accepts it"`
	Format   string  `json:"format,omitempty" jsonschema:"description=Audio format to ask for (e.g. wav or mp3); the server's default when omitted"`
}

type Speak struct {
	mediaTool
	schema *schema.Schema
}

func NewSpeak(client api.MediaClient, models []api.ModelInfo) (*Speak, error) {
	s, err := schema.For[SpeakInput]()
	if err != nil {
		return nil, fmt.Errorf("speak: build schema: %w", err)
	}
	return &Speak{mediaTool{client, models, api.ModelKindSpeech}, s}, nil
}

func (t *Speak) Name() string { return "Speak" }

func (t *Speak) Description(context.Context) (string, error) {
	return "Synthesises speech from text with a text-to-speech model on the configured endpoint and saves it as an " +
		"audio file: a voice reading the text, for narration, announcements and voice messages. For a song use " +
		"GenerateMusic. The file_path extension picks the format (.mp3, .wav, ...) unless format is given. " +
		savePathHint + t.catalog(), nil
}

func (t *Speak) InputSchema() json.RawMessage { return t.schema.Raw }

func (t *Speak) ValidateInput(raw json.RawMessage) error {
	if err := t.schema.Validate(raw); err != nil {
		return err
	}
	var in SpeakInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.Text) == "" {
		return fmt.Errorf("text is required")
	}
	_, err := t.pick(in.Model)
	return err
}

func (t *Speak) PermissionRequest(raw json.RawMessage) permission.PermissionRequest {
	var in SpeakInput
	_ = json.Unmarshal(raw, &in)
	return permission.PermissionRequest{Specifier: in.FilePath}
}

func (t *Speak) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return writeMediaDecision(pctx)
}

func (t *Speak) Execute(ctx context.Context, tctx Context, raw json.RawMessage) ([]Result, error) {
	var in SpeakInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	m, err := t.pick(in.Model)
	if err != nil {
		return []Result{{Content: err.Error(), IsError: true}}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, mediaTimeout)
	defer cancel()
	audio, err := t.client.Speech(ctx, api.SpeechRequest{
		Model: m.ID, Input: in.Text, Voice: in.Voice, Speed: in.Speed,
		ResponseFormat: firstNonBlank(in.Format, audioFormatFor(in.FilePath)),
	})
	if err != nil {
		return mediaErr("Speech synthesis failed", err), nil
	}
	path, note := savePath(tctx, in.FilePath, in.Text, "speech", audio.ContentType)
	if err := saveMedia(path, audio.Data); err != nil {
		return []Result{{Content: fmt.Sprintf("Error saving audio: %v", err), IsError: true}}, nil
	}
	return []Result{{Content: fmt.Sprintf("Saved %s (%s, %d KB) to %s%s", m.ID, audio.ContentType, len(audio.Data)>>10, path, note)}}, nil
}

// --- GenerateMusic ---

type GenerateMusicInput struct {
	Lyrics   string `json:"lyrics" jsonschema:"description=The song's lyrics. Section tags such as [verse] and [chorus] on their own lines help structure it"`
	Style    string `json:"style" jsonschema:"description=The style prompt: genre, mood, instrumentation, tempo (e.g. lo-fi hip hop, mellow, 80 bpm)"`
	FilePath string `json:"file_path,omitempty" jsonschema:"description=Where to save the song; see the tool description"`
	Model    string `json:"model,omitempty" jsonschema:"description=Music model id; see the tool description"`
}

type GenerateMusic struct {
	mediaTool
	schema *schema.Schema
}

func NewGenerateMusic(client api.MediaClient, models []api.ModelInfo) (*GenerateMusic, error) {
	s, err := schema.For[GenerateMusicInput]()
	if err != nil {
		return nil, fmt.Errorf("generate music: build schema: %w", err)
	}
	return &GenerateMusic{mediaTool{client, models, api.ModelKindMusic}, s}, nil
}

func (t *GenerateMusic) Name() string { return "GenerateMusic" }

func (t *GenerateMusic) Description(context.Context) (string, error) {
	return "Generates a song (sung lyrics over music) from lyrics and a style prompt with a music model on the " +
		"configured endpoint and saves it as an audio file. It sings: for spoken words (a welcome message, narration) " +
		"use Speak instead, loading it with ToolSearch if it is not available. Generation takes about as long as the " +
		"song (~20s for a short one). " + savePathHint + t.catalog(), nil
}

func (t *GenerateMusic) InputSchema() json.RawMessage { return t.schema.Raw }

func (t *GenerateMusic) ValidateInput(raw json.RawMessage) error {
	if err := t.schema.Validate(raw); err != nil {
		return err
	}
	var in GenerateMusicInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.Lyrics) == "" && strings.TrimSpace(in.Style) == "" {
		return fmt.Errorf("lyrics or style is required")
	}
	_, err := t.pick(in.Model)
	return err
}

func (t *GenerateMusic) PermissionRequest(raw json.RawMessage) permission.PermissionRequest {
	var in GenerateMusicInput
	_ = json.Unmarshal(raw, &in)
	return permission.PermissionRequest{Specifier: in.FilePath}
}

func (t *GenerateMusic) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return writeMediaDecision(pctx)
}

func (t *GenerateMusic) Execute(ctx context.Context, tctx Context, raw json.RawMessage) ([]Result, error) {
	var in GenerateMusicInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	m, err := t.pick(in.Model)
	if err != nil {
		return []Result{{Content: err.Error(), IsError: true}}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, mediaTimeout)
	defer cancel()
	// Music models share /audio/speech: the lyrics are the input and the style
	// rides in instructions (measured against one gateway's MiniMax-Music3).
	audio, err := t.client.Speech(ctx, api.SpeechRequest{
		Model: m.ID, Input: in.Lyrics, Instructions: in.Style, ResponseFormat: audioFormatFor(in.FilePath),
	})
	if err != nil {
		return mediaErr("Music generation failed", err), nil
	}
	from := in.Style
	if strings.TrimSpace(from) == "" {
		from = in.Lyrics
	}
	path, note := savePath(tctx, in.FilePath, from, "song", audio.ContentType)
	if err := saveMedia(path, audio.Data); err != nil {
		return []Result{{Content: fmt.Sprintf("Error saving audio: %v", err), IsError: true}}, nil
	}
	return []Result{{Content: fmt.Sprintf("Saved %s (%s, %d KB) to %s%s", m.ID, audio.ContentType, len(audio.Data)>>10, path, note)}}, nil
}

// --- GenerateImage ---

type GenerateImageInput struct {
	Prompt   string `json:"prompt" jsonschema:"description=What to draw: subject, style, composition, colours"`
	FilePath string `json:"file_path,omitempty" jsonschema:"description=Where to save the image; see the tool description. With count above 1, -1, -2 ... are added before the extension"`
	Model    string `json:"model,omitempty" jsonschema:"description=Image model id; see the tool description"`
	Size     string `json:"size,omitempty" jsonschema:"description=WIDTHxHEIGHT in pixels (e.g. 1024x1024); the model's default when omitted"`
	Count    int    `json:"count,omitempty" jsonschema:"description=How many images to generate (1-4; default 1)"`
}

type GenerateImage struct {
	mediaTool
	schema *schema.Schema
}

func NewGenerateImage(client api.MediaClient, models []api.ModelInfo) (*GenerateImage, error) {
	s, err := schema.For[GenerateImageInput]()
	if err != nil {
		return nil, fmt.Errorf("generate image: build schema: %w", err)
	}
	return &GenerateImage{mediaTool{client, models, api.ModelKindImage}, s}, nil
}

func (t *GenerateImage) Name() string { return "GenerateImage" }

func (t *GenerateImage) Description(context.Context) (string, error) {
	return "Generates images from a text prompt with an image model on the configured endpoint, saves them, and shows " +
		"them to you so you can check the result before using it. " + savePathHint + t.catalog(), nil
}

func (t *GenerateImage) InputSchema() json.RawMessage { return t.schema.Raw }

func (t *GenerateImage) ValidateInput(raw json.RawMessage) error {
	if err := t.schema.Validate(raw); err != nil {
		return err
	}
	var in GenerateImageInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.Prompt) == "" {
		return fmt.Errorf("prompt is required")
	}
	if in.Count < 0 || in.Count > 4 {
		return fmt.Errorf("count must be between 1 and 4")
	}
	_, err := t.pick(in.Model)
	return err
}

func (t *GenerateImage) PermissionRequest(raw json.RawMessage) permission.PermissionRequest {
	var in GenerateImageInput
	_ = json.Unmarshal(raw, &in)
	return permission.PermissionRequest{Specifier: in.FilePath}
}

func (t *GenerateImage) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return writeMediaDecision(pctx)
}

func (t *GenerateImage) Execute(ctx context.Context, tctx Context, raw json.RawMessage) ([]Result, error) {
	var in GenerateImageInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	m, err := t.pick(in.Model)
	if err != nil {
		return []Result{{Content: err.Error(), IsError: true}}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, mediaTimeout)
	defer cancel()
	images, err := t.client.GenerateImage(ctx, api.ImageRequest{Model: m.ID, Prompt: in.Prompt, Size: in.Size, N: in.Count})
	if err != nil {
		return mediaErr("Image generation failed", err), nil
	}
	var saved []string
	var shown []ResultImage
	var notes []string
	for i, img := range images {
		chosen := in.FilePath
		if chosen != "" && len(images) > 1 {
			ext := filepath.Ext(chosen)
			chosen = strings.TrimSuffix(chosen, ext) + fmt.Sprintf("-%d", i+1) + ext
		}
		path, note := savePath(tctx, chosen, in.Prompt, "image", img.ContentType)
		if err := saveMedia(path, img.Data); err != nil {
			return []Result{{Content: fmt.Sprintf("Error saving image: %v", err), IsError: true}}, nil
		}
		saved = append(saved, path+note)
		// Shown only within the limits Read holds an image to: one over them
		// stays in the conversation and fails every later request.
		switch img.ContentType {
		case "image/png", "image/jpeg", "image/gif", "image/webp":
			if len(img.Data) <= maxImageBytes {
				shown = append(shown, ResultImage{MediaType: img.ContentType, Base64: base64.StdEncoding.EncodeToString(img.Data)})
				continue
			}
		}
		notes = append(notes, fmt.Sprintf("%s is not shown (%s, %d KB)", path, img.ContentType, len(img.Data)>>10))
	}
	msg := fmt.Sprintf("Saved %d image(s) from %s to %s", len(saved), m.ID, strings.Join(saved, ", "))
	if len(notes) > 0 {
		msg += ". " + strings.Join(notes, "; ")
	}
	return []Result{{Content: msg, Images: shown}}, nil
}
