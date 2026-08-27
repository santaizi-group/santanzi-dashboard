<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { findIntersectingGroups } from '../../utils/worldMap'
import worldMapSvg from '../../assets/world-map.svg?url'

export type WorldMapPointStatus = 'online' | 'offline' | 'mixed'

export interface WorldMapLocation {
  key: string
  x: number
  y: number
  size?: number
  label?: string
  status?: WorldMapPointStatus
}

type LayoutPoint = {
  key: string
  left: number
  top: number
  size: number
  label: string
  status: WorldMapPointStatus
  topLeft: { left: number; top: number }
  bottomRight: { left: number; top: number }
  parent?: LayoutPoint
  children: LayoutPoint[]
}

function mergeStatus(a: WorldMapPointStatus, b: WorldMapPointStatus): WorldMapPointStatus {
  if (a === b) return a
  return 'mixed'
}

const props = withDefaults(defineProps<{
  locations: WorldMapLocation[]
  width?: number | null
}>(), {
  width: null,
})

const mapPoints = ref<Array<{
  key: string
  left: number
  top: number
  size: number
  label: string
  status: WorldMapPointStatus
}>>([])

const computedSize = computed(() => {
  if (props.width) {
    const adjusted = Math.max(props.width, 300)
    return { width: adjusted, height: Math.ceil((621 / 1280) * adjusted) }
  }
  return { width: 1280, height: 621 }
})

const mapStyle = computed(() => ({
  '--world-map-width': `${computedSize.value.width}px`,
  '--world-map-height': `${computedSize.value.height}px`,
  '--world-map-image': `url(${worldMapSvg})`,
}))

function computeMapPoints() {
  if (!props.locations.length) {
    mapPoints.value = []
    return
  }
  const scaleX = computedSize.value.width / 1280
  const scaleY = computedSize.value.height / 621
  const points: LayoutPoint[] = props.locations.map((item) => {
    const size = item.size || 4
    const left = scaleX * item.x
    const top = scaleY * item.y
    const half = (size + 8) / 2
    return {
      key: item.key,
      left,
      top,
      size,
      label: item.label || '',
      status: item.status || 'online',
      topLeft: { left: left - half, top: top - half },
      bottomRight: { left: left + half, top: top + half },
      parent: undefined,
      children: [],
    }
  })
  const groups = findIntersectingGroups(points)
  Object.entries(groups).forEach(([key, group]) => {
    const item = points.find((p) => p.key === key)
    if (!item || item.parent) return
    item.size = 6
    let label = item.label
    group.forEach((child) => {
      if (!child.parent) {
        child.parent = item
        label += `\n${child.label}`
        item.status = mergeStatus(item.status, child.status)
      }
    })
    item.label = label
  })
  mapPoints.value = points.filter((p) => !p.parent).map(({ key, left, top, size, label, status }) => ({
    key, left, top, size, label, status,
  }))
}

watch([() => props.locations, computedSize], computeMapPoints, { immediate: true, deep: true })

const tips = ref('')
const tipsStyle = ref<Record<string, string>>({})
let tipsTimer: number | undefined

function showTips(point: typeof mapPoints.value[number]) {
  tips.value = point.label
  tipsStyle.value = {
    top: `${point.top + 16}px`,
    left: `${point.left}px`,
  }
  window.clearTimeout(tipsTimer)
}

function toggleTips(point: typeof mapPoints.value[number]) {
  if (tips.value === point.label) {
    tips.value = ''
    return
  }
  showTips(point)
}

function hideTips() {
  tipsTimer = window.setTimeout(() => {
    tips.value = ''
  }, 120)
}

onBeforeUnmount(() => window.clearTimeout(tipsTimer))
</script>

<template>
  <div class="nazhua-world-map" :style="mapStyle">
    <div class="nazhua-world-map__image" aria-hidden="true"></div>
    <button
      v-for="point in mapPoints"
      :key="point.key"
      type="button"
      class="nazhua-world-map__point"
      :class="{ 'is-offline': point.status === 'offline', 'is-mixed': point.status === 'mixed', 'is-grouped': point.size >= 6 }"
      :aria-label="point.label"
      :style="{ left: `${point.left}px`, top: `${point.top}px`, '--point-size': `${point.size}px` }"
      @mouseenter="showTips(point)"
      @mouseleave="hideTips"
      @focus="showTips(point)"
      @blur="hideTips"
      @click="toggleTips(point)"
    ><i></i></button>
    <div v-if="tips" class="nazhua-world-map__tips" :style="tipsStyle">{{ tips }}</div>
  </div>
</template>
