package model

import "testing"

func TestParseBotRole(t *testing.T) {
	role, ok := ParseBotRole("operator")
	if !ok || role != BotRoleOperator {
		t.Fatalf("operator role=%d ok=%v", role, ok)
	}
	if BotRoleName(BotRoleAdmin) != "admin" {
		t.Fatal(BotRoleName(BotRoleAdmin))
	}
}

func TestParseCSVAndScope(t *testing.T) {
	ids := ParseUint64CSV("1, 2;2,abc,0")
	if len(ids) != 2 || ids[0] != 1 || ids[1] != 2 {
		t.Fatalf("%v", ids)
	}
	ignore := map[uint64]bool{2: true}
	if !ServerInBotScope(1, RuleCoverAll, ignore) || ServerInBotScope(2, RuleCoverAll, ignore) {
		t.Fatal("cover all")
	}
	if ServerInBotScope(1, RuleCoverIgnoreAll, ignore) || !ServerInBotScope(2, RuleCoverIgnoreAll, ignore) {
		t.Fatal("cover ignore all")
	}
	chat := BotChat{SubscribeTags: "default, ops", Enabled: BoolPtr(true), Role: BotRoleViewer}
	if !chat.Subscribes("default") || chat.Subscribes("other") {
		t.Fatal("subscribe")
	}
}
