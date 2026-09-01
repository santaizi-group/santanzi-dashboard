<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ServerRecord } from '@santaizi/api'
import { formatCompactBytes, toNazhuaServerView } from '../../domain/nazhuaServerView'
import { formatDateTime } from '../../utils/host'
import OsLogo from '../common/OsLogo.vue'

const props = defineProps<{ server: ServerRecord }>()
const { t, locale, te } = useI18n()
const view = computed(() => toNazhuaServerView(props.server))

function planTag(tag: string) {
  if (tag === '__dual_stack__') return t('dualStack')
  if (tag === '__ipv4_only__') return t('ipv4Only')
  if (tag === '__ipv6_only__') return t('ipv6Only')
  return tag
}

const rows = computed(() => {
  const item = view.value
  const note = item.publicNote
  const platform = [item.platformLabel || item.platform, item.platformVersion].filter(Boolean).join(' ')
  const load = item.load1 || item.load5 || item.load15
    ? `${item.load1.toFixed(2)} / ${item.load5.toFixed(2)} / ${item.load15.toFixed(2)}`
    : ''
  const temps = item.temperatures
    .map(row => `${row.name} ${row.value.toFixed(1)}°C`)
    .join(' · ')
  const billing = (() => {
    if (note.bill.amountKind === 'free') return t('freeBilling')
    if (note.bill.amountKind === 'metered') return t('meteredBilling')
    if (!note.bill.amountValue) return ''
    const cycle = note.bill.cycleLabel && te(note.bill.cycleLabel) ? t(note.bill.cycleLabel) : note.bill.cycleLabel
    return cycle ? `${note.bill.amountValue}/${cycle}` : note.bill.amountValue
  })()
  const remaining = note.bill.remainingKind === 'days' && note.bill.remainingDays !== null
    ? t('remainingDays', { n: note.bill.remainingDays })
    : note.bill.remainingKind === 'infinity'
      ? t('foreverValid')
      : note.bill.remainingKind === 'expired'
        ? t('expired')
        : ''
  const list: Array<{ key: string; label: string; value: string; flag?: boolean; os?: boolean; span?: boolean }> = []
  const push = (key: string, label: string, value: string, extra?: { flag?: boolean; os?: boolean; span?: boolean }) => {
    if (value) list.push({ key, label, value, ...extra })
  }
  push('cpu', t('nazhua.cpuModel'), item.cpuModels.join(' · '), { span: true })
  push('gpu', t('nazhua.gpu'), item.gpuNames.join(' · ') || (item.gpuPercent > 0 ? `${item.gpuPercent.toFixed(1)}%` : ''))
  push('temp', t('nazhua.temperature'), temps)
  push('platform', t('nazhua.platform'), platform, { os: true })
  push('arch', t('nazhua.arch'), item.arch)
  push('virt', t('nazhua.virtualization'), item.virtualization)
  push('version', t('nazhua.version'), item.agentVersion)
  push('load', t('nazhua.loadAvg'), load)
  if (item.processCount > 0) push('proc', t('nazhua.processCount'), String(item.processCount))
  if (item.tcpConnCount > 0) push('tcp', t('nazhua.tcpConn'), String(item.tcpConnCount))
  if (item.udpConnCount > 0) push('udp', t('nazhua.udpConn'), String(item.udpConnCount))
  if (item.transferIn > 0) push('tin', t('nazhua.transferIn'), formatCompactBytes(item.transferIn, 1))
  if (item.transferOut > 0) push('tout', t('nazhua.transferOut'), formatCompactBytes(item.transferOut, 1))
  push('boot', t('nazhua.bootTime'), formatDateTime(item.bootTime, locale.value))
  push('active', t('nazhua.lastActive'), formatDateTime(item.lastActive, locale.value))
  push('loc', t('nazhua.location'), note.presentation.locationLabel || item.location?.name || '', { flag: true })
  push('bill', t('nazhua.billing'), billing)
  push('remain', t('remaining'), remaining)
  push('bw', t('nazhua.bandwidth'), note.bill.bandwidth)
  return list
})
</script>

<template>
  <section v-if="rows.length || view.publicNote.planTags.length || view.orderLink" class="nazhua-info-box">
    <h2>{{ t('nazhua.serverInfo') }}</h2>
    <dl>
      <div v-for="row in rows" :key="row.key" :class="{ 'is-span': row.span }">
        <dt>{{ row.label }}</dt>
        <dd>
          <span v-if="row.flag && view.flagClass" :class="view.flagClass" class="nazhua-flag" aria-hidden="true" />
          <OsLogo v-if="row.os" :platform="view.platform" />
          {{ row.value }}
        </dd>
      </div>
    </dl>
    <div v-if="view.publicNote.planTags.length" class="nazhua-info-box__tags">
      <span v-for="tag in view.publicNote.planTags" :key="tag">{{ planTag(tag) }}</span>
    </div>
    <a v-if="view.orderLink" :href="view.orderLink" target="_blank" rel="noopener noreferrer" class="nazhua-info-box__buy">
      <i class="ri-shopping-bag-3-line"></i>{{ view.publicNote.presentation.buyBtnText || t('purchase') }}
    </a>
  </section>
</template>
