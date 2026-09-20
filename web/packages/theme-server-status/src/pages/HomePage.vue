<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { listPublicCycleTransfer, type CycleTransfer } from '@santaizi/api'
import { AppEmpty } from '@santaizi/ui'
import {
  SERVER_SORT_COLUMNS,
  sortServers,
  useInjectedStatusStore,
  type ServerSortProp,
} from '@santaizi/status-core'
import { mapCycleTransfers, toServerStatusView } from '../domain/serverStatusView'
import { resolveStatusNoteColumns, type StatusTableColumns } from '../domain/statusTableColumns'
import { registerStatusPageActions } from '../composables/statusPageActions'
import { useStatusSort } from '../composables/useStatusSort'
import ServerDetailDrawer from '../components/ServerDetailDrawer.vue'
import StatusGlobe from '../components/StatusGlobe.vue'
import StatusTable from '../components/StatusTable.vue'

const { t, locale } = useI18n()
const store = useInjectedStatusStore()
const grouped = ref(localStorage.getItem('santaizi-status-grouped') !== '0')
const selectedId = ref(0)
const mapOpen = ref(false)
const mapDialog = ref<HTMLDialogElement>()
const cycleRows = ref<CycleTransfer[]>([])
const { sortProp, sortOrder, sortOption, setSortProp, toggleSort, toggleOrder } = useStatusSort()

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
  set: (open) => {
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

function showMap() {
  mapOpen.value = true
  const dialog = mapDialog.value
  if (dialog && !dialog.open) dialog.show()
}

function closeMap() {
  mapDialog.value?.close()
}

function onMapClosed() {
  mapOpen.value = false
}

function onGlobeSelect(id: number) {
  selectedId.value = id
}

function onMapEscape(event: KeyboardEvent) {
  if (selectedId.value > 0) return
  event.preventDefault()
  closeMap()
}

function onScrimClick() {
  if (selectedId.value > 0) return
  closeMap()
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

    <div
      v-if="mapOpen"
      class="map-scrim"
      @click="onScrimClick"
    ></div>
    <dialog ref="mapDialog" class="map-dialog" @close="onMapClosed" @keydown.esc="onMapEscape">
      <header>
        <h2 class="icon-text"><i class="ri-earth-line"></i>{{ t('worldMap') }}</h2>
        <button type="button" :aria-label="t('close')" @click="closeMap"><i class="ri-close-line"></i></button>
      </header>
      <div class="world-map">
        <StatusGlobe
          v-if="mapOpen"
          :servers="store.servers"
          :locale="locale"
          @select="onGlobeSelect"
        />
      </div>
    </dialog>
  </div>
</template>
