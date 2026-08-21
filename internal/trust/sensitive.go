package trust

import (
	"strings"

	"github.com/greenthread-ai/klaudia/internal/native/bashparser"
)

// Using a credential is not the same as disclosing one.
//
// `ssh -i ~/.ssh/deploy_key host`, `curl --cert client.pem`, `aws --profile x`
// all name credential material on the command line, but the secret goes to the
// program that needs it and never enters the conversation. Prompting for those
// would fire on ordinary work several times an hour, which is how a protection
// gets turned off.
//
// What we do care about is a credential's *contents* reaching somewhere they
// can be read back — printed to the transcript, copied elsewhere, or altered.
// That is decided by the verb, not by the path, so the exemption below is
// narrow: it applies only when the path is the value of a flag whose documented
// job is to consume a key.

// credentialInputFlags are flags whose value is a credential the program will
// use rather than reveal. Keyed by program so that a coincidental `-i` on some
// other command does not inherit the exemption.
var credentialInputFlags = map[string]map[string]bool{
	"ssh":              {"-i": true, "-F": true},
	"scp":              {"-i": true, "-F": true},
	"sftp":             {"-i": true, "-F": true},
	"rsync":            {"-e": true},
	"ssh-add":          {"": true}, // every operand is a key being loaded into the agent
	"ssh-agent":        {"": true},
	"git":              {"--git-dir": true, "-c": true},
	"curl":             {"--cert": true, "--key": true, "--cacert": true, "-E": true, "--netrc-file": true},
	"wget":             {"--certificate": true, "--private-key": true, "--ca-certificate": true},
	"openssl":          {"-key": true, "-cert": true, "-CAfile": true, "-inkey": true},
	"docker":           {"--tlscert": true, "--tlskey": true, "--tlscacert": true},
	"kubectl":          {"--kubeconfig": true, "--client-key": true, "--client-certificate": true},
	"helm":             {"--kubeconfig": true},
	"terraform":        {"--kubeconfig": true},
	"aws":              {"--ca-bundle": true},
	"gpg":              {"--keyring": true, "--secret-keyring": true},
	"ansible":          {"--private-key": true, "--key-file": true, "--vault-password-file": true},
	"ansible-playbook": {"--private-key": true, "--key-file": true, "--vault-password-file": true},
	"borg":             {"--keyfile": true},
	"restic":           {"--password-file": true},
	"mysql":            {"--defaults-file": true},
	"psql":             {"--service-file": true},
}

// credentialUseExemptions returns the indices of arguments that are credentials
// being used rather than disclosed, and must not raise an effect.
func credentialUseExemptions(prog string, args []bashparser.Word) map[int]bool {
	flags, ok := credentialInputFlags[prog]
	if !ok {
		return nil
	}
	out := map[int]bool{}
	if flags[""] {
		// Whole-command exemption: every operand is a key being handed over.
		for i, w := range args {
			if w.Literal && strings.HasPrefix(w.Text, "-") {
				continue
			}
			out[i] = true
		}
		return out
	}
	for i, w := range args {
		if !w.Literal {
			continue
		}
		t := w.Text
		if eq := strings.IndexByte(t, '='); eq > 0 && flags[t[:eq]] {
			out[i] = true
			continue
		}
		if flags[t] && i+1 < len(args) {
			out[i+1] = true
		}
	}
	return out
}
