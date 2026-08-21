package trust

import (
	"encoding/json"
	"strings"
)

// Tool calls reach this machine through more doors than Bash.
//
// A zone model that only reads command lines would let `Write` put a file in
// /etc and `Read` pull ~/.ssh/id_rsa into the model's context, which are the two
// most likely ways an agent actually crosses the boundary. So every tool that
// names a path is classified, using the same zones as the shell reader.
//
// The read side deserves its own note, because it is the one place where the
// same action is fine through one door and not through another. `ssh` opening
// its own private key is invisible to us and must stay that way. The `Read`
// tool opening the same file is the model putting a secret into its context and
// then, very likely, into a transcript. They are distinguishable precisely
// because they arrive differently, and this function is the door we can see.

// ClassifyToolCall reports what a tool call does. input is the raw JSON the
// model supplied. cwd is the directory the tool will run relative to.
//
// An unrecognised tool yields no effects: this decides what is protected, not
// what is permitted, and inventing effects for tools we have not modelled would
// prompt on TodoWrite.
func ClassifyToolCall(name string, input []byte, cwd string, roots Roots) Assessment {
	switch name {
	case "Bash":
		var in struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(input, &in); err != nil || strings.TrimSpace(in.Command) == "" {
			return Assessment{}
		}
		return ClassifyCommandIn(in.Command, cwd, roots)

	case "Write", "Edit", "NotebookEdit":
		var in struct {
			FilePath     string `json:"file_path"`
			NotebookPath string `json:"notebook_path"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return Assessment{}
		}
		p := firstNonEmpty(in.FilePath, in.NotebookPath)
		return pathAssessment(name, KindWrite, p, cwd, roots)

	case "Read":
		var in struct {
			FilePath string `json:"file_path"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return Assessment{}
		}
		// Reading the operating system is ordinary debugging and must never
		// prompt; only credential material is a concern on the way in.
		as := pathAssessment(name, KindRead, in.FilePath, cwd, roots)
		return keepOnly(as, ZoneSensitive)

	case "Grep", "Glob":
		// A search rooted in a credential directory is a fishing expedition; a
		// search rooted anywhere else is not our business, whatever it matches.
		var in struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(input, &in); err != nil || in.Path == "" {
			return Assessment{}
		}
		as := pathAssessment(name, KindRead, in.Path, cwd, roots)
		return keepOnly(as, ZoneSensitive)
	}
	return Assessment{}
}

func pathAssessment(tool string, kind Kind, path, cwd string, roots Roots) Assessment {
	if strings.TrimSpace(path) == "" {
		return Assessment{}
	}
	c := &cmdCtx{roots: roots, cwd: canonical(cwd), cwdOK: cwd != "", prog: tool, target: LocalTarget()}
	resolved := roots.Resolve(c.cwd, path)
	if resolved == "" {
		// A relative path with no working directory to resolve it against. The
		// tool will resolve it somehow; we cannot say where.
		return Assessment{Effects: []Effect{{
			Zone: ZoneProject, Kind: kind, Target: LocalTarget(),
			Res:      Resource{Class: "path"},
			Evidence: tool + " " + path, Certain: false,
		}}}
	}
	e := c.effect(kind, "path", resolved, tool+" "+path, true)
	if e.Zone == ZoneSensitive {
		if kind == KindRead {
			e.Kind = KindCredDisclose
		} else {
			e.Kind = KindCredModify
		}
	}
	return Assessment{Effects: []Effect{e}}
}

// keepOnly drops every effect below a zone. Used for the read paths, where the
// whole point is that most reads are free.
func keepOnly(as Assessment, min Zone) Assessment {
	var out []Effect
	for _, e := range as.Effects {
		if e.Zone >= min {
			out = append(out, e)
		}
	}
	as.Effects = out
	return as
}
