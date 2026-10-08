package controller

import (
	"bytes"
	"testing"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRuntimeForServerUsesClosedBucketWhileRecovering(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.ServerRuntime{}, &model.AvailabilityBucket{}); err != nil {
		t.Fatal(err)
	}
	previous := singleton.DB
	singleton.DB = db
	t.Cleanup(func() {
		singleton.DB = previous
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})

	node := bytes.Repeat([]byte{0x41}, 16)
	now := time.Now()
	spanStart := now.Add(-2 * time.Hour).UnixNano()
	spanEnd := now.Add(-time.Minute).UnixNano()
	rawStart := now.Add(-30 * time.Minute).UnixNano()
	if err := db.Create(&model.ServerRuntime{
		ServerID: 1, Status: model.ServerRuntimeStatusRecovering, Protocol: "v2",
		CurrentNodeUUID: node, HostState: model.HostStateUnknown, ConnectivityState: model.ConnectivityUnknown,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AvailabilityBucket{
		NodeUUID: node, BucketStart: spanStart, WindowEnd: spanEnd,
		HostState: model.HostStateOnline, ConnectivityState: model.ConnectivityFull,
		ExpectedObservers: 3, HealthyObservers: 3, SeenObservers: 3,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AvailabilityBucket{
		NodeUUID: node, BucketStart: rawStart, WindowEnd: rawStart + int64(30*time.Second),
		HostState: model.HostStateUnknown, ConnectivityState: model.ConnectivityUnknown,
	}).Error; err != nil {
		t.Fatal(err)
	}

	server := model.Server{Common: model.Common{ID: 1}, Name: "LAX-VMISS.TRI"}
	got := runtimeForServer(server)
	if got.HostState != model.HostStateOnline || got.Connectivity != model.ConnectivityFull || got.Coverage != "3/3" {
		t.Fatalf("recovering 仍应展示已结束的完整桶，得到 host=%s connectivity=%s coverage=%s", got.HostState, got.Connectivity, got.Coverage)
	}
	if got.Availability == nil || !*got.Availability {
		t.Fatal("完整连通应标记为可用")
	}
	if !serverOnlineFlag(server, got) {
		t.Fatal("已结束桶为在线时列表应判在线")
	}
}
