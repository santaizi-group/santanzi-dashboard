import type { CollectorRecord, ResourceRecord, ServerRecord } from '@santaizi/api'

export type ScopeMode = 'all' | 'include' | 'exclude'
export interface MonitorScope { mode: ScopeMode; server_ids: number[] }

export interface MonitorRecord extends ResourceRecord {
  id: number
  name: string
  type: 'http' | 'icmp' | 'tcp'
  target: string
  interval_seconds: number
  scope: MonitorScope
  notify: boolean
  notification_tag: string
  show_in_service: boolean
  latency_notify: boolean
  min_latency_ms: number
  max_latency_ms: number
}

export interface KeyValueRow { key: string; value: string }

export interface NotificationChannelRecord extends ResourceRecord {
  id: number
  name: string
  tag: string
  url: string
  method: 'get' | 'post'
  request_type: 'json' | 'form'
  headers: KeyValueRow[]
  body: string
  verify_tls: boolean
}

export type AlertMetric =
  | 'cpu' | 'gpu' | 'temperature_max' | 'memory' | 'swap' | 'disk'
  | 'net_in_speed' | 'net_out_speed' | 'net_all_speed' | 'offline'
  | 'load1' | 'load5' | 'load15' | 'tcp_conn_count' | 'udp_conn_count' | 'process_count'

export interface AlertCondition {
  type: AlertMetric
  min?: number | null
  max?: number | null
  duration_seconds: number
  scope: MonitorScope
}

export interface AlertRuleRecord extends ResourceRecord {
  id: number
  name: string
  notification_tag: string
  trigger_mode: 'always' | 'once'
  enabled: boolean
  conditions: AlertCondition[]
}

export interface DDNSProfileRecord extends ResourceRecord {
  id: number
  name: string
  provider: string
  domains: string[]
  enable_ipv4: boolean
  enable_ipv6: boolean
  access_id?: string
  access_secret?: string
  max_retries?: number
  webhook_url?: string
  webhook_method?: 'get' | 'post'
  webhook_request_type?: 'json' | 'form'
  webhook_headers?: KeyValueRow[]
  webhook_body?: string
}

export interface NATTunnelRecord extends ResourceRecord {
  id: number
  name: string
  server_id: number
  target: string
  domain: string
}

export type TrafficDirection = 'inbound' | 'outbound' | 'total'
export type TrafficMode = 'cumulative' | 'recurring'
export type TrafficCycleUnit = 'hour' | 'day' | 'week' | 'month' | 'year'
export interface TrafficUsage {
  used_bytes: number
  quota_bytes: number
  usage_percent: number
  status: 'normal' | 'warning' | 'exceeded'
  window_start: string
  window_end?: string | null
}

export interface TrafficPolicyRecord extends ResourceRecord {
  id?: number
  server_id?: number
  name: string
  direction: TrafficDirection
  mode: TrafficMode
  cycle_start?: string | null
  cycle_interval?: number
  cycle_unit?: TrafficCycleUnit
  quota_bytes: number
  warning_percent: number
  notification_tag?: string
  enabled: boolean
  usage?: TrafficUsage
}

export interface PublicNoteBilling {
  startDate: string
  endDate: string
  autoRenewal: '0' | '1'
  cycle: string
  amount: string
}
export interface PublicNotePlan {
  bandwidth: string
  trafficVol: string
  trafficType: string
  IPv4: '0' | '1'
  IPv6: '0' | '1'
  networkRoute: string[]
  extra: string[]
}
export interface PublicNotePresentation {
  location: string
  flag: string
  orderLink: string
  buyBtnText: string
  buyBtnIcon: string
  slogan: string
  lat: string
  lng: string
  latlng: string
  locationLabel: string
}
export interface PublicNoteForm {
  billing: PublicNoteBilling
  plan: PublicNotePlan
  presentation: PublicNotePresentation
  unlimitedEnd: boolean
}

export interface MonitoringOptions {
  cpu: boolean
  memory: boolean
  disk: boolean
  network: boolean
  connections: boolean
  processes: boolean
  temperature: boolean
  gpu: boolean
  host_info: boolean
  ip_report: boolean
  http_probe: boolean
  icmp_probe: boolean
  tcp_probe: boolean
  nat: boolean
}

export interface ProbeCapabilitiesMetadata {
  required: string[]
  optional: Array<{ id: string; enable_flag?: string; disable_flag?: string }>
  presets: Record<string, MonitoringOptions>
}

export type BotRole = 'blocked' | 'viewer' | 'operator' | 'admin'
export type BotPeriod = 'daily' | 'weekly' | 'monthly'
export type BotCover = 'all' | 'include' | 'exclude'
export type BotSection = 'status' | 'servers' | 'traffic' | 'uptime' | 'probes' | 'alerts'

export interface BotSettings {
  enabled: boolean
  provider: string
  mode: 'polling' | 'webhook'
  api_endpoint?: string
  webhook_base_url?: string
  charts: boolean
  language?: string
  rate_per_minute: number
  token_set: boolean
  token_suffix?: string
  webhook_secret_set: boolean
}

export interface BotChatRecord extends ResourceRecord {
  id: number
  chat_id: number
  kind: string
  title: string
  role: BotRole
  allowed_user_ids: number[]
  subscribe_tags: string[]
  note: string
  bound_by?: number
  last_seen_at?: string
  enabled: boolean
}

export interface BotBindCodeRecord extends ResourceRecord {
  id: number
  code: string
  role: BotRole
  expires_at: string
  used_by_chat_id?: number
  used_at?: string
  created_by?: string
}

export interface BotReportRecord extends ResourceRecord {
  id: number
  name: string
  chat_ids: number[]
  period: BotPeriod
  hour_local: number
  minute: number
  weekday: number
  day_of_month: number
  sections: BotSection[]
  cover: BotCover
  ignore_ids: number[]
  with_charts: boolean
  enabled: boolean
  last_period_key?: string
  last_run_at?: string
  last_status?: string
}

export interface ServerEditorValue extends ServerRecord {
  traffic_policies?: TrafficPolicyRecord[]
}
export type CollectorEditorValue = CollectorRecord
