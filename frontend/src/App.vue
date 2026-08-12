<template>
  <div class="app-shell">
    <header class="nav">
      <div class="nav-inner">
        <router-link to="/" class="brand">
          <span class="brand-icon">C</span>
          <span class="brand-name">Comfy<span class="gradient-text">Studio</span></span>
        </router-link>
        <nav class="nav-links">
          <router-link to="/projects" class="nav-link" active-class="active">漫剧工作台</router-link>
          <router-link to="/skills" class="nav-link" active-class="active">漫剧 Skill</router-link>
          <router-link to="/materials" class="nav-link" active-class="active">素材库</router-link>
          <router-link to="/create" class="nav-link" active-class="active">创建任务</router-link>
          <router-link to="/dashboard" class="nav-link" active-class="active">总览</router-link>
          <router-link to="/tasks" class="nav-link" active-class="active">任务中心</router-link>
          <router-link to="/instances" class="nav-link" active-class="active">实例管理</router-link>
          <router-link to="/settings" class="nav-link" active-class="active">平台设置</router-link>
        </nav>
        <div class="nav-status">
          <button class="nav-theme-btn" @click="toggleTheme" :title="theme === 'light' ? '切换深色' : '切换浅色'">{{ theme === 'light' ? '🌙' : '☀️' }}</button>
          <span v-if="store.connected" class="status-chip">
            <span class="dot green"></span>实时连接
          </span>
          <span v-else class="status-chip">
            <span class="dot gray pulse"></span>连接中
          </span>
          <span class="status-chip" v-if="runningCount > 0">
            <span class="dot blue"></span>{{ runningCount }}/{{ store.instances.length }} 实例运行
          </span>
        </div>
      </div>
    </header>
    <main class="main">
      <router-view />
    </main>
    <div class="toast-wrap" aria-live="polite">
      <transition-group name="toast">
        <div v-for="t in toast.items" :key="t.id" class="toast" :class="'toast-' + t.type" @click="toast.dismiss(t.id)">
          <span class="toast-icon">{{ toastIcon(t.type) }}</span>
          <span class="toast-msg">{{ t.message }}</span>
        </div>
      </transition-group>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, onBeforeUnmount, ref } from 'vue'
import { useAppStore } from './stores/app'
import { useToastStore } from './stores/toast'
import { wsUrl } from './api'
import { taskSuccessNotification } from './utils/taskNotifications'

const store = useAppStore()
const toast = useToastStore()
function toastIcon(type) {
  return { success: '✓', error: '!', info: 'i' }[type] || 'i'
}

// 主题切换（深色/浅色），持久化到 localStorage
const theme = ref(localStorage.getItem('theme') || 'light')
function toggleTheme() {
  theme.value = theme.value === 'light' ? 'dark' : 'light'
  document.documentElement.setAttribute('data-theme', theme.value)
  localStorage.setItem('theme', theme.value)
}
let ws = null
let reconnectTimer = null
const notifiedTaskStates = new Set()

const runningCount = computed(
  () => store.instances.filter((i) => i.status === 'running').length
)

function connect() {
  ws = new WebSocket(wsUrl())
  ws.onopen = () => store.setConnected(true)
  ws.onclose = () => {
    store.setConnected(false)
    reconnectTimer = setTimeout(connect, 3000)
  }
  ws.onmessage = (e) => {
    try {
      const msg = JSON.parse(e.data)
      if (msg.type === 'snapshot') store.setSnapshot(msg.data)
      if (msg.type === 'task_update') {
        store.lastEvent = msg.data
        const message = taskSuccessNotification(msg.data)
        const notificationKey = `${msg.data?.task_id}:${msg.data?.status}`
        if (message && !notifiedTaskStates.has(notificationKey)) {
          notifiedTaskStates.add(notificationKey)
          toast.success(message, 8000)
        }
      }
      if (msg.type === 'project_update') store.lastProjectUpdate = msg.data
    } catch (_) {}
  }
}

onMounted(connect)
onBeforeUnmount(() => {
  if (ws) ws.close()
  if (reconnectTimer) clearTimeout(reconnectTimer)
})
</script>

<style scoped>
.nav {
  position: sticky;
  top: 0;
  z-index: 100;
  background: rgba(22, 22, 23, 0.8);
  backdrop-filter: saturate(180%) blur(20px);
  -webkit-backdrop-filter: saturate(180%) blur(20px);
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
  height: var(--nav-h);
}
[data-theme="light"] .nav {
  background: rgba(255, 255, 255, 0.72);
  border-bottom: 1px solid var(--border);
}
.nav-inner {
  max-width: 1180px;
  margin: 0 auto;
  height: 100%;
  padding: 0 24px;
  display: flex;
  align-items: center;
  gap: 24px;
}
.brand {
  display: flex;
  align-items: center;
  gap: 8px;
  text-decoration: none;
  color: #f5f5f7;
  font-weight: 700;
  font-size: 16px;
}
[data-theme="light"] .brand { color: var(--text); }
.brand-icon {
  width: 26px;
  height: 26px;
  border-radius: 50%;
  background: linear-gradient(180deg, #0071e3, #0077ed);
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 14px;
  font-weight: 800;
}
.nav-links {
  display: flex;
  gap: 2px;
  flex: 1;
}
.nav-link {
  text-decoration: none;
  color: rgba(245, 245, 247, 0.8);
  font-size: 13px;
  font-weight: 400;
  padding: 5px 12px;
  border-radius: 980px;
  transition: all 0.2s;
}
[data-theme="light"] .nav-link { color: var(--text-secondary); }
.nav-link:hover { color: #fff; background: rgba(255, 255, 255, 0.12); }
[data-theme="light"] .nav-link:hover { color: var(--text); background: var(--hover-bg); }
.nav-link.active { color: #fff; font-weight: 600; }
[data-theme="light"] .nav-link.active { color: var(--text); font-weight: 600; }
.nav-status { display: flex; gap: 8px; align-items: center; }
.nav-theme-btn {
  border: none;
  background: rgba(255, 255, 255, 0.12);
  font-size: 15px;
  cursor: pointer;
  padding: 4px 10px;
  border-radius: 980px;
  color: #f5f5f7;
  transition: all 0.2s;
}
[data-theme="light"] .nav-theme-btn { background: var(--chip-bg); color: var(--text-secondary); }
.nav-theme-btn:hover { background: rgba(255, 255, 255, 0.2); }
[data-theme="light"] .nav-theme-btn:hover { background: var(--hover-bg); }
.status-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  font-weight: 600;
  color: rgba(245, 245, 247, 0.85);
  background: rgba(255, 255, 255, 0.1);
  padding: 4px 12px;
  border-radius: 980px;
}
[data-theme="light"] .status-chip { color: var(--text-secondary); background: var(--chip-bg); }
.dot { width: 7px; height: 7px; border-radius: 50%; display: inline-block; }
.dot.green { background: var(--green); }
.dot.gray { background: var(--text-tertiary); }
.dot.blue { background: var(--accent); }
.main { min-height: calc(100vh - var(--nav-h)); }
.toast-wrap { position: fixed; top: calc(var(--nav-h) + 12px); right: 20px; z-index: 9999; display: flex; flex-direction: column; gap: 10px; pointer-events: none; }
.toast { pointer-events: auto; display: flex; align-items: center; gap: 10px; min-width: 240px; max-width: 380px; padding: 12px 16px; border-radius: 14px; background: var(--card-solid); backdrop-filter: blur(20px); -webkit-backdrop-filter: blur(20px); box-shadow: 0 12px 32px rgba(0, 0, 0, 0.16); border: 1px solid var(--border); font-size: 13.5px; color: var(--text); cursor: pointer; }
.toast-icon { width: 22px; height: 22px; border-radius: 50%; display: flex; align-items: center; justify-content: center; color: #fff; font-size: 13px; font-weight: 700; flex: 0 0 auto; }
.toast-success { border-color: rgba(52, 199, 89, 0.3); }
.toast-success .toast-icon { background: var(--green); }
.toast-error { border-color: rgba(255, 69, 58, 0.3); }
.toast-error .toast-icon { background: var(--red); }
.toast-info { border-color: rgba(10, 132, 255, 0.3); }
.toast-info .toast-icon { background: var(--accent); }
.toast-msg { line-height: 1.45; }
.toast-enter-active, .toast-leave-active { transition: all 0.28s cubic-bezier(0.2, 0.8, 0.2, 1); }
.toast-enter-from { opacity: 0; transform: translateX(24px); }
.toast-leave-to { opacity: 0; transform: translateX(24px); }
@media (max-width: 780px) {
  .nav { height: auto; }
  .nav-inner { min-height: var(--nav-h); padding: 0 16px; gap: 14px; flex-wrap: wrap; }
  .brand { margin-right: auto; }
  .nav-status .status-chip:not(:first-child) { display: none; }
  .nav-links { order: 3; width: calc(100% + 32px); flex-basis: calc(100% + 32px); margin: 0 -16px; padding: 0 12px 8px; overflow-x: auto; }
  .nav-link { flex: 0 0 auto; }
}
</style>
