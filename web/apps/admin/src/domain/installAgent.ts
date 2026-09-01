import type { InstallPreviewWrite } from '@santaizi/api'
import type { MonitoringOptions } from '@/types/admin'

export type InstallProfile = 'standard_cloud' | 'standard_physical' | 'light' | 'alive'

/** 列表一键复制与安装弹窗打开时的默认值必须同源。 */
export const INSTALL_PRESETS: Record<InstallProfile, MonitoringOptions> = {
  standard_cloud: {
    cpu: true, memory: true, disk: true, network: true, connections: true, processes: true,
    temperature: false, gpu: false, host_info: true, ip_report: true, http_probe: true, icmp_probe: true, tcp_probe: true, nat: false,
  },
  standard_physical: {
    cpu: true, memory: true, disk: true, network: true, connections: true, processes: true,
    temperature: true, gpu: true, host_info: true, ip_report: true, http_probe: true, icmp_probe: true, tcp_probe: true, nat: false,
  },
  light: {
    cpu: true, memory: true, disk: true, network: true, connections: false, processes: false,
    temperature: false, gpu: false, host_info: true, ip_report: true, http_probe: true, icmp_probe: true, tcp_probe: true, nat: false,
  },
  alive: {
    cpu: false, memory: false, disk: false, network: false, connections: false, processes: false,
    temperature: false, gpu: false, host_info: false, ip_report: false, http_probe: false, icmp_probe: false, tcp_probe: false, nat: false,
  },
}

export const DEFAULT_INSTALL_PROFILE: InstallProfile = 'standard_cloud'
export const DEFAULT_INSTALL_PLATFORM: InstallPreviewWrite['platform'] = 'linux'
export const DEFAULT_INSTALL_IMPLEMENTATION: NonNullable<InstallPreviewWrite['implementation']> = 'go'
export const DEFAULT_CLEAN_INSTALL = true

export function defaultInstallPreviewBody(): InstallPreviewWrite {
  return {
    platform: DEFAULT_INSTALL_PLATFORM,
    clean_install: DEFAULT_CLEAN_INSTALL,
    implementation: DEFAULT_INSTALL_IMPLEMENTATION,
    options: { ...INSTALL_PRESETS[DEFAULT_INSTALL_PROFILE] },
  }
}
