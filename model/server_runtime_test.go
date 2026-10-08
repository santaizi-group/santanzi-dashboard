package model

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func consensusTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开内存数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&ServerRuntime{}, &AvailabilityBucket{}); err != nil {
		t.Fatalf("自动迁移失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("取出 SQL 连接失败: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func consensusNode(id byte) []byte {
	node := make([]byte, 16)
	node[15] = id
	return node
}

func TestServerConsensusOffline_NoRuntime(t *testing.T) {
	db := consensusTestDB(t)
	offline, ok, err := ServerConsensusOffline(db, 1)
	if err != nil || ok || offline {
		t.Fatalf("无运行态应 ok=false，得到 offline=%v ok=%v err=%v", offline, ok, err)
	}
}

func TestServerConsensusOffline_NilDB(t *testing.T) {
	offline, ok, err := ServerConsensusOffline(nil, 1)
	if err != nil || ok || offline {
		t.Fatalf("空库应 ok=false，得到 offline=%v ok=%v err=%v", offline, ok, err)
	}
}

func TestServerConsensusOffline_V1Protocol(t *testing.T) {
	db := consensusTestDB(t)
	if err := db.Create(&ServerRuntime{ServerID: 2, Protocol: "", CurrentNodeUUID: consensusNode(2)}).Error; err != nil {
		t.Fatal(err)
	}
	offline, ok, err := ServerConsensusOffline(db, 2)
	if err != nil || ok || offline {
		t.Fatalf("V1 应 ok=false，得到 offline=%v ok=%v err=%v", offline, ok, err)
	}
}

func TestServerConsensusOffline_V2WithoutBucket(t *testing.T) {
	db := consensusTestDB(t)
	if err := db.Create(&ServerRuntime{ServerID: 3, Protocol: v2RuntimeProtocol, CurrentNodeUUID: consensusNode(3)}).Error; err != nil {
		t.Fatal(err)
	}
	offline, ok, err := ServerConsensusOffline(db, 3)
	if err != nil || !ok || offline {
		t.Fatalf("V2 无桶应不算离线，得到 offline=%v ok=%v err=%v", offline, ok, err)
	}
}

func TestServerConsensusOffline_LatestBucket(t *testing.T) {
	db := consensusTestDB(t)
	node := consensusNode(4)
	if err := db.Create(&ServerRuntime{ServerID: 4, Protocol: v2RuntimeProtocol, CurrentNodeUUID: node}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&AvailabilityBucket{NodeUUID: node, BucketStart: 1, HostState: HostStateOffline, ConnectivityState: ConnectivityUnavailable}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&AvailabilityBucket{NodeUUID: node, BucketStart: 2, HostState: HostStateOnline, ConnectivityState: ConnectivityPartial}).Error; err != nil {
		t.Fatal(err)
	}
	offline, ok, err := ServerConsensusOffline(db, 4)
	if err != nil || !ok || offline {
		t.Fatalf("最新桶 partial 不应离线，得到 offline=%v ok=%v err=%v", offline, ok, err)
	}
	if err := db.Create(&AvailabilityBucket{NodeUUID: node, BucketStart: 3, HostState: HostStateOffline, ConnectivityState: ConnectivityUnavailable}).Error; err != nil {
		t.Fatal(err)
	}
	offline, ok, err = ServerConsensusOffline(db, 4)
	if err != nil || !ok || !offline {
		t.Fatalf("最新桶 offline 应离线，得到 offline=%v ok=%v err=%v", offline, ok, err)
	}
}

func TestLatestClosedAvailabilityBucketPrefersLaterWindowEnd(t *testing.T) {
	db := consensusTestDB(t)
	node := consensusNode(5)
	now := time.Unix(1_800_000_000, 0)
	spanStart := now.Add(-2 * time.Hour).UnixNano()
	spanEnd := now.Add(-time.Minute).UnixNano()
	rawStart := now.Add(-30 * time.Minute).UnixNano()
	rawEnd := rawStart + int64(30*time.Second)
	openStart := now.Add(-20 * time.Second).UnixNano()
	if err := db.Create(&AvailabilityBucket{
		NodeUUID: node, BucketStart: spanStart, WindowEnd: spanEnd,
		HostState: HostStateOnline, ConnectivityState: ConnectivityFull,
		ExpectedObservers: 3, HealthyObservers: 3, SeenObservers: 3,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&AvailabilityBucket{
		NodeUUID: node, BucketStart: rawStart, WindowEnd: rawEnd,
		HostState: HostStateUnknown, ConnectivityState: ConnectivityUnknown,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&AvailabilityBucket{
		NodeUUID: node, BucketStart: openStart, WindowEnd: now.Add(10 * time.Second).UnixNano(),
		HostState: HostStateOffline, ConnectivityState: ConnectivityUnavailable,
		ExpectedObservers: 3,
	}).Error; err != nil {
		t.Fatal(err)
	}
	bucket, found, err := LatestClosedAvailabilityBucket(db, node, now)
	if err != nil || !found {
		t.Fatalf("应读到已结束的桶 found=%v err=%v", found, err)
	}
	if bucket.BucketStart != spanStart || bucket.HostState != HostStateOnline || bucket.SeenObservers != 3 {
		t.Fatalf("应选终点更晚的完整 span，得到 start=%d host=%s seen=%d", bucket.BucketStart, bucket.HostState, bucket.SeenObservers)
	}
}
