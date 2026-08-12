<template>
  <div class="page fade-up">
    <div class="inst-head">
      <div>
        <h1 class="page-title">实例管理</h1>
        <p class="page-sub">ComfyUI 多实例启停 · 每张 GPU 对应一个独立实例（端口 8188-8195）</p>
      </div>
      <div class="inst-actions">
        <span class="conc-label" title="并发由平台设置「视频生成并发数」控制">并发数 <strong class="conc-val">{{ concurrency }}</strong></span>
        <button class="btn btn-lg" @click="startAll" :disabled="busy">启动全部</button>
        <button class="btn btn-lg btn-secondary" @click="stopAll" :disabled="busy">停止全部</button>
        <button class="btn btn-lg btn-secondary" @click="restartAll" :disabled="busy">重启全部</button>
      </div>
    </div>

    <!-- 宿主机 CPU/内存 -->
    <div class="card host-card" v-if="host.collected">
      <div class="host-title">宿主机资源（{{ host.collected_at }}）</div>
      <div class="host-stats">
        <div class="stat">
          <div class="stat-v">{{ host.cpu_util != null ? host.cpu_util.toFixed(1) + '%' : '—' }}</div>
          <div class="stat-k">CPU 使用率</div>
        </div>
        <div class="stat">
          <div class="stat-v">{{ host.mem_pct != null ? host.mem_pct.toFixed(1) + '%' : '—' }}</div>
          <div class="stat-k">内存使用率</div>
        </div>
        <div class="stat">
          <div class="stat-v">{{ gb(host.mem_used) }} / {{ gb(host.mem_total) }}</div>
          <div class="stat-k">内存 已用 / 总量</div>
        </div>
        <div class="stat">
          <div class="stat-v">{{ host.load_avg || '—' }}</div>
          <div class="stat-k">负载 (1/5/15min)</div>
        </div>
        <div class="stat">
          <div class="stat-v">{{ host.up_seconds ? upTime(host.up_seconds) : '—' }}</div>
          <div class="stat-k">运行时长</div>
        </div>
      </div>
    </div>

    <!-- 批量操作进度弹窗 -->
    <div v-if="batchOp" class="modal-mask" @click.self="closeBatch">
      <div class="modal card batch-modal">
        <div class="batch-head">
          <h2>{{ batchOpTitle }}</h2>
          <button class="batch-close" @click="closeBatch">✕</button>
        </div>
        <p class="modal-sub">{{ batchDone }}/{{ batchTotal }} 完成 · {{ batchOpHint }}</p>
        <div class="progress-track big">
          <div class="progress-fill" :style="{ width: batchPct + '%' }"></div>
        </div>
        <div class="batch-list">
          <div v-for="inst in batchRows" :key="inst.gpu_index" class="batch-row"
            :class="{ ok: batchReached(inst), err: inst.status === 'error' }">
            <span class="batch-gpu">GPU {{ inst.gpu_index }}</span>
            <span class="batch-state">{{ instText(inst.status) }}</span>
            <span class="batch-check">{{ batchReached(inst) ? '✓' : (inst.status === 'error' ? '!' : '…') }}</span>
          </div>
        </div>
        <div class="modal-actions">
          <button class="btn btn-ghost" @click="closeBatch">后台执行</button>
        </div>
      </div>
    </div>

    <div class="inst-grid">
      <div v-for="inst in sortedInstances" :key="inst.id" class="card inst-card"
        :class="{ 'inst-on': inst.status === 'running' }">
        <div class="inst-top">
          <div class="inst-gpu">
            <span class="inst-chip">GPU {{ inst.gpu_index }}</span>
            <span class="inst-port">端口 {{ inst.port }}</span>
          </div>
          <span class="badge" :class="instBadge(inst.status)">
            <span class="dot" :class="{ pulse: inst.status === 'running' }"></span>{{ instText(inst.status) }}
          </span>
        </div>

        <div class="inst-stats">
          <div class="stat">
            <div class="stat-v">{{ inst.pid || '—' }}</div>
            <div class="stat-k">PID</div>
          </div>
          <div class="stat">
            <div class="stat-v">{{ inst.queue_len }}</div>
            <div class="stat-k">队列任务</div>
          </div>
          <div class="stat">
            <div class="stat-v">{{ gb(inst.vram_free) }}</div>
            <div class="stat-k">显存空闲</div>
          </div>
          <div class="stat">
            <div class="stat-v">{{ inst.temp ? inst.temp.toFixed(0) + '°' : '—' }}</div>
            <div class="stat-k">温度</div>
          </div>
        </div>

        <div class="inst-bar">
          <div class="progress-track">
            <div class="progress-fill" :style="{ width: Math.min(100, utilPct(inst)) + '%' }"></div>
          </div>
          <span class="bar-label">显存占用 {{ utilPct(inst).toFixed(0) }}%</span>
        </div>

        <div class="inst-actions-row">
          <button v-if="inst.status === 'running'" class="btn btn-sm btn-secondary"
            @click="restart(inst)" :disabled="busy">重启</button>
          <button v-if="inst.status === 'running'" class="btn btn-sm btn-danger"
            @click="stop(inst)" :disabled="busy">停止</button>
          <button v-else class="btn btn-sm" @click="start(inst)" :disabled="busy">启动</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { api } from '../api'
import { useAppStore } from '../stores/app'

const store = useAppStore()
const busy = ref(false)
const batchOp = ref(null)
const host = ref({})
let batchTimer = null

const batchOpTitle = computed(() => ({ start: '启动全部', stop: '停止全部', restart: '重启全部' }[batchOp.value] || ''))
const batchTarget = computed(() => ({ start: 'running', stop: 'stopped', restart: 'running' }[batchOp.value] || ''))
const batchRows = computed(() => sortedInstances.value)
const batchTotal = computed(() => batchRows.value.length || 8)
const batchDone = computed(() => batchRows.value.filter(i => batchReached(i)).length)
const batchPct = computed(() => Math.round((batchDone.value / batchTotal.value) * 100))
const batchOpHint = computed(() => batchOp.value === 'restart' ? '先停后启 · 并发由平台设置「视频生成并发数」控制，约需数分钟' : (batchOp.value === 'start' ? '启动中 · 并发由平台设置「视频生成并发数」控制，约需 1-3 分钟' : '停止中 · 并发由平台设置「视频生成并发数」控制'))
function batchReached(inst) {
  return batchTarget.value && inst.status === batchTarget.value
}

function openBatch(op) {
  batchOp.value = op
  closeBatchKeepPoll()
  batchTimer = setInterval(reload, 2000)
  reload()
}
function closeBatch() {
  batchOp.value = null
  if (batchTimer) { clearInterval(batchTimer); batchTimer = null }
}
function closeBatchKeepPoll() {
  if (batchTimer) { clearInterval(batchTimer); batchTimer = null }
}

const sortedInstances = computed(() => {
  const list = [...store.instances].sort((a, b) => a.gpu_index - b.gpu_index)
  return list
})

function gb(v) { return v ? (v / 1024 / 1024 / 1024).toFixed(1) + ' GB' : '—' }
function upTime(s) {
  const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60)
  return (d > 0 ? d + '天 ' : '') + h + '小时 ' + m + '分'
}
function utilPct(inst) {
  if (!inst.vram_total) return 0
  return ((inst.vram_total - inst.vram_free) / inst.vram_total) * 100
}
function instBadge(s) {
  return { running: 'badge-green', starting: 'badge-orange', stopped: 'badge-gray', error: 'badge-red' }[s] || 'badge-gray'
}
function instText(s) {
  return { running: '运行中', starting: '启动中', stopped: '已停止', error: '异常' }[s] || s
}

async function reload() {
  const { data } = await api.instances()
  store.instances = data
  try {
    const g = await api.gpus()
    if (g.data && g.data.host) host.value = g.data.host
  } catch (_) {}
}
async function run(fn) {
  busy.value = true
  try {
    await fn()
    setTimeout(reload, 1500)
  } catch (e) {
    alert(e.response?.data?.error || e.message)
  } finally {
    busy.value = false
  }
}
const start = (inst) => run(() => api.startInstance(inst.gpu_index))
const stop = (inst) => run(() => api.stopInstance(inst.gpu_index))
const restart = (inst) => run(() => api.restartInstance(inst.gpu_index))
const concurrency = ref(4)
const startAll = () => {
  openBatch('start')
  run(() => api.startAll())
}
const stopAll = () => {
  if (!confirm('确定停止全部 ComfyUI 实例？正在执行的任务会被中断！')) return
  openBatch('stop')
  run(() => api.stopAll())
}
const restartAll = () => {
  if (!confirm('确定重启全部 ComfyUI 实例？正在执行的任务会被中断，重启约需数分钟。')) return
  openBatch('restart')
  run(() => api.restartAll())
}

onMounted(async () => {
  reload()
  try {
    const { data } = await api.settings()
    concurrency.value = Number(data.video_concurrency) || 4
  } catch (_) {}
})
onBeforeUnmount(() => { if (batchTimer) clearInterval(batchTimer) })
</script>

<style scoped>
.inst-head { display: flex; justify-content: space-between; align-items: flex-start; }
.inst-actions { display: flex; gap: 12px; align-items: center; flex-wrap: wrap; }
.host-card { margin-bottom: 16px; padding: 16px 20px; }
.host-title { font-size: 12px; color: var(--text-tertiary); margin-bottom: 12px; }
.host-stats { display: grid; grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); gap: 12px; }
.conc-label { display: flex; align-items: center; gap: 6px; font-size: 13px; color: var(--text-secondary); }
.conc-val { font-size: 15px; font-weight: 700; color: var(--accent); }
.inst-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr)); gap: 16px; }
.inst-card { padding: 20px; }
.inst-card.inst-on { box-shadow: 0 4px 24px rgba(52, 199, 89, 0.14); }
.inst-top { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
.inst-gpu { display: flex; align-items: center; gap: 10px; }
.inst-chip {
  background: var(--gradient);
  color: #fff;
  font-weight: 700;
  font-size: 13px;
  padding: 5px 12px;
  border-radius: 8px;
}
.inst-port { font-size: 12px; color: var(--text-secondary); font-family: monospace; }
.inst-stats { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; margin-bottom: 16px; }
.stat { background: rgba(0, 0, 0, 0.03); border-radius: var(--radius-md); padding: 10px 12px; }
.stat-v { font-size: 17px; font-weight: 700; }
.stat-k { font-size: 11px; color: var(--text-tertiary); margin-top: 2px; }
.inst-bar { margin-bottom: 16px; }
.bar-label { display: block; font-size: 11px; color: var(--text-tertiary); margin-top: 6px; }
.inst-actions-row { display: flex; gap: 8px; }

/* 批量操作进度弹窗 */
.modal-mask {
  position: fixed; inset: 0; background: rgba(0, 0, 0, 0.35);
  backdrop-filter: blur(8px); display: flex; align-items: center; justify-content: center; z-index: 200;
}
.modal { width: min(440px, calc(100vw - 32px)); padding: 26px; animation: pop 0.25s ease; }
.modal h2 { margin: 0; font-size: 20px; }
.modal-sub { margin: 6px 0 16px; font-size: 13px; color: var(--text-secondary); }
.batch-head { display: flex; justify-content: space-between; align-items: center; }
.batch-close { border: none; background: transparent; color: var(--text-tertiary); font-size: 16px; cursor: pointer; padding: 4px 8px; border-radius: 8px; }
.batch-close:hover { background: var(--hover-bg); color: var(--text); }
.batch-modal .progress-track.big { height: 8px; border-radius: 4px; }
.batch-modal .progress-fill { transition: width 0.5s ease; }
.batch-list { display: flex; flex-direction: column; gap: 8px; margin-top: 16px; max-height: 320px; overflow-y: auto; }
.batch-row {
  display: flex; align-items: center; gap: 10px;
  padding: 10px 14px; border-radius: var(--radius-md);
  background: rgba(0, 0, 0, 0.035); font-size: 13px;
}
.batch-row.ok { background: rgba(48, 209, 88, 0.1); }
.batch-row.err { background: rgba(255, 69, 58, 0.1); }
.batch-gpu { font-weight: 700; width: 64px; flex: 0 0 auto; }
.batch-state { color: var(--text-secondary); flex: 1; }
.batch-check { font-weight: 800; color: var(--text-tertiary); }
.batch-row.ok .batch-check { color: var(--green); }
.batch-row.err .batch-check { color: var(--red); }
.modal-actions { display: flex; justify-content: flex-end; gap: 10px; margin-top: 20px; }
@keyframes pop { from { transform: scale(0.94); opacity: 0; } to { transform: scale(1); opacity: 1; } }
</style>
