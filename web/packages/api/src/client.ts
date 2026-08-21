import { setCSRFToken } from './request'
import { getSantaiziHTTPAPI } from './generated/santaizi'
import type {
  AlertRule, AlertRuleWriteBody,   APIToken, APITokenPatchBody, APITokenWriteBody, AgentReliabilityRecord, CollectorCreated,
  CollectorInstallPreview, CollectorInstallPreviewWriteBody, CollectorScopeWriteBody, CollectorToken, CollectorWriteBody, ConnectionLatencyBucket, ConnectionPath, ConnectionSummary, DDNSProfile, DDNSProfileWriteBody, DDNSProvider,
  GetProbeTraceParams, GetProbeRouteParams, IncidentRecord, IncidentRevisionRecord, InstallPreview, InstallPreviewWriteBody, ListConnectionLatencyParams, ListProbePathsParams, ListProbeSamplesParams, Monitor, MonitorWriteBody, NATTunnel,
  NATTunnelWriteBody, NotificationChannel, NotificationChannelWriteBody, ObserverAssignmentRecord,
  ProbeCapabilities, ProbePath, ProbeRouteHistory, ProbeRouteJob, ProbeRouteWrite, ProbeSampleBucket, ProbeSummary, ProbeTrace, ServerBackup, ServerCredential, ServerDisplayIndexWriteBody, ServerGroup,
  ServerGroupRenameWriteBody, ServerImportPreview, ServerImportResult, ServerImportWrite, ServerWriteBody, ScriptCommands, TelemetryAlertRecord, TelemetryDataLossRecord, TrafficPolicy,
  TrafficPolicyHistory, TrafficPolicyWriteBody, TrafficUsage, UpgradePreview, UpgradePreviewWriteBody,
  CycleTransfer, GetPublicMetricsParams, GetPublicServerAvailabilityParams, GetServerTrafficHistoryParams, MonitorHistory, PublicAvailability, PublicMetricPoint,
  DatabaseStatus,
} from './generated/model'
import type {
  ApiData, ApiList, CollectorRecord, ResourceQuery, ResourceRecord, ServerRecord,
  SessionState, SiteBootstrap,
} from './types'

const api = getSantaiziHTTPAPI()
const data = <T>(value: unknown) => (value as ApiData<T>).data
const list = <T>(value: unknown) => value as ApiList<T>

export async function getSession() {
  const result = data<SessionState>(await api.getSession())
  setCSRFToken(result.csrf_token)
  return result
}

export const logout = () => api.logout()
export const getBootstrap = () => api.getPublicBootstrap().then((value) => {
  const result = data<SiteBootstrap>(value)
  if (result.csrf_token) setCSRFToken(result.csrf_token)
  return result
})
export const verifyViewPassword = (password: string) => api.createViewPasswordSession({ password })
export const listPublicServers = () => api.listPublicServers().then(value => list<ServerRecord>(value))
export const getPublicServer = (id: number) => api.getPublicServer(id).then(value => data<ServerRecord>(value))
export const listPublicServices = () => api.listPublicServices().then(value => list<ResourceRecord>(value))
export const getPublicNetwork = (id: number) => api.getPublicNetworkHistory(id).then(value => list<MonitorHistory>(value))
export const listPublicCycleTransfer = (serverId?: number) => api.listPublicCycleTransfer(
  serverId ? { server_id: serverId } : undefined,
).then(value => list<CycleTransfer>(value))
export const getPublicServerAvailability = (id: number, params?: GetPublicServerAvailabilityParams) =>
  api.getPublicServerAvailability(id, params).then(value => data<PublicAvailability>(value))
export const getPublicMetrics = (id: number, params?: GetPublicMetricsParams) =>
  api.getPublicMetrics(id, params).then(value => list<PublicMetricPoint>(value))

export const getAdminSummary = () => api.getAdminSummary().then(value => data<Record<string, unknown>>(value))
export const listServers = (params: ResourceQuery = {}) => api.listServers(params).then(value => list<ServerRecord>(value))
export const getServer = (id: number) => api.getServer(id).then(value => data<ServerRecord>(value))
export const createServer = (body: ServerWriteBody) => api.createServer(body).then(value => data<ServerRecord>(value))
export const exportServers = () => api.exportServers().then(value => data<ServerBackup>(value))
export const previewServerImport = (body: ServerBackup) => api.previewServerImport(body).then(value => data<ServerImportPreview>(value))
export const importServers = (body: ServerImportWrite) => api.importServers(body).then(value => data<ServerImportResult>(value))
export const updateServer = (id: number, body: ServerWriteBody) => api.updateServer(id, body).then(value => data<ServerRecord>(value))
export const deleteServer = (id: number) => api.deleteServer(id)
export const resetServerSecret = (id: number) => api.resetServerSecret(id).then(value => data<{ secret: string }>(value))
export const resetServerAvailability = (id: number) => api.resetServerAvailability(id).then(value => data<Record<string, unknown>>(value))
export const batchUpdateServerGroup = (ids: number[], group: string) => api.batchUpdateServerGroup({ ids, group }).then(value => data<Record<string, unknown>>(value))
export const batchDeleteServers = (ids: number[]) => api.batchDeleteServers({ ids })
export const updateServerDisplayIndex = (id: number, body: ServerDisplayIndexWriteBody) => api.updateServerDisplayIndex(id, body).then(value => data<ServerRecord>(value))
export const listServerGroups = () => api.listServerGroups().then(value => list<ServerGroup>(value))
export const renameServerGroup = (body: ServerGroupRenameWriteBody) => api.renameServerGroup(body).then(value => data<Record<string, unknown>>(value))
export const getServerCredential = (id: number) => api.getServerCredential(id).then(value => data<ServerCredential>(value))
export const getServerInstallPreview = (id: number, body: InstallPreviewWriteBody) => api.getServerInstallPreview(id, body).then(value => data<InstallPreview>(value))
export const getServerUpgradePreview = (id: number, body: UpgradePreviewWriteBody) => api.getServerUpgradePreview(id, body).then(value => data<UpgradePreview>(value))
export const listScriptCommands = () => api.listScriptCommands().then(value => data<ScriptCommands>(value))
export const getProbeCapabilities = () => api.getProbeCapabilities().then(value => data<ProbeCapabilities>(value))
export const listServerAvailability = (serverId: number, params: { from?: string; to?: string; limit?: number; cursor?: string } = {}) => api.listServerAvailability(serverId, params).then(value => list<ResourceRecord>(value))

export const listMonitors = (params: ResourceQuery = {}) => api.listMonitors(params).then(value => list<Monitor>(value))
export const createMonitor = (body: MonitorWriteBody) => api.createMonitor(body).then(value => data<Monitor>(value))
export const updateMonitor = (id: number, body: MonitorWriteBody) => api.updateMonitor(id, body).then(value => data<Monitor>(value))
export const deleteMonitor = (id: number) => api.deleteMonitor(id)
export const listMonitorHistory = (id: number, params: { from?: string; to?: string; limit?: number; cursor?: string } = {}) => api.listMonitorHistory(id, params).then(value => list<ResourceRecord>(value))

export const listNotifications = (params: ResourceQuery = {}) => api.listNotifications(params).then(value => list<NotificationChannel>(value))
export const createNotification = (body: NotificationChannelWriteBody) => api.createNotification(body).then(value => data<NotificationChannel>(value))
export const updateNotification = (id: number, body: NotificationChannelWriteBody) => api.updateNotification(id, body).then(value => data<NotificationChannel>(value))
export const deleteNotification = (id: number) => api.deleteNotification(id)
export const testNotification = (id: number) => api.testNotification(id).then(value => data<Record<string, unknown>>(value))

export const listAlertRules = (params: ResourceQuery = {}) => api.listAlertRules(params).then(value => list<AlertRule>(value))
export const createAlertRule = (body: AlertRuleWriteBody) => api.createAlertRule(body).then(value => data<AlertRule>(value))
export const updateAlertRule = (id: number, body: AlertRuleWriteBody) => api.updateAlertRule(id, body).then(value => data<AlertRule>(value))
export const deleteAlertRule = (id: number) => api.deleteAlertRule(id)

export const listDDNSProviders = () => api.listDDNSProviders().then(value => list<DDNSProvider>(value))
export const listDDNSProfiles = (params: ResourceQuery = {}) => api.listDDNSProfiles(params).then(value => list<DDNSProfile>(value))
export const createDDNSProfile = (body: DDNSProfileWriteBody) => api.createDDNSProfile(body).then(value => data<DDNSProfile>(value))
export const updateDDNSProfile = (id: number, body: DDNSProfileWriteBody) => api.updateDDNSProfile(id, body).then(value => data<DDNSProfile>(value))
export const deleteDDNSProfile = (id: number) => api.deleteDDNSProfile(id)

export const listNATTunnels = (params: ResourceQuery = {}) => api.listNATTunnels(params).then(value => list<NATTunnel>(value))
export const createNATTunnel = (body: NATTunnelWriteBody) => api.createNATTunnel(body).then(value => data<NATTunnel>(value))
export const updateNATTunnel = (id: number, body: NATTunnelWriteBody) => api.updateNATTunnel(id, body).then(value => data<NATTunnel>(value))
export const deleteNATTunnel = (id: number) => api.deleteNATTunnel(id)

export const listTrafficPolicies = (serverId: number) => api.listTrafficPolicies(serverId).then(value => list<TrafficPolicy>(value))
export const createTrafficPolicy = (serverId: number, body: TrafficPolicyWriteBody) => api.createTrafficPolicy(serverId, body).then(value => data<TrafficPolicy>(value))
export const updateTrafficPolicy = (serverId: number, id: number, body: TrafficPolicyWriteBody) => api.updateTrafficPolicy(serverId, id, body).then(value => data<TrafficPolicy>(value))
export const deleteTrafficPolicy = (serverId: number, id: number) => api.deleteTrafficPolicy(serverId, id)
export const getTrafficPolicyUsage = (serverId: number, id: number) => api.getTrafficPolicyUsage(serverId, id).then(value => data<TrafficUsage>(value))
export const getServerTrafficHistory = (serverId: number, params?: GetServerTrafficHistoryParams) => api.getServerTrafficHistory(serverId, params).then(value => list<TrafficPolicyHistory>(value))

export const listCollectors = () => api.listCollectors().then(value => list<CollectorRecord>(value))
export const getConnectionSummary = () => api.getConnectionSummary().then(value => data<ConnectionSummary>(value))
export const listConnectionPaths = (params: { server_id?: number; observer_id?: string } = {}) => api.listConnectionPaths(params).then(value => list<ConnectionPath>(value))
export const listConnectionLatency = (params: ListConnectionLatencyParams = {}) => api.listConnectionLatency(params).then(value => list<ConnectionLatencyBucket>(value))
export const getProbeSummary = () => api.getProbeSummary().then(value => data<ProbeSummary>(value))
export const listProbePaths = (params: ListProbePathsParams = {}) => api.listProbePaths(params).then(value => list<ProbePath>(value))
export const listProbeSamples = (params: ListProbeSamplesParams = {}) => api.listProbeSamples(params).then(value => list<ProbeSampleBucket>(value))
export const getProbeTrace = (params: GetProbeTraceParams) => api.getProbeTrace(params).then(value => data<ProbeTrace | null>(value))
export const getProbeRoute = (params: GetProbeRouteParams) => api.getProbeRoute(params).then(value => data<ProbeRouteHistory | null>(value))
export const createProbeRoute = (body: ProbeRouteWrite) => api.createProbeRoute(body).then(value => data<ProbeRouteJob>(value))
export const createCollector = (body: CollectorWriteBody) => api.createCollector(body).then(value => data<CollectorCreated>(value))
export const updateCollector = (id: string, body: CollectorWriteBody) => api.updateCollector(id, body).then(value => data<CollectorRecord>(value))
export const updateCollectorScope = (id: string, body: CollectorScopeWriteBody) => api.updateCollectorScope(id, body).then(value => data<CollectorRecord>(value))
export const rotateCollectorToken = (id: string) => api.rotateCollectorToken(id).then(value => data<CollectorToken>(value))
export const getCollectorToken = (id: string) => api.getCollectorToken(id).then(value => data<CollectorToken>(value))
export const getCollectorInstallPreview = (id: string, body: CollectorInstallPreviewWriteBody) => api.getCollectorInstallPreview(id, body).then(value => data<CollectorInstallPreview>(value))
export const revokeCollector = (id: string) => api.revokeCollector(id).then(value => data<CollectorRecord>(value))
export const deleteCollector = (id: string) => api.deleteCollector(id)

export type TelemetryDatasetRecord =
  | ObserverAssignmentRecord
  | AgentReliabilityRecord
  | IncidentRecord
  | IncidentRevisionRecord
  | TelemetryDataLossRecord
  | TelemetryAlertRecord

const telemetryOperations: Record<string, (params?: ResourceQuery) => Promise<unknown>> = {
  assignments: params => api.listObserverAssignments(params),
  agents: params => api.listAgentReliability(params),
  incidents: params => api.listIncidents(params),
  'incident-revisions': params => api.listIncidentRevisions(params),
  'data-loss': params => api.listTelemetryDataLoss(params),
  alerts: params => api.listTelemetryAlerts(params),
}
export const telemetryList = (name: string, params: ResourceQuery = {}) => {
  const operation = telemetryOperations[name]
  if (!operation) return Promise.reject(new Error(`Unsupported telemetry dataset: ${name}`))
  return operation(params).then(value => list<TelemetryDatasetRecord>(value))
}

export const listApiTokens = () => api.listApiTokens().then(value => list<APIToken>(value))
export const createApiToken = (body: APITokenWriteBody) => api.createApiToken(body).then(value => data<APIToken>(value))
export const getApiToken = (id: number) => api.getApiToken(id).then(value => data<APIToken>(value))
export const patchApiToken = (id: number, body: APITokenPatchBody) => api.patchApiToken(id, body).then(value => data<APIToken>(value))
export const deleteApiToken = (id: number) => api.deleteApiToken(id)

export const getSettings = () => api.getSettings().then(value => data<Record<string, unknown>>(value))
export const updateSettings = (body: unknown) => api.updateSettings(body as Record<string, unknown>).then(value => data<Record<string, unknown>>(value))
export const listOfflineHistory = (serverId: number, page = 1, pageSize = 20) => api.listOfflineHistory({ server_id: serverId, page, page_size: pageSize }).then(value => list<ResourceRecord>(value))
export const deleteOfflineHistory = (id: number) => api.deleteOfflineHistory(id)
export const cleanupOfflineHistory = (body: unknown = {}) => api.cleanupOfflineHistory(body as Record<string, unknown>).then(value => data<Record<string, unknown>>(value))
export const getDatabase = () => api.getDatabase().then(value => data<DatabaseStatus>(value))
export const optimizeDatabase = () => api.optimizeDatabase().then(value => data<DatabaseStatus>(value))

export function websocketURL(path: string) {
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${protocol}//${location.host}${path}`
}
