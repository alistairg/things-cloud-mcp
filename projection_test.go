package main

// Tests for recurring-template projection (Option A) and tag/area fallback
// through rt (Option B): a repeating to-do whose current occurrence was
// completed must not vanish from read tools, and instances must resolve
// tags/areas stored only on their template.

import (
	"context"
	"strings"
	"testing"
	"time"

	thingscloud "github.com/arthursoares/things-cloud-sdk"
)

// recurringFixture builds a weekly repeating to-do stored the way Things
// Cloud stores it: a template (rr set, holds the tag) plus a completed
// instance (rt -> template, empty tg). Next fire date is 3 days out.
func recurringFixture(t *testing.T, extra ...thingscloud.Item) *ThingsMCP {
	t.Helper()
	nextFire := time.Now().UTC().AddDate(0, 0, 3)
	items := []thingscloud.Item{
		makeTagItem("tag-errand", "errand"),
		makeAreaItem("area-home", "Home"),
		makeTaskItem("tpl-laundry",
			withTitle("Laundry"),
			withSchedule(thingscloud.TaskScheduleSomeday),
			withRepeater(),
			withTags("tag-errand"),
			withArea("area-home"),
			withTodayIndexRefDate(nextFire),
			withAlarmOffset(54000), // 15:00
		),
		makeTaskItem("inst-done",
			withTitle("Laundry"),
			withStatus(thingscloud.TaskStatusCompleted),
			withRecurrence("tpl-laundry"),
			withTodayIndexRefDate(time.Now().UTC().AddDate(0, 0, -4)),
		),
	}
	items = append(items, extra...)
	fc := newFakeCloud("proj@test.com", items...)
	t.Cleanup(fc.Close)
	return newTestThingsMCP(t, fc)
}

func findTasks(t *testing.T, tmcp *ThingsMCP, args map[string]any) []TaskOutput {
	t.Helper()
	result, err := tmcp.handleFindTasks(context.Background(), makeReq(args))
	if err != nil {
		t.Fatalf("handleFindTasks: %v", err)
	}
	assertNotError(t, result)
	return resultJSON[[]TaskOutput](t, result)
}

func taskByUUID(tasks []TaskOutput, uuid string) *TaskOutput {
	for i := range tasks {
		if tasks[i].UUID == uuid {
			return &tasks[i]
		}
	}
	return nil
}

func TestProjectionAppearsInFindTasks(t *testing.T) {
	tmcp := recurringFixture(t)

	tasks := findTasks(t, tmcp, map[string]any{})
	proj := taskByUUID(tasks, "tpl-laundry")
	if proj == nil {
		t.Fatalf("projected occurrence missing from pending tasks: %+v", tasks)
	}
	if proj.Materialized == nil || *proj.Materialized {
		t.Errorf("projection must carry materialized:false, got %v", proj.Materialized)
	}
	if !proj.IsRecurring {
		t.Error("projection must be isRecurring")
	}
	wantDate := time.Now().UTC().AddDate(0, 0, 3).Format("2006-01-02")
	if proj.ScheduledDate == nil || *proj.ScheduledDate != wantDate {
		t.Errorf("scheduledDate = %v, want %s", proj.ScheduledDate, wantDate)
	}
	if proj.Schedule != "upcoming" {
		t.Errorf("schedule = %q, want upcoming", proj.Schedule)
	}
	if len(proj.Tags) != 1 || proj.Tags[0].Name != "errand" {
		t.Errorf("projection lost template tag: %+v", proj.Tags)
	}
	if len(proj.Areas) != 1 || proj.Areas[0].Name != "Home" {
		t.Errorf("projection lost template area: %+v", proj.Areas)
	}
	if proj.ReminderTime == nil || *proj.ReminderTime != "15:00" {
		t.Errorf("projection lost reminder time: %v", proj.ReminderTime)
	}
}

func TestProjectionMatchesTagAndAreaFilters(t *testing.T) {
	tmcp := recurringFixture(t)

	byTag := findTasks(t, tmcp, map[string]any{"tag": "errand"})
	if taskByUUID(byTag, "tpl-laundry") == nil {
		t.Errorf("tag filter missed projected occurrence: %+v", byTag)
	}
	byArea := findTasks(t, tmcp, map[string]any{"area": "Home"})
	if taskByUUID(byArea, "tpl-laundry") == nil {
		t.Errorf("area filter missed projected occurrence: %+v", byArea)
	}
}

func TestCompletedInstanceInheritsTemplateTags(t *testing.T) {
	tmcp := recurringFixture(t)

	completed := findTasks(t, tmcp, map[string]any{"status": "completed", "tag": "errand"})
	inst := taskByUUID(completed, "inst-done")
	if inst == nil {
		t.Fatalf("completed instance not matched by inherited tag: %+v", completed)
	}
	if len(inst.Tags) != 1 || inst.Tags[0].Name != "errand" {
		t.Errorf("completed instance did not inherit template tag: %+v", inst.Tags)
	}
	if inst.Materialized != nil {
		t.Errorf("real instance must not carry materialized flag, got %v", *inst.Materialized)
	}
}

func TestOpenInstanceSuppressesProjection(t *testing.T) {
	tmcp := recurringFixture(t, makeTaskItem("inst-open",
		withTitle("Laundry"),
		withRecurrence("tpl-laundry"),
		withTodayIndexRefDate(time.Now().UTC().AddDate(0, 0, 3)),
	))

	tasks := findTasks(t, tmcp, map[string]any{})
	if taskByUUID(tasks, "tpl-laundry") != nil {
		t.Errorf("projection must be suppressed when an open instance exists: %+v", tasks)
	}
	inst := taskByUUID(tasks, "inst-open")
	if inst == nil {
		t.Fatalf("open instance missing: %+v", tasks)
	}
	if inst.Materialized != nil {
		t.Errorf("real instance must not carry materialized flag")
	}
}

func TestTrashedTemplateNotProjected(t *testing.T) {
	fc := newFakeCloud("proj@test.com",
		makeTaskItem("tpl-gone",
			withTitle("Old habit"),
			withRepeater(),
			withTodayIndexRefDate(time.Now().UTC().AddDate(0, 0, 2)),
			withTrashed(),
		),
	)
	t.Cleanup(fc.Close)
	tmcp := newTestThingsMCP(t, fc)

	tasks := findTasks(t, tmcp, map[string]any{})
	if taskByUUID(tasks, "tpl-gone") != nil {
		t.Errorf("trashed template must not be projected: %+v", tasks)
	}
}

func TestProjectionInOverviewUpcoming(t *testing.T) {
	tmcp := recurringFixture(t)

	result, err := tmcp.handleOverview(context.Background(), makeReq(map[string]any{"lookahead_days": 7}))
	if err != nil {
		t.Fatalf("handleOverview: %v", err)
	}
	assertNotError(t, result)
	overview := resultJSON[struct {
		TodayTasks    []TaskOutput `json:"todayTasks"`
		UpcomingTasks []TaskOutput `json:"upcomingTasks"`
	}](t, result)

	proj := taskByUUID(overview.UpcomingTasks, "tpl-laundry")
	if proj == nil {
		t.Fatalf("projected occurrence missing from upcomingTasks: %+v", overview.UpcomingTasks)
	}
	if proj.Materialized == nil || *proj.Materialized {
		t.Errorf("overview projection must carry materialized:false")
	}
}

func TestShowTaskOnTemplateReturnsProjection(t *testing.T) {
	tmcp := recurringFixture(t)

	result, err := tmcp.handleShowTask(context.Background(), makeReq(map[string]any{"uuid": "tpl-laundry"}))
	if err != nil {
		t.Fatalf("handleShowTask: %v", err)
	}
	assertNotError(t, result)
	detail := resultJSON[TaskDetailOutput](t, result)
	if detail.Materialized == nil || *detail.Materialized {
		t.Errorf("show_task projection must carry materialized:false")
	}
	if detail.UUID != "tpl-laundry" {
		t.Errorf("uuid = %q", detail.UUID)
	}
}

func TestEditProjectedOccurrenceRejectedWithGuidance(t *testing.T) {
	tmcp := recurringFixture(t)

	result, err := tmcp.handleEditTask(context.Background(), makeReq(map[string]any{
		"uuid":  "tpl-laundry",
		"title": "renamed",
	}))
	if err != nil {
		t.Fatalf("handleEditTask: %v", err)
	}
	assertIsError(t, result)
	if text := resultText(t, result); !strings.Contains(text, "materialized") {
		t.Errorf("edit error should explain projection semantics, got: %s", text)
	}
}
