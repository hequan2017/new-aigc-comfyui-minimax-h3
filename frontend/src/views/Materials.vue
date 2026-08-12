<template>
  <div class="page fade-up">
    <section class="materials-hero">
      <div class="hero-copy">
        <span class="hero-eyebrow">MATERIAL LIBRARY</span>
        <h1>素材库</h1>
        <p>漫剧分镜生成的画面自动归档于此，也支持手动上传图片 / 视频 / 音频素材，统一管理复用。</p>
        <div class="hero-actions">
          <button class="btn btn-lg" @click="pickFile">
            <span class="plus">＋</span> 上传素材
          </button>
          <input ref="fileInput" type="file" hidden accept="image/*,video/*,audio/*" @change="onPick" />
        </div>
      </div>
      <div class="hero-orb" aria-hidden="true"><span>图</span></div>
    </section>

    <!-- 筛选 -->
    <div class="filter-bar card">
      <div class="chips">
        <button class="chip" :class="{ active: filter.type === '' }" @click="setType('')">全部</button>
        <button class="chip" :class="{ active: filter.type === 'image' }" @click="setType('image')">🖼 图片</button>
        <button class="chip" :class="{ active: filter.type === 'video' }" @click="setType('video')">🎬 视频</button>
        <button class="chip" :class="{ active: filter.type === 'audio' }" @click="setType('audio')">🎵 音频</button>
      </div>
      <select v-model="filter.project_id" class="input select-sm" @change="load">
        <option value="">全部项目</option>
        <option v-for="p in projects" :key="p.id" :value="p.id">{{ p.title }}</option>
      </select>
      <span class="count">{{ materials.length }} 个素材</span>
      <span v-if="uploading" class="uploading"><span class="dot blue pulse"></span>上传中…</span>
    </div>

    <div v-if="loading" class="card empty">加载中…</div>
    <div v-else-if="materials.length === 0" class="card empty">
      <div class="empty-icon">✦</div>
      <div>素材库还是空的，生成分镜画面或点击「上传素材」添加</div>
    </div>

    <!-- 素材网格 -->
    <div v-else class="material-grid">
      <div v-for="m in materials" :key="m.id" class="card material-card">
        <div class="mat-preview" :class="typeTone(m.type)">
          <img v-if="m.type === 'image'" :src="api.materialUrl(m.path)" loading="lazy" alt="素材" @click="viewer = m" />
          <video v-else-if="m.type === 'video'" :src="api.materialUrl(m.path)" controls preload="metadata" muted></video>
          <div v-else class="audio-preview">
            <span class="audio-icon">♪</span>
            <audio :src="api.materialUrl(m.path)" controls preload="none"></audio>
          </div>
          <span class="mat-type-badge" :class="typeTone(m.type)">{{ typeText(m.type) }}</span>
        </div>
        <div class="mat-info">
          <div class="mat-name" :title="m.name">{{ m.name }}</div>
          <div class="mat-meta">
            <span class="tag" :class="m.source === 'scene' ? 'tag-blue' : 'tag-gray'">
              {{ m.source === 'scene' ? '场景生成' : '手动上传' }}
            </span>
            <span v-if="m.project_id" class="mat-project">项目 #{{ m.project_id }}</span>
          </div>
          <p v-if="m.prompt" class="mat-prompt" :title="m.prompt">{{ m.prompt }}</p>
        </div>
        <div class="mat-actions">
          <a class="btn btn-sm btn-ghost" :href="api.materialUrl(m.path) + '?download=1'">下载</a>
          <button class="btn btn-sm btn-danger" @click="remove(m)">删除</button>
        </div>
      </div>
    </div>

    <!-- 预览弹窗 -->
    <div v-if="viewer" class="modal-mask viewer-mask" tabindex="-1" @click.self="viewer = null" @keydown.esc="viewer = null">
      <button class="viewer-close" @click="viewer = null" title="关闭 (Esc)">✕</button>
      <img :src="api.materialUrl(viewer.path)" alt="素材预览" class="viewer-img" @click="viewer = null" />
      <div class="viewer-foot">
        <span class="viewer-name">{{ viewer.name }}</span>
        <a :href="api.materialUrl(viewer.path) + '?download=1'" class="btn btn-sm btn-download" @click.stop>下载</a>
        <button class="btn btn-sm btn-ghost btn-close" @click="viewer = null">关闭</button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { api } from '../api'

const materials = ref([])
const projects = ref([])
const loading = ref(true)
const uploading = ref(false)
const viewer = ref(null)
const fileInput = ref(null)
const filter = reactive({ type: '', project_id: '' })

onMounted(async () => {
  load()
  try {
    const { data } = await api.projects()
    projects.value = data.map((i) => i.project)
  } catch (_) {}
})

async function load() {
  loading.value = true
  try {
    const params = {}
    if (filter.type) params.type = filter.type
    if (filter.project_id) params.project_id = filter.project_id
    const { data } = await api.materials(params)
    materials.value = data
  } catch (_) {
  } finally {
    loading.value = false
  }
}

function setType(t) {
  filter.type = t
  load()
}

function pickFile() {
  fileInput.value?.click()
}

async function onPick(e) {
  const file = e.target.files?.[0]
  e.target.value = ''
  if (!file) return
  const type = file.type.startsWith('image/') ? 'image'
    : file.type.startsWith('video/') ? 'video'
    : file.type.startsWith('audio/') ? 'audio' : 'image'
  uploading.value = true
  try {
    await api.uploadMaterial(file, type, filter.project_id || undefined)
    load()
  } catch (err) {
    alert('上传失败：' + (err.response?.data?.error || err.message))
  } finally {
    uploading.value = false
  }
}

async function remove(m) {
  if (!confirm(`删除素材「${m.name}」？`)) return
  try {
    await api.deleteMaterial(m.id)
    load()
  } catch (err) {
    alert('删除失败：' + (err.response?.data?.error || err.message))
  }
}

function typeText(t) {
  return { image: '图片', video: '视频', audio: '音频' }[t] || t
}
function typeTone(t) {
  return { image: 'tone-blue', video: 'tone-purple', audio: 'tone-green' }[t] || 'tone-gray'
}
</script>

<style scoped>
.materials-hero {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 32px;
  padding: 36px 0 8px;
}
.hero-eyebrow {
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 1.5px;
  color: var(--accent);
}
.materials-hero h1 { margin: 8px 0 8px; font-size: 32px; }
.materials-hero p { color: var(--text-secondary); margin: 0 0 20px; font-size: 14.5px; max-width: 560px; }
.plus { font-size: 16px; margin-right: 2px; }
.hero-orb {
  width: 120px;
  height: 120px;
  border-radius: 50%;
  background: radial-gradient(circle at 30% 25%, #40c8e0, #0a84ff 60%, #5e5ce6);
  box-shadow: 0 20px 60px rgba(64, 200, 224, 0.35);
  display: flex;
  align-items: center;
  justify-content: center;
  flex: 0 0 auto;
}
.hero-orb span { color: #fff; font-size: 42px; font-weight: 800; }
.filter-bar { display: flex; align-items: center; gap: 14px; padding: 12px 18px; margin: 22px 0 16px; flex-wrap: wrap; }
.chips { display: flex; gap: 8px; }
.chip {
  border: 1.5px solid var(--border); background: transparent; border-radius: 980px;
  padding: 6px 14px; font-size: 13px; font-weight: 500; cursor: pointer; color: var(--text-secondary);
  transition: all 0.2s;
}
.chip.active { border-color: var(--accent); background: var(--accent-soft); color: var(--accent); font-weight: 600; }
.select-sm { width: 150px; padding: 7px 10px; }
.count { margin-left: auto; font-size: 12.5px; color: var(--text-tertiary); font-weight: 600; }
.uploading { display: inline-flex; align-items: center; gap: 6px; font-size: 12.5px; font-weight: 600; color: var(--accent); }
.material-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr)); gap: 16px; }
.material-card { padding: 14px; display: flex; flex-direction: column; }
.mat-preview {
  position: relative; border-radius: 12px; overflow: hidden; background: rgba(0, 0, 0, 0.05);
  aspect-ratio: 16/10; display: flex; align-items: center; justify-content: center; margin-bottom: 10px;
}
.mat-preview img { width: 100%; height: 100%; object-fit: cover; cursor: zoom-in; display: block; }
.mat-preview video { width: 100%; height: 100%; object-fit: contain; }
.mat-type-badge {
  position: absolute; top: 8px; left: 8px; font-size: 10.5px; font-weight: 700;
  padding: 3px 9px; border-radius: 980px; color: #fff;
}
.tone-blue { background: rgba(10, 132, 255, 0.9); }
.tone-purple { background: rgba(191, 90, 242, 0.9); }
.tone-green { background: rgba(48, 209, 88, 0.9); }
.tone-gray { background: rgba(0, 0, 0, 0.4); }
.audio-preview { display: flex; flex-direction: column; align-items: center; gap: 10px; width: 100%; padding: 12px; }
.audio-icon { font-size: 32px; color: var(--text-tertiary); }
.audio-preview audio { width: 100%; }
.mat-info { flex: 1; min-width: 0; }
.mat-name { font-weight: 700; font-size: 13.5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.mat-meta { display: flex; align-items: center; gap: 8px; margin-top: 6px; }
.tag { font-size: 10.5px; font-weight: 600; padding: 2px 8px; border-radius: 980px; }
.tag-blue { background: var(--accent-soft); color: var(--accent); }
.tag-gray { background: rgba(0, 0, 0, 0.06); color: var(--text-secondary); }
.mat-project { font-size: 11.5px; color: var(--text-tertiary); }
.mat-prompt {
  margin: 8px 0 0; font-size: 11.5px; color: var(--text-tertiary); line-height: 1.5;
  display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden;
}
.mat-actions { display: flex; gap: 8px; margin-top: 12px; }
.modal-mask {
  position: fixed; inset: 0; background: rgba(0, 0, 0, 0.6); backdrop-filter: blur(10px);
  display: flex; align-items: center; justify-content: center; z-index: 200; padding: 24px;
}
.viewer-mask { overflow: hidden; padding: 0; }
.viewer-img {
  max-width: 94vw; max-height: 94vh; border-radius: 12px;
  box-shadow: 0 24px 60px rgba(0, 0, 0, 0.5); cursor: zoom-out; object-fit: contain;
}
.viewer-close {
  position: fixed; top: 18px; right: 18px; width: 38px; height: 38px; border-radius: 50%;
  border: none; background: rgba(0, 0, 0, 0.65); color: #fff; font-size: 16px; cursor: pointer;
  display: flex; align-items: center; justify-content: center; transition: background 0.2s; z-index: 3;
}
.viewer-close:hover { background: var(--red); }
.viewer-foot {
  position: fixed; bottom: 20px; left: 50%; transform: translateX(-50%);
  display: flex; align-items: center; gap: 10px; z-index: 3;
}
.viewer-name { color: #fff; font-size: 13px; max-width: 40vw; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.viewer-foot .btn-download { background: var(--accent); color: #fff; border-color: transparent; }
.viewer-foot .btn-close { background: rgba(255, 255, 255, 0.9); color: #333; border-color: transparent; }
@media (max-width: 780px) {
  .materials-hero { flex-direction: column; align-items: flex-start; }
  .hero-orb { display: none; }
}
</style>
