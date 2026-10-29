package trust

import "testing"

// The media tools write where the model says and Transcribe sends a file off
// the machine, so they take Write's and Read's classification respectively:
// the project is free, the operating system and credentials are not.
func TestMediaToolCalls(t *testing.T) {
	roots := NewRoots("/home/u", "/repo")
	for _, tc := range []struct {
		tool, input string
		want        Zone
		none        bool
	}{
		{tool: "GenerateImage", input: `{"prompt":"x","file_path":"assets/fox.png"}`, want: ZoneProject},
		{tool: "Speak", input: `{"text":"x","file_path":"/etc/motd.wav"}`, want: ZoneHost},
		{tool: "GenerateMusic", input: `{"lyrics":"x","file_path":"/home/u/.ssh/song.wav"}`, want: ZoneSensitive},
		// No path: the working directory, which needs no effect.
		{tool: "Speak", input: `{"text":"x"}`, none: true},
		// Transcribing a credential is Read's case; anything else is free.
		{tool: "Transcribe", input: `{"file_path":"/home/u/.ssh/id_rsa"}`, want: ZoneSensitive},
		{tool: "Transcribe", input: `{"file_path":"/var/log/call.wav"}`, none: true},
	} {
		as := ClassifyToolCall(tc.tool, []byte(tc.input), "/repo", roots)
		if tc.none {
			if len(as.Effects) != 0 {
				t.Errorf("%s %s: effects %+v, want none", tc.tool, tc.input, as.Effects)
			}
			continue
		}
		if len(as.Effects) != 1 || as.Effects[0].Zone != tc.want {
			t.Errorf("%s %s: effects %+v, want one in %s", tc.tool, tc.input, as.Effects, tc.want)
		}
	}
}
