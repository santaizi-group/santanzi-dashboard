import { describe, expect, it } from 'vitest'
import { formatAdminValue, formatAPIError, formatBytes, formatClockTime, formatDateTime, formatLatencyMs, formatProductVersion } from './format'

const values: Record<string, string> = {
  yes: '是', no: '否', healthy: '健康', loadFailed: '加载失败', requestFailedWithCode: '请求失败（错误码：x）',
  'errors.authentication_required': '登录已过期，请重新登录', 'errors.bot_test_failed': 'Token 测试失败',
  connectivity_degraded: '连通性降级',
}
const t = (key: string) => values[key] || key
const te = (key: string) => key in values

describe('localized value formatting', () => {
  it('formats bytes and protocol states without exposing raw values', () => {
    expect(formatBytes(1536, 'zh-CN')).toBe('1.5 KiB')
    expect(formatAdminValue('healthy', 'status', 'zh-CN', t, te)).toBe('健康')
    expect(formatAdminValue(true, 'active', 'zh-CN', t, te)).toBe('是')
    expect(formatAdminValue('2026-08-13T06:00:00.000Z', 'last_sync', 'en-US', t, te)).not.toBe('2026-08-13T06:00:00.000Z')
    expect(formatAdminValue('2026-08-13T06:00:00.000Z', 'last_primary_seen', 'en-US', t, te)).not.toBe('2026-08-13T06:00:00.000Z')
    expect(formatDateTime(1_700_000_000_000_000_000, 'en-US')).not.toBe('1700000000000000000')
    expect(formatLatencyMs(12.5, 'en-US')).toBe('12.5 ms')
    expect(formatLatencyMs(18.5, 'zh-CN')).toBe('18.5 ms')
    expect(formatLatencyMs(16, 'zh-CN')).toBe('16 ms')
    expect(formatLatencyMs(1500, 'en-US')).toContain('s')
    expect(formatAdminValue(18.5, 'heartbeat_rtt_ms', 'en-US', t, te)).toBe('18.5 ms')
    expect(formatAdminValue('2026-08-13T06:00:00.000Z', 'bucket_start', 'en-US', t, te)).not.toBe('2026-08-13T06:00:00.000Z')
    expect(formatAdminValue('CONNECTIVITY_DEGRADED', 'current_classification', 'zh-CN', t, te)).toBe('连通性降级')
    expect(formatProductVersion('1.2.3')).toBe('v1.2.3')
    expect(formatProductVersion('v2.0.0')).toBe('v2.0.0')
    expect(formatProductVersion('dev-08015')).toBe('dev-08015')
    expect(formatClockTime('2026-08-13T06:00:00.000Z', 'en-US')).toBe(
      new Intl.DateTimeFormat('en-US', { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(new Date('2026-08-13T06:00:00.000Z')),
    )
  })

  it('uses stable problem codes for localized API errors', () => {
    expect(formatAPIError({ code: 'authentication_required' }, t, te)).toBe('登录已过期，请重新登录')
  })

  it('surfaces problem detail instead of opaque numeric codes', () => {
    expect(formatAPIError({ code: 'bot_test_failed', message: '尚未保存 Token。' }, t, te)).toBe('Token 测试失败：尚未保存 Token。')
    expect(formatAPIError({ code: '10', message: '无法连接 Telegram Bot API。' }, t, te)).toBe('无法连接 Telegram Bot API。')
    expect(formatAPIError({ code: '10' }, t, te)).toBe('加载失败')
  })
})
