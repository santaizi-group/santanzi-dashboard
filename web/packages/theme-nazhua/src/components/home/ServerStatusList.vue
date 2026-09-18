<script setup lang="ts">
import type { CSSProperties } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatConnPair, usageTone } from '@santaizi/theme-server-status'
import type { NazhuaServerView } from '../../domain/nazhuaServerView'
import { formatCompactBytes, formatNazhuaBilling, formatUptime } from '../../domain/nazhuaServerView'
import { formatSpeed } from '../../utils/host'
import OsLogo from '../common/OsLogo.vue'

defineProps<{ servers: NazhuaServerView[] }>()
const { t, te, locale } = useI18n()

function billingOf(server: NazhuaServerView) {
  return formatNazhuaBilling(server.publicNote, t, te) || '—'
}

function uptimeOf(server: NazhuaServerView) {
  const dayUnit = locale.value.startsWith('zh') ? t('day') : 'd'
  return formatUptime(server.uptimeSeconds, dayUnit)
}

function cycleTitle(server: NazhuaServerView) {
  if (!server.cycle) return undefined
  return `${formatCompactBytes(server.trafficBytes)} (${Number(server.cycle.usagePercent.toFixed(1))}%)`
}

function platformOf(server: NazhuaServerView) {
  return [server.platformLabel, server.platformVersion].filter(Boolean).join(' ') || '—'
}

function usageClass(percent: number, withTrack = false) {
  const n = Math.min(100, Math.max(0, Number.isFinite(percent) ? percent : 0))
  const tone = usageTone(n)
  return [
    'nazhua-status-table__usage',
    withTrack ? 'has-track' : undefined,
    n > 0 ? 'has-fill' : undefined,
    tone === 'ok' ? undefined : `is-${tone}`,
  ]
}

function usageStyle(percent: number): CSSProperties {
  const n = Math.min(100, Math.max(0, Number.isFinite(percent) ? percent : 0))
  return { '--usage': `${n}%` } as CSSProperties
}
</script>

<template>
  <div class="nazhua-status-table" role="table">
    <div class="nazhua-status-table__head" role="row">
      <span role="columnheader" class="nazhua-status-table__logo" :aria-label="t('nazhua.sortPlatform')"></span>
      <span role="columnheader">{{ t('nazhua.name') }}</span>
      <span role="columnheader">{{ t('nazhua.location') }}</span>
      <span role="columnheader" class="is-platform">{{ t('nazhua.platform') }}</span>
      <span role="columnheader" class="is-spec">{{ t('moreSpecs') }}</span>
      <span role="columnheader">{{ t('nazhua.uptime') }}</span>
      <span role="columnheader" class="is-metric">CPU</span>
      <span role="columnheader" class="is-metric">{{ t('nazhua.memory') }}</span>
      <span role="columnheader" class="is-metric">{{ t('nazhua.disk') }}</span>
      <span role="columnheader" class="is-metric">{{ t('nazhua.netSpeed') }}</span>
      <span role="columnheader" class="is-metric is-cycle">{{ t('nazhua.cycleTransfer') }}</span>
      <span role="columnheader" class="is-load">{{ t('load') }}</span>
      <span role="columnheader" class="is-metric is-conn">{{ t('connCount') }}</span>
      <span role="columnheader" class="is-billing">{{ t('nazhua.billing') }}</span>
    </div>
    <div
      v-for="server in servers"
      :key="server.id"
      class="nazhua-status-table__row"
      :class="{ offline: !server.online }"
      role="row"
    >
      <span role="cell" class="nazhua-status-table__logo">
        <OsLogo :platform="server.platform" />
      </span>
      <span role="cell" class="nazhua-status-table__name">
        <RouterLink
          :to="{ name: 'public-detail', params: { serverId: String(server.id) } }"
          class="nazhua-status-table__link"
        >
          <strong>{{ server.name }}</strong>
          <small>{{ server.slogan || server.group }}</small>
        </RouterLink>
      </span>
      <span role="cell" class="nazhua-status-table__flag">
        <span v-if="server.flagClass" :class="server.flagClass" class="nazhua-flag" aria-hidden="true" />
        <span v-else class="nazhua-flag-fallback" aria-hidden="true"><i class="ri-global-line"></i></span>
        {{ server.flagCode.toUpperCase() || '—' }}
      </span>
      <span role="cell" class="nazhua-status-table__os is-platform">{{ platformOf(server) }}</span>
      <span role="cell" class="is-spec">{{ server.tableSpec || server.arch || '—' }}</span>
      <span role="cell" class="is-uptime">{{ uptimeOf(server) }}</span>
      <span
        role="cell"
        class="nazhua-status-table__speed is-metric"
        :class="usageClass(server.cpuPercent, true)"
        :style="usageStyle(server.cpuPercent)"
      >{{ server.cpuPercent.toFixed(1) }}%</span>
      <span
        role="cell"
        class="nazhua-status-table__speed is-metric"
        :class="usageClass(server.memoryPercent, true)"
        :style="usageStyle(server.memoryPercent)"
        :title="server.memoryCaption"
      >{{ server.memoryValue }}</span>
      <span
        role="cell"
        class="nazhua-status-table__speed is-metric"
        :class="usageClass(server.diskPercent, true)"
        :style="usageStyle(server.diskPercent)"
        :title="server.diskCaption"
      >{{ server.diskValue }}</span>
      <span role="cell" class="nazhua-status-table__speed">{{ formatSpeed(server.speedIn) }} / {{ formatSpeed(server.speedOut) }}</span>
      <span
        role="cell"
        class="nazhua-status-table__speed is-cycle"
        :class="server.cycle ? usageClass(server.cycle.usagePercent) : undefined"
        :style="server.cycle ? usageStyle(server.cycle.usagePercent) : undefined"
        :title="cycleTitle(server)"
      >{{ formatCompactBytes(server.trafficBytes, 1) }}</span>
      <span role="cell" class="is-load">{{ server.load1.toFixed(2) }}</span>
      <span role="cell" class="nazhua-status-table__speed is-conn">{{ formatConnPair(server.tcpConnCount, server.udpConnCount) }}</span>
      <span role="cell" class="is-billing" :title="billingOf(server)">{{ billingOf(server) }}</span>
    </div>
  </div>
</template>
