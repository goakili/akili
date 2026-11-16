// Copyright (c) 2026 Jonas Kaninda.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

// Package autonomy decides when a call that needs approval may run without one: grants, change windows and runbooks.
package autonomy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/models"
)

// ValidateConstraint rejects an empty constraint or one that mixes kinds.
func ValidateConstraint(c models.Constraint) error {
	kinds := 0
	if len(c.Equals) > 0 {
		kinds++
		if !json.Valid(c.Equals) {
			return errors.New("equals is not valid JSON")
		}
	}
	if c.Glob != "" {
		kinds++
	}
	if len(c.OneOf) > 0 {
		kinds++
	}
	if c.Min != nil || c.Max != nil {
		kinds++
		if c.Min != nil && c.Max != nil && *c.Min > *c.Max {
			return errors.New("min is greater than max")
		}
	}
	if kinds != 1 {
		return errors.New("a constraint needs exactly one of equals, glob, one_of, or min/max")
	}
	return nil
}

// Satisfies reports whether a decoded JSON value meets the constraint.
func Satisfies(c models.Constraint, v any) bool {
	switch {
	case len(c.Equals) > 0:
		var want any
		if json.Unmarshal(c.Equals, &want) != nil {
			return false
		}
		return canonical(want) == canonical(v)
	case c.Glob != "":
		s, ok := v.(string)
		return ok && proto.GlobMatch(c.Glob, s)
	case len(c.OneOf) > 0:
		s, ok := scalarString(v)
		return ok && slices.Contains(c.OneOf, s)
	case c.Min != nil || c.Max != nil:
		n, ok := v.(float64)
		return ok && (c.Min == nil || n >= *c.Min) && (c.Max == nil || n <= *c.Max)
	}
	return false
}

// MatchArgs requires the call's arguments and the constraints to name exactly the same keys, so a grant never covers an argument nobody looked at.
func MatchArgs(match map[string]models.Constraint, input json.RawMessage) bool {
	args := map[string]any{}
	if len(bytes.TrimSpace(input)) > 0 {
		if err := json.Unmarshal(input, &args); err != nil {
			return false
		}
	}
	if len(args) != len(match) {
		return false
	}
	for k, v := range args {
		c, ok := match[k]
		if !ok || !Satisfies(c, v) {
			return false
		}
	}
	return true
}

var placeholderRE = regexp.MustCompile(`\{\{\s*([a-z][a-z0-9_]*)\s*\}\}`)

// Placeholders lists the parameter names used in a runbook plan template.
func Placeholders(plan json.RawMessage) []string {
	var names []string
	for _, m := range placeholderRE.FindAllSubmatch(plan, -1) {
		if n := string(m[1]); !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	return names
}

// MatchPlan binds a proposed plan to a runbook template; title and reason are free text and never compared.
func MatchPlan(tmpl, plan proto.ChangePlan) (map[string]any, error) {
	b := map[string]any{}
	phases := []struct {
		name string
		t, p []proto.ChangeCall
	}{{proto.PhaseStep, tmpl.Steps, plan.Steps}, {proto.PhaseVerify, tmpl.Verify, plan.Verify}, {proto.PhaseRollback, tmpl.Rollback, plan.Rollback}}
	for _, ph := range phases {
		if len(ph.t) != len(ph.p) {
			return nil, fmt.Errorf("%s has %d calls, the runbook has %d", ph.name, len(ph.p), len(ph.t))
		}
		for i := range ph.t {
			t, p := ph.t[i], ph.p[i]
			if t.Tool != p.Tool || t.Expect != p.Expect || t.Reject != p.Reject {
				return nil, fmt.Errorf("%s %d differs from the runbook", ph.name, i+1)
			}
			var tv, pv any
			if json.Unmarshal(orEmpty(t.Input), &tv) != nil || json.Unmarshal(orEmpty(p.Input), &pv) != nil {
				return nil, fmt.Errorf("%s %d: invalid input", ph.name, i+1)
			}
			if err := unify(tv, pv, b); err != nil {
				return nil, fmt.Errorf("%s %d (%s): %w", ph.name, i+1, t.Tool, err)
			}
		}
	}
	return b, nil
}

func unify(t, v any, b map[string]any) error {
	switch tt := t.(type) {
	case map[string]any:
		vm, ok := v.(map[string]any)
		if !ok || len(vm) != len(tt) {
			return errors.New("arguments differ from the runbook")
		}
		for k, tv := range tt {
			pv, ok := vm[k]
			if !ok {
				return fmt.Errorf("argument %q is missing", k)
			}
			if err := unify(tv, pv, b); err != nil {
				return err
			}
		}
		return nil
	case []any:
		vs, ok := v.([]any)
		if !ok || len(vs) != len(tt) {
			return errors.New("a list differs from the runbook")
		}
		for i := range tt {
			if err := unify(tt[i], vs[i], b); err != nil {
				return err
			}
		}
		return nil
	case string:
		return unifyString(tt, v, b)
	}
	if canonical(t) != canonical(v) {
		return errors.New("a value differs from the runbook")
	}
	return nil
}

func unifyString(t string, v any, b map[string]any) error {
	locs := placeholderRE.FindAllStringSubmatchIndex(t, -1)
	if len(locs) == 0 {
		if s, ok := v.(string); !ok || s != t {
			return errors.New("a value differs from the runbook")
		}
		return nil
	}
	// A whole-string placeholder may bind a number or bool; one inside text binds the matched text.
	if len(locs) == 1 && locs[0][0] == 0 && locs[0][1] == len(t) {
		switch v.(type) {
		case map[string]any, []any, nil:
			return errors.New("a parameter must be a single value")
		}
		return bind(b, t[locs[0][2]:locs[0][3]], v)
	}
	s, ok := v.(string)
	if !ok {
		return errors.New("a value differs from the runbook")
	}
	var re strings.Builder
	re.WriteString("^")
	var names []string
	last := 0
	for _, l := range locs {
		re.WriteString(regexp.QuoteMeta(t[last:l[0]]))
		re.WriteString("(.+?)")
		names = append(names, t[l[2]:l[3]])
		last = l[1]
	}
	re.WriteString(regexp.QuoteMeta(t[last:]))
	re.WriteString("$")
	m := regexp.MustCompile(re.String()).FindStringSubmatch(s)
	if m == nil {
		return errors.New("a value differs from the runbook")
	}
	for i, n := range names {
		if err := bind(b, n, m[i+1]); err != nil {
			return err
		}
	}
	return nil
}

func bind(b map[string]any, name string, v any) error {
	if prev, ok := b[name]; ok && canonical(prev) != canonical(v) {
		return fmt.Errorf("parameter %q takes two different values", name)
	}
	b[name] = v
	return nil
}

func canonical(v any) string {
	out, _ := json.Marshal(v)
	return string(out)
}

func scalarString(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(x), true
	}
	return "", false
}

func orEmpty(in json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(in)) == 0 {
		return json.RawMessage("{}")
	}
	return in
}
