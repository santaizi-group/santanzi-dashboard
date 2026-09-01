import { describe, expect, it } from 'vitest'
import type { ResourceRecord, ServerRecord } from '@santaizi/api'
import {
  clampPercent,
  formatCompactBytes,
  formatUptime,
  mapCycleTransfers,
  parseCpuCores,
  percentOf,
  toNazhuaServerView,
} from '../../../../packages/theme-nazhua/src/domain/nazhuaServerView'

function server(overrides: Partial<ServerRecord> = {}): ServerRecord {
  return {
    id: 7,
    name: 'HKG-EDGE',
    tag: 'HKG',
    display_index: 10,
    hide_for_guest: false,
    enable_ddns: false,
    online: true,
    host: {
      CountryCode: 'HK',
      CPU: ['AMD EPYC 2 Physical Core'],
      MemTotal: 2_147_483_648,
      DiskTotal: 21_474_836_480,
      SwapTotal: 1_073_741_824,
      Platform: 'debian',
      Arch: 'amd64',
    },
    state: {
      CPU: 12.5,
      MemUsed: 1_073_741_824,
      DiskUsed: 10_737_418_240,
      SwapUsed: 268_435_456,
      Uptime: 172_900,
      NetInSpeed: 4096,
      NetOutSpeed: 2048,
      NetInTransfer: 1024,
      NetOutTransfer: 2048,
    },
    public_note: {
      customData: { location: 'HKG', slogan: '香港边缘', flag: 'hk', orderLink: 'https%3A%2F%2Fexample.com' },
      planDataMod: { networkRoute: 'CN2,GIA', IPv4: '1', IPv6: '1' },
      billingDataMod: { amount: '9.99CNY', cycle: '月' },
    },
    ...overrides,
  }
}

describe('Nazhua server view adapter', () => {
  it('reads totals from host, cores from CPU text, and builds used/total captions', () => {
    const view = toNazhuaServerView(server())
    expect(view.online).toBe(true)
    expect(view.cpuPercent).toBe(12.5)
    expect(view.cpuCores).toBe(2)
    expect(view.spec).toBe('2C2G20G')
    expect(view.memoryPercent).toBe(50)
    expect(view.diskPercent).toBe(50)
    expect(view.memoryValue).toBe('1024M')
    expect(view.memoryTotalLabel).toBe('2048M')
    expect(view.memoryText).toBe('1024M / 2048M')
    expect(view.memoryCaption).toBe('1024M / 2048M (50%)')
    expect(view.diskValue).toBe('10G')
    expect(view.diskTotalLabel).toBe('20G')
    expect(view.diskCaption).toBe('10G / 20G (50%)')
    expect(view.uptime).toBe('2')
    expect(view.slogan).toBe('香港边缘')
    expect(view.publicNote.planTags).toEqual(['CN2', 'GIA', '__dual_stack__'])
    expect(view.location?.code).toBeTruthy()
    expect(view.flagClass).toBe('fi fi-hk')
    expect(view.orderLink).toBe('https://example.com')
    expect(view.platform).toBe('debian')
    expect(view.platformLabel).toBe('Debian')
    expect(view.platformVersion).toBe('')
  })

  it('takes capacity totals from host and usage from state', () => {
    const view = toNazhuaServerView(server({
      host: { CountryCode: 'HK', CPU: ['8'], MemTotal: 100, DiskTotal: 40 },
      state: { CPU: 10, MemUsed: 50, DiskUsed: 20 },
    }))
    expect(view.cpuCores).toBe(8)
    expect(view.memoryPercent).toBe(50)
    expect(view.diskPercent).toBe(50)
  })

  it('parses physical and virtual cores and ignores unknown text as length fallback', () => {
    expect(parseCpuCores(['AMD EPYC 2 Physical Core', 'Intel 4 Virtual Core'])).toBe(6)
    expect(parseCpuCores(4)).toBe(4)
    expect(parseCpuCores(['not a core count'])).toBe(1)
  })

  it('resolves flags through the shared normalizer instead of trusting raw codes', () => {
    const host = { ...server().host, CountryCode: '' }
    const flagOf = (note: Record<string, unknown>) => toNazhuaServerView(server({ host, public_note: note })).flagClass
    // flag-icons 只有 ISO2，IATA 位置码必须先归一，否则渲染不出旗帜
    expect(flagOf({ customData: { location: 'LAX' } })).toBe('fi fi-us')
    expect(flagOf({ customData: { location: 'LON' } })).toBe('fi fi-gb')
    expect(flagOf({ customData: { flag: 'hkg', location: 'HKG' } })).toBe('fi fi-hk')
    expect(flagOf({ customData: { flag: 'uk' } })).toBe('fi fi-gb')
    expect(flagOf({})).toBe('')
    expect(toNazhuaServerView(server({ host, public_note: { customData: { location: 'LAX' } } })).flagCode).toBe('us')
  })

  it('does not infer online from stale telemetry', () => {
    expect(toNazhuaServerView(server({ online: false })).online).toBe(false)
  })

  it('maps and aggregates one homepage cycle-transfer response', () => {
    const rows: ResourceRecord[] = [
      { server_id: 7, name: '月流量', direction: 'both', used_bytes: 30, quota_bytes: 100, remaining_bytes: 70, status: 'normal' },
      { server_id: 7, name: '附加', direction: 'both', used_bytes: 20, quota_bytes: 100, remaining_bytes: 80, status: 'warning' },
      { server_id: 8, used_bytes: 5, quota_bytes: 10 },
    ]
    const cycles = mapCycleTransfers(rows)
    expect(cycles.get(7)).toMatchObject({ usedBytes: 50, quotaBytes: 200, remainingBytes: 150, usagePercent: 25, status: 'warning' })
    expect(toNazhuaServerView(server(), cycles).trafficBytes).toBe(150)
  })

  it('uses trafficType when there is no cycle remaining', () => {
    const inbound = 100
    const outbound = 250
    const withType = (trafficType: string) => toNazhuaServerView(server({
      state: { ...server().state, NetInTransfer: inbound, NetOutTransfer: outbound },
      public_note: { planDataMod: { trafficType } },
    })).trafficBytes
    expect(withType('1')).toBe(outbound)
    expect(withType('2')).toBe(inbound + outbound)
    expect(withType('3')).toBe(outbound)
    expect(withType('')).toBe(inbound + outbound)
  })

  it('keeps format helpers bounded and deterministic', () => {
    expect(clampPercent(140)).toBe(100)
    expect(percentOf(1, 4)).toBe(25)
    expect(formatCompactBytes(1_073_741_824)).toBe('1024M')
    expect(formatCompactBytes(2 * 1024 ** 3)).toBe('2G')
    expect(formatUptime(3_601)).toBe('1h')
  })
})
