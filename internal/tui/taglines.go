package tui

import "math/rand/v2"

// taglines are the rotating subtitles shown after "✦ Klaudia" at startup —
// a small gimmick picked once per launch. Keep them short, SFW, and on-brand
// (Go, no-JS, the terminal). They read as "Klaudia, <tagline>".
var taglines = []string{
	"your preferred coding agent",
	"the better coding agent",
	"your slightly unhinged coding agent",
	"powered by spite and pure Go",
	"your terminal's new best friend",
	"an agent of chaos (the good kind)",
	"compiling vibes since 2026",
	"the pair programmer who never sleeps",
	"30% more confidence than competence",
	"just regexes in a trench coat",
	"your coding agent, allegedly",
	"doing the needful, my man",
	"your overqualified rubber duck",
	"vibes at scale",
	"turning coffee into commits",
	"your goblin in the machine",
	"shipping bugs at the speed of thought",
	"a single static binary with big dreams",
	"purely Go, mostly harmless",
	"yak-shaving as a service",
	"your CLI companion in crime",
	"now featuring opinions",
	"deleting node_modules so you don't have to",
	"your keyboard's worst enabler",
	"vibe-coding, professionally",
	"here to help, occasionally",
	"the agent your linter warned you about",
	"running on caffeine it can't even drink",
	"your unreasonably eager coding agent",
	"committing crimes (and code)",
	"no JavaScript was harmed in the making",
	"your local agent of questionable judgment",
}

// randomTagline returns a tagline at random (math/rand/v2 is auto-seeded).
func randomTagline() string {
	return taglines[rand.IntN(len(taglines))]
}
