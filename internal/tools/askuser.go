package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/schema"
)

// AskUserQuestionInput is the AskUserQuestion tool's input.
type AskUserQuestionInput struct {
	Question string      `json:"question" jsonschema:"description=The question to ask the user"`
	Options  []AskOption `json:"options" jsonschema:"description=The choices to present (at least one)"`
}

// AskUserQuestion lets the model ask the user a multiple-choice question and
// continue based on the answer. Requires an interactive frontend; in headless
// mode it reports that no user is available so the model proceeds on its own.
type AskUserQuestion struct {
	schema *schema.Schema
}

// NewAskUserQuestion constructs the tool.
func NewAskUserQuestion() (*AskUserQuestion, error) {
	s, err := schema.For[AskUserQuestionInput]()
	if err != nil {
		return nil, fmt.Errorf("askuserquestion: build schema: %w", err)
	}
	return &AskUserQuestion{schema: s}, nil
}

func (a *AskUserQuestion) Name() string { return "AskUserQuestion" }

func (a *AskUserQuestion) Description(context.Context) (string, error) {
	return "Ask the user a multiple-choice question when you need a decision only they can make " +
		"(ambiguous requirements, a fork in approach). Provide a clear question and 2-4 options. " +
		"Use sparingly — prefer acting on reasonable defaults. Returns the user's chosen label.", nil
}

func (a *AskUserQuestion) InputSchema() json.RawMessage { return a.schema.Raw }

func (a *AskUserQuestion) ValidateInput(raw json.RawMessage) error {
	if err := a.schema.Validate(raw); err != nil {
		return err
	}
	var in AskUserQuestionInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if len(in.Options) == 0 {
		return fmt.Errorf("at least one option is required")
	}
	return nil
}

// PermissionRequest: asking a question is harmless; always allowed.
func (a *AskUserQuestion) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}

func (a *AskUserQuestion) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return allowAlways(pctx)
}

func (a *AskUserQuestion) Execute(ctx context.Context, tctx Context, raw json.RawMessage) ([]Result, error) {
	var in AskUserQuestionInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	// A model that escaped its JSON twice would otherwise be shown to the user
	// verbatim, backslashes and all. See unescapeDisplayText.
	in.Question = unescapeDisplayText(in.Question)
	for i := range in.Options {
		in.Options[i].Label = unescapeDisplayText(in.Options[i].Label)
		in.Options[i].Description = unescapeDisplayText(in.Options[i].Description)
	}
	if tctx.Ask == nil {
		return []Result{{Content: "No interactive user is available to answer (headless mode). " +
			"Proceed using your best judgment.", IsError: true}}, nil
	}
	answer, err := tctx.Ask.Ask(ctx, in.Question, in.Options)
	if err != nil {
		return []Result{{Content: "Could not get an answer: " + err.Error(), IsError: true}}, nil
	}
	return []Result{{Content: "The user chose: " + answer}}, nil
}
