<template>
  <div class="page fade-up">
    <section class="projects-hero">
      <div class="hero-glow" aria-hidden="true"></div>
      <div class="hero-copy">
        <span class="hero-eyebrow"><i></i> AI 漫剧工作台</span>
        <h1>灵感成篇，<br /><span class="hero-gradient">故事成画。</span></h1>
        <p>从剧本、分镜到视频成片，让创作流程集中在一个清晰、高效的工作台。</p>
        <div class="hero-actions">
          <button class="btn btn-lg" @click="openCreate">
            <span class="plus">＋</span> 新建项目
          </button>
          <router-link to="/skills" class="hero-link">查看创作能力 <span>›</span></router-link>
        </div>
      </div>
      <div class="hero-visual" aria-hidden="true">
        <div class="visual-head">
          <span>创作流水线</span>
          <span class="visual-live"><i></i> READY</span>
        </div>
        <div class="pipeline">
          <div class="pipeline-step"><b>01</b><span>剧本</span></div><span class="pipeline-arrow">›</span>
          <div class="pipeline-step"><b>02</b><span>分镜</span></div><span class="pipeline-arrow">›</span>
          <div class="pipeline-step active"><b>03</b><span>视频</span></div><span class="pipeline-arrow">›</span>
          <div class="pipeline-step"><b>04</b><span>成片</span></div>
        </div>
        <div class="scheduler-card">
          <div><small>GPU 智能调度</small><strong>空闲算力优先</strong></div>
          <div class="gpu-nodes"><i v-for="n in 8" :key="n" :class="{ hot: n <= 4 }"></i></div>
        </div>
      </div>
    </section>

    <section class="workspace-summary" aria-label="工作台概览">
      <div class="summary-item"><span>项目总数</span><strong>{{ summary.total }}</strong></div>
      <div class="summary-item"><span>制作中</span><strong class="summary-orange">{{ summary.producing }}</strong></div>
      <div class="summary-item"><span>已完成</span><strong class="summary-green">{{ summary.ready }}</strong></div>
      <div class="summary-item"><span>就绪视频</span><strong>{{ summary.videos }}</strong></div>
    </section>

    <div class="content-heading">
      <div><span class="overline">YOUR STORIES</span><h2>全部项目</h2></div>
      <button class="compact-create" @click="openCreate"><span>＋</span> 新建项目</button>
    </div>

    <div v-if="loading" class="project-grid" aria-label="正在加载项目">
      <div v-for="n in 3" :key="n" class="card project-card skeleton-card">
        <div class="skeleton skeleton-head"></div>
        <div class="skeleton skeleton-line wide"></div>
        <div class="skeleton skeleton-line"></div>
        <div class="skeleton skeleton-stats"></div>
      </div>
    </div>

    <div v-else-if="projects.length === 0" class="card empty">
      <div class="empty-icon">✦</div>
      <h3>开始你的第一部漫剧</h3>
      <p>创建项目后，剧本、分镜、视频和成片都会在这里统一管理。</p>
      <button class="btn" @click="openCreate">＋ 新建项目</button>
    </div>

    <div v-else class="project-grid">
      <article v-for="item in projects" :key="item.project.id" class="card project-card"
        role="link" tabindex="0" :style="{ '--project-progress': progressPct(item) + '%' }"
        @click="openProject(item.project.id)" @keydown.enter="openProject(item.project.id)">
        <div class="card-accent" :class="statusColor(item.project.status)"></div>
        <div class="project-head">
          <span class="project-icon" :class="statusColor(item.project.status)">{{ iconOf(item.project.status) }}</span>
          <div class="project-info">
            <div class="project-title">{{ item.project.title }}</div>
            <div class="project-time">更新于 {{ formatTime(item.project.updated_at) }}</div>
          </div>
          <span class="badge project-status" :class="badgeClass(item.project.status)"><i></i>{{ statusText(item.project.status) }}</span>
        </div>
        <p class="project-synopsis" :class="{ muted: !item.project.synopsis }">{{ item.project.synopsis || '尚未填写故事简介，进入工作台继续完善。' }}</p>
        <div class="project-stats">
          <span class="stat"><b>{{ item.scene_total }}</b><small>场景</small></span>
          <span class="stat"><b>{{ item.scene_videos }}</b><small>视频任务</small></span>
          <span class="stat"><b>{{ item.scene_video_ready }}</b><small>视频就绪</small></span>
        </div>
        <div class="project-progress" v-if="item.scene_videos > 0">
          <div class="progress-meta"><span>视频生成进度</span><b>{{ progressPct(item) }}%</b></div>
          <div class="progress-track">
            <div class="progress-fill" :style="{ width: progressPct(item) + '%' }"></div>
          </div>
        </div>
        <div class="project-foot">
          <span v-if="item.project.style" class="tag">{{ item.project.style }}</span>
          <span v-if="item.project.genre" class="tag tag-gray">{{ item.project.genre }}</span>
          <span class="go" aria-hidden="true"><span>→</span></span>
        </div>
      </article>
    </div>

    <!-- 视频生成完成弹窗 -->
    <div v-if="videoAlert && videoAlert.length" class="modal-mask" @click.self="videoAlert = null">
      <div class="modal card">
        <h2>🎬 视频生成完成</h2>
        <p class="modal-sub">以下项目的场景视频已生成完成，可前往查看</p>
        <div v-for="a in videoAlert" :key="a.project.id" class="alert-row" @click="goProject(a)">
          <div class="alert-info">
            <div class="alert-title">{{ a.project.title }}</div>
            <div class="alert-sub">{{ a.added }} 个新视频就绪 · {{ a.project.updated_at?.slice(0, 16).replace('T', ' ') }}</div>
          </div>
          <span class="go">查看 →</span>
        </div>
        <div class="modal-actions">
          <button class="btn" @click="videoAlert = null">知道了</button>
        </div>
      </div>
    </div>

    <!-- 新建项目跳转独立页面（/projects/new） -->

  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'

const router = useRouter()
const projects = ref([])
const loading = ref(true)
const initialized = ref(false)
const videoAlert = ref([])
const prevReady = new Map()
let pollTimer = null

const summary = computed(() => projects.value.reduce((acc, item) => {
  acc.total++
  if (item.project.status === 'producing') acc.producing++
  if (['ready', 'finished'].includes(item.project.status)) acc.ready++
  acc.videos += item.scene_video_ready || 0
  return acc
}, { total: 0, producing: 0, ready: 0, videos: 0 }))

onMounted(() => {
  load()
  pollTimer = setInterval(load, 5000)
})
onBeforeUnmount(() => clearInterval(pollTimer))

async function load() {
  if (!initialized.value) loading.value = true
  try {
    const { data } = await api.projects()
    const newReady = []
    data.forEach(item => {
      const id = item.project.id
      const ready = item.scene_video_ready || 0
      const prev = prevReady.has(id) ? prevReady.get(id) : -1
      if (prev >= 0 && ready > prev) {
        newReady.push({ project: item.project, added: ready - prev })
      }
      prevReady.set(id, ready)
    })
    projects.value = data
    if (newReady.length) {
      videoAlert.value = newReady
    }
  } catch (_) {
  } finally {
    initialized.value = true
    loading.value = false
  }
}

function goProject(a) {
  videoAlert.value = []
  router.push(`/projects/${a.project.id}`)
}

function openCreate() {
  router.push('/projects/new')
}

function openProject(id) {
  router.push(`/projects/${id}`)
}

function formatTime(value) {
  if (!value) return '刚刚'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false,
  }).format(date)
}

function iconOf(status) {
  return { draft: '✍', script_done: '📜', producing: '🎬', ready: '✔', finished: '✔', failed: '!' }[status] || '✍'
}
function statusColor(status) {
  return { draft: 'icon-gray', script_done: 'icon-blue', producing: 'icon-orange', ready: 'icon-green', finished: 'icon-green', failed: 'icon-red' }[status] || 'icon-gray'
}
function statusText(status) {
  return { draft: '草稿', script_done: '剧本就绪', producing: '制作中', ready: '成片完成', finished: '完成', failed: '异常' }[status] || status
}
function badgeClass(status) {
  return { draft: 'badge-gray', script_done: 'badge-blue', producing: 'badge-orange', ready: 'badge-green', finished: 'badge-green', failed: 'badge-red' }[status] || 'badge-gray'
}
function progressPct(item) {
  if (!item.scene_videos) return 0
  return Math.min(100, Math.round(((item.scene_video_ready || 0) / item.scene_videos) * 100))
}
</script>

<style scoped>
/* ---------- 首页 Hero（对标 apple.com 产品页） ---------- */
.projects-hero {
  position: relative;
  min-height: 420px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 32px;
  overflow: hidden;
  margin: 12px 0 56px;
  padding: 64px;
  border-radius: 28px;
  background: linear-gradient(160deg, #0b0b0f 0%, #16161c 55%, #1f1f28 100%);
  color: #fff;
  box-shadow: 0 30px 90px rgba(0, 0, 0, 0.28);
}
.hero-glow {
  position: absolute;
  inset: 0;
  background:
    radial-gradient(640px 360px at 82% 18%, rgba(0, 113, 227, 0.22), transparent 60%),
    radial-gradient(500px 300px at 92% 82%, rgba(191, 90, 242, 0.12), transparent 55%);
  pointer-events: none;
}
.hero-copy { position: relative; z-index: 2; max-width: 640px; }
.hero-eyebrow {
  display: block;
  color: #2997ff;
  font-size: 13px;
  font-weight: 700;
  letter-spacing: 0.14em;
  text-transform: uppercase;
}
.projects-hero h1 {
  margin-top: 14px;
  font-size: clamp(44px, 6vw, 72px);
  line-height: 1.04;
  letter-spacing: -0.03em;
  font-weight: 700;
}
.hero-gradient {
  background: linear-gradient(180deg, #ffffff 0%, #86868b 100%);
  -webkit-background-clip: text;
  background-clip: text;
  -webkit-text-fill-color: transparent;
  color: transparent;
}
.projects-hero p {
  max-width: 540px;
  margin-top: 20px;
  color: #a1a1a6;
  font-size: 18px;
  line-height: 1.6;
}
.hero-actions { display: flex; align-items: center; gap: 28px; margin-top: 32px; }
.hero-link {
  color: #2997ff;
  text-decoration: none;
  font-size: 17px;
  font-weight: 500;
  cursor: pointer;
}
.hero-link:hover { text-decoration: underline; }
.hero-link span { font-size: 24px; vertical-align: -2px; }
.plus { font-size: 17px; margin-right: 2px; }
/* ---------- 内容标题 ---------- */
.content-heading { display: flex; justify-content: space-between; align-items: flex-end; margin: 0 2px 18px; }
.overline { font-size: 11px; letter-spacing: 1.6px; color: var(--text-tertiary); font-weight: 700; }
.content-heading h2 { margin: 4px 0 0; font-size: 30px; font-weight: 700; letter-spacing: -0.02em; }
/* ---------- 项目卡片 ---------- */
.project-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(310px, 1fr)); gap: 18px; }
.project-card {
  padding: 22px;
  cursor: pointer;
  border-radius: var(--radius-lg);
  transition: transform 0.25s ease, box-shadow 0.25s ease;
}
.project-card:hover { transform: translateY(-4px); box-shadow: var(--shadow-hover); }
.project-head { display: flex; align-items: center; gap: 12px; }
.project-icon {
  width: 44px; height: 44px; border-radius: 12px;
  display: flex; align-items: center; justify-content: center;
  font-size: 20px; color: #fff; flex: 0 0 auto;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.12);
}
.icon-gray { background: #8e8e93; }
.icon-blue { background: var(--accent); }
.icon-orange { background: var(--orange); }
.icon-green { background: var(--green); }
.icon-red { background: var(--red); }
.project-info { flex: 1; min-width: 0; }
.project-title { font-weight: 700; font-size: 16px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.project-time { font-size: 12px; color: var(--text-tertiary); margin-top: 3px; }
.project-synopsis {
  margin: 14px 0;
  font-size: 13px;
  color: var(--text-secondary);
  line-height: 1.65;
  display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden;
}
.project-stats { display: flex; gap: 20px; border-top: 1px solid var(--border); padding-top: 13px; }
.stat { font-size: 12px; color: var(--text-tertiary); }
.stat b { color: var(--text); font-size: 15px; margin-right: 3px; }
.project-progress { margin-top: 12px; }
.project-foot { display: flex; align-items: center; gap: 8px; margin-top: 14px; }
.tag {
  font-size: 11px; font-weight: 600; padding: 4px 11px; border-radius: 980px;
  background: var(--accent-soft); color: var(--accent);
}
.tag-gray { background: rgba(0, 0, 0, 0.06); color: var(--text-secondary); }
.go { margin-left: auto; font-size: 13px; font-weight: 600; color: var(--link-blue); }
.go span { font-size: 17px; vertical-align: -2px; }
.modal-mask {
  position: fixed; inset: 0; background: rgba(0, 0, 0, 0.35);
  backdrop-filter: blur(8px); display: flex; align-items: center; justify-content: center; z-index: 200;
}
.modal { width: min(480px, calc(100vw - 32px)); padding: 28px; animation: pop 0.25s ease; }
.modal h2 { margin: 0 0 4px; }
.modal-sub { margin: 0 0 20px; font-size: 13px; color: var(--text-secondary); }
.field label { display: block; font-size: 13px; font-weight: 600; margin-bottom: 6px; }
.field .req { color: var(--red); margin-left: 4px; }
.field .optional { color: var(--text-tertiary); font-weight: 400; margin-left: 4px; }
.field-row { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; margin-top: 14px; }
.field + .field, .field-row + .field { margin-top: 14px; }
.notice { border-radius: 12px; padding: 10px 14px; font-size: 13px; margin-top: 14px; }
.error-notice { background: rgba(255, 69, 58, 0.1); color: var(--red); }
.modal-actions { display: flex; justify-content: flex-end; gap: 10px; margin-top: 22px; }
.alert-row {
  display: flex; align-items: center; gap: 12px;
  padding: 12px 14px; margin-top: 10px;
  background: var(--accent-soft); border-radius: var(--radius-md);
  cursor: pointer; transition: background 0.2s;
}
.alert-row:hover { background: rgba(10, 132, 255, 0.18); }
.alert-info { flex: 1; min-width: 0; }
.alert-title { font-weight: 700; font-size: 14px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.alert-sub { font-size: 12px; color: var(--text-secondary); margin-top: 2px; }

/* ---------- 漫剧工作台 2.0 ---------- */
.projects-hero {
  min-height: 380px;
  margin-bottom: 24px;
  padding: 56px 60px;
  background: linear-gradient(145deg, #08090c 0%, #111522 52%, #151722 100%);
  border: 1px solid rgba(255, 255, 255, 0.08);
  box-shadow: 0 28px 80px rgba(14, 18, 32, 0.22);
}
.hero-glow {
  background:
    radial-gradient(520px 320px at 84% 20%, rgba(10, 132, 255, 0.25), transparent 66%),
    radial-gradient(440px 280px at 74% 90%, rgba(94, 92, 230, 0.18), transparent 70%);
}
.hero-copy { max-width: 530px; }
.hero-eyebrow { display: inline-flex; align-items: center; gap: 8px; }
.hero-eyebrow i, .visual-live i {
  width: 7px; height: 7px; border-radius: 50%; background: #30d158;
  box-shadow: 0 0 0 5px rgba(48, 209, 88, 0.1);
}
.projects-hero h1 { font-size: clamp(44px, 5vw, 64px); }
.projects-hero p { max-width: 470px; font-size: 17px; }
.hero-actions { margin-top: 28px; }
.hero-visual {
  position: relative;
  z-index: 2;
  flex: 0 1 430px;
  min-width: 360px;
  padding: 22px;
  border: 1px solid rgba(255, 255, 255, 0.11);
  border-radius: 22px;
  background: rgba(255, 255, 255, 0.055);
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.26);
  backdrop-filter: blur(22px);
}
.visual-head { display: flex; justify-content: space-between; color: #f5f5f7; font-size: 13px; font-weight: 650; }
.visual-live { display: inline-flex; align-items: center; gap: 7px; color: #8e8e93; font-size: 10px; letter-spacing: 0.1em; }
.visual-live i { width: 5px; height: 5px; box-shadow: none; }
.pipeline { display: flex; align-items: center; justify-content: space-between; margin: 24px 0; }
.pipeline-step {
  width: 62px; height: 66px; display: flex; flex-direction: column; justify-content: center;
  padding-left: 13px; border-radius: 15px; color: #8e8e93; background: rgba(255, 255, 255, 0.055);
  border: 1px solid rgba(255, 255, 255, 0.07);
}
.pipeline-step b { color: #636366; font-size: 10px; letter-spacing: 0.08em; }
.pipeline-step span { margin-top: 5px; color: #d1d1d6; font-size: 13px; font-weight: 650; }
.pipeline-step.active { background: linear-gradient(145deg, rgba(10, 132, 255, 0.28), rgba(94, 92, 230, 0.2)); border-color: rgba(100, 190, 255, 0.3); }
.pipeline-step.active b, .pipeline-step.active span { color: #fff; }
.pipeline-arrow { color: #48484a; font-size: 20px; }
.scheduler-card {
  display: flex; justify-content: space-between; align-items: center; padding: 14px 15px;
  border-radius: 15px; background: rgba(0, 0, 0, 0.22); border: 1px solid rgba(255, 255, 255, 0.07);
}
.scheduler-card small, .scheduler-card strong { display: block; }
.scheduler-card small { color: #8e8e93; font-size: 10px; }
.scheduler-card strong { margin-top: 2px; color: #f5f5f7; font-size: 13px; }
.gpu-nodes { display: grid; grid-template-columns: repeat(4, 9px); gap: 6px; }
.gpu-nodes i { width: 9px; height: 9px; border-radius: 3px; background: #3a3a3c; }
.gpu-nodes i.hot { background: #0a84ff; box-shadow: 0 0 10px rgba(10, 132, 255, 0.6); }

.workspace-summary {
  display: grid; grid-template-columns: repeat(4, 1fr); margin-bottom: 48px;
  border: 1px solid var(--border); border-radius: 20px; background: var(--card); box-shadow: var(--shadow);
  backdrop-filter: saturate(180%) blur(20px);
}
.summary-item { position: relative; padding: 20px 24px; }
.summary-item + .summary-item::before { content: ''; position: absolute; left: 0; top: 18px; bottom: 18px; width: 1px; background: var(--border); }
.summary-item span { display: block; color: var(--text-tertiary); font-size: 11px; font-weight: 650; letter-spacing: 0.03em; }
.summary-item strong { display: block; margin-top: 4px; font-size: 25px; line-height: 1; letter-spacing: -0.03em; }
.summary-item .summary-orange { color: #d96c00; }
.summary-item .summary-green { color: #16883b; }

.content-heading { margin-bottom: 20px; }
.compact-create {
  display: inline-flex; align-items: center; gap: 4px; padding: 8px 15px; border: 1px solid var(--border);
  border-radius: 980px; background: var(--card); color: var(--text); font: 600 13px var(--font); cursor: pointer;
  transition: border-color .2s, background .2s, transform .2s;
}
.compact-create:hover { border-color: rgba(0, 113, 227, .25); background: var(--accent-soft); }
.compact-create:active { transform: scale(.97); }

.project-grid { grid-template-columns: repeat(auto-fill, minmax(340px, 1fr)); gap: 20px; }
.project-card { position: relative; overflow: hidden; min-height: 300px; padding: 24px; outline: none; }
.project-card:focus-visible { box-shadow: 0 0 0 3px rgba(0, 113, 227, .25), var(--shadow-hover); }
.card-accent { position: absolute; inset: 0 0 auto; height: 3px; border-radius: 0; opacity: .85; }
.project-icon { width: 42px; height: 42px; border-radius: 13px; box-shadow: none; font-size: 17px; }
.project-title { font-size: 17px; letter-spacing: -.015em; }
.project-status { gap: 6px; padding: 4px 9px; }
.project-status i { width: 6px; height: 6px; border-radius: 50%; background: currentColor; }
.project-synopsis { min-height: 44px; margin: 18px 0; font-size: 13px; }
.project-synopsis.muted { color: var(--text-tertiary); }
.project-stats { display: grid; grid-template-columns: repeat(3, 1fr); gap: 0; padding: 15px 0; border-bottom: 1px solid var(--border); }
.stat { display: flex; flex-direction: column; gap: 1px; }
.stat + .stat { padding-left: 16px; border-left: 1px solid var(--border); }
.stat b { margin: 0; font-size: 19px; letter-spacing: -.02em; }
.stat small { color: var(--text-tertiary); font-size: 10px; }
.project-progress { margin-top: 14px; }
.progress-meta { display: flex; justify-content: space-between; margin-bottom: 7px; color: var(--text-tertiary); font-size: 10px; }
.progress-meta b { color: var(--text-secondary); font-size: 11px; }
.project-progress .progress-track { height: 5px; }
.project-foot { min-height: 29px; margin-top: 16px; }
.go { display: grid; place-items: center; width: 29px; height: 29px; border-radius: 50%; background: var(--accent-soft); transition: transform .2s, background .2s; }
.project-card:hover .go { color: #fff; background: var(--accent); transform: translateX(2px); }
.go span { font-size: 15px; line-height: 1; vertical-align: 0; }

.empty { padding: 76px 24px; }
.empty h3 { margin-bottom: 6px; color: var(--text); font-size: 20px; }
.empty p { max-width: 430px; margin: 0 auto 20px; font-size: 13px; }
.skeleton-card { pointer-events: none; }
.skeleton { border-radius: 9px; background: linear-gradient(90deg, var(--hover-bg), rgba(255,255,255,.55), var(--hover-bg)); background-size: 220% 100%; animation: shimmer 1.4s infinite linear; }
.skeleton-head { width: 72%; height: 42px; }
.skeleton-line { width: 64%; height: 12px; margin-top: 14px; }
.skeleton-line.wide { width: 100%; margin-top: 28px; }
.skeleton-stats { height: 58px; margin-top: 25px; }
@keyframes shimmer { to { background-position: -220% 0; } }
@keyframes pop { from { transform: scale(0.94); opacity: 0; } to { transform: scale(1); opacity: 1; } }

@media (max-width: 900px) {
  .projects-hero { min-height: 360px; padding: 44px; }
  .hero-visual { min-width: 310px; padding: 18px; }
  .pipeline-step { width: 54px; padding-left: 10px; }
}
@media (max-width: 640px) {
  .projects-hero { min-height: 0; align-items: flex-start; margin-top: 0; padding: 38px 26px; }
  .projects-hero h1 { font-size: 40px; }
  .projects-hero p { font-size: 15px; }
  .hero-visual { display: none; }
  .hero-actions { align-items: flex-start; flex-direction: column; gap: 15px; }
  .workspace-summary { grid-template-columns: 1fr 1fr; margin-bottom: 38px; }
  .summary-item:nth-child(3)::before { display: none; }
  .summary-item:nth-child(n+3) { border-top: 1px solid var(--border); }
  .project-grid { grid-template-columns: 1fr; }
  .project-card { min-height: 0; }
}
</style>
