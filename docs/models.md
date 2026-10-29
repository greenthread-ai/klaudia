# Models an endpoint serves

An OpenAI-compatible endpoint can serve more than chat models. One gateway
Klaudia was measured against lists eight models from `GET /v1/models`: four chat
models (one of them abliterated), a text-to-speech model, a speech-recognition
model, an image model and a music model. This doc records how Klaudia reads that
list, what it does with the models that cannot chat, and what it asks before
using them.

## Discovery

The OpenAI spec gives a listed model nothing but an `id`. Servers that host more
than chat add their own fields, and `ModelInfo` keeps what they say:
`supported_endpoints`, `model_type` (or Together's `type`),
`architecture.input_modalities`/`output_modalities`, `supported_parameters`,
`description` and `context_length`.

Each model gets a **kind** (`pkg/api/modelkind.go`): chat, image, tts, stt,
music or embedding. Signals in order of authority:

1. The declared type, when it is one Klaudia knows. It is the only signal that
   tells music from speech, which share `POST /audio/speech`.
2. `supported_endpoints`: the route the server enforces (a chat request to the
   TTS model was refused with `400 model_not_supported`).
3. The modalities.
4. The id, for servers that list bare ids (OpenAI's own: `whisper-1`, `tts-1`,
   `gpt-image-1`).

A model nothing places is **unknown**, which counts as chat. Hiding a model
the user may need because its listing was unreadable is worse than offering one
that fails with a clear error.

What changes with the kind:

- `/model` and its Tab completion offer chat models only. Typing a non-chat id
  still sets it, with a warning that the next turn will be refused.
- `/doctor` breaks the list down by kind and warns when the configured model
  is not a chat model.
- The cache (`~/.klaudia/cache/models-*.json`) stores all of it. Its format
  is versioned, and a file from before kinds existed is treated as stale.

### Sizing

`context_length` is the context window, and it beats the offline table like any
provider figure (`api.Resolve`). Before this, every model on such a gateway ran
at the unknown-model floor with no window at all.

`top_provider.context_length` is **not read**. The gateway reported 8192 there
for models whose `context_length` said 65536–131072, and enforced neither
limit: a ~12k-token prompt and `max_tokens: 20000` were both accepted. A figure
that describes nothing the server does would only shrink the window Klaudia
compacts against. No field gives the output cap, so it stays at the floor
unless `maxTokens` is configured.

### Reasoning

Reasoning models on vLLM stream their thinking as `delta.reasoning` (DeepSeek
and older vLLM as `reasoning_content`). Klaudia counts it but does not keep it:
nothing renders thinking yet, and sending it back as content would put the
model's scratch work into its own history as if it had said it. A reply that is
all reasoning, as when a model spends its whole budget thinking, gets a
placeholder that says so instead of a bare "no content".

## Media tools

The models that cannot chat become tools the chat model calls
(`internal/tools/media.go`, HTTP in `pkg/api/media.go`):

| Tool | Route | Result |
|---|---|---|
| `Transcribe` | `POST /audio/transcriptions` | the text |
| `Speak` | `POST /audio/speech` | an audio file |
| `GenerateMusic` | `POST /audio/speech`, lyrics as `input`, style as `instructions` | an audio file |
| `GenerateImage` | `POST /images/generations`, always `b64_json` | an image file, also shown to the model |

A tool is registered only when the endpoint lists a model of its kind, so the
Anthropic backend offers none. Each description lists the models and the
parameters they accept. With one model of a kind, `model` may be omitted; with
several, it is required, because picking for the model would hide that a choice
existed.

**Outputs are files** because audio cannot go back into the conversation, and
an image the user cannot keep is no use to them. Where a file goes is the
model's decision. The description tells it to set `file_path` to the place the
work that uses the file lives. Left out, the file is saved in the working
directory under a name made from the request, and an existing file is never
overwritten. The extension in `file_path` chooses the audio format to ask for,
and if the bytes still disagree with the name, the extension is corrected and
the result says so. Before that fix, a WAV was saved as `welcome.mp3`.

**They are loaded, not deferred.** They began behind ToolSearch, and a model
asked for a header image, a spoken welcome and its transcript did not search for
them in one run of two. It pip-installed Pillow, gTTS and SpeechRecognition into
the system Python with `--break-system-packages`, drew the image with PIL, and
sent the audio to Google's speech API. ToolSearch does not name what it holds,
so a model does not look for a capability it has no reason to expect. The four
definitions cost ~1.2k tokens a request (measured: 4.9 KB), and only on an
endpoint that serves these models. Loaded, they were used in every run since.

## Permission

The endpoint decides **what a key may call**. The gateway leaves a model a key
cannot use out of `GET /v1/models`, so the list is the entitlement, and a call to
an unlisted model fails with the server's own `model_not_found`. Klaudia does
not keep a second copy of that rule.

The user decides **what they want called**, through `[models] deny`:

```toml
[models]
deny = ["*-Uncensored", "black-forest-labs/*"]
```

Entries are ids or patterns where `*` matches any run of characters, slashes
included, compared case-insensitively. A denied model is left out of `/model`
and of the media tools' catalog, refused as `--model` or the configured model
at startup, and refused by `/model <id>`. A project config may add entries but
never remove the user's, because denying is tightening (`config.projectSafe`).
This is also the opt-in for abliterated models until a gateway marks them in a
field: deny them in the user config, and remove the entry to use one.

**Using a model never asks**, in any mode. The tools take the same path rules
as `Write` (`trust.ClassifyToolCall`): a `file_path` outside the project goes
through the host gate, and `Transcribe` on a credential file is treated like
`Read` on one. Plan mode refuses all four, because each one either writes a file
or sends a file off the machine. Beyond that, generation runs like any other
project work. Klaudia has no standing "always allow", so a prompt would come on
every image and fail every unattended run, and a model you do not want used is
better kept out of the catalog than asked about each time.

That is deliberate for **headless runs** in particular. A script that runs
`klaudia -p` has already decided to let the agent work unattended, the same
decision that means no questions are asked there. Generating media on that
endpoint is part of the work it delegated. The files land in the working tree,
which `git status` shows. To keep a model out of scripted runs, deny it.

## Where it lives

- `pkg/api/models.go`, `modelkind.go`, `modelcache.go`: listing, kinds, cache.
- `pkg/api/openai_stream.go`: the reasoning count and placeholder.
- `pkg/api/media.go`: `MediaClient`, implemented by the OpenAI-compatible
  provider only.
- `internal/tools/media.go`: the four tools; `WithMedia` registers them.
- `internal/trust/toolcall.go`: their path classification.
- `internal/config` `Models`: the deny-list. `internal/cli` `mediaCatalog`
  builds the tools' catalog from the cache, fetching only when there is none.
