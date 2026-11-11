package sandhold

import (
	"context"
	"net/http"
)

// advise.go is the node's advisor (GET and POST /v1/advise): three questions
// (what will run, how much the code is trusted, what it must reach) and,
// for a set of answers, the setup the node advises, with the reason for
// each choice. The node is the source of truth: the questions, choices and
// advice are its own words, so they read the same here, in its portal and
// in its command line. Every string is made plain (control characters
// removed) before Observer shows it.

// AdviseChoice is one answer to a question.
type AdviseChoice struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Means is the node's short gloss ("runs once and ends").
	Means string `json:"means,omitempty"`
	// Hosts are what a reach choice opens ("registry.npmjs.org").
	Hosts []string `json:"hosts,omitempty"`
}

// AdviseQuestion is one of the node's questions.
type AdviseQuestion struct {
	ID      string         `json:"id"`
	Ask     string         `json:"ask"`
	Choices []AdviseChoice `json:"choices"`
}

// AdviseReach is the reach answer: package sources by choice id and extra
// host names.
type AdviseReach struct {
	Packages []string `json:"packages,omitempty"`
	Hosts    []string `json:"hosts,omitempty"`
}

// AdviseRequest is a set of answers (POST /v1/advise).
type AdviseRequest struct {
	Runs  string      `json:"runs"`
	Trust string      `json:"trust"`
	Reach AdviseReach `json:"reach"`
}

// AdviseWhy is the reason for one choice of the advice.
type AdviseWhy struct {
	About   string `json:"about"`
	Choice  string `json:"choice"`
	Because string `json:"because"`
}

// Advice is the node's answer: the isolation and network it advises, why,
// what the choice gives up, and the command that starts it.
type Advice struct {
	Isolation      string      `json:"isolation"`
	IsolationMeans string      `json:"isolationMeans,omitempty"`
	Network        string      `json:"network"`
	RunsOn         string      `json:"runsOn,omitempty"`
	Command        string      `json:"command,omitempty"`
	Why            []AdviseWhy `json:"why,omitempty"`
	Limits         []string    `json:"limits,omitempty"`
	Notes          []string    `json:"notes,omitempty"`
}

// AdviseQuestions asks the node for its questions.
func (c *Client) AdviseQuestions(ctx context.Context) ([]AdviseQuestion, error) {
	var out struct {
		Questions []AdviseQuestion `json:"questions"`
	}
	if err := c.Do(ctx, http.MethodGet, "/v1/advise", nil, &out); err != nil {
		return nil, err
	}
	for i := range out.Questions {
		q := &out.Questions[i]
		q.ID, q.Ask = PlainLine(q.ID), PlainLine(q.Ask)
		for j := range q.Choices {
			ch := &q.Choices[j]
			ch.ID, ch.Label, ch.Means = PlainLine(ch.ID), PlainLine(ch.Label), PlainLine(ch.Means)
			plainAll(ch.Hosts)
		}
	}
	return out.Questions, nil
}

// Advise sends a set of answers and returns the node's advice. An answer
// the node does not know comes back as its *APIError (why, and the fix).
func (c *Client) Advise(ctx context.Context, in AdviseRequest) (Advice, error) {
	var a Advice
	if err := c.Do(ctx, http.MethodPost, "/v1/advise", in, &a); err != nil {
		return Advice{}, err
	}
	a.Isolation, a.IsolationMeans, a.Network = PlainLine(a.Isolation), PlainLine(a.IsolationMeans), PlainLine(a.Network)
	a.RunsOn, a.Command = PlainLine(a.RunsOn), PlainLine(a.Command)
	for i := range a.Why {
		w := &a.Why[i]
		w.About, w.Choice, w.Because = PlainLine(w.About), PlainLine(w.Choice), PlainLine(w.Because)
	}
	plainAll(a.Limits)
	plainAll(a.Notes)
	return a, nil
}

func plainAll(ss []string) {
	for i := range ss {
		ss[i] = PlainLine(ss[i])
	}
}
