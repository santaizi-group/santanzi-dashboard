<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { geoGraticule, geoOrthographic, geoPath } from 'd3-geo'
import { useI18n } from 'vue-i18n'
import { AppEmpty } from '@santaizi/ui'
import type { ServerRecord } from '@santaizi/api'
import { DEFAULT_VIEW, sphericalMean } from '@santaizi/geo'
import worldUrl from '../assets/world.geo.json?url'
import {
  clusterGlobeMarkers,
  isGlobeFront,
  type GlobeMarker,
  type GlobeMarkerStatus,
} from '../domain/globeMarkers'

const props = defineProps<{
  servers: ServerRecord[]
  locale: string
}>()
const emit = defineEmits<{ select: [id: number] }>()
const { t } = useI18n()

const boxRef = ref<HTMLElement>()
const canvasRef = ref<HTMLCanvasElement>()
const tooltip = ref<{ x: number; y: number; marker: GlobeMarker } | null>(null)
const pinned = ref<{ x: number; y: number; marker: GlobeMarker } | null>(null)
const ready = ref(false)

const clustered = computed(() => clusterGlobeMarkers(props.servers, props.locale))
const markers = computed(() => clustered.value.markers)
const unlocated = computed(() => clustered.value.unlocated)

type WorldGeo = { type: 'FeatureCollection'; features: Array<{ type: 'Feature'; geometry: object; properties?: { iso_a2?: string } | null }> }
type Palette = ReturnType<typeof palette>
type Projection = ReturnType<typeof geoOrthographic>
type Hit = { marker: GlobeMarker; x: number; y: number; r: number }

let world: WorldGeo | null = null
let resizeObserver: ResizeObserver | undefined
let themeObserver: MutationObserver | undefined
let frame = 0
let pulse = 0
let baseLayer: HTMLCanvasElement | null = null
let baseKey = ''
let haloDiameter = 0
let dragging = false
let viewed = false
let lambda = 0
let phi = -20
let scaleMul = 0.82
let dragStart: { x: number; y: number; lambda: number; phi: number } | null = null
let hits: Hit[] = []

const PULSE_MS = 2400
const GLOBE_RATIO = 0.5
const GLOBE_FIT_PAD = 0
const GLOBE_SCALE_MIN = 0.64
const GLOBE_SCALE_DEFAULT = 0.82

function cssVar(name: string, fallback: string) {
  const host = boxRef.value || document.documentElement
  return getComputedStyle(host).getPropertyValue(name).trim() || fallback
}

function withAlpha(color: string, alpha: number) {
  const value = color.trim()
  const comma = value.match(/^rgba?\(\s*([\d.]+)\s*,\s*([\d.]+)\s*,\s*([\d.]+)(?:\s*,\s*([\d.]+))?\s*\)$/i)
  if (comma) return `rgba(${comma[1]}, ${comma[2]}, ${comma[3]}, ${alpha})`
  const space = value.match(/^rgba?\(\s*([\d.]+)\s+([\d.]+)\s+([\d.]+)(?:\s*\/\s*[\d.%]+)?\s*\)$/i)
  if (space) return `rgba(${space[1]}, ${space[2]}, ${space[3]}, ${alpha})`
  return `rgba(56, 189, 248, ${alpha})`
}

function colorAlpha(color: string) {
  const value = color.trim()
  const comma = value.match(/^rgba?\(\s*[\d.]+\s*,\s*[\d.]+\s*,\s*[\d.]+(?:\s*,\s*([\d.]+))?\s*\)$/i)
  if (comma) return comma[1] === undefined ? 1 : Number(comma[1])
  return 1
}

function palette() {
  return {
    ocean: cssVar('--ss-globe-ocean', '#d0e6fa'),
    land: cssVar('--ss-globe-land', '#eef6ff'),
    landStroke: cssVar('--ss-globe-land-stroke', 'rgba(96, 140, 184, .36)'),
    sphere: cssVar('--ss-globe-sphere-stroke', 'rgba(96, 140, 184, .45)'),
    graticule: cssVar('--ss-globe-graticule', 'rgba(96, 140, 184, .18)'),
    limb: cssVar('--ss-globe-limb', 'rgba(168, 204, 234, .28)'),
    surface: cssVar('--sz-surface', '#ffffff'),
    online: cssVar('--sz-success', '#16a34a'),
    offline: cssVar('--sz-danger', '#dc2626'),
    mixed: cssVar('--sz-warning', '#d97706'),
  }
}

function reduceMotion() {
  return typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches
}

function coarsePointer() {
  return typeof matchMedia === 'function' && matchMedia('(pointer: coarse)').matches
}

function shouldPulse() {
  return !document.hidden && !reduceMotion() && markers.value.some(marker => marker.status === 'online')
}

function shouldLoop() {
  return shouldPulse()
}

function loRes() {
  return dragging
}

function schedule() {
  if (frame) return
  if (pulse) return
  frame = requestAnimationFrame(() => {
    frame = 0
    render()
    ensurePulse()
  })
}

function ensurePulse() {
  if (pulse || !shouldLoop()) return
  const tick = () => {
    pulse = 0
    render()
    if (shouldLoop()) pulse = requestAnimationFrame(tick)
  }
  pulse = requestAnimationFrame(tick)
}

function stopPulse() {
  if (!pulse) return
  cancelAnimationFrame(pulse)
  pulse = 0
}

function invalidateBase() {
  baseKey = ''
  schedule()
}

function statusColor(status: GlobeMarkerStatus, colors: Palette) {
  if (status === 'offline') return colors.offline
  if (status === 'mixed') return colors.mixed
  return colors.online
}

function markerRadius(count: number) {
  if (count > 4) return 7
  if (count > 1) return 6
  return 5
}

function projectionFor(width: number, height: number, size: number) {
  return geoOrthographic()
    .translate([width / 2, height / 2])
    .scale(size * GLOBE_RATIO * scaleMul)
    .rotate([lambda, phi])
    .clipAngle(90)
    .precision(loRes() ? 1.2 : 0.4)
}

function projectVisible(projection: Projection, lon: number, lat: number) {
  if (!isGlobeFront(lon, lat, lambda, phi)) return null
  const point = projection([lon, lat])
  if (!point) return null
  return { x: point[0], y: point[1] }
}

function drawLand(
  ctx: CanvasRenderingContext2D,
  path: (feature: never) => void,
  features: WorldGeo['features'],
  fill: string,
  stroke: string,
) {
  if (!features.length) return
  ctx.beginPath()
  for (const feature of features) path(feature as never)
  ctx.fillStyle = fill
  ctx.fill()
  ctx.strokeStyle = stroke
  ctx.lineWidth = 0.6
  ctx.stroke()
}

function buildBase(width: number, height: number, size: number, ratio: number, colors: Palette) {
  const canvas = baseLayer || (baseLayer = document.createElement('canvas'))
  const bitmapW = Math.round(width * ratio)
  const bitmapH = Math.round(height * ratio)
  if (canvas.width !== bitmapW || canvas.height !== bitmapH) {
    canvas.width = bitmapW
    canvas.height = bitmapH
  }
  const ctx = canvas.getContext('2d')
  if (!ctx) return
  ctx.setTransform(1, 0, 0, 1, 0, 0)
  ctx.clearRect(0, 0, bitmapW, bitmapH)
  ctx.setTransform(ratio, 0, 0, ratio, 0, 0)
  const path = geoPath(projectionFor(width, height, size), ctx)
  const radius = size * GLOBE_RATIO * scaleMul
  const cx = width / 2
  const cy = height / 2

  ctx.save()
  ctx.beginPath()
  path({ type: 'Sphere' } as never)
  ctx.fillStyle = colors.ocean
  ctx.fill()
  ctx.strokeStyle = colors.sphere
  ctx.lineWidth = 1
  ctx.stroke()
  ctx.clip()

  ctx.beginPath()
  path(geoGraticule()() as never)
  ctx.strokeStyle = colors.graticule
  ctx.lineWidth = 0.45
  ctx.stroke()

  if (world) drawLand(ctx, path, world.features, colors.land, colors.landStroke)

  if (colorAlpha(colors.limb) > 0.01) {
    const limb = ctx.createRadialGradient(cx, cy, radius * 0.62, cx, cy, radius)
    limb.addColorStop(0, withAlpha(colors.limb, 0))
    limb.addColorStop(1, colors.limb)
    ctx.beginPath()
    path({ type: 'Sphere' } as never)
    ctx.fillStyle = limb
    ctx.fill()
  }
  ctx.restore()
}

function drawMarker(
  ctx: CanvasRenderingContext2D,
  marker: GlobeMarker,
  x: number,
  y: number,
  colors: Palette,
  now: number,
) {
  const visual = markerRadius(marker.ids.length)
  const fill = statusColor(marker.status, colors)
  if (marker.status === 'online' && !reduceMotion()) {
    const t = (now / PULSE_MS) % 1
    ctx.beginPath()
    ctx.arc(x, y, visual * (0.9 + t * 0.85), 0, Math.PI * 2)
    ctx.strokeStyle = fill
    ctx.globalAlpha = 0.34 * (1 - t)
    ctx.lineWidth = 1.25
    ctx.stroke()
    ctx.globalAlpha = 1
  }
  ctx.beginPath()
  ctx.arc(x, y, visual, 0, Math.PI * 2)
  ctx.fillStyle = fill
  ctx.fill()
  ctx.lineWidth = 1
  ctx.strokeStyle = colors.surface
  ctx.stroke()
  const hitR = Math.max(coarsePointer() ? 22 : 16, visual + 8)
  hits.push({ marker, x, y, r: hitR })
}

function maxScale(size: number) {
  return Math.max(GLOBE_SCALE_MIN, (size - GLOBE_FIT_PAD) / (size * GLOBE_RATIO * 2))
}

function render() {
  const canvas = canvasRef.value
  const box = boxRef.value
  if (!canvas || !box) return
  const width = Math.max(160, box.clientWidth)
  const height = Math.max(160, box.clientHeight)
  const size = Math.min(width, height)
  scaleMul = Math.min(maxScale(size), Math.max(GLOBE_SCALE_MIN, scaleMul || GLOBE_SCALE_DEFAULT))
  const ratio = Math.min(window.devicePixelRatio || 1, 2)
  const nextW = Math.round(width * ratio)
  const nextH = Math.round(height * ratio)
  if (canvas.width !== nextW || canvas.height !== nextH) {
    canvas.width = nextW
    canvas.height = nextH
  }
  const cssW = `${width}px`
  const cssH = `${height}px`
  if (canvas.style.width !== cssW) canvas.style.width = cssW
  if (canvas.style.height !== cssH) canvas.style.height = cssH
  const ctx = canvas.getContext('2d')
  if (!ctx) return
  const colors = palette()
  const diameter = Math.round(size * GLOBE_RATIO * scaleMul * 2)
  if (diameter !== haloDiameter) {
    haloDiameter = diameter
    box.style.setProperty('--ss-globe-d', `${diameter}px`)
  }

  const key = [
    nextW, nextH, lambda.toFixed(2), phi.toFixed(2), scaleMul.toFixed(3),
    loRes() ? 'drag' : 'still', world ? 'geo' : 'bare',
    colors.ocean, colors.land, colors.limb, colors.landStroke, colors.graticule,
  ].join('|')
  if (key !== baseKey) {
    buildBase(width, height, size, ratio, colors)
    baseKey = key
  }

  ctx.setTransform(1, 0, 0, 1, 0, 0)
  ctx.clearRect(0, 0, nextW, nextH)
  if (baseLayer) ctx.drawImage(baseLayer, 0, 0)
  ctx.setTransform(ratio, 0, 0, ratio, 0, 0)

  const projection = projectionFor(width, height, size)
  const now = performance.now()
  hits = []
  for (const marker of markers.value) {
    const projected = projectVisible(projection, marker.lon, marker.lat)
    if (!projected) continue
    drawMarker(ctx, marker, projected.x, projected.y, colors, now)
  }

  box.dataset.markerCount = String(hits.length)
  box.dataset.hits = JSON.stringify(hits.map(item => ({
    x: Math.round(item.x),
    y: Math.round(item.y),
    id: item.marker.ids[0],
  })))
  box.dataset.ready = ready.value ? '1' : '0'
  syncPinned()
}

function centerView() {
  if (viewed) return
  const mean = sphericalMean(markers.value.map(marker => ({ lon: marker.lon, lat: marker.lat })))
  if (!mean && !ready.value) return
  const view = mean || DEFAULT_VIEW
  lambda = -view.lon
  phi = Math.max(-60, Math.min(60, -view.lat))
  viewed = true
  invalidateBase()
}

function hitTest(x: number, y: number) {
  for (let i = hits.length - 1; i >= 0; i--) {
    const item = hits[i]
    if (!item) continue
    const dx = x - item.x
    const dy = y - item.y
    if (dx * dx + dy * dy <= item.r * item.r) return item
  }
  return null
}

function syncPinned() {
  const current = pinned.value
  if (!current) return
  const match = hits.find(item => item.marker.key === current.marker.key)
  if (!match) {
    pinned.value = null
    return
  }
  const dx = match.x - current.x
  const dy = match.y - current.y
  if (dx * dx + dy * dy > 1 || match.marker !== current.marker) {
    pinned.value = { x: match.x, y: match.y, marker: match.marker }
  }
}

function clearPinned() {
  pinned.value = null
}

function pinMarker(hit: Hit) {
  if (pinned.value?.marker.key === hit.marker.key) {
    pinned.value = null
    return
  }
  tooltip.value = null
  pinned.value = { x: hit.x, y: hit.y, marker: hit.marker }
}

function selectHost(id: number) {
  emit('select', id)
}

function localPoint(event: PointerEvent) {
  const canvas = canvasRef.value
  if (!canvas) return { x: 0, y: 0 }
  const rect = canvas.getBoundingClientRect()
  return { x: event.clientX - rect.left, y: event.clientY - rect.top }
}

function onPointerDown(event: PointerEvent) {
  if (event.button !== 0) return
  dragging = true
  viewed = true
  const point = localPoint(event)
  dragStart = { x: point.x, y: point.y, lambda, phi }
  canvasRef.value?.setPointerCapture(event.pointerId)
}

function onPointerMove(event: PointerEvent) {
  const point = localPoint(event)
  if (dragging && dragStart) {
    const moved = Math.hypot(point.x - dragStart.x, point.y - dragStart.y) > 4
    if (moved) {
      tooltip.value = null
      clearPinned()
    }
    lambda = dragStart.lambda + (point.x - dragStart.x) * 0.35
    phi = Math.max(-80, Math.min(80, dragStart.phi - (point.y - dragStart.y) * 0.25))
    schedule()
    return
  }
  const hit = hitTest(point.x, point.y)
  canvasRef.value?.style.setProperty('cursor', hit ? 'pointer' : 'grab')
  tooltip.value = pinned.value || !hit ? null : { x: hit.x, y: hit.y, marker: hit.marker }
}

function onPointerUp(event: PointerEvent) {
  const point = localPoint(event)
  const moved = dragStart ? Math.hypot(point.x - dragStart.x, point.y - dragStart.y) > 4 : false
  dragging = false
  dragStart = null
  schedule()
  if (moved || event.type === 'pointercancel') {
    if (moved) clearPinned()
    return
  }
  const hit = hitTest(point.x, point.y)
  if (!hit) {
    clearPinned()
    return
  }
  pinMarker(hit)
}

function onPointerLeave() {
  tooltip.value = null
  if (!dragging) canvasRef.value?.style.setProperty('cursor', 'grab')
}

function onWheel(event: WheelEvent) {
  event.preventDefault()
  clearPinned()
  const box = boxRef.value
  const size = box ? Math.min(Math.max(160, box.clientWidth), Math.max(160, box.clientHeight)) : 160
  scaleMul = Math.min(maxScale(size), Math.max(GLOBE_SCALE_MIN, scaleMul * (event.deltaY > 0 ? 0.92 : 1.08)))
  schedule()
}

function onVisibility() {
  if (document.hidden) stopPulse()
  else ensurePulse()
}

async function loadGeo() {
  try {
    const response = await fetch(worldUrl)
    if (response.ok) world = await response.json() as WorldGeo
  }
  catch {
    world = null
  }
  ready.value = true
  centerView()
  invalidateBase()
}

const overlayStyle = computed(() => {
  const tip = pinned.value || tooltip.value
  if (!tip) return {}
  const below = tip.y < 96
  return {
    left: `${tip.x}px`,
    top: `${tip.y}px`,
    transform: below ? 'translate(-50%, 12px)' : 'translate(-50%, calc(-100% - 10px))',
  }
})

function tipTitle(marker: GlobeMarker) {
  return marker.names.length === 1 ? marker.names[0] : marker.label
}

function tipMeta(marker: GlobeMarker) {
  if (marker.names.length <= 1) return ''
  return t('serversHere', { n: marker.names.length })
}

watch(markers, () => {
  centerView()
  schedule()
})

onMounted(() => {
  void loadGeo()
  const canvas = canvasRef.value
  canvas?.addEventListener('wheel', onWheel, { passive: false })
  document.addEventListener('visibilitychange', onVisibility)
  if (boxRef.value && typeof ResizeObserver !== 'undefined') {
    resizeObserver = new ResizeObserver(() => schedule())
    resizeObserver.observe(boxRef.value)
  }
  themeObserver = new MutationObserver(() => invalidateBase())
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class', 'data-theme'] })
  schedule()
})

onBeforeUnmount(() => {
  stopPulse()
  if (frame) cancelAnimationFrame(frame)
  canvasRef.value?.removeEventListener('wheel', onWheel)
  document.removeEventListener('visibilitychange', onVisibility)
  resizeObserver?.disconnect()
  themeObserver?.disconnect()
})
</script>

<template>
  <div ref="boxRef" class="status-globe">
    <div class="status-globe__halo" aria-hidden="true"></div>
    <canvas
      ref="canvasRef"
      class="status-globe__canvas"
      @pointerdown="onPointerDown"
      @pointermove="onPointerMove"
      @pointerup="onPointerUp"
      @pointercancel="onPointerUp"
      @pointerleave="onPointerLeave"
    />
    <AppEmpty
      v-if="ready && markers.length === 0"
      class="status-globe__empty"
      icon="ri-earth-line"
      :description="t(unlocated ? 'unlocatedServers' : 'noData', { n: unlocated })"
    />
    <p v-else-if="unlocated > 0" class="status-globe__note">{{ t('unlocatedServers', { n: unlocated }) }}</p>
    <div v-if="pinned" class="status-globe__hosts" :style="overlayStyle">
      <strong>{{ pinned.marker.label }}</strong>
      <span>{{ t('serversHere', { n: pinned.marker.hosts.length }) }}</span>
      <button
        v-for="host in pinned.marker.hosts"
        :key="host.id"
        type="button"
        class="status-globe__host"
        @click.stop="selectHost(host.id)"
        @pointerdown.stop
      >
        <span class="status-dot" :class="host.online ? 'online' : 'offline'"></span>
        {{ host.name }}
      </button>
    </div>
    <div v-else-if="tooltip" class="status-globe__tip" :style="overlayStyle">
      <strong>{{ tipTitle(tooltip.marker) }}</strong>
      <span v-if="tipMeta(tooltip.marker)">{{ tipMeta(tooltip.marker) }}</span>
    </div>
  </div>
</template>
