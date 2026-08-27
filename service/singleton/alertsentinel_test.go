package singleton

import "testing"

func TestOnDeleteServerRemovesRuleSnapshots(t *testing.T) {
	AlertsLock.Lock()
	alertsStore = map[uint64]map[uint64][][]interface{}{
		1: {9: {{"sample"}}, 8: {{"keep"}}},
	}
	alertsPrevState = map[uint64]map[uint64]uint{
		1: {9: _RuleCheckFail, 8: _RuleCheckPass},
	}
	AlertsLock.Unlock()
	t.Cleanup(func() {
		AlertsLock.Lock()
		alertsStore = nil
		alertsPrevState = nil
		AlertsLock.Unlock()
	})

	OnDeleteServer(9)

	AlertsLock.RLock()
	defer AlertsLock.RUnlock()
	if _, ok := alertsStore[1][9]; ok {
		t.Fatal("deleted server snapshots still in alertsStore")
	}
	if _, ok := alertsPrevState[1][9]; ok {
		t.Fatal("deleted server state still in alertsPrevState")
	}
	if _, ok := alertsStore[1][8]; !ok {
		t.Fatal("unrelated server snapshots were removed")
	}
}
