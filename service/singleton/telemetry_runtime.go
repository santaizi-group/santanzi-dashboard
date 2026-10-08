package singleton

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/pkg/geoip"
	pb "github.com/hi2shark/santaizi-dashboard/proto"
	"github.com/hi2shark/santaizi-dashboard/service/availability"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
)

const primaryObserverID = "primary"

var lookupCountryCode = geoip.LookupCodeFromAddr

func BindServerNode(serverID uint64, nodeUUID []byte, now time.Time) (bool, error) {
	return BindServerNodeForProtocol(serverID, nodeUUID, now, pb.SourceProtocol_SOURCE_PROTOCOL_UNSPECIFIED)
}

// BindServerNodeForProtocol atomically updates the authenticated server binding.
// A V2-to-V2 identity replacement is preserved as a P0 lifecycle fact so a
// reinstall never rewrites the history belonging to the previous node UUID.
func BindServerNodeForProtocol(serverID uint64, nodeUUID []byte, now time.Time, source pb.SourceProtocol) (bool, error) {
	if len(nodeUUID) != 16 {
		return false, errors.New("node UUID must be 16 bytes")
	}
	bindingReason := "authenticated_control_binding"
	if source == pb.SourceProtocol_SOURCE_PROTOCOL_SANTAIZI_V2 {
		bindingReason = "authenticated_v2_control_binding"
	}
	changed := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		var current model.ServerNodeBinding
		err := tx.Where("server_id = ? AND current = ?", serverID, true).First(&current).Error
		if err == nil && bytes.Equal(current.NodeUUID, nodeUUID) {
			return nil
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil {
			previousNodeUUID := append([]byte(nil), current.NodeUUID...)
			current.Current = false
			current.ValidTo = now.UnixNano()
			if err := tx.Save(&current).Error; err != nil {
				return err
			}
			if err := closeObserverAssignments(tx, current.NodeUUID, now); err != nil {
				return err
			}
			if err := availability.CloseOpenIncidents(tx, previousNodeUUID, now); err != nil {
				return err
			}
			if current.Reason == "authenticated_v2_control_binding" && source == pb.SourceProtocol_SOURCE_PROTOCOL_SANTAIZI_V2 {
				if err := recordIdentityChange(tx, nodeUUID, previousNodeUUID, now); err != nil {
					return err
				}
			}
		}
		binding := model.ServerNodeBinding{
			ServerID: serverID, NodeUUID: append([]byte(nil), nodeUUID...),
			ValidFrom: now.UnixNano(), Current: true, Reason: bindingReason,
		}
		if err := tx.Create(&binding).Error; err != nil {
			return err
		}
		assignment := model.ObserverAssignment{
			NodeUUID: append([]byte(nil), nodeUUID...), ObserverID: primaryObserverID,
			ValidFrom: now.UnixNano(), ConfigVersion: currentTelemetryConfigVersion(tx), Generation: 1,
		}
		if err := tx.Create(&assignment).Error; err != nil {
			return err
		}
		if err := syncCollectorAssignmentsForServer(tx, serverID, nodeUUID, now); err != nil {
			return err
		}
		changed = true
		return nil
	})
	return changed, err
}

// RefreshObserverAssignmentsForServer applies current Collector scopes after a
// Server group/tag change and advances only the Collector config versions whose
// effective assignments changed.
func RefreshObserverAssignmentsForServer(serverID uint64, now time.Time) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var binding model.ServerNodeBinding
		if err := tx.Where("server_id = ? AND current = ?", serverID, true).First(&binding).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		return syncCollectorAssignmentsForServer(tx, serverID, binding.NodeUUID, now)
	})
}

// EndServerNodeBinding ends only future assignments. Historical bindings,
// observations and availability evidence remain immutable.
func EndServerNodeBinding(serverID uint64, now time.Time) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var bindings []model.ServerNodeBinding
		if err := tx.Where("server_id = ? AND current = ?", serverID, true).Find(&bindings).Error; err != nil {
			return err
		}
		for _, binding := range bindings {
			if err := tx.Model(&binding).Updates(map[string]any{"current": false, "valid_to": now.UnixNano()}).Error; err != nil {
				return err
			}
			if err := closeObserverAssignments(tx, binding.NodeUUID, now); err != nil {
				return err
			}
			if err := availability.CloseOpenIncidents(tx, binding.NodeUUID, now); err != nil {
				return err
			}
		}
		return nil
	})
}

func closeObserverAssignments(tx *gorm.DB, nodeUUID []byte, now time.Time) error {
	var assignments []model.ObserverAssignment
	if err := tx.Where("node_uuid = ? AND valid_to = 0", nodeUUID).Find(&assignments).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.ObserverAssignment{}).Where("node_uuid = ? AND valid_to = 0", nodeUUID).
		Update("valid_to", now.UnixNano()).Error; err != nil {
		return err
	}
	seen := make(map[string]bool)
	for _, assignment := range assignments {
		if assignment.ObserverID == primaryObserverID || seen[assignment.ObserverID] {
			continue
		}
		seen[assignment.ObserverID] = true
		if _, err := bumpCollectorConfigVersion(tx, assignment.ObserverID); err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	return nil
}

func syncCollectorAssignmentsForServer(tx *gorm.DB, serverID uint64, nodeUUID []byte, now time.Time) error {
	var server model.Server
	if err := tx.First(&server, serverID).Error; err != nil {
		return err
	}
	var collectors []model.Collector
	if err := tx.Where("revoked = ? AND deleted = ?", false, false).Find(&collectors).Error; err != nil {
		return err
	}
	desired := make(map[string]model.Collector)
	for _, collector := range collectors {
		if collector.IsProbe() {
			continue
		}
		var scopes []model.CollectorScope
		if err := tx.Where("collector_uuid = ?", collector.CollectorUUID).Find(&scopes).Error; err != nil {
			return err
		}
		if CollectorScopesIncludeServer(scopes, server) {
			desired[collector.CollectorUUID] = collector
		}
	}
	var active []model.ObserverAssignment
	if err := tx.Where("node_uuid = ? AND observer_id != ? AND valid_to = 0", nodeUUID, primaryObserverID).Find(&active).Error; err != nil {
		return err
	}
	current := make(map[string]model.ObserverAssignment)
	for _, assignment := range active {
		current[assignment.ObserverID] = assignment
	}
	for observerID, assignment := range current {
		if _, keep := desired[observerID]; keep {
			continue
		}
		if err := tx.Model(&assignment).Update("valid_to", now.UnixNano()).Error; err != nil {
			return err
		}
		if _, err := bumpCollectorConfigVersion(tx, observerID); err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	for observerID, collector := range desired {
		if _, exists := current[observerID]; exists {
			continue
		}
		version, err := bumpCollectorConfigVersion(tx, observerID)
		if err != nil {
			return err
		}
		if err := tx.Create(&model.ObserverAssignment{
			NodeUUID: append([]byte(nil), nodeUUID...), ObserverID: observerID, ValidFrom: now.UnixNano(),
			ConfigVersion: version, Generation: collector.Generation,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func RefreshProbeCollectorConfigsForServer(serverID uint64) error {
	if DB == nil || serverID == 0 {
		return nil
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		return bumpProbeCollectorsForServer(tx, serverID)
	})
}

func bumpProbeCollectorsForServer(tx *gorm.DB, serverID uint64) error {
	var server model.Server
	if err := tx.First(&server, serverID).Error; err != nil {
		return err
	}
	var collectors []model.Collector
	if err := tx.Where("deleted = ? AND revoked = ? AND kind = ?", false, false, model.CollectorKindProbe).Find(&collectors).Error; err != nil {
		return err
	}
	for _, collector := range collectors {
		var scopes []model.CollectorScope
		if err := tx.Where("collector_uuid = ?", collector.CollectorUUID).Find(&scopes).Error; err != nil {
			return err
		}
		if !CollectorScopesIncludeServer(scopes, server) {
			continue
		}
		if _, err := bumpCollectorConfigVersion(tx, collector.CollectorUUID); err != nil {
			return err
		}
	}
	return nil
}

func bumpCollectorConfigVersion(tx *gorm.DB, collectorUUID string) (uint64, error) {
	var collector model.Collector
	if err := tx.First(&collector, "collector_uuid = ?", collectorUUID).Error; err != nil {
		return 0, err
	}
	collector.ConfigVersion++
	if err := tx.Model(&collector).Update("config_version", collector.ConfigVersion).Error; err != nil {
		return 0, err
	}
	return collector.ConfigVersion, nil
}

func CollectorScopesIncludeServer(scopes []model.CollectorScope, server model.Server) bool {
	for _, scope := range scopes {
		switch scope.ScopeType {
		case "all":
			return true
		case "server":
			if scope.ScopeValue == fmt.Sprintf("%d", server.ID) {
				return true
			}
		case "group", "tag":
			if scope.ScopeValue == server.Tag {
				return true
			}
		}
	}
	return false
}

func recordIdentityChange(tx *gorm.DB, nodeUUID, previousNodeUUID []byte, now time.Time) error {
	sessionID := make([]byte, 16)
	if _, err := rand.Read(sessionID); err != nil {
		return fmt.Errorf("create identity-change session: %w", err)
	}
	var input [40]byte
	copy(input[:16], nodeUUID)
	copy(input[16:32], sessionID)
	binary.BigEndian.PutUint64(input[32:], 1)
	digest := sha256.Sum256(input[:])
	eventID := append([]byte(nil), digest[:16]...)
	event := &pb.TelemetryEvent{
		EventId: eventID, NodeUuid: append([]byte(nil), nodeUUID...), SessionId: sessionID, Sequence: 1,
		EventType:           pb.TelemetryEventType_TELEMETRY_EVENT_TYPE_LIFECYCLE,
		Priority:            pb.TelemetryPriority_TELEMETRY_PRIORITY_P0_CRITICAL,
		CollectedAtUnixNano: now.UnixNano(), SourceProtocol: pb.SourceProtocol_SOURCE_PROTOCOL_SANTAIZI_V2,
		Reliability: pb.Reliability_RELIABILITY_BEST_EFFORT, ProtocolVersion: 2,
		Payload: &pb.TelemetryEvent_Lifecycle{Lifecycle: &pb.LifecyclePayload{
			Kind:   pb.LifecycleKind_LIFECYCLE_KIND_AGENT_IDENTITY_CHANGED,
			Reason: "authenticated node UUID changed", PreviousNodeUuid: append([]byte(nil), previousNodeUUID...),
		}},
	}
	payload, err := proto.Marshal(event)
	if err != nil {
		return err
	}
	if err := tx.Create(&model.TelemetryEvent{
		EventID: eventID, NodeUUID: append([]byte(nil), nodeUUID...), SessionID: sessionID, Sequence: 1,
		EventType: int32(event.GetEventType()), Priority: int32(event.GetPriority()), CollectedAt: now.UnixNano(),
		SourceProtocol: int32(event.GetSourceProtocol()), Reliability: int32(event.GetReliability()), Payload: payload, PayloadRetained: true,
	}).Error; err != nil {
		return err
	}
	return tx.Create(&model.TelemetryObservation{
		EventID: eventID, ObserverID: primaryObserverID, NodeUUID: append([]byte(nil), nodeUUID...), ReceivedAt: now.UnixNano(),
	}).Error
}

func CurrentTelemetryConfigVersion() uint64 {
	return currentTelemetryConfigVersion(DB)
}

func currentTelemetryConfigVersion(db *gorm.DB) uint64 {
	var version uint64
	db.Model(&model.Collector{}).Select("COALESCE(MAX(config_version), 0)").Scan(&version)
	if version == 0 {
		return 1
	}
	return version
}

func EndpointAssignmentForNode(nodeUUID, sessionID []byte, activationSequence uint64) (*pb.EndpointAssignment, error) {
	if len(nodeUUID) != 16 || len(sessionID) != 16 {
		return nil, errors.New("node UUID and session ID must be 16 bytes")
	}
	if activationSequence == 0 {
		activationSequence = 1
	}
	conf := Conf
	if conf == nil {
		return nil, errors.New("config is not initialized")
	}
	version := CurrentTelemetryConfigVersion()
	primaryAddress := conf.Telemetry.PrimaryEndpoint
	if primaryAddress == "" {
		primaryAddress = fmt.Sprintf("localhost:%d", conf.GRPCPort)
	}
	assignment := &pb.EndpointAssignment{ConfigVersion: version, Endpoints: []*pb.TelemetryEndpoint{{
		EndpointId: "primary", Kind: pb.EndpointKind_ENDPOINT_KIND_PRIMARY, Address: primaryAddress,
		Reliable: true, Tls: conf.TLS || conf.GRPCTLS.Enabled, Generation: 1, ActivationSessionId: append([]byte(nil), sessionID...),
		ActivationSequence: 1,
	}}}
	var observerAssignments []model.ObserverAssignment
	if err := DB.Where("node_uuid = ? AND observer_id != ? AND valid_to = 0", nodeUUID, primaryObserverID).Find(&observerAssignments).Error; err != nil {
		return nil, err
	}
	for _, observer := range observerAssignments {
		var collector model.Collector
		if err := DB.Where("collector_uuid = ? AND revoked = ? AND deleted = ?", observer.ObserverID, false, false).First(&collector).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return nil, err
		}
		if collector.IsProbe() {
			continue
		}
		assignment.Endpoints = append(assignment.Endpoints, &pb.TelemetryEndpoint{
			EndpointId: collector.CollectorUUID, Kind: pb.EndpointKind_ENDPOINT_KIND_COLLECTOR,
			Address: collector.Address, Reliable: true, Tls: collector.TLS, InsecureTls: collector.InsecureTLS,
			Generation: collector.Generation, ActivationSessionId: append([]byte(nil), sessionID...), ActivationSequence: activationSequence,
		})
	}
	return assignment, nil
}

// ServerIDFromNodeUUID resolves the live server ID for a bound node UUID.
// It reads ServerNodeBinding then checks ServerList under ServerLock.
func ServerIDFromNodeUUID(nodeUUID []byte) (uint64, error) {
	if len(nodeUUID) != 16 {
		return 0, errors.New("node UUID must be 16 bytes")
	}
	var binding model.ServerNodeBinding
	if err := DB.Where("node_uuid = ? AND current = ?", nodeUUID, true).First(&binding).Error; err != nil {
		return 0, err
	}
	ServerLock.RLock()
	defer ServerLock.RUnlock()
	if _, ok := ServerList[binding.ServerID]; !ok {
		return 0, fmt.Errorf("server %d is not loaded", binding.ServerID)
	}
	return binding.ServerID, nil
}

// EnsureServerNodeAvailableForEnroll reports whether the server is currently
// bound to a different node UUID. Enroll still allows rebind when no Control
// session is online (uninstall/reinstall); a live session keeps the conflict.
func EnsureServerNodeAvailableForEnroll(serverID uint64, nodeUUID []byte) error {
	if len(nodeUUID) != 16 {
		return errors.New("node UUID must be 16 bytes")
	}
	var binding model.ServerNodeBinding
	err := DB.Where("server_id = ? AND current = ?", serverID, true).First(&binding).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !bytes.Equal(binding.NodeUUID, nodeUUID) {
		return errServerBoundToOtherNode
	}
	return nil
}

var errServerBoundToOtherNode = errors.New("server is already bound to another node")

func IsServerBoundToOtherNode(err error) bool {
	return errors.Is(err, errServerBoundToOtherNode)
}

// trustedRuntimeSkew 与 service/telemetry 的时钟可信窗一致。超出的事件只留证据，不改运行态。
const trustedRuntimeSkew = 5 * time.Minute

// ApplyTrustedTelemetryEvents 用从端复制来的事件结束 recovering 并刷新主机快照。
// 时钟不可信的事件跳过。已处理过的序号由 touchV2Runtime 丢掉，重复批次可以再调。
func ApplyTrustedTelemetryEvents(events []*pb.TelemetryEvent, receivedAt time.Time) error {
	for _, event := range events {
		if event == nil || len(event.GetNodeUuid()) != 16 {
			continue
		}
		delta := receivedAt.Sub(time.Unix(0, event.GetCollectedAtUnixNano()))
		if delta < 0 {
			delta = -delta
		}
		if delta > trustedRuntimeSkew {
			continue
		}
		if err := ApplyV2Event(event, receivedAt); err != nil {
			return err
		}
	}
	return nil
}

func ApplyV2Event(event *pb.TelemetryEvent, receivedAt time.Time) error {
	if event == nil {
		return nil
	}
	var state *pb.State
	var host *pb.Host
	switch payload := event.GetPayload().(type) {
	case *pb.TelemetryEvent_State:
		state = payload.State
	case *pb.TelemetryEvent_Host:
		host = payload.Host
	default:
		return touchV2Runtime(event.GetNodeUuid(), event.GetSessionId(), event.GetSequence(), event.GetCollectedAtUnixNano(), receivedAt, nil, nil)
	}
	return touchV2Runtime(event.GetNodeUuid(), event.GetSessionId(), event.GetSequence(), event.GetCollectedAtUnixNano(), receivedAt, state, host)
}

func ApplyRealtimeSnapshot(snapshot *pb.RealtimeSnapshot, receivedAt time.Time) error {
	if snapshot == nil || len(snapshot.GetNodeUuid()) != 16 || len(snapshot.GetSessionId()) != 16 {
		return errors.New("invalid realtime snapshot identity")
	}
	if receivedAt.Sub(time.Unix(0, snapshot.GetCollectedAtUnixNano())) > time.Duration(Conf.Telemetry.OfflineThresholdSeconds)*time.Second {
		return nil
	}
	return touchV2Runtime(snapshot.GetNodeUuid(), snapshot.GetSessionId(), snapshot.GetLatestSequence(), snapshot.GetCollectedAtUnixNano(), receivedAt, snapshot.GetState(), snapshot.GetHost())
}

func preservePBHostIP(host *pb.Host, previousIP string) *pb.Host {
	if host == nil {
		return nil
	}
	if strings.TrimSpace(host.GetIp()) != "" || strings.TrimSpace(previousIP) == "" {
		return host
	}
	cloned := proto.Clone(host).(*pb.Host)
	cloned.Ip = previousIP
	return cloned
}

func previousHostCountry(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	previous := new(pb.Host)
	if proto.Unmarshal(payload, previous) != nil {
		return ""
	}
	return strings.TrimSpace(previous.GetCountryCode())
}

// enrichPBHost keeps LastIP / last CountryCode across empty HOST frames, then
// fills CountryCode from the embedded GeoIP database when the agent did not
// send a manual code. V2 never calls the unary LookupGeoIP RPC.
func enrichPBHost(host *pb.Host, previousIP, previousCountry string) *pb.Host {
	if host == nil {
		return nil
	}
	merged := preservePBHostIP(host, previousIP)
	if strings.TrimSpace(merged.GetCountryCode()) != "" {
		return merged
	}
	ip := strings.TrimSpace(merged.GetIp())
	prevIP := strings.TrimSpace(previousIP)
	prevCode := strings.TrimSpace(previousCountry)
	ipUnchanged := ip == "" || ip == prevIP
	code := ""
	if ipUnchanged {
		code = prevCode
	}
	if code == "" {
		code = lookupCountryCode(ip)
	}
	if code == "" {
		code = prevCode
	}
	if code == "" {
		return merged
	}
	if merged == host {
		merged = proto.Clone(host).(*pb.Host)
		if strings.TrimSpace(merged.GetIp()) == "" && prevIP != "" {
			merged.Ip = prevIP
		}
	}
	merged.CountryCode = code
	return merged
}

func touchV2Runtime(nodeUUID, sessionID []byte, sequence uint64, collectedAt int64, receivedAt time.Time, state *pb.State, host *pb.Host) error {
	var binding model.ServerNodeBinding
	if err := DB.Where("node_uuid = ? AND current = ?", nodeUUID, true).First(&binding).Error; err != nil {
		return err
	}
	var statePayload, hostPayload []byte
	var err error
	if state != nil {
		statePayload, err = proto.Marshal(state)
		if err != nil {
			return err
		}
	}
	if host != nil {
		hostPayload, err = proto.Marshal(host)
		if err != nil {
			return err
		}
	}

	var mergedHost *pb.Host
	var closed *model.ServerOfflineHistory
	ipChanged := false
	serverRuntimeMu.Lock()
	applied := false
	err = DB.Transaction(func(tx *gorm.DB) error {
		var runtime model.ServerRuntime
		findErr := tx.Where("server_id = ?", binding.ServerID).First(&runtime).Error
		if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			runtime.ServerID = binding.ServerID
		}
		if runtime.LastCollectedAt > collectedAt ||
			(runtime.LastCollectedAt == collectedAt && bytes.Equal(runtime.CurrentSessionID, sessionID) && runtime.CurrentSequence >= sequence) {
			return nil
		}
		if host != nil {
			mergedHost = enrichPBHost(host, runtime.LastIP, previousHostCountry(runtime.HostPayload))
		}
		// 必须在覆盖 LastBootTime / LastUptime / LastIP 之前关闭：离线原因比较的是
		// 「离线前」运行态与「本次恢复」上报。V2 以前只把 status 改回 online，
		// 离线记录留给 Reconcile 静默关掉，恢复通知永远发不出来。
		h, closeErr := closeV2OfflineHistory(tx, &runtime, state, mergedHost, receivedAt)
		if closeErr != nil {
			return closeErr
		}
		closed = h
		runtime.Status = model.ServerRuntimeStatusOnline
		runtime.HostState = model.HostStateOnline
		runtime.ConnectivityState = model.ConnectivityFull
		runtime.Protocol = "v2"
		runtime.CurrentNodeUUID = append([]byte(nil), nodeUUID...)
		runtime.CurrentSessionID = append([]byte(nil), sessionID...)
		runtime.CurrentSequence = sequence
		runtime.LastCollectedAt = collectedAt
		runtime.LastReceivedAt = receivedAt.UnixNano()
		runtime.LastSeenAt = &receivedAt
		runtime.LastOnlineAt = &receivedAt
		if len(statePayload) > 0 {
			runtime.StatePayload = statePayload
			if state != nil {
				runtime.LastUptime = state.GetUptime()
			}
		}
		if len(hostPayload) > 0 {
			if mergedHost != host {
				var marshalErr error
				hostPayload, marshalErr = proto.Marshal(mergedHost)
				if marshalErr != nil {
					return marshalErr
				}
			}
			runtime.HostPayload = hostPayload
			if mergedHost != nil {
				runtime.LastBootTime = mergedHost.GetBootTime()
				if ip := strings.TrimSpace(mergedHost.GetIp()); ip != "" {
					if runtime.LastIP != ip {
						ipChanged = true
					}
					runtime.LastIP = ip
				}
				runtime.LastAgentVersion = mergedHost.GetVersion()
			}
		}
		if err := tx.Save(&runtime).Error; err != nil {
			return err
		}
		applied = true
		return nil
	})
	serverRuntimeMu.Unlock()
	if err != nil {
		return err
	}
	if closed != nil {
		afterOfflineHistoryClosed(binding.ServerID, closed)
	}
	if !applied {
		return nil
	}
	if ipChanged {
		_ = RefreshProbeCollectorConfigsForServer(binding.ServerID)
	}

	ServerLock.Lock()
	defer ServerLock.Unlock()
	server := ServerList[binding.ServerID]
	if server == nil {
		return nil
	}
	server.LastActive = receivedAt
	if state != nil {
		converted := model.PB2State(state)
		server.State = &converted
	}
	if mergedHost != nil {
		converted := model.PB2Host(mergedHost)
		if converted.IP == "" && server.Host != nil && strings.TrimSpace(server.Host.IP) != "" {
			converted.IP = server.Host.IP
		}
		if converted.CountryCode == "" && server.Host != nil && strings.TrimSpace(server.Host.CountryCode) != "" {
			converted.CountryCode = server.Host.CountryCode
		}
		server.Host = &converted
	}
	return nil
}

func closeV2OfflineHistory(tx *gorm.DB, runtime *model.ServerRuntime, state *pb.State, host *pb.Host, now time.Time) (*model.ServerOfflineHistory, error) {
	if Conf == nil || !Conf.EnableOfflineHistory || runtime == nil || runtime.CurrentOfflineID == 0 {
		return nil, nil
	}
	var hostState *model.HostState
	var hostModel *model.Host
	if state != nil {
		converted := model.PB2State(state)
		hostState = &converted
	}
	if host != nil {
		converted := model.PB2Host(host)
		hostModel = &converted
	}
	return closeOfflineHistoryTx(tx, runtime, hostState, hostModel, now)
}

func loadTelemetryRuntimeSnapshots() {
	var runtimes []model.ServerRuntime
	if err := DB.Where("protocol = ?", "v2").Find(&runtimes).Error; err != nil {
		return
	}
	for _, runtime := range runtimes {
		server := ServerList[runtime.ServerID]
		if server == nil {
			continue
		}
		if len(runtime.StatePayload) > 0 {
			state := new(pb.State)
			if proto.Unmarshal(runtime.StatePayload, state) == nil {
				converted := model.PB2State(state)
				server.State = &converted
			}
		}
		if len(runtime.HostPayload) > 0 {
			host := new(pb.Host)
			if proto.Unmarshal(runtime.HostPayload, host) == nil {
				converted := model.PB2Host(host)
				server.Host = &converted
			}
		}
		// A persisted snapshot is useful for display but never proves current liveness.
		DB.Model(&model.ServerRuntime{}).Where("server_id = ?", runtime.ServerID).Updates(map[string]any{
			"status": model.ServerRuntimeStatusRecovering, "host_state": model.HostStateUnknown,
			"connectivity_state": model.ConnectivityUnknown,
		})
	}
}
