<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { formatConnPair } from '@santaizi/theme-server-status'
import type { NazhuaServerView } from '../../domain/nazhuaServerView'
import { formatCompactBytes } from '../../domain/nazhuaServerView'
import { formatSpeed } from '../../utils/host'
import OsLogo from '../common/OsLogo.vue'

defineProps<{ servers: NazhuaServerView[] }>()
const { t } = useI18n()

function tone(percent: number) {
  if (percent >= 90) return 'critical'
  if (percent >= 70) return 'warning'
  return 'ok'
}
</script>

<template>
  <div class="nazhua-status-table" role="table">
    <div class="nazhua-status-table__head" role="row">
      <span role="columnheader">{{ t('nazhua.name') }}</span>
      <span role="columnheader">{{ t('nazhua.location') }}</span>
      <span role="columnheader" class="is-platform">{{ t('nazhua.platform') }}</span>
      <span role="columnheader" class="is-spec">{{ t('nazhua.arch') }}</span>
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
      <span role="cell" class="nazhua-status-table__os is-platform"><OsLogo :platform="server.platform" />{{ [server.platformLabel, server.platformVersion].filter(Boolean).join(' ') || '—' }}</span>
      <span role="cell" class="is-spec">{{ server.spec || server.arch || '—' }}</span>
      <span role="cell">{{ server.uptime }}</span>
      <span role="cell" class="nazhua-status-table__metric">
        <small>{{ server.cpuPercent.toFixed(1) }}%{{ server.cpuCaption ? ` ${server.cpuCaption}` : '' }}</small>
        <i class="nazhua-mini-bar" :class="`is-${tone(server.cpuPercent)}`"><b :style="{ width: `${server.cpuPercent}%` }" /></i>
      </span>
      <span role="cell" class="nazhua-status-table__metric">
        <small>{{ server.memoryCaption }}</small>
        <i class="nazhua-mini-bar" :class="`is-${tone(server.memoryPercent)}`"><b :style="{ width: `${server.memoryPercent}%` }" /></i>
      </span>
      <span role="cell" class="nazhua-status-table__metric">
        <small>{{ server.diskCaption }}</small>
        <i class="nazhua-mini-bar" :class="`is-${tone(server.diskPercent)}`"><b :style="{ width: `${server.diskPercent}%` }" /></i>
      </span>
      <span role="cell" class="nazhua-status-table__speed">{{ formatSpeed(server.speedIn) }} / {{ formatSpeed(server.speedOut) }}</span>
      <span role="cell" class="nazhua-status-table__metric is-cycle">
        <small>{{ formatCompactBytes(server.trafficBytes, 1) }}</small>
        <i v-if="server.cycle" class="nazhua-mini-bar" :class="`is-${tone(server.cycle.usagePercent)}`"><b :style="{ width: `${Math.min(100, server.cycle.usagePercent)}%` }" /></i>
      </span>
      <span role="cell" class="is-load">{{ server.load1.toFixed(2) }}</span>
      <span role="cell" class="nazhua-status-table__speed is-conn">{{ formatConnPair(server.tcpConnCount, server.udpConnCount) }}</span>
      <span role="cell" class="is-billing">{{ server.billing || '—' }}</span>
    </div>
  </div>
</template>
