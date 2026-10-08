package singleton

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	pb "github.com/hi2shark/santaizi-dashboard/proto"
	"google.golang.org/protobuf/proto"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestV2NodeReplacementCreatesIdentityLifecycleFact(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.Server{}, &model.ServerNodeBinding{}, &model.ObserverAssignment{}, &model.Collector{}, &model.CollectorScope{},
		&model.TelemetryEvent{}, &model.TelemetryObservation{},
	); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Server{Common: model.Common{ID: 7}, Name: "node-7", Secret: "secret-7"}).Error; err != nil {
		t.Fatal(err)
	}
	previousDB := DB
	DB = db
	t.Cleanup(func() {
		DB = previousDB
		_ = CloseDB(db)
	})

	oldNode := bytes.Repeat([]byte{0x11}, 16)
	newNode := bytes.Repeat([]byte{0x22}, 16)
	now := time.Unix(1_800_000_000, 0)
	if _, err := BindServerNodeForProtocol(7, oldNode, now, pb.SourceProtocol_SOURCE_PROTOCOL_SANTAIZI_V2); err != nil {
		t.Fatal(err)
	}
	if changed, err := BindServerNodeForProtocol(7, newNode, now.Add(time.Minute), pb.SourceProtocol_SOURCE_PROTOCOL_SANTAIZI_V2); err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}

	var row model.TelemetryEvent
	if err := db.Where("node_uuid = ? AND event_type = ?", newNode, pb.TelemetryEventType_TELEMETRY_EVENT_TYPE_LIFECYCLE).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	var event pb.TelemetryEvent
	if err := proto.Unmarshal(row.Payload, &event); err != nil {
		t.Fatal(err)
	}
	if event.GetPriority() != pb.TelemetryPriority_TELEMETRY_PRIORITY_P0_CRITICAL ||
		event.GetLifecycle().GetKind() != pb.LifecycleKind_LIFECYCLE_KIND_AGENT_IDENTITY_CHANGED ||
		!bytes.Equal(event.GetLifecycle().GetPreviousNodeUuid(), oldNode) {
		t.Fatalf("unexpected lifecycle fact: %#v", &event)
	}
	var observation model.TelemetryObservation
	if err := db.First(&observation, "event_id = ? AND observer_id = ?", row.EventID, primaryObserverID).Error; err != nil {
		t.Fatal(err)
	}
	var oldBinding model.ServerNodeBinding
	if err := db.First(&oldBinding, "server_id = ? AND node_uuid = ?", 7, oldNode).Error; err != nil {
		t.Fatal(err)
	}
	if oldBinding.Current || oldBinding.ValidTo == 0 {
		t.Fatalf("old binding not closed: %#v", oldBinding)
	}
}

func TestHistoricalReplayDoesNotOverwriteFreshRuntime(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.Server{}, &model.ServerNodeBinding{}, &model.ObserverAssignment{}, &model.Collector{}, &model.CollectorScope{},
		&model.ServerRuntime{},
	); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Server{Common: model.Common{ID: 9}, Name: "node-9", Secret: "secret-9"}).Error; err != nil {
		t.Fatal(err)
	}
	previousDB, previousConf, previousServers := DB, Conf, ServerList
	DB = db
	Conf = &model.Config{Telemetry: model.TelemetryConfig{OfflineThresholdSeconds: 30}}
	ServerList = map[uint64]*model.Server{9: {Common: model.Common{ID: 9}, State: &model.HostState{}, Host: &model.Host{}}}
	t.Cleanup(func() {
		DB, Conf, ServerList = previousDB, previousConf, previousServers
		_ = CloseDB(db)
	})

	node := bytes.Repeat([]byte{0x31}, 16)
	session := bytes.Repeat([]byte{0x32}, 16)
	now := time.Now()
	if _, err := BindServerNode(9, node, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := ApplyRealtimeSnapshot(&pb.RealtimeSnapshot{
		NodeUuid: node, SessionId: session, LatestSequence: 10,
		CollectedAtUnixNano: now.UnixNano(), State: &pb.State{Cpu: 90},
	}, now); err != nil {
		t.Fatal(err)
	}
	oldEventID := make([]byte, 16)
	if err := ApplyV2Event(&pb.TelemetryEvent{
		EventId: oldEventID, NodeUuid: node, SessionId: session, Sequence: 5,
		CollectedAtUnixNano: now.Add(-time.Hour).UnixNano(),
		Payload:             &pb.TelemetryEvent_State{State: &pb.State{Cpu: 10}},
	}, now); err != nil {
		t.Fatal(err)
	}
	var runtime model.ServerRuntime
	if err := db.First(&runtime, "server_id = ?", 9).Error; err != nil {
		t.Fatal(err)
	}
	var state pb.State
	if err := proto.Unmarshal(runtime.StatePayload, &state); err != nil {
		t.Fatal(err)
	}
	if runtime.CurrentSequence != 10 || state.GetCpu() != 90 || ServerList[9].State.CPU != 90 {
		t.Fatalf("historical replay overwrote runtime: sequence=%d state=%v memory=%v", runtime.CurrentSequence, state.GetCpu(), ServerList[9].State.CPU)
	}
}

func TestApplyTrustedTelemetryEventsClearsRecovering(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.Server{}, &model.ServerNodeBinding{}, &model.ServerRuntime{},
		&model.ObserverAssignment{}, &model.Collector{}, &model.CollectorScope{},
	); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Server{Common: model.Common{ID: 21}, Name: "node-21", Secret: "secret-21"}).Error; err != nil {
		t.Fatal(err)
	}
	previousDB, previousConf, previousServers := DB, Conf, ServerList
	DB = db
	Conf = &model.Config{Telemetry: model.TelemetryConfig{OfflineThresholdSeconds: 30}}
	ServerList = map[uint64]*model.Server{21: {Common: model.Common{ID: 21}, State: &model.HostState{}, Host: &model.Host{}}}
	t.Cleanup(func() {
		DB, Conf, ServerList = previousDB, previousConf, previousServers
		_ = CloseDB(db)
	})

	node := bytes.Repeat([]byte{0x51}, 16)
	session := bytes.Repeat([]byte{0x52}, 16)
	now := time.Now()
	if _, err := BindServerNode(21, node, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ServerRuntime{
		ServerID: 21, Status: model.ServerRuntimeStatusRecovering, Protocol: "v2",
		CurrentNodeUUID: node, HostState: model.HostStateUnknown, ConnectivityState: model.ConnectivityUnknown,
	}).Error; err != nil {
		t.Fatal(err)
	}
	stale := &pb.TelemetryEvent{
		EventId: bytes.Repeat([]byte{1}, 16), NodeUuid: node, SessionId: session, Sequence: 1,
		CollectedAtUnixNano: now.Add(-6 * time.Minute).UnixNano(),
		Payload:             &pb.TelemetryEvent_Heartbeat{Heartbeat: &pb.HeartbeatPayload{}},
	}
	if err := ApplyTrustedTelemetryEvents([]*pb.TelemetryEvent{stale}, now); err != nil {
		t.Fatal(err)
	}
	var runtime model.ServerRuntime
	if err := db.First(&runtime, "server_id = ?", 21).Error; err != nil {
		t.Fatal(err)
	}
	if runtime.Status != model.ServerRuntimeStatusRecovering || runtime.HostState != model.HostStateUnknown {
		t.Fatalf("超出可信窗的事件不应结束 recovering，得到 status=%s host=%s", runtime.Status, runtime.HostState)
	}
	fresh := &pb.TelemetryEvent{
		EventId: bytes.Repeat([]byte{2}, 16), NodeUuid: node, SessionId: session, Sequence: 2,
		CollectedAtUnixNano: now.Add(-90 * time.Second).UnixNano(),
		Payload:             &pb.TelemetryEvent_State{State: &pb.State{Cpu: 12}},
	}
	if err := ApplyTrustedTelemetryEvents([]*pb.TelemetryEvent{fresh}, now); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&runtime, "server_id = ?", 21).Error; err != nil {
		t.Fatal(err)
	}
	if runtime.Status != model.ServerRuntimeStatusOnline || runtime.HostState != model.HostStateOnline {
		t.Fatalf("可信复制事件应结束 recovering，得到 status=%s host=%s", runtime.Status, runtime.HostState)
	}
}

func TestBindingAndTagChangeRefreshCollectorAssignments(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.Server{}, &model.ServerNodeBinding{}, &model.ObserverAssignment{},
		&model.Collector{}, &model.CollectorScope{}, &model.TelemetryEvent{}, &model.TelemetryObservation{},
	); err != nil {
		t.Fatal(err)
	}
	server := model.Server{Common: model.Common{ID: 12}, Name: "node-12", Secret: "secret-12", Tag: "edge"}
	collector := model.Collector{CollectorUUID: "collector-edge", Name: "Edge", Address: "edge:5555", TokenHash: bytes.Repeat([]byte{1}, 32), RegistrationToken: "test-registration-token", Generation: 3, ConfigVersion: 1}
	if err := db.Create(&server).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&collector).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CollectorScope{CollectorUUID: collector.CollectorUUID, ScopeType: "tag", ScopeValue: "edge"}).Error; err != nil {
		t.Fatal(err)
	}
	previousDB := DB
	DB = db
	t.Cleanup(func() {
		DB = previousDB
		_ = CloseDB(db)
	})

	node := bytes.Repeat([]byte{0x41}, 16)
	now := time.Unix(1_800_100_000, 0)
	if _, err := BindServerNodeForProtocol(server.ID, node, now, pb.SourceProtocol_SOURCE_PROTOCOL_SANTAIZI_V2); err != nil {
		t.Fatal(err)
	}
	var assignment model.ObserverAssignment
	if err := db.First(&assignment, "node_uuid = ? AND observer_id = ? AND valid_to = 0", node, collector.CollectorUUID).Error; err != nil {
		t.Fatal(err)
	}
	if assignment.Generation != 3 || assignment.ConfigVersion != 2 {
		t.Fatalf("assignment=%#v", assignment)
	}

	if err := db.Model(&server).Update("tag", "core").Error; err != nil {
		t.Fatal(err)
	}
	if err := RefreshObserverAssignmentsForServer(server.ID, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&assignment, assignment.ID).Error; err != nil {
		t.Fatal(err)
	}
	if assignment.ValidTo == 0 {
		t.Fatal("scope removal deleted or left the historical assignment active")
	}
	if err := db.First(&collector, "collector_uuid = ?", collector.CollectorUUID).Error; err != nil {
		t.Fatal(err)
	}
	if collector.ConfigVersion != 3 {
		t.Fatalf("collector config version=%d", collector.ConfigVersion)
	}
}

func stubCountryLookup(t *testing.T, codes map[string]string) {
	t.Helper()
	previous := lookupCountryCode
	lookupCountryCode = func(addr string) string {
		for ip, code := range codes {
			if strings.Contains(addr, ip) {
				return code
			}
		}
		return ""
	}
	t.Cleanup(func() { lookupCountryCode = previous })
}

func TestPreservePBHostIP(t *testing.T) {
	if got := preservePBHostIP(&pb.Host{Ip: "", Version: "1"}, "203.0.113.10").GetIp(); got != "203.0.113.10" {
		t.Fatalf("empty incoming should keep previous, got %q", got)
	}
	incoming := &pb.Host{Ip: "198.51.100.8"}
	if got := preservePBHostIP(incoming, "203.0.113.10"); got != incoming || got.GetIp() != "198.51.100.8" {
		t.Fatalf("non-empty incoming should win, got %#v", got)
	}
	if preservePBHostIP(nil, "203.0.113.10") != nil {
		t.Fatal("nil host should stay nil")
	}
}

func TestEnrichPBHostFillsCountryFromGeoIP(t *testing.T) {
	stubCountryLookup(t, map[string]string{"8.8.8.8": "us"})
	incoming := &pb.Host{Ip: "8.8.8.8", Version: "1.0.0"}
	got := enrichPBHost(incoming, "", "")
	if got == incoming {
		t.Fatal("GeoIP fill must clone so the original event is not mutated")
	}
	if incoming.GetCountryCode() != "" {
		t.Fatalf("original mutated: %q", incoming.GetCountryCode())
	}
	if got.GetCountryCode() == "" {
		t.Fatal("expected GeoIP country code")
	}
	if got.GetIp() != "8.8.8.8" || got.GetVersion() != "1.0.0" {
		t.Fatalf("other fields drifted: %#v", got)
	}
}

func TestEnrichPBHostManualCodeWins(t *testing.T) {
	incoming := &pb.Host{Ip: "8.8.8.8", CountryCode: "CN"}
	got := enrichPBHost(incoming, "1.1.1.1", "us")
	if got != incoming || got.GetCountryCode() != "CN" {
		t.Fatalf("manual code should win, got %#v", got)
	}
}

func TestEnrichPBHostKeepsPreviousWhenIPEmpty(t *testing.T) {
	incoming := &pb.Host{Ip: "", Version: "2"}
	got := enrichPBHost(incoming, "8.8.8.8", "us")
	if got.GetIp() != "8.8.8.8" {
		t.Fatalf("ip=%q", got.GetIp())
	}
	if got.GetCountryCode() != "us" {
		t.Fatalf("country=%q", got.GetCountryCode())
	}
	if got.GetVersion() != "2" {
		t.Fatalf("version=%q", got.GetVersion())
	}
}

func TestEmptyHostIPDoesNotOverwriteLastKnown(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.Server{}, &model.ServerNodeBinding{}, &model.ObserverAssignment{}, &model.Collector{}, &model.CollectorScope{},
		&model.ServerRuntime{},
	); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Server{Common: model.Common{ID: 9}, Name: "node-9", Secret: "secret-9"}).Error; err != nil {
		t.Fatal(err)
	}
	previousDB, previousConf, previousServers := DB, Conf, ServerList
	DB = db
	Conf = &model.Config{Telemetry: model.TelemetryConfig{OfflineThresholdSeconds: 30}}
	ServerList = map[uint64]*model.Server{9: {Common: model.Common{ID: 9}, State: &model.HostState{}, Host: &model.Host{}}}
	t.Cleanup(func() {
		DB, Conf, ServerList = previousDB, previousConf, previousServers
		_ = CloseDB(db)
	})

	node := bytes.Repeat([]byte{0x51}, 16)
	session := bytes.Repeat([]byte{0x52}, 16)
	now := time.Now()
	if _, err := BindServerNode(9, node, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}

	knownIP := "203.0.113.10/2001:db8::1"
	if err := ApplyV2Event(&pb.TelemetryEvent{
		EventId: bytes.Repeat([]byte{0x01}, 16), NodeUuid: node, SessionId: session, Sequence: 1,
		CollectedAtUnixNano: now.UnixNano(),
		Payload:             &pb.TelemetryEvent_Host{Host: &pb.Host{Ip: knownIP, Version: "1.0.0"}},
	}, now); err != nil {
		t.Fatal(err)
	}
	if ServerList[9].Host == nil || ServerList[9].Host.IP != knownIP {
		t.Fatalf("initial host IP=%q", ServerList[9].Host.IP)
	}

	later := now.Add(time.Second)
	if err := ApplyV2Event(&pb.TelemetryEvent{
		EventId: bytes.Repeat([]byte{0x02}, 16), NodeUuid: node, SessionId: session, Sequence: 2,
		CollectedAtUnixNano: later.UnixNano(),
		Payload:             &pb.TelemetryEvent_Host{Host: &pb.Host{Ip: "", Version: "1.0.1"}},
	}, later); err != nil {
		t.Fatal(err)
	}
	if ServerList[9].Host.IP != knownIP {
		t.Fatalf("empty host overwrote IP: %q", ServerList[9].Host.IP)
	}
	if ServerList[9].Host.Version != "1.0.1" {
		t.Fatalf("expected other host fields to update, version=%q", ServerList[9].Host.Version)
	}

	var runtime model.ServerRuntime
	if err := db.First(&runtime, "server_id = ?", 9).Error; err != nil {
		t.Fatal(err)
	}
	if runtime.LastIP != knownIP {
		t.Fatalf("LastIP=%q", runtime.LastIP)
	}
	var stored pb.Host
	if err := proto.Unmarshal(runtime.HostPayload, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.GetIp() != knownIP {
		t.Fatalf("HostPayload IP=%q", stored.GetIp())
	}

	rtNow := later.Add(time.Second)
	if err := ApplyRealtimeSnapshot(&pb.RealtimeSnapshot{
		NodeUuid: node, SessionId: session, LatestSequence: 3,
		CollectedAtUnixNano: rtNow.UnixNano(),
		Host:                &pb.Host{Ip: "", Platform: "linux"},
	}, rtNow); err != nil {
		t.Fatal(err)
	}
	if ServerList[9].Host.IP != knownIP {
		t.Fatalf("realtime empty host overwrote IP: %q", ServerList[9].Host.IP)
	}
	if ServerList[9].Host.Platform != "linux" {
		t.Fatalf("platform=%q", ServerList[9].Host.Platform)
	}

	updatedIP := "198.51.100.8"
	fresh := rtNow.Add(time.Second)
	if err := ApplyV2Event(&pb.TelemetryEvent{
		EventId: bytes.Repeat([]byte{0x03}, 16), NodeUuid: node, SessionId: session, Sequence: 4,
		CollectedAtUnixNano: fresh.UnixNano(),
		Payload:             &pb.TelemetryEvent_Host{Host: &pb.Host{Ip: updatedIP}},
	}, fresh); err != nil {
		t.Fatal(err)
	}
	if ServerList[9].Host.IP != updatedIP {
		t.Fatalf("non-empty IP should update, got %q", ServerList[9].Host.IP)
	}
}

func TestV2HostFillsCountryCodeFromGeoIP(t *testing.T) {
	stubCountryLookup(t, map[string]string{"8.8.8.8": "us"})
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.Server{}, &model.ServerNodeBinding{}, &model.ObserverAssignment{}, &model.Collector{}, &model.CollectorScope{},
		&model.ServerRuntime{},
	); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Server{Common: model.Common{ID: 11}, Name: "node-11", Secret: "secret-11"}).Error; err != nil {
		t.Fatal(err)
	}
	previousDB, previousConf, previousServers := DB, Conf, ServerList
	DB = db
	Conf = &model.Config{Telemetry: model.TelemetryConfig{OfflineThresholdSeconds: 30}}
	ServerList = map[uint64]*model.Server{11: {Common: model.Common{ID: 11}, State: &model.HostState{}, Host: &model.Host{}}}
	t.Cleanup(func() {
		DB, Conf, ServerList = previousDB, previousConf, previousServers
		_ = CloseDB(db)
	})

	node := bytes.Repeat([]byte{0x61}, 16)
	session := bytes.Repeat([]byte{0x62}, 16)
	now := time.Now()
	if _, err := BindServerNode(11, node, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}

	if err := ApplyV2Event(&pb.TelemetryEvent{
		EventId: bytes.Repeat([]byte{0x11}, 16), NodeUuid: node, SessionId: session, Sequence: 1,
		CollectedAtUnixNano: now.UnixNano(),
		Payload:             &pb.TelemetryEvent_Host{Host: &pb.Host{Ip: "8.8.8.8", Version: "1.0.0"}},
	}, now); err != nil {
		t.Fatal(err)
	}
	code := ServerList[11].Host.CountryCode
	if code == "" {
		t.Fatal("expected GeoIP CountryCode on first host")
	}

	later := now.Add(time.Second)
	if err := ApplyV2Event(&pb.TelemetryEvent{
		EventId: bytes.Repeat([]byte{0x12}, 16), NodeUuid: node, SessionId: session, Sequence: 2,
		CollectedAtUnixNano: later.UnixNano(),
		Payload:             &pb.TelemetryEvent_Host{Host: &pb.Host{Ip: "", Version: "1.0.1"}},
	}, later); err != nil {
		t.Fatal(err)
	}
	if ServerList[11].Host.CountryCode != code {
		t.Fatalf("empty host overwrote CountryCode: %q", ServerList[11].Host.CountryCode)
	}

	var runtime model.ServerRuntime
	if err := db.First(&runtime, "server_id = ?", 11).Error; err != nil {
		t.Fatal(err)
	}
	var stored pb.Host
	if err := proto.Unmarshal(runtime.HostPayload, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.GetCountryCode() != code {
		t.Fatalf("HostPayload CountryCode=%q", stored.GetCountryCode())
	}

	manual := later.Add(time.Second)
	if err := ApplyV2Event(&pb.TelemetryEvent{
		EventId: bytes.Repeat([]byte{0x13}, 16), NodeUuid: node, SessionId: session, Sequence: 3,
		CollectedAtUnixNano: manual.UnixNano(),
		Payload:             &pb.TelemetryEvent_Host{Host: &pb.Host{Ip: "8.8.8.8", CountryCode: "CN"}},
	}, manual); err != nil {
		t.Fatal(err)
	}
	if ServerList[11].Host.CountryCode != "CN" {
		t.Fatalf("manual CountryCode should win, got %q", ServerList[11].Host.CountryCode)
	}
}

func TestEndpointAssignmentPrimaryTLSFollowsGRPCTLS(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Server{}, &model.ServerNodeBinding{}, &model.ObserverAssignment{}, &model.Collector{}, &model.CollectorScope{}); err != nil {
		t.Fatal(err)
	}
	previousDB, previousConf := DB, Conf
	DB = db
	Conf = &model.Config{GRPCPort: 5555, GRPCTLS: model.GRPCTLSConfig{Enabled: true}, Telemetry: model.TelemetryConfig{PrimaryEndpoint: "primary.example:5555"}}
	t.Cleanup(func() {
		DB = previousDB
		Conf = previousConf
		_ = CloseDB(db)
	})
	node, session := bytes.Repeat([]byte{0x51}, 16), bytes.Repeat([]byte{0x52}, 16)
	assignment, err := EndpointAssignmentForNode(node, session, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(assignment.GetEndpoints()) != 1 || !assignment.GetEndpoints()[0].GetTls() {
		t.Fatalf("primary tls=%v", assignment.GetEndpoints())
	}
}

func TestEndpointAssignmentForNodeNilConf(t *testing.T) {
	previous := Conf
	Conf = nil
	t.Cleanup(func() { Conf = previous })
	node, session := bytes.Repeat([]byte{0x51}, 16), bytes.Repeat([]byte{0x52}, 16)
	_, err := EndpointAssignmentForNode(node, session, 1)
	if err == nil {
		t.Fatal("expected error when Conf is nil")
	}
}

func TestServerIDFromNodeUUIDUsesBindingAndLock(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Server{}, &model.ServerNodeBinding{}, &model.ObserverAssignment{}, &model.Collector{}, &model.CollectorScope{}); err != nil {
		t.Fatal(err)
	}
	previousDB, previousList := DB, ServerList
	DB = db
	InitServer()
	ServerList[8] = &model.Server{Common: model.Common{ID: 8}}
	t.Cleanup(func() {
		DB = previousDB
		ServerList = previousList
		_ = CloseDB(db)
	})
	if err := db.Create(&model.Server{Common: model.Common{ID: 8}, Name: "n8", Secret: "secret-8"}).Error; err != nil {
		t.Fatal(err)
	}
	node := bytes.Repeat([]byte{0x61}, 16)
	if _, err := BindServerNodeForProtocol(8, node, time.Unix(1_800_200_000, 0), pb.SourceProtocol_SOURCE_PROTOCOL_SANTAIZI_V2); err != nil {
		t.Fatal(err)
	}
	got, err := ServerIDFromNodeUUID(node)
	if err != nil || got != 8 {
		t.Fatalf("serverID=%d err=%v", got, err)
	}
	if err := EnsureServerNodeAvailableForEnroll(8, bytes.Repeat([]byte{0x62}, 16)); !IsServerBoundToOtherNode(err) {
		t.Fatalf("conflict err=%v", err)
	}
}

func setupV2OfflineRuntime(t *testing.T, serverID uint64) (node, session []byte, historyID uint64, now time.Time) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.Server{}, &model.ServerNodeBinding{}, &model.ObserverAssignment{}, &model.Collector{}, &model.CollectorScope{},
		&model.ServerRuntime{}, &model.ServerOfflineHistory{},
	); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Server{Common: model.Common{ID: serverID}, Name: "edge-offline", Secret: "secret-offline"}).Error; err != nil {
		t.Fatal(err)
	}
	previousDB, previousConf, previousServers, previousLoc := DB, Conf, ServerList, Loc
	previousNotifications, previousIDToTag := NotificationList, NotificationIDToTag
	DB = db
	Loc = time.UTC
	InitNotification()
	Conf = &model.Config{
		EnableOfflineHistory:       true,
		EnableRecoveryNotification: true,
		Telemetry:                  model.TelemetryConfig{OfflineThresholdSeconds: 30},
	}
	ServerList = map[uint64]*model.Server{
		serverID: {Common: model.Common{ID: serverID}, Name: "edge-offline", State: &model.HostState{}, Host: &model.Host{}},
	}
	t.Cleanup(func() {
		resetNotificationAggregates()
		DB, Conf, ServerList, Loc = previousDB, previousConf, previousServers, previousLoc
		NotificationList, NotificationIDToTag = previousNotifications, previousIDToTag
		_ = CloseDB(db)
	})

	node = bytes.Repeat([]byte{0x71}, 16)
	session = bytes.Repeat([]byte{0x72}, 16)
	now = time.Now()
	if _, err := BindServerNode(serverID, node, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	lastSeen := now.Add(-time.Hour)
	rt := model.ServerRuntime{
		ServerID:     serverID,
		Status:       model.ServerRuntimeStatusOffline,
		LastSeenAt:   &lastSeen,
		LastBootTime: 100,
		LastUptime:   50,
		LastIP:       "203.0.113.10",
		Protocol:     "v2",
	}
	if err := db.Create(&rt).Error; err != nil {
		t.Fatal(err)
	}
	open := model.ServerOfflineHistory{
		ServerID:     serverID,
		StartedAt:    lastSeen.Add(30 * time.Second),
		DetectedAt:   lastSeen.Add(40 * time.Second),
		Status:       model.OfflineHistoryStatusOpen,
		Reason:       model.OfflineReasonUnknown,
		LastSeenAt:   lastSeen,
		LastBootTime: 100,
		LastUptime:   50,
		LastIP:       "203.0.113.10",
	}
	if err := db.Create(&open).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&rt).Update("current_offline_id", open.ID).Error; err != nil {
		t.Fatal(err)
	}
	return node, session, open.ID, now
}

func assertV2OfflineHistoryClosed(t *testing.T, serverID, historyID uint64, wantReason string) {
	t.Helper()
	var gotRt model.ServerRuntime
	if err := DB.Where("server_id = ?", serverID).First(&gotRt).Error; err != nil {
		t.Fatal(err)
	}
	if gotRt.Status != model.ServerRuntimeStatusOnline {
		t.Errorf("status=%q", gotRt.Status)
	}
	if gotRt.CurrentOfflineID != 0 {
		t.Errorf("CurrentOfflineID=%d", gotRt.CurrentOfflineID)
	}
	var gotH model.ServerOfflineHistory
	if err := DB.First(&gotH, historyID).Error; err != nil {
		t.Fatal(err)
	}
	if gotH.Status != model.OfflineHistoryStatusClosed || gotH.EndedAt == nil || gotH.RecoveredAt == nil {
		t.Fatalf("history status=%q ended=%v recovered=%v", gotH.Status, gotH.EndedAt, gotH.RecoveredAt)
	}
	if wantReason != "" && gotH.Reason != wantReason {
		t.Errorf("reason=%q want %q", gotH.Reason, wantReason)
	}
}

func TestV2StateReportClosesOpenOfflineHistory(t *testing.T) {
	const serverID = uint64(21)
	node, session, historyID, now := setupV2OfflineRuntime(t, serverID)
	if err := ApplyV2Event(&pb.TelemetryEvent{
		EventId: bytes.Repeat([]byte{0x81}, 16), NodeUuid: node, SessionId: session, Sequence: 8,
		CollectedAtUnixNano: now.UnixNano(),
		Payload:             &pb.TelemetryEvent_State{State: &pb.State{Uptime: 60}},
	}, now); err != nil {
		t.Fatal(err)
	}
	assertV2OfflineHistoryClosed(t, serverID, historyID, model.OfflineReasonNetworkDisconnect)
}

func TestV2HeartbeatClosesOpenOfflineHistory(t *testing.T) {
	const serverID = uint64(22)
	node, session, historyID, now := setupV2OfflineRuntime(t, serverID)
	if err := ApplyV2Event(&pb.TelemetryEvent{
		EventId: bytes.Repeat([]byte{0x82}, 16), NodeUuid: node, SessionId: session, Sequence: 9,
		CollectedAtUnixNano: now.UnixNano(),
	}, now); err != nil {
		t.Fatal(err)
	}
	assertV2OfflineHistoryClosed(t, serverID, historyID, model.OfflineReasonNetworkDisconnect)
}

func TestProbeCollectorSkippedFromAssignments(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Server{}, &model.ServerNodeBinding{}, &model.ObserverAssignment{}, &model.Collector{}, &model.CollectorScope{}); err != nil {
		t.Fatal(err)
	}
	previous, previousConf := DB, Conf
	DB = db
	Conf = &model.Config{GRPCPort: 5555, Telemetry: model.TelemetryConfig{PrimaryEndpoint: "primary.example:5555"}}
	t.Cleanup(func() { DB = previous; Conf = previousConf; _ = CloseDB(db) })
	if err := db.Create(&model.Server{Common: model.Common{ID: 4}, Name: "n4", Secret: "secret-4"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Collector{CollectorUUID: "probe-x", Name: "probe", Kind: model.CollectorKindProbe, Address: "", TokenHash: bytes.Repeat([]byte{7}, 32), RegistrationToken: "token-probe", Generation: 1, ConfigVersion: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CollectorScope{CollectorUUID: "probe-x", ScopeType: "all"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Collector{CollectorUUID: "obs-x", Name: "obs", Kind: model.CollectorKindObserver, Address: "obs.example:5556", TokenHash: bytes.Repeat([]byte{8}, 32), RegistrationToken: "token-obs", Generation: 1, ConfigVersion: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CollectorScope{CollectorUUID: "obs-x", ScopeType: "all"}).Error; err != nil {
		t.Fatal(err)
	}
	node := bytes.Repeat([]byte{0x44}, 16)
	if _, err := BindServerNodeForProtocol(4, node, time.Unix(1_800_000_000, 0), pb.SourceProtocol_SOURCE_PROTOCOL_SANTAIZI_V2); err != nil {
		t.Fatal(err)
	}
	var rows []model.ObserverAssignment
	if err := db.Where("valid_to = 0").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ObserverID == "probe-x" {
			t.Fatal("probe collector must not receive observer assignment")
		}
	}
	assignment, err := EndpointAssignmentForNode(node, bytes.Repeat([]byte{0x55}, 16), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range assignment.Endpoints {
		if endpoint.GetEndpointId() == "probe-x" {
			t.Fatal("probe collector must not appear in agent endpoint assignment")
		}
	}
}
