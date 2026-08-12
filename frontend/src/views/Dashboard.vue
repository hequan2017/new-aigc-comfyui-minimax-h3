<template>
  <div class="page fade-up">
    <section class="dashboard-hero">
      <div class="hero-copy">
        <span class="hero-eyebrow">COMFYSTUDIO</span>
        <h1>让每一次生成，<br><span>都快得恰到好处。</span></h1>
        <p>MiniMax H3 全模态视频生成，由 8 张 NVIDIA L40 智能调度。</p>
        <div class="hero-actions">
          <router-link to="/create" class="btn btn-lg">开始创作</router-link>
          <router-link to="/tasks" class="hero-link">查看任务 <span>›</span></router-link>
        </div>
      </div>
      <div class="hero-orb" aria-hidden="true"><span>H3</span></div>
    </section>

    <div class="content-heading">
      <div><span class="overline">实时算力</span><h2>GPU 状态</h2></div>
      <span class="capacity">{{ runningInstances }} / {{ instances.length }} 个实例在线</span>
    </div>

    <!-- GPU 卡片 -->
    <div class="gpu-grid">
      <div v-for="g in gpus" :key="g.index" class="card gpu-card"
        :class="{ 'gpu-busy': g.util > 10 }">
        <div class="gpu-head">
          <span class="gpu-name">{{ g.name }}</span>
          <span class="badge" :class="g.up ? 'badge-green' : 'badge-red'">
            <span class="dot"></span>{{ g.up ? '在线' : '离线' }}
          </span>
        </div>
        <div class="gpu-temp">{{ g.temp }}°C</div>
        <div class="metric-row">
          <div class="metric">
            <div class="metric-label">利用率</div>
            <div class="metric-value">{{ g.util.toFixed(0) }}%</div>
          </div>
          <div class="metric">
            <div class="metric-label">功耗</div>
            <div class="metric-value">{{ g.power.toFixed(0) }}W</div>
          </div>
        </div>
        <div class="progress-track">
          <div class="progress-fill" :style="{ width: g.util + '%' }"></div>
        </div>
        <div class="vram">
          <span>{{ gb(g.vram_used) }} GB</span>
          <span class="vram-total">/ {{ gb(g.vram_total) }} GB</span>
        </div>
        <div v-if="g.processes && g.processes.length" class="gpu-procs">
          <div v-for="p in g.processes.slice(0, 3)" :key="p.pid" class="proc-line">
            <span class="proc-name">{{ p.process }}</span>
            <span>{{ p.mem_mb }}MB</span>
          </div>
        </div>
      </div>
    </div>

    <!-- 快速操作 -->
    <div class="content-heading compact-heading">
      <div><span class="overline">工作台</span><h2>快速访问</h2></div>
    </div>
    <div class="quick-row">
      <router-link to="/create" class="card quick-card">
        <div class="quick-icon create-icon">＋</div>
        <div>
          <div class="quick-title">新建生成任务</div>
          <div class="quick-sub">4 种 MiniMax H3 工作流</div>
        </div>
      </router-link>
      <div class="card quick-card">
        <div class="quick-icon icon-orange">⏱</div>
        <div>
          <div class="quick-title">{{ activeTasks }}</div>
          <div class="quick-sub">进行中的任务</div>
        </div>
      </div>
      <div class="card quick-card">
        <div class="quick-icon icon-teal">🖥</div>
        <div>
          <div class="quick-title">{{ runningInstances }}/{{ instances.length }}</div>
          <div class="quick-sub">运行中实例</div>
        </div>
      </div>
      <div class="card quick-card">
        <div class="quick-icon icon-green">✔</div>
        <div>
          <div class="quick-title">{{ doneTasks }}</div>
          <div class="quick-sub">已完成任务</div>
        </div>
      </div>
    </div>

    <!-- 最近任务 -->
    <div class="card">
      <div class="section-head">
        <h2>最近任务</h2>
        <router-link to="/tasks" class="link-more">查看全部 →</router-link>
      </div>
      <div v-if="recent.length" class="task-list">
        <router-link v-for="t in recent" :key="t.task_id" :to="`/tasks/${t.task_id}`"
          class="task-row">
          <div>
            <div class="task-name">{{ t.template_name }}</div>
            <div class="task-time">{{ t.task_id }}</div>
          </div>
          <div class="task-progress">
            <div v-if="t.status === 'running' || t.status === 'queued'" class="progress-track">
              <div class="progress-fill" :style="{ width: t.progress + '%' }"></div>
            </div>
          </div>
          <span class="badge" :class="badgeClass(t.status)">
            <span class="dot" :class="{ pulse: t.status === 'running' }"></span>{{ statusText(t.status) }}
          </span>
          <span class="task-gpu" v-if="t.gpu_index !== null">GPU {{ t.gpu_index }}</span>
        </router-link>
      </div>
      <div v-else class="empty">
        <div class="empty-icon">✦</div>
        <div>暂无任务，去创建第一个生成任务吧</div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, onBeforeUnmount, ref } from 'vue'
import { api } from '../api'
import { useAppStore } from '../stores/app'

const store = useAppStore()
const gpus = computed(() => store.gpus)
const instances = computed(() => store.instances)
const runningInstances = computed(() => instances.value.filter((i) => i.status === 'running').length)

const recent = ref([])
const activeTasks = ref(0)
const doneTasks = ref(0)

async function load() {
  const [t1, t2, t3] = await Promise.all([
    api.gpus(),
    api.tasks({ page: 1, size: 5 }),
    api.tasks({ page: 1, size: 100 })
  ])
  store.gpus = t1.data
  recent.value = t2.data.items
  activeTasks.value = t3.data.items.filter(
    (t) => ['pending', 'queued', 'running'].includes(t.status)
  ).length
  doneTasks.value = t3.data.items.filter((t) => t.status === 'success').length
}

function gb(v) {
  return (v / 1024 / 1024 / 1024).toFixed(1)
}
function badgeClass(s) {
  return { pending: 'badge-gray', queued: 'badge-orange', running: 'badge-blue', success: 'badge-green', failed: 'badge-red', cancelled: 'badge-gray' }[s] || 'badge-gray'
}
function statusText(s) {
  return { pending: '待提交', queued: '排队中', running: '生成中', success: '已完成', failed: '失败', cancelled: '已取消' }[s] || s
}

onMounted(load)
onBeforeUnmount(() => {})
</script>

<style scoped>
.dashboard-hero {
  position: relative;
  min-height: 390px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 40px;
  overflow: hidden;
  margin: 12px 0 54px;
  padding: 54px 64px;
  border-radius: 34px;
  background: linear-gradient(145deg, #050505 0%, #19191c 55%, #27272b 100%);
  color: #fff;
  box-shadow: 0 30px 80px rgba(0, 0, 0, .2);
}
.dashboard-hero::after { content: ''; position: absolute; inset: 0; background: radial-gradient(circle at 78% 45%, rgba(41,151,255,.2), transparent 34%); pointer-events: none; }
.hero-copy { position: relative; z-index: 2; max-width: 650px; }
.hero-eyebrow, .overline { display: block; color: #2997ff; font-size: 12px; font-weight: 700; letter-spacing: .13em; }
.hero-copy h1 { margin-top: 12px; font-size: clamp(42px, 5vw, 64px); line-height: 1.02; letter-spacing: -.055em; font-weight: 760; }
.hero-copy h1 span { color: #a1a1a6; }
.hero-copy p { max-width: 540px; margin-top: 20px; color: #d2d2d7; font-size: 18px; line-height: 1.55; }
.hero-actions { display: flex; align-items: center; gap: 24px; margin-top: 30px; }
.hero-link { color: #2997ff; text-decoration: none; font-size: 16px; }.hero-link span { font-size: 23px; vertical-align: -1px; }
.hero-orb { position: relative; z-index: 1; flex: 0 0 210px; width: 210px; height: 210px; display: grid; place-items: center; border-radius: 50%; background: radial-gradient(circle at 32% 26%, #65bfff 0%, #0a84ff 26%, #bf5af2 56%, #ff375f 84%, #2a0a3a 100%); box-shadow: 0 0 80px rgba(191, 90, 242, .42), inset -20px -25px 50px rgba(0,0,0,.42); }
.hero-orb::after { content: ''; position: absolute; inset: 5px; border: 1px solid rgba(255,255,255,.26); border-radius: inherit; }
.hero-orb span { font-size: 44px; font-weight: 760; letter-spacing: -.06em; text-shadow: 0 4px 18px rgba(0,0,0,.3); }
.content-heading { display: flex; justify-content: space-between; align-items: end; margin: 0 2px 18px; }
.content-heading h2 { margin-top: 3px; font-size: 28px; letter-spacing: -.035em; }
.capacity { color: var(--text-secondary); font-size: 13px; }
.compact-heading { margin-top: 42px; }
.gpu-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: 16px;
  margin-bottom: 24px;
}
.gpu-card { position: relative; overflow: hidden; padding: 20px; background: rgba(255,255,255,.92); }
.gpu-card::before { content: ''; position: absolute; top: 0; left: 20px; right: 20px; height: 2px; background: linear-gradient(90deg, transparent, rgba(0,113,227,.45), transparent); opacity: 0; transition: opacity .25s; }
.gpu-card.gpu-busy::before { opacity: 1; }
.gpu-card.gpu-busy { box-shadow: 0 10px 34px rgba(10, 132, 255, 0.13); }
.gpu-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 10px; }
.gpu-name { font-weight: 700; font-size: 14px; }
.gpu-temp { font-size: 34px; font-weight: 720; letter-spacing: -0.045em; margin-bottom: 10px; }
.metric-row { display: flex; gap: 24px; margin-bottom: 10px; }
.metric-label { font-size: 12px; color: var(--text-secondary); }
.metric-value { font-size: 16px; font-weight: 600; }
.vram { font-size: 12px; color: var(--text-tertiary); margin-top: 8px; }
.vram-total { color: var(--text-tertiary); }
.gpu-procs { margin-top: 10px; border-top: 1px solid var(--border); padding-top: 8px; }
.proc-line { display: flex; justify-content: space-between; font-size: 11px; color: var(--text-secondary); }
.proc-name { max-width: 60%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.quick-row {
  display: grid;
  grid-template-columns: 2fr 1fr 1fr 1fr;
  gap: 16px;
  margin-bottom: 24px;
}
.quick-card {
  display: flex;
  align-items: center;
  gap: 14px;
  text-decoration: none;
  color: var(--text);
  cursor: pointer;
  min-height: 100px;
  background: rgba(255,255,255,.92);
}
.quick-card:hover { box-shadow: var(--shadow-hover); }
.quick-icon {
  width: 44px; height: 44px;
  border-radius: 12px;
  background: var(--accent-soft);
  color: var(--accent);
  display: flex; align-items: center; justify-content: center;
  font-size: 18px;
}
.create-icon { color: #fff; background: var(--gradient); font-size: 25px; box-shadow: 0 4px 12px rgba(10, 132, 255, 0.3); }
.quick-icon.icon-orange { background: rgba(255, 159, 10, 0.14); color: var(--orange); }
.quick-icon.icon-teal { background: rgba(64, 200, 224, 0.16); color: #0a8fa8; }
.quick-icon.icon-green { background: rgba(48, 209, 88, 0.15); color: #1a7f37; }
.quick-title { font-weight: 700; font-size: 16px; }
.quick-sub { font-size: 12px; color: var(--text-secondary); }

.section-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 14px;
}
.section-head h2 { font-size: 20px; font-weight: 700; letter-spacing: -0.01em; }
.link-more { color: var(--accent); text-decoration: none; font-size: 14px; font-weight: 500; }

.task-list { display: flex; flex-direction: column; }
.task-row {
  display: grid;
  grid-template-columns: 1.4fr 1.6fr auto auto;
  align-items: center;
  gap: 16px;
  padding: 12px 8px;
  text-decoration: none;
  color: var(--text);
  border-bottom: 1px solid var(--border);
  transition: background 0.2s;
  border-radius: var(--radius-sm);
}
.task-row:hover { background: rgba(0, 0, 0, 0.03); }
.task-row:last-child { border-bottom: none; }
.task-name { font-weight: 600; font-size: 14px; }
.task-time { font-size: 12px; color: var(--text-tertiary); font-family: monospace; }
.task-progress { min-width: 120px; }
.task-gpu { font-size: 12px; color: var(--text-secondary); background: rgba(0,0,0,0.05); padding: 3px 10px; border-radius: 980px; }

@media (max-width: 900px) {
  .dashboard-hero { min-height: 350px; padding: 42px; }
  .hero-orb { flex-basis: 160px; width: 160px; height: 160px; }
  .quick-row { grid-template-columns: 1fr 1fr; }
}
@media (max-width: 640px) {
  .dashboard-hero { min-height: 430px; align-items: flex-start; margin-top: 0; padding: 36px 26px; border-radius: 26px; }
  .hero-copy h1 { font-size: 40px; }
  .hero-copy p { font-size: 16px; }
  .hero-orb { position: absolute; right: -38px; bottom: -42px; width: 170px; height: 170px; }
  .content-heading { align-items: flex-start; flex-direction: column; gap: 7px; }
  .gpu-grid, .quick-row { grid-template-columns: 1fr; }
  .task-row { grid-template-columns: 1fr auto; }
  .task-progress, .task-gpu { display: none; }
}
</style>
