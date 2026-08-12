<template>
  <div class="page fade-up editor-page" v-if="project">
    <!-- 顶栏 -->
    <div class="project-head">
      <div>
        <div class="head-links">
          <router-link :to="`/projects/${id()}`" class="back">← 返回项目</router-link>
        </div>
        <h1>🎬 剪辑台 · {{ project.title }}</h1>
        <p class="synopsis">第{{ activeEpN }}集 · 时间轴预览 / 配音 / 字幕 编辑</p>
      </div>
      <div class="head-actions">
        <button class="btn btn-ghost btn-sm" :disabled="epIndex <= 0" @click="switchEp(-1)">← 上一集</button>
        <button class="btn btn-ghost btn-sm" :disabled="epIndex >= epCount - 1" @click="switchEp(1)">下一集 →</button>
        <label class="merge-opt"><input type="checkbox" v-model="mergeSub" />烧录字幕</label>
        <label class="merge-opt"><input type="checkbox" v-model="mergeDub" />保留原声</label>
        <button class="btn btn-lg" :disabled="busy || curMerging || readyVideoCount < 2" @click="mergeEpisode">
          {{ curMerging ? '合并中…' : '⚡ 合并第' + activeEpN + '集成片' }}
        </button>
      </div>
    </div>

    <!-- 时间轴 -->
    <section class="section">
      <div class="section-head">
        <div>
          <span class="overline">TIMELINE</span>
          <h2>时间轴（{{ scenes.length }} 个场景）</h2>
          <p class="sub">点击场景选中，可拖拽调整顺序；右侧面板编辑配音与字幕</p>
        </div>
      </div>
      <div class="card timeline-card">
        <div class="timeline">
          <div v-for="(sc, i) in scenes" :key="sc.id"
            class="tl-clip" :class="{ 'tl-active': selected?.id === sc.id }"
            :draggable="true"
            @dragstart="dragFrom = i" @dragover.prevent @drop="dropScene(i)"
            @click="selectScene(sc)">
            <div class="tl-thumb">
              <video v-if="sc.video_url" :src="sc.video_url" preload="metadata" muted></video>
              <img v-else-if="sc.image_url" :src="sc.image_url" />
              <span v-else class="tl-empty">🎞️</span>
              <span class="tl-dur">{{ fmtDur(sc.video_dur || sc.duration || 5) }}</span>
            </div>
            <div class="tl-info">
              <span class="tl-n">场景 {{ sc.order }}</span>
              <span class="tl-title">{{ sc.title || '未命名' }}</span>
            </div>
            <div class="tl-tools">
              <button class="tl-btn" title="左移" :disabled="i === 0" @click.stop="moveScene(i, -1)">◀</button>
              <button class="tl-btn" title="右移" :disabled="i === scenes.length - 1" @click.stop="moveScene(i, 1)">▶</button>
            </div>
          </div>
          <div v-if="!scenes.length" class="tl-empty-hint">该集暂无场景，请先在项目页生成分镜</div>
        </div>
      </div>
    </section>

    <div class="editor-split" v-if="selected">
      <!-- 左：视频预览 -->
      <section class="section preview-section">
        <div class="section-head">
          <div>
            <span class="overline">PREVIEW</span>
            <h2>场景 {{ selected.order }} · {{ selected.title || '未命名' }}</h2>
            <p class="sub">{{ selected.content }}</p>
          </div>
          <div class="section-actions" v-if="selected.video_url">
            <a class="btn btn-sm btn-ghost" :href="selected.video_url + '?download=1'">下载视频</a>
          </div>
        </div>
        <div class="card preview-card">
          <video v-if="selected.video_url" :src="selected.video_url" controls autoplay class="editor-video"></video>
          <div v-else class="preview-empty">
            <span class="ph-icon">🎞️</span>
            <p>该场景视频未就绪</p>
          </div>
          <div class="duration-edit">
            <label>目标时长</label>
            <input type="number" class="input input-sm" v-model.number="durationInput" min="3" max="15" step="0.5" @change="saveDuration" />
            <span class="dur-unit">秒</span>
            <button class="btn btn-sm btn-secondary" :disabled="durationSaving" @click="saveDuration">{{ durationSaving ? '保存中…' : '保存' }}</button>
          </div>
        </div>
      </section>

      <!-- 右：配音 + 字幕 -->
      <section class="section edit-section">
        <div class="section-head">
          <div>
            <span class="overline">AUDIO & SUBTITLE</span>
            <h2>配音 · 字幕</h2>
          </div>
          <div class="section-actions">
            <button class="btn btn-sm btn-secondary" :disabled="busy || sceneDubs(selected).length === 0" @click="dubScene(selected)">
              生成/重试本场景配音
            </button>
          </div>
        </div>
        <div class="card edit-card">
          <div class="tabs">
            <button class="tab" :class="{ active: tab === 'dub' }" @click="tab = 'dub'">🎙️ 配音</button>
            <button class="tab" :class="{ active: tab === 'sub' }" @click="tab = 'sub'">📝 字幕</button>
          </div>

          <!-- 配音面板 -->
          <div v-if="tab === 'dub'" class="dub-panel">
            <div v-if="!sceneDubs(selected).length" class="panel-empty">
              <p>该场景暂无对白。可在项目页剧本中补充对白后重新生成。</p>
            </div>
            <div v-for="d in sceneDubs(selected)" :key="d.id" class="dub-item" :class="'st-' + d.status">
              <div class="dub-row">
                <span class="dl-char">{{ d.character || '旁白' }}</span>
                <span class="dl-order">#{{ d.order }}</span>
                <span class="dub-state" :class="{ fail: d.status === 'failed' }">
                  {{ d.status === 'ready' ? '✅ 已合成' : d.status === 'synthesizing' ? '合成中…' : d.status === 'failed' ? '失败' : '待合成' }}
                </span>
              </div>
              <textarea v-model="d._text" class="textarea dub-text" rows="2" @blur="saveDub(d)"></textarea>
              <div class="dub-voice-row">
                <select v-model="d._voice" class="input input-sm" @change="saveDub(d)">
                  <option value="">默认（按设置/角色映射）</option>
                  <optgroup label="👩 女声">
                    <option value="Cherry">Cherry · 甜美清澈</option>
                    <option value="Serena">Serena · 温柔成熟</option>
                    <option value="Chelsie">Chelsie · 清爽知性</option>
                  </optgroup>
                  <optgroup label="👨 男声">
                    <option value="Ethan">Ethan · 沉稳磁性</option>
                  </optgroup>
                </select>
              </div>
              <div class="dub-actions">
                <audio v-if="d.status === 'ready' && d.audio_file" :src="d.audio_file" controls preload="none" class="dl-audio"></audio>
                <button class="btn btn-sm btn-secondary" :disabled="busy" @click="redub(d)">🔊 重新合成</button>
              </div>
            </div>
          </div>

          <!-- 字幕面板 -->
          <div v-else class="sub-panel">
            <div v-if="!subtitles.length" class="panel-empty"><p>该集暂无字幕（需要对白）</p></div>
            <div v-for="(s, i) in subtitles" :key="i" class="sub-item" :class="{ 'sub-active': selected && s.scene_id === selected.id }">
              <div class="sub-time">
                <input type="number" class="input input-sm sub-t" step="0.1" :value="round1(s.start)" @change="shiftSub(i, +$event.target.value - s.start)" />
                <span>→</span>
                <input type="number" class="input input-sm sub-t" step="0.1" :value="round1(s.end)" @change="shiftSub(i, +$event.target.value - s.end)" />
              </div>
              <div class="sub-body">
                <span class="dl-char">{{ s.character || '旁白' }}</span>
                <span class="dl-text">{{ s.text }}</span>
              </div>
              <span class="sub-scene">场景{{ s.scene_order }}</span>
            </div>
            <p class="sub-hint">时间轴按配音真实时长自动对齐；拖动上方场景条可调整顺序。字幕仅展示，合并时自动烧录。</p>
          </div>
        </div>
      </section>
    </div>

    <!-- 合并记录 -->
    <section class="section" v-if="merges.length">
      <div class="section-head">
        <div>
          <span class="overline">MERGES</span>
          <h2>合并记录</h2>
        </div>
      </div>
      <div class="card merge-list">
        <div v-for="m in merges" :key="m.id" class="merge-item">
          <div class="merge-info">
            <span class="badge" :class="mergeBadgeClass(m.status)">{{ mergeStatusText(m.status) }}</span>
            <span class="merge-time">第{{ m.episode_n || 1 }}集 · {{ (m.created_at || '').slice(0, 16).replace('T', ' ') }}</span>
            <span v-if="m.error" class="fail-msg">{{ m.error }}</span>
          </div>
          <div v-if="m.status === 'success' && m.output_file" class="merge-result">
            <video :src="outputUrl(m.output_file)" controls preload="metadata" class="merge-video"></video>
            <div class="merge-links">
              <a class="btn btn-sm btn-ghost" :href="outputUrl(m.output_file) + '?download=1'">下载成片</a>
              <a v-if="m.subtitle" class="btn btn-sm btn-ghost" :href="outputUrl(m.output_file.replace(/\.mp4$/, '.srt')) + '?download=1'">下载字幕</a>
            </div>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../api'
import { useToastStore } from '../stores/toast'

const route = useRoute()
const toast = useToastStore()

const project = ref(null)
const scenes = ref([])
const dialogues = ref([])
const subtitles = ref([])
const merges = ref([])
const selected = ref(null)
const tab = ref('dub')
const busy = ref(false)
const curMerging = ref(false)
const mergeSub = ref(true)
const mergeDub = ref(true)
const durationSaving = ref(false)
const durationInput = ref(5)
const dragFrom = ref(null)
const activeEpN = ref(1)
const epIndex = ref(0)
const epCount = ref(1)

function id() { return route.params.id }
function outputUrl(path) { return `/api/output/0/${path}` }
function fmtDur(d) {
  const s = Math.round(Number(d) || 0)
  const mm = String(Math.floor(s / 60)).padStart(2, '0')
  const ss = String(s % 60).padStart(2, '0')
  return `${mm}:${ss}`
}
function round1(v) { return Math.round(Number(v) * 10) / 10 }

function sceneDubs(sc) {
  return dialogues.value.filter(d => d.scene_id === sc.id)
}

function syncDraftTexts() {
  for (const d of dialogues.value) {
    if (d._text === undefined) d._text = d.text
    if (d._voice === undefined) d._voice = d.voice || ''
  }
}

function epNums() {
  const set = new Set(scenes.value.map(s => s.episode_n || 1))
  const arr = [...set].sort((a, b) => a - b)
  epCount.value = Math.max(1, arr.length)
  return arr
}

async function load() {
  try {
    const { data } = await api.editorData(id(), activeEpN.value)
    project.value = data.project
    scenes.value = data.scenes || []
    dialogues.value = (data.dialogues || []).map(d => ({ ...d }))
    subtitles.value = data.subtitles || []
    syncDraftTexts()
    if (!selected.value || !scenes.value.find(s => s.id === selected.value.id)) {
      selected.value = scenes.value.find(s => s.status === 'video_ready') || scenes.value[0] || null
    }
    if (selected.value) durationInput.value = selected.value.duration || 5
    epNums()
    await loadMerges()
  } catch (e) {
    toast.error(e.response?.data?.error || '加载剪辑台失败')
  }
}

async function loadMerges() {
  try {
    const { data } = await api.merges(id())
    merges.value = data
  } catch { /* ignore */ }
}

function selectScene(sc) {
  selected.value = sc
  durationInput.value = sc.duration || 5
  tab.value = 'dub'
}

function moveScene(i, dir) {
  const j = i + dir
  if (j < 0 || j >= scenes.value.length) return
  const arr = scenes.value.map(s => s.id)
  ;[arr[i], arr[j]] = [arr[j], arr[i]]
  const reordered = scenes.value.slice()
  ;[reordered[i], reordered[j]] = [reordered[j], reordered[i]]
  scenes.value = reordered
  persistOrder(arr)
}

function dropScene(j) {
  if (dragFrom.value === null || dragFrom.value === j) { dragFrom.value = null; return }
  const i = dragFrom.value
  dragFrom.value = null
  const arr = scenes.value.map(s => s.id)
  const [moved] = arr.splice(i, 1)
  arr.splice(j, 0, moved)
  const reordered = scenes.value.slice()
  const [it] = reordered.splice(i, 1)
  reordered.splice(j, 0, it)
  scenes.value = reordered
  persistOrder(arr)
}

async function persistOrder(ids) {
  try {
    await api.reorderScenes(id(), { scene_ids: ids })
    toast.show('场景顺序已保存')
    await load()
  } catch (e) {
    toast.error(e.response?.data?.error || '保存顺序失败')
  }
}

async function saveDuration() {
  if (!selected.value) return
  durationSaving.value = true
  try {
    const d = Number(durationInput.value)
    await api.updateSceneDuration(id(), selected.value.id, { duration: d })
    selected.value.duration = d
    toast.show('场景时长已更新')
  } catch (e) {
    toast.error(e.response?.data?.error || '保存时长失败')
  } finally {
    durationSaving.value = false
  }
}

async function saveDub(d) {
  const text = (d._text || '').trim()
  const voice = (d._voice || '').trim()
  if (text === d.text && voice === (d.voice || '')) return
  try {
    await api.updateDialogue(id(), d.id, { text, voice })
    d.text = text
    d.voice = voice
    d.status = 'pending'
    toast.show('对白已更新，请重新合成配音')
  } catch (e) {
    toast.error(e.response?.data?.error || '保存对白失败')
  }
}

async function redub(d) {
  busy.value = true
  try {
    await api.redubDialogue(id(), d.id)
    d.status = 'synthesizing'
    toast.show('配音重新合成已提交')
    setTimeout(load, 3000)
  } catch (e) {
    toast.error(e.response?.data?.error || '合成失败')
  } finally {
    busy.value = false
  }
}

async function dubScene(sc) {
  busy.value = true
  try {
    const { data } = await api.generateSceneDub(id(), sc.id)
    toast.show(data.message || '配音已提交')
    setTimeout(load, 3000)
  } catch (e) {
    toast.error(e.response?.data?.error || '配音失败')
  } finally {
    busy.value = false
  }
}

function shiftSub(i, delta) {
  const s = subtitles.value[i]
  s.start = Math.max(0, round1(s.start + delta))
  s.end = Math.max(s.start + 0.1, round1(s.end + delta))
  toast.show('字幕时间已微调（合并时生效）')
}

const readyVideoCount = computed(() => scenes.value.filter(s => s.status === 'video_ready').length)

async function mergeEpisode() {
  const ids = scenes.value.filter(s => s.status === 'video_ready').map(s => s.id)
  if (ids.length < 2) {
    toast.show('该集至少需要 2 个视频就绪的场景')
    return
  }
  curMerging.value = true
  try {
    await api.mergeScenes(id(), { scene_ids: ids, dub: mergeDub.value, subtitles: mergeSub.value })
    toast.show(`第${activeEpN.value}集合并已启动（配音+字幕）`)
    setTimeout(loadMerges, 3000)
  } catch (e) {
    toast.error(e.response?.data?.error || '合并失败')
  } finally {
    curMerging.value = false
  }
}

function switchEp(dir) {
  const arr = epNums()
  epIndex.value = Math.max(0, Math.min(arr.length - 1, epIndex.value + dir))
  activeEpN.value = arr[epIndex.value] || 1
  selected.value = null
  load()
}

function mergeStatusText(s) { return { pending: '等待中', running: '合并中', success: '成片完成', failed: '失败' }[s] || s }
function mergeBadgeClass(s) { return { pending: 'badge-gray', running: 'badge-orange', success: 'badge-green', failed: 'badge-red' }[s] || 'badge-gray' }

let timer = null
onMounted(() => {
  load()
  timer = setInterval(load, 8000)
})
onUnmounted(() => { clearInterval(timer) })
</script>

<style scoped>
.editor-page { max-width: 1280px; }
.editor-split { display: grid; grid-template-columns: 1.1fr 1fr; gap: 20px; align-items: start; }
@media (max-width: 980px) { .editor-split { grid-template-columns: 1fr; } }

.timeline-card { padding: 16px; overflow-x: auto; }
.timeline { display: flex; gap: 12px; min-width: max-content; }
.tl-clip {
  width: 168px; flex: 0 0 auto; border-radius: 12px; border: 2px solid var(--border);
  overflow: hidden; cursor: pointer; background: var(--card); transition: all 0.2s; position: relative;
}
.tl-clip:hover { border-color: var(--accent); }
.tl-clip.tl-active { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
.tl-thumb { position: relative; aspect-ratio: 16/9; background: #000; }
.tl-thumb video, .tl-thumb img { width: 100%; height: 100%; object-fit: cover; display: block; }
.tl-empty { position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; font-size: 26px; }
.tl-dur {
  position: absolute; right: 6px; bottom: 6px; font-size: 11px; font-weight: 700; color: #fff;
  background: rgba(0, 0, 0, 0.65); padding: 2px 6px; border-radius: 6px;
}
.tl-info { padding: 8px 10px; display: flex; flex-direction: column; gap: 2px; }
.tl-n { font-size: 11px; font-weight: 700; color: var(--accent); }
.tl-title { font-size: 12px; color: var(--text-secondary); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.tl-tools { position: absolute; top: 6px; left: 6px; display: flex; gap: 4px; }
.tl-btn {
  width: 22px; height: 22px; border-radius: 6px; border: none; cursor: pointer; font-size: 10px;
  background: rgba(0, 0, 0, 0.6); color: #fff;
}
.tl-btn:disabled { opacity: 0.35; cursor: default; }
.tl-empty-hint { padding: 40px; color: var(--text-tertiary); }

.preview-card { padding: 16px; }
.editor-video { width: 100%; border-radius: 12px; background: #000; max-height: 420px; }
.preview-empty { height: 220px; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 8px; color: var(--text-tertiary); }
.duration-edit { display: flex; align-items: center; gap: 8px; margin-top: 14px; font-size: 13px; color: var(--text-secondary); }
.dur-unit { color: var(--text-tertiary); }

.edit-card { padding: 0; }
.tabs { display: flex; border-bottom: 1px solid var(--border); }
.tab {
  flex: 1; padding: 12px; border: none; background: transparent; cursor: pointer;
  font-size: 14px; font-weight: 600; color: var(--text-secondary); border-bottom: 2px solid transparent;
}
.tab.active { color: var(--accent); border-bottom-color: var(--accent); }
.dub-panel, .sub-panel { padding: 14px; max-height: 520px; overflow-y: auto; }
.dub-item { border: 1px solid var(--border); border-radius: 12px; padding: 10px; margin-bottom: 10px; }
.dub-row { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
.dub-state { font-size: 11px; padding: 2px 8px; border-radius: 980px; background: var(--accent-soft); color: var(--accent); }
.dub-state.fail { background: rgba(220, 38, 38, 0.1); color: var(--red); }
.dub-text { font-size: 13px; }
.dub-voice-row { margin: 8px 0; }
.dub-actions { display: flex; align-items: center; gap: 8px; }
.dub-actions .dl-audio { flex: 1; }

.sub-item {
  display: flex; align-items: center; gap: 10px; padding: 8px 10px; border-radius: 10px;
  border: 1px solid var(--border); margin-bottom: 8px; font-size: 13px;
}
.sub-item.sub-active { border-color: var(--accent); background: var(--accent-soft); }
.sub-time { display: flex; align-items: center; gap: 4px; flex: 0 0 auto; }
.sub-t { width: 64px; }
.sub-body { flex: 1; min-width: 0; display: flex; gap: 8px; align-items: center; }
.sub-scene { font-size: 11px; color: var(--text-tertiary); flex: 0 0 auto; }
.sub-hint { font-size: 12px; color: var(--text-tertiary); margin-top: 8px; }
.panel-empty { padding: 30px; text-align: center; color: var(--text-tertiary); }
.merge-opt { display: inline-flex; align-items: center; gap: 4px; font-size: 13px; color: var(--text-secondary); cursor: pointer; white-space: nowrap; }
.merge-opt input { width: 14px; height: 14px; cursor: pointer; margin: 0; }
</style>
