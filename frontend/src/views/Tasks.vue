<template>
  <div class="page fade-up">
    <h1 class="page-title">任务中心</h1>
    <p class="page-sub">所有生成任务与实时进度</p>

    <div class="filter-row">
      <div class="filter-group">
        <button v-for="f in filters" :key="f.value" class="chip"
          :class="{ active: filter === f.value }" @click="filter = f.value; page = 1; load()">
          {{ f.label }}
        </button>
        <select class="tpl-filter" v-model="tplFilter" @change="page = 1; load()">
          <option value="">全部模板</option>
          <option v-for="t in allTemplates" :key="t.id" :value="t.name">{{ t.name }}</option>
        </select>
        <button class="chip" @click="load()">⟳ 刷新</button>
      </div>
      <div class="filter-group">
        <button class="btn btn-sm btn-secondary" :disabled="cancelling" @click="cancelAll">
          {{ cancelling ? '取消中…' : '⏹ 取消全部' }}
        </button>
        <button class="btn btn-sm btn-danger clear-btn" :disabled="clearing || !total" @click="clearAll">
          {{ clearing ? '清空中…' : '清空任务记录' }}
        </button>
      </div>
    </div>

    <div class="card">
      <div v-if="items.length" class="task-table">
        <div class="tr head">
          <div class="td id">任务 ID</div>
          <div class="td">模板</div>
          <div class="td">状态</div>
          <div class="td progress">进度</div>
          <div class="td">GPU</div>
          <div class="td">耗时</div>
          <div class="td actions"></div>
        </div>
        <div v-for="t in items" :key="t.task_id" class="tr" @click="go(t.task_id)">
          <div class="td id mono">{{ shortId(t.task_id) }}</div>
          <div class="td td-tpl">{{ t.template_name }}</div>
          <div class="td"><span class="badge" :class="badgeClass(t.status)">
            <span class="dot" :class="{ pulse: t.status === 'running' }"></span>{{ statusText(t.status) }}
          </span></div>
          <div class="td progress">
            <div class="progress-track">
              <div class="progress-fill" :style="{ width: (t.status === 'success' ? 100 : t.progress) + '%' }"></div>
            </div>
            <span class="pct">{{ t.status === 'success' ? 100 : t.progress.toFixed(0) }}%</span>
          </div>
          <div class="td">{{ t.gpu_index !== null ? `GPU ${t.gpu_index}` : '—' }}</div>
          <div class="td time" :title="`创建于 ${fmtTime(t.created_at)}`">{{ durationText(t, now) }}</div>
          <div class="td actions">
            <button v-if="['pending','queued','running'].includes(t.status)" class="btn btn-sm btn-danger"
              @click.stop="cancel(t)">取消</button>
            <button v-if="t.status === 'queued'" class="btn btn-sm btn-secondary" title="解除 GPU 绑定后按并发与空闲 GPU 重新分配"
              @click.stop="requeue(t)">重新排队</button>
            <button v-else-if="['failed','cancelled'].includes(t.status)" class="btn btn-sm btn-secondary"
              @click.stop="rerun(t)">重试</button>
            <button v-else class="btn btn-sm btn-ghost" @click.stop="go(t.task_id)">查看</button>
          </div>
        </div>
      </div>
      <div v-else class="empty">
        <div class="empty-icon">✦</div>
        <div>暂无任务</div>
      </div>
    </div>

    <div class="pager" v-if="total > pageSize">
      <button class="btn btn-sm btn-secondary" :disabled="page <= 1" @click="page--; load()">← 上一页</button>
      <span class="page-info">第 {{ page }} / {{ Math.ceil(total / pageSize) }} 页 · 共 {{ total }} 条</span>
      <button class="btn btn-sm btn-secondary" :disabled="page * pageSize >= total" @click="page++; load()">下一页 →</button>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { useRouter } from 'vue-router'
import { api, wsUrl } from '../api'

const router = useRouter()
const items = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const filter = ref('')
const tplFilter = ref('')
const allTemplates = ref([])
const clearing = ref(false)
const cancelling = ref(false)
const now = ref(Date.now())
const filters = [
  { label: '全部', value: '' },
  { label: '进行中', value: 'running' },
  { label: '排队中', value: 'queued' },
  { label: '已完成', value: 'success' },
  { label: '失败', value: 'failed' }
]
let ws = null
let clock = null

async function load() {
  const { data } = await api.tasks({
    page: page.value, size: pageSize,
    status: filter.value || undefined,
    template_name: tplFilter.value || undefined
  })
  items.value = data.items
  total.value = data.total
}

function go(id) { router.push(`/tasks/${id}`) }
function shortId(id) { return id.length > 22 ? id.slice(0, 22) + '…' : id }
function fmtTime(t) { return t ? t.replace('T', ' ').slice(0, 19) : '' }
function badgeClass(s) {
  return { pending: 'badge-gray', queued: 'badge-orange', running: 'badge-blue', success: 'badge-green', failed: 'badge-red', cancelled: 'badge-gray' }[s] || 'badge-gray'
}
function statusText(s) {
  return { pending: '待提交', queued: '排队中', running: '生成中', success: '已完成', failed: '失败', cancelled: '已取消' }[s] || s
}
async function cancel(t) {
  if (!confirm(`确定取消任务 ${t.task_id}？`)) return
  await api.cancelTask(t.task_id)
  load()
}

async function cancelAll() {
  if (!confirm('确定取消全部进行中的任务？（排队/生成中的任务将全部停止）')) return
  cancelling.value = true
  try {
    const { data } = await api.cancelAllTasks()
    alert(`已取消 ${data.cancelled} 个任务`)
    await load()
  } catch (err) {
    alert(err.response?.data?.error || err.message)
  } finally {
    cancelling.value = false
  }
}
async function rerun(t) {
  await api.rerunTask(t.task_id)
  load()
}

async function requeue(t) {
  if (!confirm(`确定重新排队任务 ${t.task_id}？将解除当前 GPU 绑定并按并发与空闲 GPU 重新分配。`)) return
  try {
    await api.requeueTask(t.task_id)
    await load()
  } catch (err) {
    alert(err.response?.data?.error || err.message)
  }
}

async function clearAll() {
  if (!confirm('确定清空全部任务记录？此操作不会删除已生成的结果文件，但任务列表无法恢复。')) return
  clearing.value = true
  try {
    await api.clearTasks()
    page.value = 1
    await load()
  } catch (err) {
    alert(err.response?.data?.error || err.message)
  } finally {
    clearing.value = false
  }
}

function durationText(task, timestamp) {
  if (task.status === 'cancelled') return '—'
  const start = Date.parse(task.started_at || task.created_at)
  const end = task.finished_at ? Date.parse(task.finished_at) : timestamp
  if (!Number.isFinite(start) || !Number.isFinite(end)) return '—'
  const seconds = Math.max(0, Math.floor((end - start) / 1000))
  if (seconds < 60) return `${seconds} 秒`
  const minutes = Math.floor(seconds / 60)
  const rest = seconds % 60
  if (minutes < 60) return `${minutes} 分 ${rest} 秒`
  return `${Math.floor(minutes / 60)} 小时 ${minutes % 60} 分`
}

onMounted(() => {
  load()
  api.templates().then(({ data }) => { allTemplates.value = data }).catch(() => {})
  clock = setInterval(() => { now.value = Date.now() }, 1000)
  ws = new WebSocket(wsUrl())
  ws.onmessage = (e) => {
    try {
      const msg = JSON.parse(e.data)
      if (msg.type === 'task_update') {
        const upd = msg.data
        const idx = items.value.findIndex((t) => t.task_id === upd.task_id)
        if (idx >= 0) Object.assign(items.value[idx], upd)
      }
    } catch (_) {}
  }
})
onBeforeUnmount(() => {
  if (ws) ws.close()
  if (clock) clearInterval(clock)
})
</script>

<style scoped>
.filter-row { display: flex; gap: 16px; margin-bottom: 16px; align-items: center; justify-content: space-between; }
.filter-group { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.tpl-filter {
  border: 1px solid var(--border);
  background: #fff;
  border-radius: 980px;
  padding: 6px 16px;
  font-size: 13px;
  font-family: var(--font);
  color: var(--text);
  width: 200px;
  outline: none;
  transition: all 0.2s;
  appearance: none;
  background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='10' height='6' viewBox='0 0 10 6'%3E%3Cpath d='M1 1l4 4 4-4' stroke='%23999' stroke-width='1.5' fill='none' stroke-linecap='round'/%3E%3C/svg%3E");
  background-repeat: no-repeat;
  background-position: right 14px center;
  cursor: pointer;
}
.tpl-filter:focus { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
.chip {
  border: 1px solid var(--border);
  background: #fff;
  border-radius: 980px;
  padding: 6px 16px;
  font-size: 13px;
  cursor: pointer;
  font-family: var(--font);
  color: var(--text-secondary);
  transition: all 0.2s;
}
.chip.active { border-color: var(--accent); color: var(--accent); background: var(--accent-soft); font-weight: 600; }
.task-table { display: flex; flex-direction: column; }
.tr {
  display: grid;
  grid-template-columns: 1fr 1.8fr 0.8fr 1.5fr 0.5fr 1fr 0.8fr;
  gap: 12px;
  align-items: center;
  padding: 13px 10px;
  border-bottom: 1px solid var(--border);
  cursor: pointer;
  border-radius: var(--radius-sm);
  transition: background 0.15s;
}
.tr:hover { background: rgba(0, 0, 0, 0.03); }
.tr:last-child { border-bottom: none; }
.tr.head { cursor: default; font-size: 12px; color: var(--text-tertiary); font-weight: 600; }
.tr.head:hover { background: transparent; }
.td { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 14px; }
.td.td-tpl { white-space: normal; word-break: break-word; line-height: 1.35; }
.td.id { font-weight: 600; }
.td.progress { display: flex; align-items: center; gap: 8px; }
.td.progress .progress-track { flex: 1; }
.pct { font-size: 12px; color: var(--text-secondary); min-width: 34px; text-align: right; }
.td.time { color: var(--text-secondary); font-size: 12px; }
.mono { font-family: monospace; font-size: 12.5px; }
.actions { display: flex; gap: 6px; }
.pager { display: flex; justify-content: center; align-items: center; gap: 16px; margin-top: 20px; }
.page-info { font-size: 13px; color: var(--text-secondary); }
@media (max-width: 760px) {
  .filter-row { align-items: stretch; flex-direction: column; }
  .clear-btn { align-self: flex-start; }
  .card { overflow-x: auto; }
  .task-table { min-width: 900px; }
}
</style>
