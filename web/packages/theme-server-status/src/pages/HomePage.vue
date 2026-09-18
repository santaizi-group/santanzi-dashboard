<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import * as echarts from 'echarts/core'
import { MapChart } from 'echarts/charts'
import { TooltipComponent, VisualMapComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import { listPublicCycleTransfer, type CycleTransfer } from '@santaizi/api'
import { AppEmpty } from '@santaizi/ui'
import {
  SERVER_SORT_COLUMNS,
  sortServers,
  useInjectedStatusStore,
  type ServerSortProp,
} from '@santaizi/status-core'
import { getPresentation } from '../domain/publicNoteView'
import { choroplethIso2, regionDisplayName } from '../domain/regionDisplay'
import { mapCycleTransfers, toServerStatusView } from '../domain/serverStatusView'
import { resolveStatusNoteColumns, type StatusTableColumns } from '../domain/statusTableColumns'
import { registerStatusPageActions } from '../composables/statusPageActions'
import { useStatusSort } from '../composables/useStatusSort'
import ServerDetailDrawer from '../components/ServerDetailDrawer.vue'
import StatusTable from '../components/StatusTable.vue'

echarts.use([MapChart, TooltipComponent, VisualMapComponent, CanvasRenderer])

const { t, locale } = useI18n()
const store = useInjectedStatusStore()
const grouped = ref(localStorage.getItem('santaizi-status-grouped') !== '0')
const selectedId = ref(0)
const mapDialog = ref<HTMLDialogElement>()
const mapNode = ref<HTMLElement>()
const cycleRows = ref<CycleTransfer[]>([])
const { sortProp, sortOrder, sortOption, setSortProp, toggleSort, toggleOrder } = useStatusSort()
let chart: echarts.ECharts | undefined

const cycles = computed(() => mapCycleTransfers(cycleRows.value))
const sortedAll = computed(() => sortServers(store.servers, sortProp.value, sortOrder.value))
const sortedGroups = computed(() => store.groups.map((group) => ({
  ...group,
  items: sortServers(group.items, sortProp.value, sortOrder.value),
})))
const sortLabel = computed(() => t(sortOption.value?.labelKey || 'nazhua.sortWeight'))
const showAvailability = computed(() => store.bootstrap?.show_availability !== false)
const tableColumns = computed((): StatusTableColumns => ({
  availability: showAvailability.value,
  ...resolveStatusNoteColumns(store.servers, Date.now(), locale.value),
}))
const connectionClass = computed(() => {
  if (store.loadError) return 'failed'
  if (store.connected) return 'connected'
  return ''
})
const connectionLabel = computed(() => {
  if (store.loadError) return t('loadFailed')
  if (store.connected) return t('liveConnected')
  return t('reconnecting')
})
const emptyDescription = computed(() => {
  if (store.loadError) return t('requestFailed')
  if (store.loading) return t('loading')
  return t('noData')
})
const drawerOpen = computed({
  get: () => selectedId.value > 0,
  set: (open: boolean) => {
    if (!open) selectedId.value = 0
  },
})
const selectedServer = computed(() => {
  const record = store.servers.find((row) => row.id === selectedId.value)
  if (!record) return null
  return toServerStatusView(record, cycles.value, Date.now(), locale.value)
})

function toggle() {
  grouped.value = !grouped.value
  selectedId.value = 0
  localStorage.setItem('santaizi-status-grouped', grouped.value ? '1' : '0')
}

function selectRow(id: number) {
  selectedId.value = selectedId.value === id ? 0 : id
}

async function loadCycles() {
  try {
    const result = await listPublicCycleTransfer()
    cycleRows.value = result.data || []
  } catch {
    cycleRows.value = []
  }
}

async function showMap() {
  mapDialog.value?.showModal()
  await nextTick()
  if (!mapNode.value) return
  try {
    const response = await fetch('/static/theme-server-status/maps/santaizi.world.geo.json')
    if (!response.ok) return
    const geo = await response.json()
    echarts.registerMap('santaizi-world', geo)
    const counts = new Map<string, number>()
    for (const row of store.servers) {
      const location = getPresentation(row.public_note).location
      const country = row.host?.CountryCode || ''
      const code = choroplethIso2(location, country)
      if (code) counts.set(code, (counts.get(code) || 0) + 1)
    }
    chart?.dispose()
    chart = echarts.init(mapNode.value)
    const accent = getComputedStyle(document.documentElement).getPropertyValue('--ss-accent').trim() || '#2563eb'
    const soft = getComputedStyle(document.documentElement).getPropertyValue('--sz-primary-soft').trim() || '#dbeafe'
    const mapLocale = locale.value
    chart.setOption({
      tooltip: {
        trigger: 'item',
        formatter: (params: { name?: string; value?: number }) => {
          const iso = String(params.name || '')
          const label = regionDisplayName(iso, mapLocale) || iso
          const count = Number(params.value) || 0
          return `${label} ${count}`
        },
      },
      visualMap: {
        min: 0,
        max: Math.max(1, ...counts.values()),
        left: 20,
        bottom: 20,
        inRange: { color: [soft, accent] },
      },
      series: [{
        type: 'map',
        map: 'santaizi-world',
        nameProperty: 'iso_a2',
        roam: true,
        data: [...counts].map(([name, value]) => ({ name, value })),
      }],
    })
  } catch {
    chart?.dispose()
    chart = undefined
  }
}

function closeMap() {
  mapDialog.value?.close()
}

function onMapClosed() {
  chart?.dispose()
  chart = undefined
}

onMounted(loadCycles)

registerStatusPageActions(() => [
  ...(store.loadError
    ? [{ id: 'refresh', label: t('refresh'), icon: 'ri-refresh-line', run: () => { void store.load() } }]
    : []),
  { id: 'map', label: t('worldMap'), icon: 'ri-earth-line', run: () => { void showMap() } },
  {
    id: 'group',
    label: t(grouped.value ? 'flatView' : 'groupView'),
    icon: grouped.value ? 'ri-list-check-2' : 'ri-folder-chart-line',
    run: toggle,
  },
])
</script>

<template>
  <div class="status-container">
    <div class="status-toolbar">
      <span :class="['connection-state', connectionClass]" :aria-label="connectionLabel">
        <i></i><span class="connection-state__label">{{ connectionLabel }}</span>
      </span>
      <span />
      <el-dropdown trigger="click" popper-class="ss-sort-menu" @command="setSortProp($event as ServerSortProp)">
        <button type="button" class="ss-sort" :aria-label="t('nazhua.sort')">
          <span>{{ sortLabel }}</span>
          <span
            class="ss-sort__order"
            role="button"
            tabindex="0"
            :aria-label="t('nazhua.sort')"
            @click.stop="toggleOrder"
            @keydown.enter.stop="toggleOrder"
            @keydown.space.prevent.stop="toggleOrder"
          ><i :class="sortOrder === 'asc' ? 'ri-arrow-up-line' : 'ri-arrow-down-line'"></i></span>
        </button>
        <template #dropdown>
          <el-dropdown-menu>
            <div v-for="column in SERVER_SORT_COLUMNS" :key="column.group" class="ss-sort-menu__column" role="none">
              <el-dropdown-item
                v-for="option in column.options"
                :key="option.prop"
                :command="option.prop"
                :class="{ 'is-current': option.prop === sortProp }"
              >{{ t(option.labelKey) }}</el-dropdown-item>
            </div>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
      <button v-if="store.loadError" type="button" @click="store.load">
        <i class="ri-refresh-line"></i>{{ t('refresh') }}
      </button>
      <button type="button" class="status-toolbar__page-action" @click="showMap">
        <i class="ri-earth-line"></i>{{ t('worldMap') }}
      </button>
      <button type="button" class="status-toolbar__page-action" @click="toggle">
        <i :class="grouped ? 'ri-list-check-2' : 'ri-folder-chart-line'"></i>
        {{ t(grouped ? 'flatView' : 'groupView') }}
      </button>
    </div>

    <template v-if="store.servers.length">
      <template v-if="grouped">
        <StatusTable
          v-for="group in sortedGroups"
          :key="group.name"
          :title="group.name"
          :servers="group.items"
          :cycles="cycles"
          :columns="tableColumns"
          :sort-prop="sortProp"
          :sort-order="sortOrder"
          @select="selectRow"
          @sort="toggleSort"
        />
      </template>
      <StatusTable
        v-else
        :servers="sortedAll"
        :cycles="cycles"
        :columns="tableColumns"
        :sort-prop="sortProp"
        :sort-order="sortOrder"
        @select="selectRow"
        @sort="toggleSort"
      />
    </template>
    <div v-else class="empty-status status-page-empty">
      <AppEmpty
        :tone="store.loadError ? 'danger' : 'default'"
        :icon="store.loadError ? 'ri-error-warning-line' : 'ri-server-line'"
        :title="store.loadError ? t('loadFailed') : ''"
        :description="emptyDescription"
      />
      <el-button v-if="store.loadError" type="primary" @click="store.load">
        <i class="ri-refresh-line"></i>{{ t('refresh') }}
      </el-button>
    </div>

    <ServerDetailDrawer
      v-model="drawerOpen"
      :server="selectedServer"
      :show-availability="showAvailability"
    />

    <dialog ref="mapDialog" class="map-dialog" @close="onMapClosed">
      <header>
        <h2 class="icon-text"><i class="ri-earth-line"></i>{{ t('worldMap') }}</h2>
        <button type="button" :aria-label="t('close')" @click="closeMap"><i class="ri-close-line"></i></button>
      </header>
      <div ref="mapNode" class="world-map" />
    </dialog>
  </div>
</template>
