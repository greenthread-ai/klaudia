package tui

import (
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/trust"
)

type fakeTrust struct {
	policy  agent.HostPolicy
	grants  []*trust.Grant
	reports []agent.HostReport
	revoked []string
}

func (f *fakeTrust) Policy() agent.HostPolicy     { return f.policy }
func (f *fakeTrust) SetPolicy(p agent.HostPolicy) { f.policy = p }
func (f *fakeTrust) Grants() []*trust.Grant       { return f.grants }
func (f *fakeTrust) Reports() []agent.HostReport  { return f.reports }
func (f *fakeTrust) Revoke(id string) bool {
	f.revoked = append(f.revoked, id)
	return true
}
func (f *fakeTrust) RevokeAll() int { return len(f.grants) }

func trustModel(policy agent.HostPolicy, mode permission.Mode) (*Model, *fakeTrust) {
	ft := &fakeTrust{policy: policy}
	m := &Model{sess: &Session{PermissionMode: string(mode), Trust: ft}}
	return m, ft
}

// The copy must not claim more than the mechanism delivers. This reads command
// lines; it does not watch syscalls, and a user who believes otherwise will
// calibrate their trust wrongly. Overclaiming here is the most damaging bug
// this feature could ship, and it would never fail a behavioural test.
func TestTrustCopyDoesNotOverclaim(t *testing.T) {
	m, _ := trustModel(agent.HostEnforce, permission.ModeAutonomous)
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
	m, ft := trustModel(agent.HostEnforce, permission.ModeAutonomous)
	l := trust.NewLedger(trust.NewRoots(t.TempDir(), t.TempDir()))
	g, err := l.Mint(trust.Request{
		Summary: "Install nginx", Reason: "the app runs behind a proxy", Services: []string{"nginx"},
	})
	if err != nil {
		t.Fatal(err)
	}
	ft.grants = []*trust.Grant{g}
	ft.reports = []agent.HostReport{
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

// Observe mode is the migration state. Without a view it is indistinguishable
// from off, so /trust has to say what it is and how to leave it.
func TestObserveModeExplainsItself(t *testing.T) {
	m, _ := trustModel(agent.HostObserve, permission.ModeDefault)
	out := stripANSI(m.renderTrust())
	if !strings.Contains(out, "nothing is stopped") {
		t.Errorf("observe mode does not say it changes no decisions:\n%s", out)
	}
	if !strings.Contains(out, "/trust upgrade") {
		t.Errorf("observe mode does not say how to leave it:\n%s", out)
	}
}

func TestUpgradeSwitchesToEnforcingAndAutonomous(t *testing.T) {
	m, ft := trustModel(agent.HostObserve, permission.ModeDefault)
	m.trustCommand([]string{"upgrade"})
	if ft.policy != agent.HostEnforce {
		t.Errorf("policy = %v, want enforce", ft.policy)
	}
	if m.sess.PermissionMode != string(permission.ModeAutonomous) {
		t.Errorf("mode = %q, want autonomous", m.sess.PermissionMode)
	}
}

// Turning the guardrail off while autonomous would leave nothing at all between
// the model and the machine.
func TestTurningTheGuardrailOffLeavesAutonomous(t *testing.T) {
	m, ft := trustModel(agent.HostEnforce, permission.ModeAutonomous)
	m.trustCommand([]string{"off"})
	if ft.policy != agent.HostOff {
		t.Errorf("policy = %v, want off", ft.policy)
	}
	if m.sess.PermissionMode == string(permission.ModeAutonomous) {
		t.Error("still autonomous with the guardrail off — nothing would be checking anything")
	}
}

func TestAutonomousRefusedWithoutAnEnforcingGuardrail(t *testing.T) {
	for _, policy := range []agent.HostPolicy{agent.HostObserve, agent.HostOff} {
		m, _ := trustModel(policy, permission.ModeDefault)
		if why := m.modeRefusal(permission.ModeAutonomous); why == "" {
			t.Errorf("autonomous was allowed with the guardrail %v", policy)
		}
		if why := m.modeRefusal(permission.ModePlan); why != "" {
			t.Errorf("plan mode was refused: %q", why)
		}
	}
	m := &Model{sess: &Session{PermissionMode: "default"}} // no controller at all
	if why := m.modeRefusal(permission.ModeAutonomous); why == "" {
		t.Error("autonomous was allowed with no guardrail at all")
	}
}

func TestRevoke(t *testing.T) {
	m, ft := trustModel(agent.HostEnforce, permission.ModeAutonomous)
	m.trustCommand([]string{"revoke", "g1"})
	if len(ft.revoked) != 1 || ft.revoked[0] != "g1" {
		t.Errorf("revoked = %v", ft.revoked)
	}
	m.trustCommand([]string{"revoke"})
	if len(ft.revoked) != 1 {
		t.Error("a bare /trust revoke revoked something")
	}
}

// Rules from the old model still work, and the user should be able to find out
// they have a pile of them.
func TestLegacyRulesAreListed(t *testing.T) {
	m, _ := trustModel(agent.HostEnforce, permission.ModeAutonomous)
	m.sessionAllow = []permission.Rule{{Tool: "Bash", Specifier: "git status:*"}}
	m.sessionDeny = []permission.Rule{{Tool: "WebFetch"}}
	out := stripANSI(m.renderTrust())
	for _, want := range []string{"per-command model", "allow Bash(git status:*)", "deny WebFetch"} {
		if !strings.Contains(out, want) {
			t.Errorf("/trust output missing %q:\n%s", want, out)
		}
	}
}

func TestTrustWithoutAGateSaysSo(t *testing.T) {
	m := &Model{sess: &Session{PermissionMode: "default"}}
	out := stripANSI(m.renderTrust())
	if !strings.Contains(out, "not available") {
		t.Errorf("a session with no gate does not say so:\n%s", out)
	}
}
