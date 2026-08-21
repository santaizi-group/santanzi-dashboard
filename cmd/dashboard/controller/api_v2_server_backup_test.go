package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupServerBackupTest(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.Server{},
		&model.TrafficPolicy{},
		&model.TrafficPolicyState{},
		&model.DDNSProfile{},
		&model.Collector{},
		&model.CollectorScope{},
		&model.ObserverAssignment{},
		&model.ServerNodeBinding{},
	); err != nil {
		t.Fatal(err)
	}
	previousDB := singleton.DB
	previousList := singleton.ServerList
	previousSecrets := singleton.SecretToID
	previousTags := singleton.ServerTagToIDList
	singleton.DB = db
	singleton.InitServer()
	t.Cleanup(func() {
		singleton.DB = previousDB
		singleton.ServerList = previousList
		singleton.SecretToID = previousSecrets
		singleton.ServerTagToIDList = previousTags
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func backupHTTP(t *testing.T, method, path string, body any) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	ctx.Request = httptest.NewRequest(method, path, reader)
	if body != nil {
		ctx.Request.Header.Set("Content-Type", "application/json")
	}
	return ctx, recorder
}

func createBackupServer(t *testing.T, db *gorm.DB, name, secret, tag string) model.Server {
	t.Helper()
	icmp, tcp, mtr := true, true, true
	server := model.Server{
		Name: name, Secret: secret, Tag: tag,
		PublicNote: "{}", MonitoringOptionsRaw: "{}", DDNSProfilesRaw: "[]",
		ProbeEnableICMP: &icmp, ProbeEnableTCP: &tcp, ProbeEnableMTR: &mtr,
	}
	if err := db.Create(&server).Error; err != nil {
		t.Fatal(err)
	}
	return server
}

func TestNormalizeServerBackupValidatesSecretAndDuplicates(t *testing.T) {
	if err := normalizeServerBackup(&serverBackupDocument{Format: "nope"}); err == nil {
		t.Fatal("expected invalid format")
	}
	secret := "reusable-secret"
	if err := normalizeServerBackup(&serverBackupDocument{
		Format:  serverBackupFormatV1,
		Servers: []serverBackupItem{{Name: "edge", Secret: &secret}},
	}); err != nil {
		t.Fatalf("valid secret: %v", err)
	}
	tooLong := strings.Repeat("a", serverBackupSecretMaxLen+1)
	if err := normalizeServerBackup(&serverBackupDocument{
		Format:  serverBackupFormatV1,
		Servers: []serverBackupItem{{Name: "edge", Secret: &tooLong}},
	}); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("long secret: %v", err)
	}
	blank := "  "
	document := &serverBackupDocument{
		Format:  serverBackupFormatV1,
		Servers: []serverBackupItem{{Name: "edge", Secret: &blank}},
	}
	if err := normalizeServerBackup(document); err != nil {
		t.Fatalf("blank secret: %v", err)
	}
	if document.Servers[0].Secret != nil {
		t.Fatal("blank secret should normalize to nil")
	}
	if err := normalizeServerBackup(&serverBackupDocument{
		Format:  serverBackupFormatV1,
		Servers: []serverBackupItem{{Name: "edge"}, {Name: "edge"}},
	}); err == nil {
		t.Fatal("expected duplicate names")
	}
}

func TestPreviewServerBackupMatchesByName(t *testing.T) {
	snapshot := &serverBackupSnapshot{
		byName: map[string][]model.Server{
			"solo": {{Common: model.Common{ID: 7}, Name: "solo", PublicNote: "{}", MonitoringOptionsRaw: "{}", DDNSProfilesRaw: "[]"}},
			"dup":  {{Common: model.Common{ID: 1}, Name: "dup"}, {Common: model.Common{ID: 2}, Name: "dup"}},
		},
		policies: map[uint64][]model.TrafficPolicy{},
		ddnsIDs:  map[uint64]struct{}{},
	}
	icmp, tcp, mtr := true, true, true
	snapshot.byName["solo"][0].ProbeEnableICMP = &icmp
	snapshot.byName["solo"][0].ProbeEnableTCP = &tcp
	snapshot.byName["solo"][0].ProbeEnableMTR = &mtr
	document := serverBackupDocument{
		Format: serverBackupFormatV1,
		Servers: []serverBackupItem{
			{Name: "new", ProbeEnableICMP: &icmp, ProbeEnableTCP: &tcp, ProbeEnableMTR: &mtr, PublicNote: map[string]any{}, MonitoringOptions: map[string]bool{}},
			{Name: "solo", ProbeEnableICMP: &icmp, ProbeEnableTCP: &tcp, ProbeEnableMTR: &mtr, PublicNote: map[string]any{}, MonitoringOptions: map[string]bool{}, Note: "changed"},
			{Name: "dup", ProbeEnableICMP: &icmp, ProbeEnableTCP: &tcp, ProbeEnableMTR: &mtr},
		},
	}
	items := previewServerBackup(document, snapshot)
	if items[0].Match != serverImportMatchCreate || items[1].Match != serverImportMatchUpdate || items[2].Match != serverImportMatchAmbiguous {
		t.Fatalf("matches=%#v", items)
	}
	if items[1].CurrentID != 7 || !containsString(items[1].Changes, "note") {
		t.Fatalf("update=%#v", items[1])
	}
	if containsString(items[2].AllowedActions, serverImportActionOverwrite) {
		t.Fatal("ambiguous must not allow overwrite")
	}
}

func TestV2ExportServersIncludesSecret(t *testing.T) {
	db := setupServerBackupTest(t)
	server := createBackupServer(t, db, "edge-a", "super-secret-token", "edge")
	if err := db.Create(&model.TrafficPolicy{
		ServerID: server.ID, Name: "month", Direction: model.TrafficDirectionTotal,
		Mode: model.TrafficModeCumulative, QuotaBytes: 1000, WarningPercent: 80, Enabled: true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	ctx, recorder := backupHTTP(t, http.MethodGet, "/api/v2/admin/servers/export", nil)
	v2ExportServers(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"secret":"super-secret-token"`) {
		t.Fatalf("export missing secret: %s", body)
	}
	if !strings.Contains(body, `"format":"santaizi.servers.v1"`) || !strings.Contains(body, `"name":"edge-a"`) || !strings.Contains(body, `"month"`) {
		t.Fatalf("export missing fields: %s", body)
	}
}

func TestV2ImportServersCreateOverwriteAndRejectConflict(t *testing.T) {
	db := setupServerBackupTest(t)
	existing := createBackupServer(t, db, "keep", "original-secret", "old")
	oldPolicy := model.TrafficPolicy{
		ServerID: existing.ID, Name: "old", Direction: model.TrafficDirectionInbound,
		Mode: model.TrafficModeCumulative, QuotaBytes: 100, WarningPercent: 80, Enabled: true,
	}
	if err := db.Create(&oldPolicy).Error; err != nil {
		t.Fatal(err)
	}
	icmp, tcp, mtr := true, true, true
	overwriteSecret := "should-not-apply"
	freshSecret := "imported-fresh-secret"
	document := serverBackupDocument{
		Format: serverBackupFormatV1,
		Servers: []serverBackupItem{
			{
				Name: "keep", Tag: "new", ProbeEnableICMP: &icmp, ProbeEnableTCP: &tcp, ProbeEnableMTR: &mtr,
				PublicNote: map[string]any{}, MonitoringOptions: map[string]bool{},
				Secret: &overwriteSecret,
				TrafficPolicies: []trafficPolicyWriteDTO{{
					Name: "fresh", Direction: model.TrafficDirectionTotal, Mode: model.TrafficModeCumulative,
					QuotaBytes: 500, WarningPercent: 80, Enabled: true,
				}},
			},
			{
				Name: "fresh-host", Tag: "edge", ProbeEnableICMP: &icmp, ProbeEnableTCP: &tcp, ProbeEnableMTR: &mtr,
				PublicNote: map[string]any{}, MonitoringOptions: map[string]bool{},
				Secret: &freshSecret,
			},
		},
	}
	ctx, recorder := backupHTTP(t, http.MethodPost, "/api/v2/admin/servers/import/preview", document)
	v2PreviewServerImport(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var count int64
	if err := db.Model(&model.Server{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("preview wrote servers: %d", count)
	}

	conflict := serverImportWrite{
		Document: document,
		Actions:  []serverImportActionItem{{Index: 0, Action: serverImportActionCreate}, {Index: 1, Action: serverImportActionCreate}},
	}
	ctx, recorder = backupHTTP(t, http.MethodPost, "/api/v2/admin/servers/import", conflict)
	v2ImportServers(ctx)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "server_import_conflict") {
		t.Fatalf("conflict=%d %s", recorder.Code, recorder.Body.String())
	}

	apply := serverImportWrite{
		Document: document,
		Actions:  []serverImportActionItem{{Index: 0, Action: serverImportActionOverwrite}, {Index: 1, Action: serverImportActionCreate}},
	}
	ctx, recorder = backupHTTP(t, http.MethodPost, "/api/v2/admin/servers/import", apply)
	v2ImportServers(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	counts := decodeImportResult(t, recorder.Body.Bytes())
	if counts.Created != 1 || counts.Overwritten != 1 || counts.Skipped != 0 || counts.SecretsReused != 1 || counts.SecretsRegenerated != 0 {
		t.Fatalf("counts=%#v", counts)
	}

	var stored model.Server
	if err := db.First(&stored, existing.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Tag != "new" || stored.Secret != "original-secret" {
		t.Fatalf("overwrite mutated identity: tag=%q secret=%q", stored.Tag, stored.Secret)
	}
	var policies []model.TrafficPolicy
	if err := db.Where("server_id = ?", existing.ID).Find(&policies).Error; err != nil {
		t.Fatal(err)
	}
	if len(policies) != 1 || policies[0].Name != "fresh" || policies[0].ID == oldPolicy.ID {
		t.Fatalf("policies=%#v old=%d", policies, oldPolicy.ID)
	}
	var created model.Server
	if err := db.Where("name = ?", "fresh-host").First(&created).Error; err != nil {
		t.Fatal(err)
	}
	if created.Secret != "imported-fresh-secret" {
		t.Fatalf("created secret=%q", created.Secret)
	}
}

func TestV2ImportServersRegeneratesConflictingSecret(t *testing.T) {
	db := setupServerBackupTest(t)
	createBackupServer(t, db, "keep", "original-secret", "old")
	icmp, tcp, mtr := true, true, true
	conflict := "original-secret"
	document := serverBackupDocument{
		Format: serverBackupFormatV1,
		Servers: []serverBackupItem{{
			Name: "fresh-host", Tag: "edge", ProbeEnableICMP: &icmp, ProbeEnableTCP: &tcp, ProbeEnableMTR: &mtr,
			PublicNote: map[string]any{}, MonitoringOptions: map[string]bool{},
			Secret: &conflict,
		}},
	}
	ctx, recorder := backupHTTP(t, http.MethodPost, "/api/v2/admin/servers/import", serverImportWrite{
		Document: document,
		Actions:  []serverImportActionItem{{Index: 0, Action: serverImportActionCreate}},
	})
	v2ImportServers(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	counts := decodeImportResult(t, recorder.Body.Bytes())
	if counts.Created != 1 || counts.SecretsReused != 0 || counts.SecretsRegenerated != 1 {
		t.Fatalf("counts=%#v", counts)
	}
	var created model.Server
	if err := db.Where("name = ?", "fresh-host").First(&created).Error; err != nil {
		t.Fatal(err)
	}
	if created.Secret == "" || created.Secret == "original-secret" {
		t.Fatalf("created secret=%q", created.Secret)
	}
}

func TestPreviewServerBackupWarnsSecretConflict(t *testing.T) {
	setupServerBackupTest(t)
	taken, shared, free := "taken-secret", "shared-secret", "free-secret"
	icmp, tcp, mtr := true, true, true
	item := func(name, secret string) serverBackupItem {
		value := secret
		return serverBackupItem{
			Name: name, Secret: &value, ProbeEnableICMP: &icmp, ProbeEnableTCP: &tcp, ProbeEnableMTR: &mtr,
			PublicNote: map[string]any{}, MonitoringOptions: map[string]bool{},
		}
	}
	snapshot := &serverBackupSnapshot{
		servers:  []model.Server{{Name: "keep", Secret: taken}},
		byName:   map[string][]model.Server{},
		policies: map[uint64][]model.TrafficPolicy{},
		ddnsIDs:  map[uint64]struct{}{},
	}
	items := previewServerBackup(serverBackupDocument{
		Format:  serverBackupFormatV1,
		Servers: []serverBackupItem{item("a", taken), item("b", shared), item("c", shared), item("d", free)},
	}, snapshot)
	if !containsString(items[0].Warnings, serverImportWarnSecretConflict) {
		t.Fatalf("existing secret should warn: %#v", items[0])
	}
	if containsString(items[1].Warnings, serverImportWarnSecretConflict) {
		t.Fatalf("first shared secret should reuse: %#v", items[1])
	}
	if !containsString(items[2].Warnings, serverImportWarnSecretConflict) {
		t.Fatalf("second shared secret should warn: %#v", items[2])
	}
	if containsString(items[3].Warnings, serverImportWarnSecretConflict) {
		t.Fatalf("free secret should not warn: %#v", items[3])
	}
}

func TestPlanServerImportRequiresCompleteActions(t *testing.T) {
	snapshot := &serverBackupSnapshot{byName: map[string][]model.Server{}, policies: map[uint64][]model.TrafficPolicy{}, ddnsIDs: map[uint64]struct{}{}}
	document := serverBackupDocument{Format: serverBackupFormatV1, Servers: []serverBackupItem{{Name: "a"}, {Name: "b"}}}
	if _, _, _, _, err := planServerImport(document, snapshot, []serverImportActionItem{{Index: 0, Action: serverImportActionCreate}}); err == nil {
		t.Fatal("expected incomplete actions")
	}
}

func TestFilterKnownDDNSWarnsAndDropsMissing(t *testing.T) {
	kept, skipped := filterKnownDDNS([]uint64{1, 9, 2}, map[uint64]struct{}{1: {}, 2: {}})
	if !skipped || len(kept) != 2 || kept[0] != 1 || kept[1] != 2 {
		t.Fatalf("kept=%v skipped=%v", kept, skipped)
	}
}

func TestDiffUnchangedServer(t *testing.T) {
	icmp, tcp, mtr := true, true, true
	current := model.Server{
		Name: "solo", PublicNote: "{}", MonitoringOptionsRaw: "{}", DDNSProfilesRaw: "[]",
		ProbeEnableICMP: &icmp, ProbeEnableTCP: &tcp, ProbeEnableMTR: &mtr,
	}
	item := serverBackupItem{
		Name: "solo", PublicNote: map[string]any{}, MonitoringOptions: map[string]bool{},
		ProbeEnableICMP: &icmp, ProbeEnableTCP: &tcp, ProbeEnableMTR: &mtr,
	}
	if changes := diffServerBackup(item, current, nil); len(changes) != 0 {
		t.Fatalf("changes=%v", changes)
	}
}

func TestCanonicalJSONTreatsNullAsEmpty(t *testing.T) {
	if canonicalJSON(nil) != canonicalJSON(map[string]any{}) {
		t.Fatal("nil public note should match empty object")
	}
}

func TestTrafficPolicyBackupJSONOmitsIdentity(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	raw, err := json.Marshal(trafficPolicyBackupJSON(model.TrafficPolicy{
		Common: model.Common{ID: 44}, ServerID: 9, Name: "cycle", Direction: model.TrafficDirectionTotal,
		Mode: model.TrafficModeRecurring, CycleStart: &start, CycleInterval: 1, CycleUnit: "month",
		QuotaBytes: 10, WarningPercent: 80, Enabled: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"id"`) || strings.Contains(string(raw), `"server_id"`) {
		t.Fatalf("policy backup leaked ids: %s", raw)
	}
}

type importResultCounts struct {
	Created            int `json:"created"`
	Overwritten        int `json:"overwritten"`
	Skipped            int `json:"skipped"`
	SecretsReused      int `json:"secrets_reused"`
	SecretsRegenerated int `json:"secrets_regenerated"`
}

func decodeImportResult(t *testing.T, body []byte) importResultCounts {
	t.Helper()
	var payload struct {
		Data importResultCounts `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Data
}
