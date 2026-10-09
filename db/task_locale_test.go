package db

import (
	"fmt"
	"github.com/Autumn-27/artex/locale"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestTaskLanguageAtomicCreation(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	marker := fmt.Sprintf("locale_atomic_%d", time.Now().UnixNano())
	// Fail only this test task's metadata write, within an isolated test database.
	// The trigger does not affect other settings or tasks and is removed on return.
	function := marker + "_fn"
	trigger := marker + "_tr"
	query := fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.key LIKE 'task_language.%%' AND EXISTS(SELECT 1 FROM tasks WHERE description='%s' AND NEW.key='task_language.'||id::text) THEN
 RAISE EXCEPTION 'injected task language failure'; END IF; RETURN NEW; END $$`, function, marker)
	if _, err = d.Exec(query); err != nil {
		t.Fatal(err)
	}
	defer d.Exec("DROP FUNCTION " + function + "()")
	if _, err = d.Exec("CREATE TRIGGER " + trigger + " BEFORE INSERT ON settings FOR EACH ROW EXECUTE FUNCTION " + function + "()"); err != nil {
		t.Fatal(err)
	}
	defer d.Exec("DROP TRIGGER " + trigger + " ON settings")
	task, err := d.CreateTaskWithOptions(marker, "no network target", TaskCreateOptions{Language: locale.En})
	if err == nil || task != nil || !strings.Contains(err.Error(), "injected task language failure") {
		t.Fatalf("expected atomic metadata failure: task=%v err=%v", task, err)
	}
	for _, table := range []string{"tasks", "explorations"} {
		var count int
		if err = d.QueryRow("SELECT count(*) FROM "+table+" WHERE description=$1", marker).Scan(&count); err != nil || count != 0 {
			t.Fatalf("partial %s remained: count=%d err=%v", table, count, err)
		}
	}
}

func TestTaskLanguageSavedAndRemovedWithTask(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	task, err := d.CreateTaskWithOptions("language persistence", "no network target", TaskCreateOptions{Language: locale.En})
	if err != nil {
		t.Fatal(err)
	}
	defer d.DeleteTask(task.ID)
	key := "task_language." + strconv.FormatInt(task.ID, 10)
	value, ok, err := d.GetSetting(key)
	if err != nil || !ok || value != "en" {
		t.Fatalf("language missing: %q %v %v", value, ok, err)
	}
	if err = d.DeleteTask(task.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, err = d.GetSetting(key); err != nil || ok {
		t.Fatalf("language remained after task deletion: %v %v", ok, err)
	}
}
