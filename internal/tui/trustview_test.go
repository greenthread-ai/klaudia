package tui

import (
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/hostgate"
	"github.com/greenthread-ai/klaudia/internal/trust"
	"github.com/greenthread-ai/klaudia/pkg/permission"
)

type fakeTrust struct {
	grants  []*trust.Grant
	reports []hostgate.HostReport
	revoked []string
	covers  bool
}

func (f *fakeTrust) Grants() []*trust.Grant         { return f.grants }
func (f *fakeTrust) Reports() []hostgate.HostReport { return f.reports }

// covers is what the fake ledger authorises; nil means nothing is covered.
func (f *fakeTrust) Covers(e []trust.Effect) bool { return f.covers && len(e) > 0 }
func (f *fakeTrust) Revoke(id string) bool {
	f.revoked = append(f.revoked, id)
	return true
}
func (f *fakeTrust) RevokeAll() int { return len(f.grants) }

func trustModel(mode permission.Mode) (*Model, *fakeTrust) {
	ft := &fakeTrust{}
	m := &Model{sess: &Session{PermissionMode: string(mode), Trust: ft}}
	return m, ft
}

// The copy must not claim more than the mechanism delivers. This reads command
// lines; it does not watch syscalls, and a user who believes otherwise will
// calibrate their trust wrongly. Overclaiming here is the most damaging bug
// this feature could ship, and it would never fail a behavioural test.
func TestTrustCopyDoesNotOverclaim(t *testing.T) {
	m, _ := trustModel(permission.ModeAutonomous)
	text := strings.ToLower(stripANSI(m.renderTrust()))

	for _, phrase := range []string{
		"protected", "secure", "safe", "cannot", "prevents", "blocks all",
		"guaranteed", "sandboxed", "isolated",
	} {
		if strings.Contains(text, phrase) {
			t.Errorf("/trust copy contains %q, which claims more than reading command lines delivers:\n%s",
				phrase, text)
		}
	}
	// And it must say the limit out loud, not merely avoid denying it.
	for _, required := range []string{"it can detect", "does not watch"} {
		if !strings.Contains(text, required) {
			t.Errorf("/trust copy never states the limit (%q missing):\n%s", required, text)
		}
	}
}

func TestTrustShowsGrantsAndFindings(t *testing.T) {
	m, ft := trustModel(permission.ModeAutonomous)
	l := trust.NewLedger(trust.NewRoots(t.TempDir(), t.TempDir()))
	g, err := l.Mint(trust.Request{
		Summary: "Install nginx", Reason: "the app runs behind a proxy", Services: []string{"nginx"},
	})
	if err != nil {
		t.Fatal(err)
	}
	ft.grants = []*trust.Grant{g}
	ft.reports = []hostgate.HostReport{
		{Tool: "Bash", Summary: "controls service nginx", Covered: true},
		{Tool: "Bash", Summary: "controls service nginx", Covered: true},
		{Tool: "Bash", Summary: "writes /etc/hosts", Enforced: true},
	}

	out := stripANSI(m.renderTrust())
	for _, want := range []string{
		"enforcing", "Install nginx", "the app runs behind a proxy",
		"covered by an approval (×2)", "writes /etc/hosts", "stopped",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("/trust output missing %q:\n%s", want, out)
		}
	}
}
func TestRevoke(t *testing.T) {
	m, ft := trustModel(permission.ModeAutonomous)
	m.trustCommand([]string{"revoke", "g1"})
	if len(ft.revoked) != 1 || ft.revoked[0] != "g1" {
		t.Errorf("revoked = %v", ft.revoked)
	}
	m.trustCommand([]string{"revoke"})
	if len(ft.revoked) != 1 {
		t.Error("a bare /trust revoke revoked something")
	}
}
func TestTrustWithoutAGateSaysSo(t *testing.T) {
	m := &Model{sess: &Session{PermissionMode: string(permission.ModeAutonomous)}}
	out := stripANSI(m.renderTrust())
	if !strings.Contains(out, "not available") {
		t.Errorf("a session with no gate does not say so:\n%s", out)
	}
}

// /trust used to carry the posture controls, and the posture was where the
// trap lived: `/trust observe` and `/trust off` each dropped the permission
// mode to one that asked before every action, and `/trust upgrade` — the way
// back — returned "Already enforcing" without looking at the mode. The gate
// has no posture now, so none of those subcommands exist and neither does the
// state they could strand a session in.
func TestRetiredTrustSubcommandsAreRejected(t *testing.T) {
	for _, sub := range []string{"upgrade", "observe", "off", "downgrade"} {
		t.Run(sub, func(t *testing.T) {
			m, _ := trustModel(permission.ModeAutonomous)
			m.trustCommand([]string{sub})
			if got := permission.Mode(m.sess.PermissionMode); got != permission.ModeAutonomous {
				t.Errorf("/trust %s changed the mode to %q", sub, got)
			}
			if out := stripANSI(m.transcript.String()); !strings.Contains(out, "revoke") {
				t.Errorf("/trust %s did not report the real usage:\n%s", sub, out)
			}
		})
	}
}

// Bypass is the only way left to stop the gate checking, so /trust must say
// so when it is on rather than reporting an enforcing guardrail that the mode
// is in fact skipping.
func TestTrustReportsBypassHonestly(t *testing.T) {
	m, _ := trustModel(permission.ModeBypassPermissions)
	out := stripANSI(m.renderTrust())
	if !strings.Contains(out, "BYPASSED") {
		t.Errorf("/trust does not say the gate is being skipped:\n%s", out)
	}

	m, _ = trustModel(permission.ModeAutonomous)
	if out := stripANSI(m.renderTrust()); !strings.Contains(out, "enforcing") {
		t.Errorf("/trust does not report enforcing in autonomous:\n%s", out)
	}
}

// Leaving plan mode must land on autonomous. acceptEdits was the old target
// for an approved plan and default for `/plan off`; both left the session
// asking before commands while the gate was already enforcing, which was the
// exact state `/trust upgrade` refused to repair.
func TestLeavingPlanModeLandsOnAutonomous(t *testing.T) {
	m, _ := trustModel(permission.ModePlan)
	m.handleSlash("/plan off")
	if got := permission.Mode(m.sess.PermissionMode); got != permission.ModeAutonomous {
		t.Errorf("/plan off left the session in %q, want autonomous", got)
	}
}
