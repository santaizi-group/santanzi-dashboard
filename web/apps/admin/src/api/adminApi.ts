import * as api from '@santaizi/api'
import type {
  AlertRuleWriteBody, CollectorRecord, DDNSProfileWriteBody, MonitorWriteBody,
  NATTunnelWriteBody, NotificationChannelWriteBody, ResourceQuery, ResourceRecord,
  ScriptCommand, ServerBackup, ServerImportWrite, ServerRecord, ServerWriteBody,
} from '@santaizi/api'
import type {
  AlertRuleRecord, DDNSProfileRecord, MonitorRecord,
  NATTunnelRecord, NotificationChannelRecord, ProbeCapabilitiesMetadata,
  TrafficPolicyRecord,
} from '@/types/admin'

export const listServers = (query: ResourceQuery = {}) => api.listServers(query)
export const listAllServers = () => api.listServers({ page: 1, page_size: 1000, sort: 'name', order: 'asc' })

export async function listAllServersPaged() {
  const pageSize = 200
  const all: ServerRecord[] = []
  for (let page = 1; page <= 50; page++) {
    const result = await listServers({ page, page_size: pageSize, sort: 'name', order: 'asc' })
    all.push(...result.data)
    const total = result.meta.total ?? all.length
    if (all.length >= total || result.data.length < pageSize) break
  }
  return all
}

export const listMonitors = (query: ResourceQuery = {}) => api.listMonitors(query) as Promise<api.ApiList<MonitorRecord>>
export const createMonitor = (body: MonitorWriteBody) => api.createMonitor(body)
export const updateMonitor = (id: number, body: MonitorWriteBody) => api.updateMonitor(id, body)
export const deleteMonitor = api.deleteMonitor
export const listMonitorHistory = api.listMonitorHistory

export const listNotifications = (query: ResourceQuery = {}) => api.listNotifications(query) as Promise<api.ApiList<NotificationChannelRecord>>
export const createNotification = (body: NotificationChannelWriteBody) => api.createNotification(body)
export const updateNotification = (id: number, body: NotificationChannelWriteBody) => api.updateNotification(id, body)
export const deleteNotification = api.deleteNotification
export const testNotification = api.testNotification

export const listAlertRules = (query: ResourceQuery = {}) => api.listAlertRules(query) as Promise<api.ApiList<AlertRuleRecord>>
export const createAlertRule = (body: AlertRuleWriteBody) => api.createAlertRule(body)
export const updateAlertRule = (id: number, body: AlertRuleWriteBody) => api.updateAlertRule(id, body)
export const deleteAlertRule = api.deleteAlertRule

export const listDDNSProfiles = (query: ResourceQuery = {}) => api.listDDNSProfiles(query) as Promise<api.ApiList<DDNSProfileRecord>>
export const listDDNSProviders = api.listDDNSProviders
export const createDDNSProfile = (body: DDNSProfileWriteBody) => api.createDDNSProfile(body)
export const updateDDNSProfile = (id: number, body: DDNSProfileWriteBody) => api.updateDDNSProfile(id, body)
export const deleteDDNSProfile = api.deleteDDNSProfile

export const listNATTunnels = (query: ResourceQuery = {}) => api.listNATTunnels(query) as Promise<api.ApiList<NATTunnelRecord>>
export const createNATTunnel = (body: NATTunnelWriteBody) => api.createNATTunnel(body)
export const updateNATTunnel = (id: number, body: NATTunnelWriteBody) => api.updateNATTunnel(id, body)
export const deleteNATTunnel = api.deleteNATTunnel

export const createServer = (body: ServerWriteBody) => api.createServer(body)
export const exportServers = () => api.exportServers()
export const previewServerImport = (body: ServerBackup) => api.previewServerImport(body)
export const importServersBackup = (body: ServerImportWrite) => api.importServers(body)
export const updateServer = (id: number, body: ServerWriteBody) => api.updateServer(id, body)
export const deleteServer = api.deleteServer
export const resetServerSecret = api.resetServerSecret
export const resetServerAvailability = api.resetServerAvailability
export const batchUpdateServerGroup = api.batchUpdateServerGroup
export const batchDeleteServers = api.batchDeleteServers
export const updateServerDisplayIndex = (id: number, displayIndex: number) => api.updateServerDisplayIndex(id, { display_index: displayIndex })
export const listServerGroups = api.listServerGroups
export const renameServerGroup = api.renameServerGroup
export const getServerCredential = (server: ServerRecord) => api.getServerCredential(server.id)

export const listTrafficPolicies = (serverId: number) => api.listTrafficPolicies(serverId) as Promise<api.ApiList<TrafficPolicyRecord>>
export const getTrafficPolicyUsage = api.getTrafficPolicyUsage
export const getServerTrafficHistory = api.getServerTrafficHistory
export const getProbeCapabilities = () => api.getProbeCapabilities() as Promise<ProbeCapabilitiesMetadata>
export const getServerInstallPreview = api.getServerInstallPreview
export const getServerUpgradePreview = api.getServerUpgradePreview
export const listScriptCommands = api.listScriptCommands

export const listCollectors = api.listCollectors
export const getConnectionSummary = api.getConnectionSummary
export const listConnectionPaths = api.listConnectionPaths
export const listConnectionLatency = api.listConnectionLatency
export const getProbeSummary = api.getProbeSummary
export const listProbePaths = api.listProbePaths
export const listProbeSamples = api.listProbeSamples
export const getProbeTrace = api.getProbeTrace
export const getProbeRoute = api.getProbeRoute
export const createProbeRoute = api.createProbeRoute
export const createCollector = api.createCollector
export const updateCollector = api.updateCollector
export const deleteCollector = api.deleteCollector
export const updateCollectorScope = api.updateCollectorScope
export const rotateCollectorToken = api.rotateCollectorToken
export const getCollectorToken = api.getCollectorToken
export const getCollectorInstallPreview = api.getCollectorInstallPreview
export const revokeCollector = api.revokeCollector
export const telemetryList = api.telemetryList
export const listServerAvailability = api.listServerAvailability
export const listOfflineHistory = api.listOfflineHistory
export const deleteOfflineHistory = api.deleteOfflineHistory
export const getSettings = api.getSettings

export async function listNotificationGroups() {
  const result = await listNotifications({ page: 1, page_size: 1000, sort: 'tag', order: 'asc' })
  return [...new Set(result.data.map(item => item.tag).filter(Boolean))].sort()
}

export type { CollectorRecord, ResourceRecord, ServerRecord, ScriptCommand }
