package controller

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/pkg/mygin"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
	"github.com/hi2shark/santaizi-dashboard/service/telemetry"
	"gorm.io/gorm"
)

func registerAPIV2(root gin.IRouter) {
	registerSPAAPIV2(root)
	read := root.Group("v2")
	read.Use(mygin.Authorize(mygin.AuthorizeOption{
		MemberOnly: false, AllowAPI: true, IsPage: false, Msg: "访问此接口需要认证", Redirect: "/login",
	}))
	read.Use(mygin.ValidateViewPassword(mygin.ValidateViewPasswordOption{IsPage: false, AbortWhenFail: true}))
	read.GET("/runtime/servers", v2RuntimeServers)
	read.GET("/runtime/servers/:id", v2RuntimeServer)
	read.GET("/availability/servers/:id", v2Availability)
	read.GET("/telemetry/servers/:id/status", v2TelemetryStatus)
	read.GET("/incidents", v2Incidents)

	admin := root.Group("v2")
	admin.Use(mygin.Authorize(mygin.AuthorizeOption{
		MemberOnly: true, AllowAPI: true, IsPage: false, Msg: "访问此接口需要管理员认证", Redirect: "/login",
	}))
	admin.Use(mygin.RejectReadOnlyAPITokenWrites())
	admin.GET("/collectors", listCollectors)
	admin.POST("/collectors", createCollector)
	admin.PATCH("/collectors/:id", updateCollector)
	admin.DELETE("/collectors/:id", deleteCollector)
	admin.POST("/collectors/:id/revoke", revokeCollector)
	admin.POST("/collectors/:id/token/rotate", rotateCollectorToken)
	admin.PUT("/collectors/:id/scope", updateCollectorScope)
}

type runtimeServerResponse struct {
	ID              uint64 `json:"id"`
	Name            string `json:"name"`
	NodeUUID        string `json:"node_uuid,omitempty"`
	Protocol        string `json:"protocol"`
	HostState       string `json:"host"`
	Connectivity    string `json:"connectivity"`
	Availability    *bool  `json:"available"`
	Coverage        string `json:"coverage"`
	LastCollectedAt int64  `json:"last_collected_at_unix_nano"`
	LastReceivedAt  int64  `json:"last_received_at_unix_nano"`
}

func v2RuntimeServers(c *gin.Context) {
	var servers []model.Server
	if err := singleton.DB.Order("display_index DESC").Find(&servers).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	response := make([]runtimeServerResponse, 0, len(servers))
	for _, server := range servers {
		response = append(response, runtimeForServer(server))
	}
	c.JSON(http.StatusOK, gin.H{"servers": response})
}

func v2RuntimeServer(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server id"})
		return
	}
	var server model.Server
	if err := singleton.DB.First(&server, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		return
	}
	c.JSON(http.StatusOK, runtimeForServer(server))
}

func runtimeForServer(server model.Server) runtimeServerResponse {
	response := runtimeServerResponse{ID: server.ID, Name: server.Name, HostState: model.HostStateUnknown, Connectivity: model.ConnectivityUnknown, Coverage: "unknown"}
	var runtime model.ServerRuntime
	recovering := false
	if err := singleton.DB.First(&runtime, "server_id = ?", server.ID).Error; err == nil {
		response.NodeUUID = hex.EncodeToString(runtime.CurrentNodeUUID)
		response.Protocol = runtime.Protocol
		response.HostState = runtime.HostState
		response.Connectivity = runtime.ConnectivityState
		response.LastCollectedAt = runtime.LastCollectedAt
		response.LastReceivedAt = runtime.LastReceivedAt
		recovering = runtime.Status == model.ServerRuntimeStatusRecovering
	}
	var availability model.AvailabilityBucket
	if !recovering && response.NodeUUID != "" && singleton.DB.Where("node_uuid = ?", runtime.CurrentNodeUUID).Order("bucket_start DESC").First(&availability).Error == nil {
		response.HostState = availability.HostState
		response.Connectivity = availability.ConnectivityState
		response.Coverage = coverageLabel(availability)
		if availability.ConnectivityState != model.ConnectivityUnknown {
			available := availability.ConnectivityState == model.ConnectivityFull || availability.ConnectivityState == model.ConnectivityPartial
			response.Availability = &available
		}
	}
	return response
}

func coverageLabel(bucket model.AvailabilityBucket) string {
	if bucket.ExpectedObservers == 0 {
		return "unknown"
	}
	return strconv.FormatUint(uint64(bucket.SeenObservers), 10) + "/" + strconv.FormatUint(uint64(bucket.ExpectedObservers), 10)
}

func v2Availability(c *gin.Context) {
	node, ok := currentNodeForServer(c)
	if !ok {
		return
	}
	query := singleton.DB.Where("node_uuid = ?", node)
	if from, err := strconv.ParseInt(c.Query("from"), 10, 64); err == nil {
		query = query.Where("bucket_start >= ?", from)
	}
	if to, err := strconv.ParseInt(c.Query("to"), 10, 64); err == nil {
		query = query.Where("bucket_start <= ?", to)
	}
	var buckets []model.AvailabilityBucket
	if err := query.Order("bucket_start ASC").Limit(5000).Find(&buckets).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"buckets": buckets})
}

func v2TelemetryStatus(c *gin.Context) {
	node, ok := currentNodeForServer(c)
	if !ok {
		return
	}
	var runtime model.AgentTelemetryRuntime
	_ = singleton.DB.First(&runtime, "node_uuid = ?", node).Error
	var cursors []model.TelemetryIngestCursor
	_ = singleton.DB.Where("node_uuid = ?", node).Find(&cursors).Error
	var gaps []model.TelemetryGap
	_ = singleton.DB.Where("node_uuid = ?", node).Order("created_at_unix_nano DESC").Limit(100).Find(&gaps).Error
	c.JSON(http.StatusOK, gin.H{"runtime": runtime, "cursors": cursors, "recent_gaps": gaps})
}

func v2Incidents(c *gin.Context) {
	query := singleton.DB.Order("started_at DESC")
	if serverID := c.Query("server_id"); serverID != "" {
		id, err := strconv.ParseUint(serverID, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server id"})
			return
		}
		var binding model.ServerNodeBinding
		if err := singleton.DB.First(&binding, "server_id = ? AND current = ?", id, true).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "node binding not found"})
			return
		}
		query = query.Where("node_uuid = ?", binding.NodeUUID)
	}
	var incidents []model.AvailabilityIncident
	if err := query.Limit(1000).Find(&incidents).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"incidents": incidents})
}

func currentNodeForServer(c *gin.Context) ([]byte, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server id"})
		return nil, false
	}
	var binding model.ServerNodeBinding
	if err := singleton.DB.First(&binding, "server_id = ? AND current = ?", id, true).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "node binding not found"})
		return nil, false
	}
	return binding.NodeUUID, true
}

type collectorRequest struct {
	Name                 string                  `json:"name" binding:"required"`
	Kind                 string                  `json:"kind"`
	Address              string                  `json:"address"`
	ListenPort           uint                    `json:"listen_port"`
	TLS                  bool                    `json:"tls"`
	InsecureTLS          bool                    `json:"insecure_tls"`
	Location             string                  `json:"location"`
	ProbeIntervalSeconds uint                    `json:"probe_interval_seconds"`
	MTRIntervalSeconds   uint                    `json:"mtr_interval_seconds"`
	MTRProbes            uint                    `json:"mtr_probes"`
	TCPPorts             string                  `json:"tcp_ports"`
	EnableICMP           *bool                   `json:"enable_icmp"`
	EnableTCP            *bool                   `json:"enable_tcp"`
	EnableMTR            *bool                   `json:"enable_mtr"`
	EnableIPv4           *bool                   `json:"enable_ipv4"`
	EnableIPv6           *bool                   `json:"enable_ipv6"`
	Notify               bool                    `json:"notify"`
	NotificationTag      string                  `json:"notification_tag"`
	LatencyNotify        bool                    `json:"latency_notify"`
	MinLatencyMs         float64                 `json:"min_latency_ms"`
	MaxLatencyMs         float64                 `json:"max_latency_ms"`
	FailThreshold        uint                    `json:"fail_threshold"`
	RouteIntervalSeconds uint                    `json:"route_interval_seconds"`
	RouteKeep            uint                    `json:"route_keep"`
	Scopes               []collectorScopeRequest `json:"scopes"`
}

type collectorScopeRequest struct {
	Type  string `json:"type" binding:"required"`
	Value string `json:"value"`
}

func listCollectors(c *gin.Context) {
	var collectors []model.Collector
	if err := singleton.DB.Where("deleted = ?", false).Order("created_at ASC").Find(&collectors).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for index := range collectors {
		collectors[index].TokenHash = nil
	}
	c.JSON(http.StatusOK, gin.H{"collectors": collectors})
}

func createCollector(c *gin.Context) {
	var request collectorRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	kind := model.NormalizeCollectorKind(request.Kind)
	if kind == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "kind must be observer or probe"})
		return
	}
	if kind == model.CollectorKindObserver && strings.TrimSpace(request.Address) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": errObserverAddressRequired.Error()})
		return
	}
	listenPort, err := normalizeCollectorListenPort(request.ListenPort, request.Address)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	id, err := randomCollectorID()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	plain, hash, err := telemetry.NewRegistrationToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	collector := model.Collector{
		CollectorUUID: id, Name: request.Name, Address: request.Address, ListenPort: listenPort, TokenHash: hash,
		RegistrationToken: plain,
		Generation:        1, ConfigVersion: singleton.CurrentTelemetryConfigVersion() + 1, TLS: request.TLS, InsecureTLS: request.InsecureTLS,
		Location: strings.TrimSpace(request.Location), Kind: kind,
	}
	if err := applyCollectorProbeRequest(&collector, request, true); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	collector.ApplyProbeDefaults()
	if err := singleton.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&collector).Error; err != nil {
			return err
		}
		return replaceCollectorScopes(tx, &collector, request.Scopes, time.Now())
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	collector.TokenHash = nil
	c.JSON(http.StatusCreated, gin.H{"collector": collector, "registration_token": plain})
}

func updateCollector(c *gin.Context) {
	var request collectorRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	listenPort, err := normalizeCollectorListenPort(request.ListenPort, request.Address)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updates := map[string]any{"name": request.Name, "address": request.Address, "listen_port": listenPort, "tls": request.TLS, "insecure_tls": request.InsecureTLS, "location": strings.TrimSpace(request.Location), "updated_at": time.Now()}
	result := singleton.DB.Model(&model.Collector{}).Where("collector_uuid = ? AND deleted = ?", c.Param("id"), false).Updates(updates)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "collector not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"updated": true})
}

func deleteCollector(c *gin.Context) {
	if err := disableCollector(c.Param("id"), true); err != nil {
		writeCollectorError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func revokeCollector(c *gin.Context) {
	if err := disableCollector(c.Param("id"), false); err != nil {
		writeCollectorError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"revoked": true})
}

func disableCollector(id string, deleted bool) error {
	return singleton.DB.Transaction(func(tx *gorm.DB) error {
		var collector model.Collector
		if err := tx.First(&collector, "collector_uuid = ? AND deleted = ?", id, false).Error; err != nil {
			return err
		}
		now := time.Now()
		collector.Revoked = true
		collector.Deleted = deleted
		collector.ConfigVersion++
		if err := tx.Save(&collector).Error; err != nil {
			return err
		}
		return tx.Model(&model.ObserverAssignment{}).Where("observer_id = ? AND valid_to = 0", id).Updates(map[string]any{"valid_to": now.UnixNano()}).Error
	})
}

func rotateCollectorToken(c *gin.Context) {
	plain, hash, err := telemetry.NewRegistrationToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var collector model.Collector
	if err := singleton.DB.First(&collector, "collector_uuid = ? AND deleted = ?", c.Param("id"), false).Error; err != nil {
		writeCollectorError(c, err)
		return
	}
	collector.TokenHash, collector.RegistrationToken, collector.Revoked = hash, plain, false
	collector.ConfigVersion++
	if err := singleton.DB.Save(&collector).Error; err != nil {
		writeCollectorError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"registration_token": plain})
}

func updateCollectorScope(c *gin.Context) {
	var scopes []collectorScopeRequest
	if err := c.ShouldBindJSON(&scopes); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	err := singleton.DB.Transaction(func(tx *gorm.DB) error {
		var collector model.Collector
		if err := tx.First(&collector, "collector_uuid = ? AND deleted = ?", c.Param("id"), false).Error; err != nil {
			return err
		}
		collector.ConfigVersion++
		if err := tx.Save(&collector).Error; err != nil {
			return err
		}
		return replaceCollectorScopes(tx, &collector, scopes, time.Now())
	})
	if err != nil {
		writeCollectorError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"updated": true})
}

func replaceCollectorScopes(tx *gorm.DB, collector *model.Collector, scopes []collectorScopeRequest, now time.Time) error {
	var err error
	scopes, err = normalizeCollectorScopes(scopes)
	if err != nil {
		return err
	}
	if err := tx.Where("collector_uuid = ?", collector.CollectorUUID).Delete(&model.CollectorScope{}).Error; err != nil {
		return err
	}
	for _, scope := range scopes {
		if scope.Type != "all" && scope.Type != "server" && scope.Type != "group" && scope.Type != "tag" {
			return errors.New("scope type must be all, server, group, or tag")
		}
		if err := tx.Create(&model.CollectorScope{CollectorUUID: collector.CollectorUUID, ScopeType: scope.Type, ScopeValue: scope.Value}).Error; err != nil {
			return err
		}
	}
	if collector.IsProbe() {
		return tx.Model(&model.ObserverAssignment{}).Where("observer_id = ? AND valid_to = 0", collector.CollectorUUID).
			Updates(map[string]any{"valid_to": now.UnixNano()}).Error
	}
	var bindings []model.ServerNodeBinding
	if err := tx.Where("current = ?", true).Find(&bindings).Error; err != nil {
		return err
	}
	for _, binding := range bindings {
		var server model.Server
		if err := tx.First(&server, binding.ServerID).Error; err != nil {
			return err
		}
		selected := collectorScopesMatch(scopes, server)
		var assignment model.ObserverAssignment
		err := tx.Where("node_uuid = ? AND observer_id = ? AND valid_to = 0", binding.NodeUUID, collector.CollectorUUID).First(&assignment).Error
		if selected && errors.Is(err, gorm.ErrRecordNotFound) {
			if err := tx.Create(&model.ObserverAssignment{
				NodeUUID: binding.NodeUUID, ObserverID: collector.CollectorUUID, ValidFrom: now.UnixNano(),
				ConfigVersion: collector.ConfigVersion, Generation: collector.Generation,
			}).Error; err != nil {
				return err
			}
		} else if !selected && err == nil {
			if err := tx.Model(&assignment).Update("valid_to", now.UnixNano()).Error; err != nil {
				return err
			}
		} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	return nil
}

func normalizeCollectorScopes(scopes []collectorScopeRequest) ([]collectorScopeRequest, error) {
	if len(scopes) == 0 {
		return nil, errors.New("at least one collector scope is required")
	}
	normalized := make([]collectorScopeRequest, 0, len(scopes))
	seen := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		scope.Type = strings.ToLower(strings.TrimSpace(scope.Type))
		scope.Value = strings.TrimSpace(scope.Value)
		switch scope.Type {
		case "all":
			if len(scopes) != 1 {
				return nil, errors.New("all scope cannot be combined with another scope")
			}
			scope.Value = ""
		case "server":
			id, err := strconv.ParseUint(scope.Value, 10, 64)
			if err != nil || id == 0 {
				return nil, errors.New("server scope requires a valid server ID")
			}
		case "group", "tag":
			if scope.Value == "" {
				return nil, fmt.Errorf("%s scope requires a value", scope.Type)
			}
		default:
			return nil, errors.New("scope type must be all, server, group, or tag")
		}
		key := scope.Type + "\x00" + scope.Value
		if _, exists := seen[key]; exists {
			return nil, errors.New("collector scopes must not contain duplicates")
		}
		seen[key] = struct{}{}
		normalized = append(normalized, scope)
	}
	return normalized, nil
}

func collectorScopesMatch(scopes []collectorScopeRequest, server model.Server) bool {
	for _, scope := range scopes {
		switch scope.Type {
		case "all":
			return true
		case "server":
			if scope.Value == strconv.FormatUint(server.ID, 10) {
				return true
			}
		case "group", "tag":
			if scope.Value == server.Tag {
				return true
			}
		}
	}
	return false
}

func randomCollectorID() (string, error) {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return "", err
	}
	return "collector-" + hex.EncodeToString(id), nil
}

func writeCollectorError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) || err == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "collector not found"})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
}
