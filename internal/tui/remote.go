// TUI integration for the /remote-control feature. Owns:
//
//   - the device-code login flow (kicked off in a goroutine; the URL +
//     code are posted to the TUI event channel so the Bubble Tea loop
//     stays responsive),
//   - the connected `*remotecontrol.Session` reference,
//   - the goroutine that drains Session.Inputs() and re-enters the TUI
//     by posting remoteInputMsg events (so server-sent user messages
//     and slash commands enter the same codepath as keyboard input).
//
// The actual emit/approver wrap-up happens in startTurn (tui.go) by
// asking m.remote when it builds the goroutine's emit + approver.

package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/greenthread/klaudia/internal/api"
	"github.com/greenthread/klaudia/internal/remotecontrol"
)

// Message types delivered to the TUI when the remote-control flow makes
// progress in the background.

type remoteCodeMsg struct {
	verificationURL string
	userCode        string
}

type remoteLoggedInMsg struct {
	cred *remotecontrol.Credential
}

type remoteOpenedMsg struct {
	sess     *remotecontrol.Session
	provider api.Provider
	models   []remotecontrol.Model
}

type remoteFailedMsg struct{ err error }

type remoteClosedMsg struct{}

type remoteInputMsg struct{ in remotecontrol.Input }

type remoteModelsRefreshedMsg struct {
	models []remotecontrol.Model
	err    error
}

// startRemoteControl kicks off device-code login (if needed) and then
// opens the WS. All work happens in goroutines; results post back to
// m.events as one of the messages above.
func (m *Model) startRemoteControl(baseURL string) {
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("KLAUDIA_AI_CONSOLE_URL"))
	}
	if baseURL == "" {
		m.events <- remoteFailedMsg{err: fmt.Errorf("ai-console URL not configured; set KLAUDIA_AI_CONSOLE_URL or pass --ai-console")}
		return
	}

	go func() {
		ctx := m.ctx
		// Re-use a saved credential if we have one.
		cred, _ := remotecontrol.LoadCredential()
		if cred == nil || cred.BaseURL != strings.TrimRight(baseURL, "/") {
			c, err := remotecontrol.Login(ctx, remotecontrol.Config{
				BaseURL: baseURL,
				Client:  "klaudia/" + clientVersionFallback(),
			}, func(url, code string) {
				m.events <- remoteCodeMsg{verificationURL: url, userCode: code}
			})
			if err != nil {
				m.events <- remoteFailedMsg{err: err}
				return
			}
			if err := c.Save(); err != nil {
				// Non-fatal: we still have the secret in memory.
				m.events <- remoteFailedMsg{err: fmt.Errorf("warning: could not persist credential: %w", err)}
			}
			cred = c
		}
		m.events <- remoteLoggedInMsg{cred: cred}

		// Open the WS.
		title := defaultSessionTitle()
		sess, err := remotecontrol.Open(ctx, remotecontrol.SessionConfig{
			BaseURL: cred.BaseURL,
			Secret:  cred.Secret,
			Title:   title,
		})
		if err != nil {
			m.events <- remoteFailedMsg{err: err}
			return
		}

		// Send initial meta so the UI shows cwd / git_branch / model.
		meta := remotecontrol.SessionMetaPayload{
			PermissionMode: string(m.currentMode()),
		}
		if m.sess != nil {
			meta.Model = m.sess.ResolvedModel
			if meta.Model == "" {
				meta.Model = m.sess.Model
			}
			meta.GitBranch = m.sess.GitBranch
			meta.Cwd = m.sess.CWD
		}
		sess.SendMeta(meta, title)

		// Build the OpenAI shim provider pointing at ai-console so the
		// agent loop's next turn routes through the user's account.
		provider := api.NewOpenAIProvider(strings.TrimRight(cred.BaseURL, "/")+"/v1", cred.Secret)

		// Pull the model catalog up-front; the /model picker reads it.
		catCtx, catCancel := context.WithTimeout(ctx, 15*time.Second)
		models, modelsErr := remotecontrol.ListModels(catCtx, cred)
		catCancel()
		if modelsErr != nil {
			m.events <- remoteFailedMsg{err: fmt.Errorf("warning: model catalog: %w", modelsErr)}
		}

		m.events <- remoteOpenedMsg{sess: sess, provider: provider, models: models}

		// Drain inputs back to the TUI event channel.
		go func(s *remotecontrol.Session) {
			for in := range s.Inputs() {
				m.events <- remoteInputMsg{in: in}
			}
			m.events <- remoteClosedMsg{}
		}(sess)
	}()
}

// refreshRemoteModels re-fetches /v1/models in the background. Called
// when the user picks /model to make sure the list is fresh.
func (m *Model) refreshRemoteModels() {
	if m.remote == nil {
		return
	}
	go func() {
		cred, _ := remotecontrol.LoadCredential()
		if cred == nil {
			return
		}
		ctx, cancel := context.WithTimeout(m.ctx, 15*time.Second)
		defer cancel()
		models, err := remotecontrol.ListModels(ctx, cred)
		m.events <- remoteModelsRefreshedMsg{models: models, err: err}
	}()
}

// stopRemoteControl tears down an open session.
func (m *Model) stopRemoteControl() {
	if m.remote == nil {
		return
	}
	_ = m.remote.Close()
	m.remote = nil
}

func defaultSessionTitle() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	parts := strings.Split(cwd, string(os.PathSeparator))
	if len(parts) == 0 {
		return cwd
	}
	return parts[len(parts)-1]
}

// clientVersionFallback returns a short version banner. Uses the
// internal/version package when available; otherwise "dev".
func clientVersionFallback() string {
	// Avoid a hard import of internal/version here to keep the package
	// graph thin; the version is mostly cosmetic on the server side.
	return "dev"
}
