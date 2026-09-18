<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ServerRecord } from '@santaizi/api'
import type { ServerSortOrder, ServerSortProp } from '@santaizi/status-core'
import type { CycleTransferMap } from '../domain/serverStatusView'
import { toServerStatusViews } from '../domain/serverStatusView'
import {
  columnSortProp,
  statusTableLayout,
  type ColumnId,
  type StatusTableColumns,
} from '../domain/statusTableColumns'
import ServerRow from './ServerRow.vue'

const props = defineProps<{
  title?: string
  servers: readonly ServerRecord[]
  cycles?: CycleTransferMap
  columns: StatusTableColumns
  sortProp: ServerSortProp
  sortOrder: ServerSortOrder
}>()

const emit = defineEmits<{ select: [id: number]; sort: [prop: ServerSortProp] }>()
const { locale, t } = useI18n()
const views = computed(() => toServerStatusViews([...props.servers], props.cycles, Date.now(), locale.value))
const layout = computed(() => statusTableLayout(props.columns))
const tableStyle = computed(() => ({
  '--ss-table-cols': layout.value.columns,
  '--ss-table-min': `${layout.value.minWidth}px`,
}))

const heads = computed(() => {
  const visible: Array<{ id: ColumnId; label: string; sort?: ServerSortProp }> = [
    { id: 'status', label: t('status'), sort: columnSortProp('status') },
    { id: 'name', label: t('name'), sort: columnSortProp('name') },
    { id: 'platform', label: t('platform'), sort: columnSortProp('platform') },
  ]
  if (props.columns.location) visible.push({ id: 'location', label: t('location'), sort: columnSortProp('location') })
  if (props.columns.price) visible.push({ id: 'price', label: t('price') })
  visible.push({ id: 'online', label: t('online'), sort: columnSortProp('online') })
  if (props.columns.availability) visible.push({ id: 'availability', label: t('availability') })
  visible.push(
    { id: 'load', label: t('load'), sort: columnSortProp('load') },
    { id: 'conn', label: t('connCount'), sort: columnSortProp('conn') },
    { id: 'speed', label: t('networkSpeed'), sort: columnSortProp('speed') },
    { id: 'traffic', label: t('traffic'), sort: columnSortProp('traffic') },
    { id: 'cores', label: t('cores'), sort: columnSortProp('cores') },
    { id: 'memory', label: t('memory'), sort: columnSortProp('memory') },
    { id: 'disk', label: t('disk'), sort: columnSortProp('disk') },
  )
  if (props.columns.remaining) visible.push({ id: 'remaining', label: t('remaining') })
  return visible
})

function ariaSort(prop: ServerSortProp | undefined) {
  if (!prop || props.sortProp !== prop) return 'none'
  return props.sortOrder === 'asc' ? 'ascending' : 'descending'
}

function sortIcon(prop: ServerSortProp) {
  if (props.sortProp !== prop) return 'ri-arrow-up-down-line'
  return props.sortOrder === 'asc' ? 'ri-arrow-up-line' : 'ri-arrow-down-line'
}

function onSort(prop?: ServerSortProp) {
  if (prop) emit('sort', prop)
}
</script>

<template>
  <section class="status-panel">
    <header v-if="title" class="group-title">
      <span>{{ title }}</span>
      <small>{{ servers.length }}</small>
    </header>
    <div class="ss-table" :style="tableStyle" role="table">
      <div class="ss-table__head" role="row">
        <template v-for="head in heads" :key="head.id">
          <button
            v-if="head.sort"
            type="button"
            class="ss-th"
            :class="{ 'is-sorted': sortProp === head.sort }"
            :aria-sort="ariaSort(head.sort)"
            @click="onSort(head.sort)"
          >
            <span>{{ head.label }}</span>
            <i :class="sortIcon(head.sort)" aria-hidden="true"></i>
          </button>
          <span v-else>{{ head.label }}</span>
        </template>
      </div>
      <ServerRow
        v-for="row in views"
        :key="row.id"
        :server="row"
        :columns="columns"
        :show-group="!title"
        @select="emit('select', row.id)"
      />
    </div>
  </section>
</template>
