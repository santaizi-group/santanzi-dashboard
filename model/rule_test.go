package model

import (
	"testing"
	"time"
)

func snapshotServer(id uint64, lastActive time.Time) *Server {
	return &Server{
		Common:     Common{ID: id},
		Host:       &Host{},
		State:      &HostState{},
		LastActive: lastActive,
	}
}

func TestRuleSnapshotOffline_V2PartialPasses(t *testing.T) {
	db := consensusTestDB(t)
	node := consensusNode(11)
	if err := db.Create(&ServerRuntime{ServerID: 11, Protocol: v2RuntimeProtocol, CurrentNodeUUID: node}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&AvailabilityBucket{NodeUUID: node, BucketStart: 10, HostState: HostStateOnline, ConnectivityState: ConnectivityPartial}).Error; err != nil {
		t.Fatal(err)
	}
	rule := Rule{Type: "offline"}
	if got := rule.Snapshot(snapshotServer(11, time.Now().Add(-time.Hour)), db); got != nil {
		t.Fatal("V2 部分连通不应记为离线采样失败")
	}
}

func TestRuleSnapshotOffline_V2OfflineFails(t *testing.T) {
	db := consensusTestDB(t)
	node := consensusNode(12)
	if err := db.Create(&ServerRuntime{ServerID: 12, Protocol: v2RuntimeProtocol, CurrentNodeUUID: node}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&AvailabilityBucket{NodeUUID: node, BucketStart: 10, HostState: HostStateOffline, ConnectivityState: ConnectivityUnavailable}).Error; err != nil {
		t.Fatal(err)
	}
	rule := Rule{Type: "offline"}
	if got := rule.Snapshot(snapshotServer(12, time.Now()), db); got == nil {
		t.Fatal("V2 共识离线应记为采样失败")
	}
}

func TestRuleSnapshotOffline_V1StaleLastActiveFails(t *testing.T) {
	rule := Rule{Type: "offline"}
	if got := rule.Snapshot(snapshotServer(13, time.Now().Add(-10*time.Second)), nil); got == nil {
		t.Fatal("无 V2 绑定时 LastActive 超过 6 秒应记为采样失败")
	}
}

func TestRuleSnapshotOffline_V1FreshLastActivePasses(t *testing.T) {
	rule := Rule{Type: "offline"}
	if got := rule.Snapshot(snapshotServer(14, time.Now()), nil); got != nil {
		t.Fatal("无 V2 绑定时新鲜 LastActive 不应记为采样失败")
	}
}
