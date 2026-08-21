import type { ServerHost, ServerState, TrafficSummary } from './generated/model'

export type Locale = 'zh-CN' | 'zh-TW' | 'en-US' | 'es-ES'
export type ThemeMode = 'system' | 'light' | 'dark'

export interface ApiData<T> { data: T }
export interface ApiList<T> {
  data: T[]
  meta: { page?: number; page_size?: number; total?: number; next_cursor?: string }
}

export interface ProblemDetails {
  type?: string
  title: string
  status: number
  code: string
  detail?: string
  trace_id?: string
  errors?: Record<string, string[]>
}

export interface SessionUser {
  id: number
  login: string
  name: string
  avatar_url?: string
  super_admin: boolean
}

export interface SessionState {
  authenticated: boolean
  user?: SessionUser
  csrf_token: string
  login_url: string
  capabilities: string[]
  version: string
}

export interface SiteBootstrap {
  brand: string
  locale: Locale
  version: string
  csrf_token?: string
  logo_url: string
  footer_text?: string
  primary_color?: string
  custom_css?: string
  requires_view_password: boolean
  view_password_verified: boolean
  show_availability: boolean
  authenticated: boolean
  theme?: 'server-status' | 'nazhua'
  allow_frontend_theme_switch?: boolean
}

export interface ServerTelemetryPresentation {
  host: string
  connectivity: string
  available: boolean | null
  coverage: string
}

export interface ServerRecord extends ResourceRecord {
  id: number
  name: string
  tag: string
  secret?: string
  note?: string
  public_note?: Record<string, unknown>
  monitoring_options?: Record<string, boolean>
  display_index: number
  hide_for_guest: boolean
  enable_ddns: boolean
  ddns_profiles?: number[]
  host?: ServerHost
  state?: ServerState
  last_active?: string
  online?: boolean
  telemetry?: ServerTelemetryPresentation
  traffic_summaries?: TrafficSummary[]
  probe_target?: string
  probe_tcp_ports?: string
  probe_enable_icmp?: boolean
  probe_enable_tcp?: boolean
  probe_enable_mtr?: boolean
}

export interface CollectorRecord {
  id: string
  name: string
  address: string
  listen_port?: number
  tls: boolean
  insecure_tls: boolean
  location?: string
  kind?: 'observer' | 'probe'
  probe_interval_seconds?: number
  mtr_interval_seconds?: number
  mtr_probes?: number
  tcp_ports?: string
  enable_icmp?: boolean
  enable_tcp?: boolean
  enable_mtr?: boolean
  enable_ipv4?: boolean
  enable_ipv6?: boolean
  notify?: boolean
  notification_tag?: string
  latency_notify?: boolean
  min_latency_ms?: number
  max_latency_ms?: number
  fail_threshold?: number
  route_interval_seconds?: number
  route_keep?: number
  generation: number
  config_version: number
  revoked: boolean
  status?: string
  last_seen?: string
  last_sync?: string
  last_primary_seen?: string
  heartbeat_rtt_ms?: number
  heartbeat_rtt_sampled_at?: string
  replication_rtt_ms?: number
  replication_rtt_sampled_at?: string
  spool_size?: number
  pending_records?: number
  oldest_pending?: string
  replication_cursor?: number
  connected_agents?: number
  protocol_version?: string
  software_version?: string
  scopes?: Array<{ type: string; value: string }>
}

export interface ResourceRecord { id?: number | string; [key: string]: unknown }
export interface ResourceQuery {
  page?: number
  page_size?: number
  q?: string
  sort?: string
  order?: 'asc' | 'desc'
  [key: string]: string | number | boolean | undefined
}
