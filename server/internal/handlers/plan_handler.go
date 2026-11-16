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

package handlers

import (
	"github.com/goakili/akili/server/internal/middlewares"
	"github.com/goakili/akili/server/internal/plans"
	"github.com/jkaninda/okapi"
)

func (h *Handlers) ListPlans(c *okapi.Context) error {
	ctx := c.Request().Context()
	if _, err := h.Coder.Project(ctx, middlewares.OrgID(c), c.Param("id")); err != nil {
		return mapErr(c, err)
	}
	out, err := h.Plans.List(ctx, middlewares.OrgID(c), c.Param("id"))
	if err != nil {
		return c.AbortInternalServerError("list failed", err)
	}
	return ok(c, out)
}

type PlanRequest struct {
	Body plans.CreateInput `json:"body"`
}

func (h *Handlers) CreatePlan(c *okapi.Context, req *PlanRequest) error {
	d, err := h.Plans.Create(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c), c.Param("id"), req.Body)
	if err != nil {
		return mapErr(c, err)
	}
	h.record(c, "plan.create", "plan", d.Plan.ID, map[string]any{"project_id": d.Plan.ProjectID, "title": d.Plan.Title, "phases": len(d.Phases)})
	return created(c, d)
}

func (h *Handlers) GetPlan(c *okapi.Context) error {
	d, err := h.Plans.Get(c.Request().Context(), middlewares.OrgID(c), c.Param("id"))
	if err != nil {
		return mapErr(c, err)
	}
	return ok(c, d)
}

// PlanUpdateRequest changes a plan; omitted fields are kept.
type PlanUpdateRequest struct {
	Body plans.UpdateInput `json:"body"`
}

func (h *Handlers) UpdatePlan(c *okapi.Context, req *PlanUpdateRequest) error {
	p, err := h.Plans.Update(c.Request().Context(), middlewares.OrgID(c), c.Param("id"), req.Body)
	if err != nil {
		return mapErr(c, err)
	}
	h.record(c, "plan.update", "plan", p.ID, map[string]any{"status": p.Status})
	return ok(c, p)
}

func (h *Handlers) DeletePlan(c *okapi.Context) error {
	if err := h.Plans.Delete(c.Request().Context(), middlewares.OrgID(c), c.Param("id")); err != nil {
		return mapErr(c, err)
	}
	h.record(c, "plan.delete", "plan", c.Param("id"), nil)
	return message(c, "plan deleted")
}

type PlanPhasesRequest struct {
	Body struct {
		Phases []plans.PhaseInput `json:"phases" maxItems:"100"`
	} `json:"body"`
}

// ReplacePlanPhases sets a plan's phases; kept phases keep their id and status.
func (h *Handlers) ReplacePlanPhases(c *okapi.Context, req *PlanPhasesRequest) error {
	d, err := h.Plans.ReplacePhases(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c), c.Param("id"), req.Body.Phases)
	if err != nil {
		return mapErr(c, err)
	}
	h.record(c, "plan.phases_update", "plan", d.Plan.ID, map[string]any{"phases": len(d.Phases)})
	return ok(c, d)
}

type PlanPhaseRequest struct {
	Body struct {
		Status string `json:"status" required:"true" description:"todo, in_progress, done or skipped"`
		Note   string `json:"note" maxLength:"1000"`
	} `json:"body"`
}

func (h *Handlers) UpdatePlanPhase(c *okapi.Context, req *PlanPhaseRequest) error {
	st, err := h.Plans.SetPhase(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c), c.Param("id"), c.Param("phase"), req.Body.Status, req.Body.Note)
	if err != nil {
		return mapErr(c, err)
	}
	h.record(c, "plan.phase_update", "plan", st.PlanID, map[string]any{"phase": st.ID, "status": st.Status})
	return ok(c, st)
}
