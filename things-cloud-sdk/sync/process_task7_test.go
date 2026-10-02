package sync

import (
	"encoding/json"
	"path/filepath"
	"testing"

	things "github.com/arthursoares/things-cloud-sdk"
)

// Task7 is a current-generation task envelope kind; it must route to the task
// handler and update persisted state instead of degrading to UnknownChange.
func TestProcessTask7Item(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	syncer, err := Open(dbPath, nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer syncer.Close()

	if _, err := syncer.processItems([]things.Item{{
		UUID:   "task-001",
		Kind:   things.ItemKindTask,
		Action: things.ItemActionCreated,
		P:      json.RawMessage(`{"tt":"Deferred","tp":0,"st":2,"rt":["template-1"]}`),
	}}, 0); err != nil {
		t.Fatalf("processItems failed: %v", err)
	}

	changes, err := syncer.processItems([]things.Item{{
		UUID:   "task-001",
		Kind:   things.ItemKindTask7,
		Action: things.ItemActionModified,
		P:      json.RawMessage(`{"st":1,"rt":[]}`),
	}}, 1)
	if err != nil {
		t.Fatalf("processItems failed: %v", err)
	}
	if len(changes) == 0 {
		t.Fatal("expected task changes from Task7 item, got none")
	}
	for _, change := range changes {
		if _, ok := change.(UnknownChange); ok {
			t.Fatalf("Task7 item produced UnknownChange: %v", change)
		}
	}

	task, err := syncer.State().Task("task-001")
	if err != nil {
		t.Fatalf("Task lookup failed: %v", err)
	}
	if task == nil {
		t.Fatal("task not persisted")
	}
	if task.Schedule != things.TaskScheduleAnytime {
		t.Errorf("Schedule = %v, want Anytime", task.Schedule)
	}
}

func TestProcessUnknownFutureKind(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	syncer, err := Open(dbPath, nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer syncer.Close()

	changes, err := syncer.processItems([]things.Item{{
		UUID:   "future-001",
		Kind:   things.ItemKind("Task8"),
		Action: things.ItemActionModified,
		P:      json.RawMessage(`{}`),
	}}, 0)
	if err != nil {
		t.Fatalf("processItems failed: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if _, ok := changes[0].(UnknownChange); !ok {
		t.Fatalf("expected UnknownChange for Task8, got %T", changes[0])
	}
}

// Mail to Things queue entries carry no task-graph state and must not surface
// as UnknownChange (which would log, and could expose, the email body).
func TestProcessMailQueueCommands(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	syncer, err := Open(dbPath, nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer syncer.Close()

	changes, err := syncer.processItems([]things.Item{
		{UUID: "8F63FEC2-1202-43D8-80D7-55B4AC4FCA37", Kind: things.ItemKindCommand, Action: things.ItemActionCreated,
			P: json.RawMessage(`{"cd":1785223359,"tp":0,"if":{"id":"E9EBA235-0000-0000-0000-000000000000","nt":"<note>body</note>"}}`)},
		{UUID: "457jbco4TLEAXyjgnrgE6X", Kind: things.ItemKindCommand3, Action: things.ItemActionDeleted, P: json.RawMessage(`{}`)},
	}, 0)
	if err != nil {
		t.Fatalf("processItems failed: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("expected no changes for mail queue commands, got %#v", changes)
	}
}
