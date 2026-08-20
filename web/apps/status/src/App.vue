<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, provide, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import zhTw from 'element-plus/es/locale/lang/zh-tw'
import en from 'element-plus/es/locale/lang/en'
import es from 'element-plus/es/locale/lang/es'
import { STATUS_STORE_KEY } from '@santaizi/status-core'
import { useStatusStore } from './stores/status'
import {
  getPublicThemeDefinition,
  activePublicTheme,
  normalizePublicTheme,
  readStoredPublicTheme,
  resolvePublicTheme,
  setActivePublicTheme,
  writeStoredPublicTheme,
  type PublicThemeId,
} from './publicThemes'
import { applyFaviconHref, resolveFaviconHref } from './domain/favicon'

const { locale } = useI18n()
const elementLocale = computed(() => ({ 'zh-CN': zhCn, 'zh-TW': zhTw, 'en-US': en, 'es-ES': es }[locale.value] || zhCn))
const route = useRoute()
const router = useRouter()
const store = useStatusStore()
provide(STATUS_STORE_KEY, store)

const publicTheme = activePublicTheme
const activeDefinition = computed(() => getPublicThemeDefinition(publicTheme.value))
const colorMode = ref(localStorage.getItem('santaizi-status-theme') || 'system')
const actualColorMode = computed(() => (
  colorMode.value === 'system'
    ? (matchMedia('(prefers-color-scheme:dark)').matches ? 'dark' : 'light')
    : colorMode.value
))
const allowThemeSwitch = computed(() => store.bootstrap?.allow_frontend_theme_switch !== false)

function toggleColorMode() {
  colorMode.value = actualColorMode.value === 'dark' ? 'light' : 'dark'
  localStorage.setItem('santaizi-status-theme', colorMode.value)
}

function setLocale(value: string) {
  locale.value = value
  localStorage.setItem('santaizi-locale', value)
}

async function setPublicTheme(theme: PublicThemeId) {
  const next = normalizePublicTheme(theme)
  if (!allowThemeSwitch.value && next !== normalizePublicTheme(store.bootstrap?.theme)) return
  writeStoredPublicTheme(next)
  setActivePublicTheme(next)
  if (!getPublicThemeDefinition(next).Detail && route.name === 'public-detail') {
    await router.replace({ name: 'home' })
  }
}

watch(actualColorMode, (value) => {
  document.documentElement.dataset.theme = value
  document.documentElement.classList.toggle('dark', value === 'dark')
  document.documentElement.style.colorScheme = value === 'dark' ? 'dark' : 'light'
}, { immediate: true })

watch(() => store.bootstrap, (value) => {
  if (!value) return
  if (value.primary_color) document.documentElement.style.setProperty('--ss-accent', value.primary_color)
  if (value.brand) document.title = value.brand
  applyFaviconHref(resolveFaviconHref(value.logo_url))
  let style = document.querySelector<HTMLStyleElement>('#santaizi-site-style')
  if (!style) {
    style = document.createElement('style')
    style.id = 'santaizi-site-style'
    document.head.append(style)
  }
  style.textContent = value.custom_css || ''
  const resolved = resolvePublicTheme({
    siteTheme: value.theme,
    allowSwitch: value.allow_frontend_theme_switch !== false,
    stored: readStoredPublicTheme(),
  })
  setActivePublicTheme(resolved)
}, { deep: true })

onMounted(async () => {
  await store.load()
  if (store.bootstrap?.requires_view_password && !store.bootstrap.view_password_verified) {
    if (route.path !== '/view-password') await router.replace('/view-password')
    return
  }
  store.connect()
})

onBeforeUnmount(store.stop)
</script>

<template>
  <el-config-provider :locale="elementLocale">
    <component
      :is="activeDefinition.Shell"
      :key="publicTheme"
      :public-theme="publicTheme"
      :allow-theme-switch="allowThemeSwitch"
      :actual-color-mode="actualColorMode"
      @select-theme="setPublicTheme"
      @select-locale="setLocale"
      @toggle-color="toggleColorMode"
    >
      <RouterView :key="`${publicTheme}:${route.fullPath}`" />
    </component>
  </el-config-provider>
</template>
