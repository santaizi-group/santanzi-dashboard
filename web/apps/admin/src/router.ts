import { createRouter, createWebHistory } from 'vue-router'

export const router = createRouter({
  history: createWebHistory('/admin/'),
  routes: [
    { path: '/', name: 'overview', component: () => import('@/pages/OverviewPage.vue') },
    { path: '/servers', name: 'servers', component: () => import('@/pages/ServersPage.vue') },
    { path: '/services', name: 'services', component: () => import('@/pages/ServicesPage.vue') },
    { path: '/notifications', name: 'notifications', component: () => import('@/pages/NotificationChannelsPage.vue') },
    { path: '/alert-rules', name: 'alert-rules', component: () => import('@/pages/AlertRulesPage.vue') },
    { path: '/ddns', name: 'ddns', component: () => import('@/pages/DDNSPage.vue') },
    { path: '/nat', name: 'nat', component: () => import('@/pages/NATPage.vue') },
    { path: '/bot', name: 'bot', component: () => import('@/pages/BotPage.vue') },
    { path: '/telemetry', name: 'telemetry', component: () => import('@/pages/TelemetryPage.vue') },
    { path: '/connections', name: 'connections', component: () => import('@/pages/ConnectionsPage.vue') },
    { path: '/probes', name: 'probes', component: () => import('@/pages/ProbesPage.vue') },
    { path: '/settings', name: 'settings', component: () => import('@/pages/SettingsPage.vue') },
    { path: '/api-tokens', name: 'api-tokens', component: () => import('@/pages/ApiTokensPage.vue') },
    { path: '/login', name: 'login', component: () => import('@/pages/LoginPage.vue'), meta: { bare: true } },
    { path: '/:pathMatch(.*)*', component: () => import('@/pages/NotFoundPage.vue'), meta: { bare: true } },
  ],
})
