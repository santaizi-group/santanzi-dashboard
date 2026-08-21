package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/pkg/utils"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
	trafficservice "github.com/hi2shark/santaizi-dashboard/service/traffic"
	"gorm.io/gorm"
)

const serverBackupFormatV1 = "santaizi.servers.v1"

const (
	serverImportMatchCreate        = "create"
	serverImportMatchUpdate        = "update"
	serverImportMatchUnchanged     = "unchanged"
	serverImportMatchAmbiguous     = "ambiguous"
	serverImportActionCreate       = "create"
	serverImportActionOverwrite    = "overwrite"
	serverImportActionSkip         = "skip"
	serverImportWarnDDNSSkipped    = "ddns_profiles_skipped"
	serverImportWarnSecretConflict = "secret_conflict"
	serverBackupSecretMaxLen       = 128
)

type serverBackupDocument struct {
	Format     string             `json:"format"`
	ExportedAt *time.Time         `json:"exported_at"`
	Servers    []serverBackupItem `json:"servers"`
}

type serverBackupItem struct {
	Name              string                  `json:"name"`
	Tag               string                  `json:"tag"`
	Note              string                  `json:"note"`
	PublicNote        map[string]any          `json:"public_note"`
	MonitoringOptions map[string]bool         `json:"monitoring_options"`
	DisplayIndex      int                     `json:"display_index"`
	HideForGuest      bool                    `json:"hide_for_guest"`
	EnableDDNS        bool                    `json:"enable_ddns"`
	DDNSProfiles      []uint64                `json:"ddns_profiles"`
	TrafficPolicies   []trafficPolicyWriteDTO `json:"traffic_policies"`
	ProbeTarget       string                  `json:"probe_target"`
	ProbeTCPPorts     string                  `json:"probe_tcp_ports"`
	ProbeEnableICMP   *bool                   `json:"probe_enable_icmp"`
	ProbeEnableTCP    *bool                   `json:"probe_enable_tcp"`
	ProbeEnableMTR    *bool                   `json:"probe_enable_mtr"`
	Secret            *string                 `json:"secret"`
}

type serverImportPreviewItem struct {
	Index           int      `json:"index"`
	Name            string   `json:"name"`
	Match           string   `json:"match"`
	CurrentID       uint64   `json:"current_id,omitempty"`
	Changes         []string `json:"changes"`
	Warnings        []string `json:"warnings"`
	SuggestedAction string   `json:"suggested_action"`
	AllowedActions  []string `json:"allowed_actions"`
}

type serverImportWrite struct {
	Document serverBackupDocument     `json:"document"`
	Actions  []serverImportActionItem `json:"actions"`
}

type serverImportActionItem struct {
	Index  int    `json:"index"`
	Action string `json:"action"`
}

type serverBackupSnapshot struct {
	servers  []model.Server
	byName   map[string][]model.Server
	policies map[uint64][]model.TrafficPolicy
	ddnsIDs  map[uint64]struct{}
}

type plannedServerImport struct {
	backup   serverBackupItem
	existing *model.Server
}

type importedServer struct {
	server  model.Server
	created bool
}

func applyServerWriteFields(server *model.Server, request serverV2Write, created bool) error {
	name := strings.TrimSpace(request.Name)
	if name == "" {
		return errors.New("name is required")
	}
	server.Name, server.Tag, server.Note = name, strings.TrimSpace(request.Tag), request.Note
	publicNote, err := json.Marshal(request.PublicNote)
	if err != nil {
		return err
	}
	if string(publicNote) == "null" {
		publicNote = []byte("{}")
	}
	server.PublicNote = string(publicNote)
	monitoringOptions, err := json.Marshal(request.MonitoringOptions)
	if err != nil {
		return err
	}
	if string(monitoringOptions) == "null" {
		monitoringOptions = []byte("{}")
	}
	server.MonitoringOptionsRaw = string(monitoringOptions)
	server.DisplayIndex, server.HideForGuest, server.EnableDDNS = request.DisplayIndex, request.HideForGuest, request.EnableDDNS
	server.ProbeTarget = strings.TrimSpace(request.ProbeTarget)
	server.ProbeTCPPorts = strings.TrimSpace(request.ProbeTCPPorts)
	server.ProbeEnableICMP = model.BoolPtr(boolOrKeep(request.ProbeEnableICMP, created, model.BoolOrTrue(server.ProbeEnableICMP), true))
	server.ProbeEnableTCP = model.BoolPtr(boolOrKeep(request.ProbeEnableTCP, created, model.BoolOrTrue(server.ProbeEnableTCP), true))
	server.ProbeEnableMTR = model.BoolPtr(boolOrKeep(request.ProbeEnableMTR, created, model.BoolOrTrue(server.ProbeEnableMTR), true))
	server.DDNSProfiles = append([]uint64(nil), request.DDNSProfiles...)
	raw, _ := utils.Json.Marshal(server.DDNSProfiles)
	server.DDNSProfilesRaw = string(raw)
	return nil
}

func (item serverBackupItem) asWrite() serverV2Write {
	upserts := make([]trafficPolicyUpsertDTO, 0, len(item.TrafficPolicies))
	for _, policy := range item.TrafficPolicies {
		upserts = append(upserts, trafficPolicyUpsertDTO{trafficPolicyWriteDTO: policy})
	}
	return serverV2Write{
		Name:              item.Name,
		Tag:               item.Tag,
		Note:              item.Note,
		PublicNote:        item.PublicNote,
		MonitoringOptions: item.MonitoringOptions,
		DisplayIndex:      item.DisplayIndex,
		HideForGuest:      item.HideForGuest,
		EnableDDNS:        item.EnableDDNS,
		DDNSProfiles:      append([]uint64(nil), item.DDNSProfiles...),
		TrafficPolicies:   &upserts,
		ProbeTarget:       item.ProbeTarget,
		ProbeTCPPorts:     item.ProbeTCPPorts,
		ProbeEnableICMP:   item.ProbeEnableICMP,
		ProbeEnableTCP:    item.ProbeEnableTCP,
		ProbeEnableMTR:    item.ProbeEnableMTR,
	}
}

func v2ExportServers(c *gin.Context) {
	snapshot, err := loadServerBackupSnapshot()
	if err != nil {
		writeV2Problem(c, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	exportedAt := time.Now().UTC().Truncate(time.Second)
	servers := make([]gin.H, 0, len(snapshot.servers))
	for _, server := range snapshot.servers {
		servers = append(servers, serverBackupItemJSON(server, snapshot.policies[server.ID]))
	}
	writeV2Data(c, http.StatusOK, gin.H{
		"format":      serverBackupFormatV1,
		"exported_at": exportedAt.Format(time.RFC3339),
		"servers":     servers,
	})
}

func v2PreviewServerImport(c *gin.Context) {
	var document serverBackupDocument
	if err := c.ShouldBindJSON(&document); err != nil {
		writeV2Problem(c, http.StatusBadRequest, "invalid_server_backup", err.Error())
		return
	}
	if err := normalizeServerBackup(&document); err != nil {
		writeV2Problem(c, http.StatusBadRequest, "invalid_server_backup", err.Error())
		return
	}
	snapshot, err := loadServerBackupSnapshot()
	if err != nil {
		writeV2Problem(c, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	writeV2Data(c, http.StatusOK, gin.H{"items": previewServerBackup(document, snapshot)})
}

func v2ImportServers(c *gin.Context) {
	var request serverImportWrite
	if err := c.ShouldBindJSON(&request); err != nil {
		writeV2Problem(c, http.StatusBadRequest, "invalid_server_backup", err.Error())
		return
	}
	if err := normalizeServerBackup(&request.Document); err != nil {
		writeV2Problem(c, http.StatusBadRequest, "invalid_server_backup", err.Error())
		return
	}
	snapshot, err := loadServerBackupSnapshot()
	if err != nil {
		writeV2Problem(c, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	planned, createdN, overwrittenN, skippedN, err := planServerImport(request.Document, snapshot, request.Actions)
	if err != nil {
		writeV2Problem(c, http.StatusBadRequest, "server_import_conflict", err.Error())
		return
	}
	usedSecrets := snapshotUsedSecrets(snapshot)
	applied := make([]importedServer, 0, len(planned))
	var secretsReused, secretsRegenerated int
	err = singleton.DB.Transaction(func(tx *gorm.DB) error {
		for _, item := range planned {
			hadSecret := backupSecret(item.backup) != ""
			row, created, reused, persistErr := persistImportedServer(tx, item.existing, item.backup, usedSecrets)
			if persistErr != nil {
				return persistErr
			}
			applied = append(applied, importedServer{server: *row, created: created})
			if created && hadSecret {
				if reused {
					secretsReused++
				} else {
					secretsRegenerated++
				}
			}
		}
		return nil
	})
	if err != nil {
		writeV2Problem(c, http.StatusBadRequest, "server_import_failed", err.Error())
		return
	}
	if err := rememberImportedServers(applied); err != nil {
		writeV2Problem(c, http.StatusInternalServerError, "assignment_refresh_failed", err.Error())
		return
	}
	writeV2Data(c, http.StatusOK, gin.H{
		"created":             createdN,
		"overwritten":         overwrittenN,
		"skipped":             skippedN,
		"secrets_reused":      secretsReused,
		"secrets_regenerated": secretsRegenerated,
	})
}

func loadServerBackupSnapshot() (*serverBackupSnapshot, error) {
	var servers []model.Server
	if err := singleton.DB.Order("display_index DESC, id ASC").Find(&servers).Error; err != nil {
		return nil, err
	}
	var policies []model.TrafficPolicy
	if err := singleton.DB.Order("id ASC").Find(&policies).Error; err != nil {
		return nil, err
	}
	var ddns []model.DDNSProfile
	if err := singleton.DB.Select("id").Find(&ddns).Error; err != nil {
		return nil, err
	}
	snapshot := &serverBackupSnapshot{
		servers:  servers,
		byName:   map[string][]model.Server{},
		policies: map[uint64][]model.TrafficPolicy{},
		ddnsIDs:  map[uint64]struct{}{},
	}
	for _, server := range servers {
		snapshot.byName[server.Name] = append(snapshot.byName[server.Name], server)
	}
	for _, policy := range policies {
		snapshot.policies[policy.ServerID] = append(snapshot.policies[policy.ServerID], policy)
	}
	for _, profile := range ddns {
		snapshot.ddnsIDs[profile.ID] = struct{}{}
	}
	return snapshot, nil
}

func normalizeServerBackup(document *serverBackupDocument) error {
	if document == nil || document.Format != serverBackupFormatV1 {
		return errors.New("unsupported backup format")
	}
	if len(document.Servers) > 5000 {
		return errors.New("backup contains too many servers")
	}
	seen := make(map[string]struct{}, len(document.Servers))
	for i := range document.Servers {
		item := &document.Servers[i]
		secret, err := normalizeBackupSecret(item.Secret)
		if err != nil {
			return err
		}
		item.Secret = secret
		item.Name = strings.TrimSpace(item.Name)
		if item.Name == "" || len(item.Name) > 100 {
			return errors.New("server name is required")
		}
		if _, exists := seen[item.Name]; exists {
			return errors.New("backup contains duplicate server names")
		}
		seen[item.Name] = struct{}{}
		item.Tag = strings.TrimSpace(item.Tag)
		if item.PublicNote == nil {
			item.PublicNote = map[string]any{}
		}
		if item.MonitoringOptions == nil {
			item.MonitoringOptions = map[string]bool{}
		}
		if item.DDNSProfiles == nil {
			item.DDNSProfiles = []uint64{}
		}
		if item.TrafficPolicies == nil {
			item.TrafficPolicies = []trafficPolicyWriteDTO{}
		}
		if _, err := policiesFromBackup(item.TrafficPolicies); err != nil {
			return err
		}
	}
	return nil
}

func policiesFromBackup(items []trafficPolicyWriteDTO) ([]model.TrafficPolicy, error) {
	upserts := make([]trafficPolicyUpsertDTO, 0, len(items))
	for _, item := range items {
		upserts = append(upserts, trafficPolicyUpsertDTO{trafficPolicyWriteDTO: item})
	}
	return trafficPoliciesFromWrite(&upserts)
}

func previewServerBackup(document serverBackupDocument, snapshot *serverBackupSnapshot) []serverImportPreviewItem {
	usedSecrets := snapshotUsedSecrets(snapshot)
	items := make([]serverImportPreviewItem, 0, len(document.Servers))
	for index, item := range document.Servers {
		preview := serverImportPreviewItem{
			Index:    index,
			Name:     item.Name,
			Changes:  []string{},
			Warnings: []string{},
		}
		filtered, skipped := filterKnownDDNS(item.DDNSProfiles, snapshot.ddnsIDs)
		item.DDNSProfiles = filtered
		if skipped {
			preview.Warnings = append(preview.Warnings, serverImportWarnDDNSSkipped)
		}
		matches := snapshot.byName[item.Name]
		switch len(matches) {
		case 0:
			preview.Match = serverImportMatchCreate
			preview.SuggestedAction = serverImportActionCreate
			preview.AllowedActions = []string{serverImportActionCreate, serverImportActionSkip}
			if secret := backupSecret(item); secret != "" {
				if _, taken := usedSecrets[secret]; taken {
					preview.Warnings = append(preview.Warnings, serverImportWarnSecretConflict)
				} else {
					usedSecrets[secret] = struct{}{}
				}
			}
		case 1:
			current := matches[0]
			preview.CurrentID = current.ID
			preview.Changes = diffServerBackup(item, current, snapshot.policies[current.ID])
			if len(preview.Changes) == 0 {
				preview.Match = serverImportMatchUnchanged
				preview.SuggestedAction = serverImportActionSkip
				preview.AllowedActions = []string{serverImportActionSkip}
			} else {
				preview.Match = serverImportMatchUpdate
				preview.SuggestedAction = serverImportActionOverwrite
				preview.AllowedActions = []string{serverImportActionOverwrite, serverImportActionSkip}
			}
		default:
			preview.Match = serverImportMatchAmbiguous
			preview.SuggestedAction = serverImportActionSkip
			preview.AllowedActions = []string{serverImportActionSkip}
		}
		items = append(items, preview)
	}
	return items
}

func planServerImport(document serverBackupDocument, snapshot *serverBackupSnapshot, actions []serverImportActionItem) ([]plannedServerImport, int, int, int, error) {
	previews := previewServerBackup(document, snapshot)
	if len(actions) != len(previews) {
		return nil, 0, 0, 0, errors.New("import actions must cover every backup row")
	}
	used := make([]bool, len(previews))
	var created, overwritten, skipped int
	planned := make([]plannedServerImport, 0, len(previews))
	for _, action := range actions {
		if action.Index < 0 || action.Index >= len(previews) || used[action.Index] {
			return nil, 0, 0, 0, errors.New("import action index is invalid")
		}
		used[action.Index] = true
		preview := previews[action.Index]
		if !containsString(preview.AllowedActions, action.Action) {
			return nil, 0, 0, 0, errors.New("import action is not allowed for " + preview.Name)
		}
		item := document.Servers[action.Index]
		item.DDNSProfiles, _ = filterKnownDDNS(item.DDNSProfiles, snapshot.ddnsIDs)
		switch action.Action {
		case serverImportActionSkip:
			skipped++
		case serverImportActionCreate:
			created++
			planned = append(planned, plannedServerImport{backup: item})
		case serverImportActionOverwrite:
			overwritten++
			current := snapshot.byName[item.Name][0]
			planned = append(planned, plannedServerImport{backup: item, existing: &current})
		default:
			return nil, 0, 0, 0, errors.New("unknown import action")
		}
	}
	for _, seen := range used {
		if !seen {
			return nil, 0, 0, 0, errors.New("import actions must cover every backup row")
		}
	}
	return planned, created, overwritten, skipped, nil
}

func persistImportedServer(tx *gorm.DB, existing *model.Server, item serverBackupItem, usedSecrets map[string]struct{}) (*model.Server, bool, bool, error) {
	created := existing == nil
	server := model.Server{}
	if !created {
		if err := tx.First(&server, existing.ID).Error; err != nil {
			return nil, false, false, err
		}
	}
	if err := applyServerWriteFields(&server, item.asWrite(), created); err != nil {
		return nil, false, false, err
	}
	policies, err := policiesFromBackup(item.TrafficPolicies)
	if err != nil {
		return nil, false, false, err
	}
	for i := range policies {
		policies[i].ID = 0
	}
	reused := false
	if created {
		if usedSecrets == nil {
			usedSecrets = map[string]struct{}{}
		}
		secret := backupSecret(item)
		if secret != "" {
			if _, taken := usedSecrets[secret]; !taken {
				reused = true
			} else {
				secret = ""
			}
		}
		if secret == "" {
			generated, err := utils.GenerateRandomString(18)
			if err != nil {
				return nil, false, false, err
			}
			secret = generated
		}
		server.Secret = secret
		usedSecrets[secret] = struct{}{}
		if err := tx.Create(&server).Error; err != nil {
			return nil, false, false, err
		}
	} else if err := tx.Save(&server).Error; err != nil {
		return nil, false, false, err
	}
	if err := trafficservice.Replace(tx, server.ID, policies); err != nil {
		return nil, false, false, err
	}
	return &server, created, reused, nil
}

func rememberImportedServers(applied []importedServer) error {
	now := time.Now()
	if singleton.ServerList == nil || singleton.SecretToID == nil || singleton.ServerTagToIDList == nil {
		singleton.InitServer()
	}
	for _, item := range applied {
		server := item.server
		if item.created {
			server.Host, server.State = &model.Host{}, &model.HostState{}
			singleton.ServerLock.Lock()
			copied := server
			singleton.SecretToID[copied.Secret] = copied.ID
			singleton.ServerList[copied.ID] = &copied
			singleton.ServerTagToIDList[copied.Tag] = append(singleton.ServerTagToIDList[copied.Tag], copied.ID)
			singleton.ServerLock.Unlock()
			continue
		}
		if err := singleton.RefreshObserverAssignmentsForServer(server.ID, now); err != nil {
			return err
		}
		if err := singleton.RefreshProbeCollectorConfigsForServer(server.ID); err != nil {
			return err
		}
		singleton.ServerLock.Lock()
		old := singleton.ServerList[server.ID]
		if old != nil {
			server.CopyFromRunningServer(old)
			if server.Tag != old.Tag {
				removeServerTag(old.Tag, server.ID)
				singleton.ServerTagToIDList[server.Tag] = append(singleton.ServerTagToIDList[server.Tag], server.ID)
			}
		} else {
			server.Host, server.State = &model.Host{}, &model.HostState{}
			singleton.SecretToID[server.Secret] = server.ID
			singleton.ServerTagToIDList[server.Tag] = append(singleton.ServerTagToIDList[server.Tag], server.ID)
		}
		copied := server
		singleton.ServerList[server.ID] = &copied
		singleton.ServerLock.Unlock()
	}
	singleton.ReSortServer()
	return nil
}

func serverBackupItemJSON(server model.Server, policies []model.TrafficPolicy) gin.H {
	monitoringOptions := map[string]bool{}
	_ = json.Unmarshal([]byte(server.MonitoringOptionsRaw), &monitoringOptions)
	if monitoringOptions == nil {
		monitoringOptions = map[string]bool{}
	}
	publicNote, _ := decodePublicNote(server.PublicNote).(map[string]any)
	if publicNote == nil {
		publicNote = map[string]any{}
	}
	rows := make([]gin.H, 0, len(policies))
	for _, policy := range policies {
		rows = append(rows, trafficPolicyBackupJSON(policy))
	}
	profiles := append([]uint64(nil), server.DDNSProfiles...)
	if profiles == nil {
		profiles = []uint64{}
	}
	return gin.H{
		"name":               server.Name,
		"tag":                server.Tag,
		"note":               server.Note,
		"public_note":        publicNote,
		"monitoring_options": monitoringOptions,
		"display_index":      server.DisplayIndex,
		"hide_for_guest":     server.HideForGuest,
		"enable_ddns":        server.EnableDDNS,
		"ddns_profiles":      profiles,
		"traffic_policies":   rows,
		"probe_target":       server.ProbeTarget,
		"probe_tcp_ports":    server.ProbeTCPPorts,
		"probe_enable_icmp":  model.BoolOrTrue(server.ProbeEnableICMP),
		"probe_enable_tcp":   model.BoolOrTrue(server.ProbeEnableTCP),
		"probe_enable_mtr":   model.BoolOrTrue(server.ProbeEnableMTR),
		"secret":             server.Secret,
	}
}

func trafficPolicyBackupJSON(row model.TrafficPolicy) gin.H {
	item := gin.H{
		"name":             row.Name,
		"direction":        row.Direction,
		"mode":             row.Mode,
		"quota_bytes":      row.QuotaBytes,
		"warning_percent":  row.WarningPercent,
		"notification_tag": row.NotificationTag,
		"enabled":          row.Enabled,
		"cycle_start":      nil,
	}
	if row.CycleStart != nil && !row.CycleStart.IsZero() {
		item["cycle_start"] = row.CycleStart.UTC().Format(time.RFC3339)
	}
	if row.Mode == model.TrafficModeRecurring {
		item["cycle_interval"] = row.CycleInterval
		item["cycle_unit"] = row.CycleUnit
	}
	return item
}

func snapshotUsedSecrets(snapshot *serverBackupSnapshot) map[string]struct{} {
	used := map[string]struct{}{}
	if snapshot != nil {
		for _, server := range snapshot.servers {
			if server.Secret != "" {
				used[server.Secret] = struct{}{}
			}
		}
	}
	singleton.ServerLock.RLock()
	for secret := range singleton.SecretToID {
		if secret != "" {
			used[secret] = struct{}{}
		}
	}
	singleton.ServerLock.RUnlock()
	return used
}

func backupSecret(item serverBackupItem) string {
	if item.Secret == nil {
		return ""
	}
	return strings.TrimSpace(*item.Secret)
}

func normalizeBackupSecret(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil, nil
	}
	if len(trimmed) > serverBackupSecretMaxLen {
		return nil, errors.New("secret is too long")
	}
	return &trimmed, nil
}

func filterKnownDDNS(ids []uint64, known map[uint64]struct{}) ([]uint64, bool) {
	kept := make([]uint64, 0, len(ids))
	skipped := false
	for _, id := range ids {
		if _, ok := known[id]; ok {
			kept = append(kept, id)
			continue
		}
		skipped = true
	}
	return kept, skipped
}

func diffServerBackup(item serverBackupItem, current model.Server, policies []model.TrafficPolicy) []string {
	changes := make([]string, 0, 8)
	if item.Tag != current.Tag {
		changes = append(changes, "tag")
	}
	if item.Note != current.Note {
		changes = append(changes, "note")
	}
	if canonicalJSON(item.PublicNote) != canonicalJSON(decodePublicNote(current.PublicNote)) {
		changes = append(changes, "public_note")
	}
	currentMonitoring := map[string]bool{}
	_ = json.Unmarshal([]byte(current.MonitoringOptionsRaw), &currentMonitoring)
	if canonicalJSON(item.MonitoringOptions) != canonicalJSON(currentMonitoring) {
		changes = append(changes, "monitoring_options")
	}
	if item.DisplayIndex != current.DisplayIndex {
		changes = append(changes, "display_index")
	}
	if item.HideForGuest != current.HideForGuest {
		changes = append(changes, "hide_for_guest")
	}
	if item.EnableDDNS != current.EnableDDNS {
		changes = append(changes, "enable_ddns")
	}
	if !equalUint64s(item.DDNSProfiles, current.DDNSProfiles) {
		changes = append(changes, "ddns_profiles")
	}
	if strings.TrimSpace(item.ProbeTarget) != current.ProbeTarget {
		changes = append(changes, "probe_target")
	}
	if strings.TrimSpace(item.ProbeTCPPorts) != current.ProbeTCPPorts {
		changes = append(changes, "probe_tcp_ports")
	}
	if boolOrTrue(item.ProbeEnableICMP) != model.BoolOrTrue(current.ProbeEnableICMP) {
		changes = append(changes, "probe_enable_icmp")
	}
	if boolOrTrue(item.ProbeEnableTCP) != model.BoolOrTrue(current.ProbeEnableTCP) {
		changes = append(changes, "probe_enable_tcp")
	}
	if boolOrTrue(item.ProbeEnableMTR) != model.BoolOrTrue(current.ProbeEnableMTR) {
		changes = append(changes, "probe_enable_mtr")
	}
	incoming, _ := policiesFromBackup(item.TrafficPolicies)
	if canonicalJSON(policyBackupList(incoming)) != canonicalJSON(policyBackupList(policies)) {
		changes = append(changes, "traffic_policies")
	}
	return changes
}

func policyBackupList(rows []model.TrafficPolicy) []gin.H {
	items := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		copyRow := row
		copyRow.ID = 0
		copyRow.ServerID = 0
		items = append(items, trafficPolicyBackupJSON(copyRow))
	}
	return items
}

func canonicalJSON(value any) string {
	if value == nil {
		return "{}"
	}
	encoded, err := json.Marshal(value)
	if err != nil || string(encoded) == "null" {
		return "{}"
	}
	return string(encoded)
}

func equalUint64s(left, right []uint64) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func boolOrTrue(value *bool) bool {
	if value == nil {
		return true
	}
	return *value
}
