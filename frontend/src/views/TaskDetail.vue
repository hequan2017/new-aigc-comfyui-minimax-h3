<template>
  <div class="page fade-up" v-if="task">
    <div class="back-row">
      <router-link to="/tasks" class="back-link">← 返回任务中心</router-link>
    </div>
    <div class="detail-head">
      <div>
        <h1 class="page-title">{{ task.template_name }}</h1>
        <div class="task-meta">
          <span class="mono">{{ task.task_id }}</span>
          <span class="badge" :class="badgeClass(task.status)">
            <span class="dot" :class="{ pulse: task.status === 'running' }"></span>{{ statusText(task.status) }}
          </span>
          <span v-if="task.gpu_index !== null" class="badge badge-blue">GPU {{ task.gpu_index }} · 端口 {{ task.port }}</span>
          <span class="badge badge-gray">总耗时 {{ totalDuration }}</span>
        </div>
      </div>
      <div class="head-actions">
        <button v-if="['pending','queued','running'].includes(task.status)" class="btn btn-danger"
          @click="cancel">取消任务</button>
        <button v-else-if="['failed','cancelled'].includes(task.status)" class="btn"
          @click="rerun">重新生成</button>
        <button v-if="task.status === 'success'" class="btn btn-secondary" @click="download">
          下载视频
        </button>
      </div>
    </div>

    <!-- 生成结果（视频置顶全宽，最显眼）-->
    <div v-if="resultFiles.length" class="card result-card-top">
      <div v-for="(r, i) in resultFiles" :key="i" class="result-item">
        <video v-if="isVideo(r)" :src="fileUrl(r)" controls
          class="result-video-big" @loadedmetadata="onVideoLoaded($event, r)"></video>
        <img v-else-if="r.type === 'images' || r.type === 'gifs'" :src="fileUrl(r)" class="result-image-big" />
        <div v-else class="result-audio"><audio :src="fileUrl(r)" controls></audio></div>
        <div class="result-name">{{ r.filename }}</div>
        <div v-if="mediaInfo[i]" class="media-info">
          <span v-if="mediaInfo[i].width">分辨率 {{ mediaInfo[i].width }} × {{ mediaInfo[i].height }}</span>
          <span v-if="mediaInfo[i].duration">时长 {{ fmtDuration(mediaInfo[i].duration) }}</span>
          <span v-if="mediaInfo[i].size">大小 {{ fmtSize(mediaInfo[i].size) }}</span>
          <span v-if="mediaInfo[i].video_codec">编码 {{ mediaInfo[i].video_codec }}</span>
        </div>
        <div v-else-if="videoMeta[i]" class="media-info">
          <span>分辨率 {{ videoMeta[i].videoWidth }} × {{ videoMeta[i].videoHeight }}</span>
          <span>时长 {{ fmtDuration(videoMeta[i].duration) }}</span>
        </div>
      </div>
    </div>

    <!-- 进度 -->
    <div class="card progress-card" :class="task.status">
      <div class="progress-main">
        <div class="progress-track big">
          <div class="progress-fill" :style="{ width: displayProgress + '%' }"></div>
        </div>
        <div class="progress-info">
          <span class="progress-pct">{{ displayProgress.toFixed(0) }}%</span>
          <span v-if="task.status === 'running' && task.current_node" class="progress-node">
            {{ nodeLabel(task.current_node) }}
          </span>
          <span v-else class="progress-node">{{ statusText(task.status) }}</span>
        </div>
      </div>
      <div v-if="task.error" class="error-box">
        <div class="error-title">执行错误</div>
        <div class="error-msg">{{ task.error }}</div>
      </div>
    </div>

    <!-- 任务参数（最下）-->
    <div class="card">
      <h2 class="section-title">任务参数</h2>
      <div class="params">
        <div class="param-row"><span class="k">提示词</span><span class="v prompt-text">{{ task.prompt || '—' }}</span></div>
        <template v-for="(v, k) in displayParams" :key="k">
          <div class="param-row">
            <span class="k">{{ paramLabel(k) }}</span>
            <span class="v">
              <img v-if="isImgParam(k) && paramFileName(v)" :src="paramImgUrl(v)" class="param-thumb" :title="paramFileName(v)" />
              <span>{{ fmtParam(v) }}</span>
            </span>
          </div>
        </template>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, ref, onMounted, onBeforeUnmount } from 'vue'
import { useRoute } from 'vue-router'
import { api, wsUrl } from '../api'

const route = useRoute()
const task = ref(null)
const params = computed(() => {
  if (!task.value) return {}
  try { return JSON.parse(task.value.params_json) } catch (_) { return {} }
})
// 过滤内部字段与重复字段：prompt 顶部已展示；ref_images/ref_videos/ref_audios 是复数素材数组(与其展开项 ref_xxx_N 重复)故折叠。
// ref_image_N 保留并展示参考图缩略图；ref_video_N / ref_audio_N 仍折叠。
const hiddenParams = new Set(['_task_id', '_files', '_force_port', 'prompt', 'ref_images', 'ref_videos', 'ref_audios'])
const displayParams = computed(() => {
  const out = {}
  for (const [k, v] of Object.entries(params.value)) {
    if (v === null || hiddenParams.has(k)) continue
    if (/^(ref_video|ref_audio)_\d+$/.test(k)) continue
    out[k] = v
  }
  return out
})
const paramLabels = {
  width: '宽度', height: '高度', duration: '时长(秒)', steps: '采样步数', seed: '随机种子',
  cfg: 'CFG', fps: '输出帧率', ref_image_size: '参考图缩放', first_frame: '首帧', last_frame: '尾帧'
}
function paramLabel(k) {
  const m = k.match(/^ref_image_(\d+)$/)
  if (m) {
    const idx = parseInt(m[1])
    return '参考图 ' + (idx + 1) + (idx === 0 ? ' · 分镜画面' : ' · 角色标准像')
  }
  return paramLabels[k] || k
}
// 图片类参数（首帧/尾帧/参考图）显示缩略图
function isImgParam(k) { return /^(first_frame|last_frame|ref_image_\d+)$/.test(k) }
function paramFileName(v) {
  if (!v) return ''
  if (typeof v === 'string') return v
  return v.name || ''
}
function paramImgUrl(v) {
  const name = paramFileName(v)
  const tid = (v && v.task_id) ? v.task_id : (task.value?.task_id || '')
  return api.inputUrl(tid, name)
}
const resultFiles = computed(() => {
  if (!task.value?.result_files) return []
  try { return JSON.parse(task.value.result_files) } catch (_) { return [] }
})
const mediaInfo = ref({})
const videoMeta = ref({})
const displayProgress = computed(() => {
  if (!task.value) return 0
  if (task.value.status === 'success') return 100
  return task.value.progress || 0
})
const now = ref(Date.now())
const totalDuration = computed(() => formatDuration(
  task.value?.created_at,
  task.value?.finished_at || now.value
))
let ws = null
let clock = null

const nodeNames = {
  UNETLoader: '加载模型',
  MiniMaxH3SigmaShift: '模型参数调整',
  CLIPLoader: '加载文本编码器',
  VAELoader: '加载 VAE',
  LoadImage: '加载图片',
  LoadVideo: '加载视频',
  LoadAudio: '加载音频',
  MiniMaxH3ImageToVideo: '视频条件编码',
  MiniMaxH3ReferenceToVideo: '参考条件编码',
  KSampler: '扩散采样',
  VAEDecode: '视频解码',
  VAEDecodeAudio: '音频解码',
  CreateVideo: '合成视频',
  SaveVideo: '保存视频'
}
function nodeLabel(id) { return nodeNames[id] || id }

async function load() {
  const { data } = await api.task(route.params.id)
  task.value = data
  loadMediaInfo()
}

// loadMediaInfo 查询每个结果文件的元数据（分辨率/时长/大小）
async function loadMediaInfo() {
  const files = resultFiles.value
  for (let i = 0; i < files.length; i++) {
    const r = files[i]
    try {
      const { data } = await api.mediaInfo(task.value.gpu_index, (r.subfolder ? r.subfolder + '/' : '') + r.filename)
      mediaInfo.value[i] = data
    } catch (_) { /* 非视频/图片文件或查询失败时忽略 */ }
  }
}

function onVideoLoaded(event, r) {
  const idx = resultFiles.value.indexOf(r)
  if (idx >= 0) {
    videoMeta.value[idx] = {
      videoWidth: event.target.videoWidth,
      videoHeight: event.target.videoHeight,
      duration: event.target.duration
    }
  }
}

function fmtDuration(sec) {
  if (!sec || !isFinite(sec)) return '—'
  const s = Math.round(sec)
  if (s < 60) return `${s} 秒`
  return `${Math.floor(s / 60)} 分 ${s % 60} 秒`
}
function fmtSize(bytes) {
  if (!bytes) return '—'
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

function fileUrl(r) {
  return api.outputUrl(task.value.gpu_index, (r.subfolder ? r.subfolder + '/' : '') + r.filename)
}

function downloadUrl(r) {
  return fileUrl(r) + '?download=1'
}

function isVideo(r) {
  return /\.(mp4|webm|mov|mkv|gif)$/i.test(r.filename)
}

function download() {
  const url = downloadUrl(resultFiles.value[0])
  const a = document.createElement('a')
  a.href = url
  a.download = resultFiles.value[0].filename
  a.click()
}

async function cancel() {
  if (!confirm('确定取消该任务？')) return
  await api.cancelTask(task.value.task_id)
  load()
}
async function rerun() {
  await api.rerunTask(task.value.task_id)
  load()
}

function badgeClass(s) {
  return { pending: 'badge-gray', queued: 'badge-orange', running: 'badge-blue', success: 'badge-green', failed: 'badge-red', cancelled: 'badge-gray' }[s] || 'badge-gray'
}
function statusText(s) {
  return { pending: '待提交', queued: '排队中', running: '生成中', success: '已完成', failed: '失败', cancelled: '已取消' }[s] || s
}
function statusEmoji(s) {
  return { pending: '⏳', queued: '⏱', running: '🎬', success: '✨', failed: '⚠️', cancelled: '✋' }[s] || '✦'
}
function fmtParam(v) {
  if (typeof v === 'object') {
    if (Array.isArray(v)) return v.map((x) => (typeof x === 'object' ? x.name || JSON.stringify(x) : x)).join(', ')
    if (v.name) return v.name
    return JSON.stringify(v)
  }
  return String(v)
}
function formatDuration(startValue, endValue) {
  const start = Date.parse(startValue)
  const end = typeof endValue === 'number' ? endValue : Date.parse(endValue)
  if (!Number.isFinite(start) || !Number.isFinite(end)) return '—'
  const seconds = Math.max(0, Math.floor((end - start) / 1000))
  if (seconds < 60) return `${seconds} 秒`
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分 ${seconds % 60} 秒`
  return `${Math.floor(seconds / 3600)} 小时 ${Math.floor((seconds % 3600) / 60)} 分`
}

onMounted(() => {
  load()
  clock = setInterval(() => { now.value = Date.now() }, 1000)
  ws = new WebSocket(wsUrl())
  ws.onmessage = (e) => {
    try {
      const msg = JSON.parse(e.data)
      if (msg.type === 'task_update' && msg.data.task_id === route.params.id) {
        Object.assign(task.value, msg.data)
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
.back-row { margin-bottom: 14px; }
.back-link { color: var(--accent); text-decoration: none; font-size: 14px; }
.detail-head { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 20px; }
.task-meta { display: flex; align-items: center; gap: 10px; margin-top: 4px; }
.task-meta .mono { font-size: 12px; color: var(--text-tertiary); font-family: monospace; }
.head-actions { display: flex; gap: 10px; }
.progress-card { margin-bottom: 20px; }
.progress-main { padding: 6px 0 2px; }
.progress-track.big { height: 14px; border-radius: 8px; background: rgba(0, 0, 0, 0.06); }
.progress-track.big .progress-fill { border-radius: 8px; background: var(--gradient); position: relative; overflow: hidden; }
/* 运行中：多彩渐变流动 + 光泽扫过 */
.progress-card.running .progress-track.big .progress-fill {
  background: linear-gradient(90deg, #0a84ff, #5e5ce6, #bf5af2, #0a84ff);
  background-size: 300% 100%;
  animation: tdGradFlow 2.4s linear infinite;
}
.progress-card.running .progress-track.big .progress-fill::after {
  content: ''; position: absolute; inset: 0;
  background: linear-gradient(90deg, transparent, rgba(255, 255, 255, 0.5), transparent);
  animation: tdShimmer 1.6s infinite;
}
/* 成功：绿色渐变；失败：红色渐变 */
.progress-card.success .progress-track.big .progress-fill { background: linear-gradient(90deg, #30d158, #34c759); }
.progress-card.failed .progress-track.big .progress-fill { background: linear-gradient(90deg, #ff453a, #ff375f); }
.progress-info { display: flex; align-items: center; gap: 14px; margin-top: 14px; flex-wrap: wrap; }
.progress-pct { font-size: 30px; font-weight: 760; letter-spacing: -.02em; line-height: 1; }
.progress-card.running .progress-pct {
  background: var(--gradient);
  -webkit-background-clip: text; background-clip: text;
  -webkit-text-fill-color: transparent; color: transparent;
}
.progress-node {
  display: inline-flex; align-items: center; gap: 7px;
  color: var(--text-secondary); font-size: 13px; font-weight: 500;
  padding: 5px 13px; border-radius: 980px; background: rgba(0, 0, 0, 0.05);
}
.progress-card.running .progress-node { color: var(--accent); background: var(--accent-soft); }
.progress-card.running .progress-node::before {
  content: ''; width: 7px; height: 7px; border-radius: 50%;
  background: var(--accent); animation: tdPulse 1.4s ease-in-out infinite;
}
@keyframes tdGradFlow { from { background-position: 100% 0; } to { background-position: 0 0; } }
@keyframes tdShimmer { 0% { transform: translateX(-100%); } 100% { transform: translateX(100%); } }
@keyframes tdPulse { 0%, 100% { opacity: 1; transform: scale(1); } 50% { opacity: 0.4; transform: scale(0.8); } }
.error-box {
  margin-top: 14px;
  background: rgba(255, 59, 48, 0.08);
  border: 1px solid rgba(255, 59, 48, 0.2);
  border-radius: var(--radius-md);
  padding: 12px 16px;
}
.error-title { font-weight: 700; color: var(--red); font-size: 13px; margin-bottom: 4px; }
.error-msg { font-size: 13px; color: var(--text-secondary); word-break: break-all; }
.detail-grid { display: grid; grid-template-columns: 1.6fr 1fr; gap: 20px; }
.section-title { font-size: 17px; font-weight: 700; margin-bottom: 16px; }
.result-video { width: 100%; border-radius: var(--radius-md); background: #000; }
.result-image { width: 100%; border-radius: var(--radius-md); }
.result-card-top { margin-bottom: 20px; padding: 18px; }
.result-card-top .result-item { margin-bottom: 0; }
.result-video-big { width: 100%; max-height: 72vh; border-radius: var(--radius-md); background: #000; display: block; }
.result-image-big { width: 100%; max-height: 72vh; object-fit: contain; border-radius: var(--radius-md); }
.result-audio audio { width: 100%; }
.result-item { margin-bottom: 18px; }
.result-name { font-size: 12px; color: var(--text-tertiary); margin-top: 6px; word-break: break-all; }
.media-info {
  display: flex; flex-wrap: wrap; gap: 6px 16px;
  margin-top: 8px; padding: 8px 12px;
  background: rgba(0, 0, 0, 0.04);
  border-radius: var(--radius-sm);
  font-size: 12px; color: var(--text-secondary);
}
.media-info span::before { content: '▸ '; color: var(--accent); }
.params { display: flex; flex-direction: column; }
.param-row {
  display: grid;
  grid-template-columns: 90px 1fr;
  gap: 10px;
  padding: 9px 0;
  border-bottom: 1px solid var(--border);
  font-size: 13.5px;
}
.param-row:last-child { border-bottom: none; }
.k { color: var(--text-secondary); }
.v { word-break: break-all; }
.param-thumb { display: block; width: 100%; max-width: 320px; border-radius: var(--radius-sm); margin-bottom: 6px; }
.prompt-text { white-space: pre-wrap; }
@media (max-width: 760px) {
  .detail-head { gap: 16px; flex-direction: column; }
  .task-meta { align-items: flex-start; flex-wrap: wrap; }
  .detail-grid { grid-template-columns: 1fr; }
}
</style>
