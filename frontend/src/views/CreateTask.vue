<template>
  <div class="create-page fade-up">
    <section class="hero">
      <div class="eyebrow">MINIMAX H3 · COMFYUI</div>
      <h1>从一个想法，生成有声影像。</h1>
      <p>选择工作流，加入参考素材，然后交给最合适的 GPU。</p>
    </section>

    <section class="flow-section">
      <div class="section-heading">
        <div><span class="step-num">1</span></div>
        <div>
          <h2>选择工作模板</h2>
          <p>已适配 4 种 MiniMax H3 工作流</p>
        </div>
      </div>

      <div v-if="loading" class="template-skeletons">
        <div v-for="i in 4" :key="i" class="template-skeleton"></div>
      </div>
      <div v-else-if="loadError" class="notice error-notice">
        <span>{{ loadError }}</span>
        <button class="btn btn-sm" @click="loadTemplates">重新加载</button>
      </div>
      <div v-else class="tpl-grid">
        <button v-for="t in templates" :key="t.id" type="button" class="tpl-card"
          :class="{ selected: selectedTpl?.id === t.id }"
          :aria-pressed="selectedTpl?.id === t.id" @click="selectTemplate(t)">
          <span class="tpl-top">
            <span class="tpl-icon" :class="tplTheme(t.code)" aria-hidden="true">{{ templateIcon(t.code) }}</span>
            <span v-if="selectedTpl?.id === t.id" class="tpl-check">✓</span>
          </span>
          <span class="tpl-name">{{ t.name }}</span>
          <span class="tpl-desc">{{ t.description }}</span>
          <span class="tpl-meta">
            <span>{{ materialLabel(t) }}</span>
            <span>最高 2K</span>
            <span>原生音频</span>
          </span>
        </button>
      </div>
    </section>

    <section v-if="selectedTpl" class="flow-section fade-up">
      <div class="section-heading">
        <div><span class="step-num">2</span></div>
        <div>
          <h2>准备素材</h2>
          <p>{{ fileInputs.length ? '支持点击或拖拽上传，素材会按模板自动编排' : '此模板无需上传文件' }}</p>
        </div>
      </div>

      <div v-if="fileInputs.length" class="upload-grid">
        <article v-for="def in fileInputs" :key="def.key" class="upload-box">
          <div class="upload-head">
            <div>
              <strong>{{ def.label }}</strong>
              <span class="upload-count">{{ fileLists[def.key]?.length || 0 }}/{{ def.max_count || 1 }}</span>
            </div>
            <span class="badge" :class="def.required ? 'badge-red' : 'badge-gray'">
              {{ def.required ? '必填' : '可选' }}
            </span>
          </div>
          <div v-for="(f, idx) in fileLists[def.key]" :key="`${f.name}-${idx}`" class="file-chip">
            <span class="file-icon">{{ fileIcon(def.type) }}</span>
            <span class="file-name">{{ f.name }}</span>
            <button type="button" class="file-x" :aria-label="`移除 ${f.name}`" @click="removeFile(def, idx)">×</button>
          </div>
          <button v-if="(fileLists[def.key]?.length || 0) < (def.max_count || 1)" type="button"
            class="drop-zone" :class="{ uploading: uploadingKey === def.key }"
            @click="inputEls[def.key]?.click()" @dragover.prevent @drop.prevent="(e) => onDrop(e, def)">
            <input :ref="(el) => setInputEl(def.key, el)" type="file" :accept="acceptOf(def.type)"
              :multiple="isMultiple(def)" hidden @change="(e) => onPick(e, def)" />
            <span class="drop-symbol">{{ uploadingKey === def.key ? '•••' : '＋' }}</span>
            <span>{{ uploadingKey === def.key ? '正在上传' : '添加素材' }}</span>
            <small>{{ uploadHint(def) }}</small>
          </button>
        </article>
      </div>
      <div v-else class="no-material">
        <span class="no-material-icon">Aa</span>
        <div><strong>只需提示词</strong><p>无需图片、视频或音频输入。</p></div>
      </div>
    </section>

    <section v-if="selectedTpl" class="flow-section fade-up">
      <div class="section-heading">
        <div><span class="step-num">3</span></div>
        <div><h2>调整生成参数</h2><p>模板默认值已经过优化，可直接开始生成</p></div>
      </div>

      <div class="config-card">
        <div v-if="promptDef" class="prompt-field">
          <label :for="promptDef.key">
            {{ promptDef.label }}
            <span v-if="promptDef.required" class="req">必填</span>
            <span v-else class="optional">可选</span>
          </label>
          <textarea :id="promptDef.key" v-model="form[promptDef.key]" class="textarea" rows="4"
            :placeholder="promptDef.help || '描述画面、动作、镜头、风格与声音…'" />
        </div>

        <div v-if="widthDef && heightDef" class="config-group resolution-group">
          <div class="group-label">画面尺寸</div>
          <div class="res-presets">
            <button v-for="p in resPresets" :key="p.label" type="button" class="chip"
              :class="{ active: form.width === p.w && form.height === p.h }"
              @click="setRes(p.w, p.h)">{{ p.label }}</button>
          </div>
          <div class="res-inputs">
            <input type="number" v-model.number="form.width" class="input" :min="widthDef.min" :max="widthDef.max" :step="widthDef.step || 1" />
            <span>×</span>
            <input type="number" v-model.number="form.height" class="input" :min="heightDef.min" :max="heightDef.max" :step="heightDef.step || 1" />
          </div>
        </div>

        <div class="param-grid">
          <div v-for="def in numberInputs" :key="def.key" class="field">
            <label :for="def.key">{{ def.label }}</label>
            <input :id="def.key" type="number" :step="def.step || 1" :min="def.min" :max="def.max"
              v-model.number="form[def.key]" class="input" />
            <div v-if="def.key === 'duration'" class="field-hint">约 {{ frameCount }} 帧 @ {{ form.fps || 24 }}fps</div>
            <div v-else-if="def.help" class="field-hint">{{ def.help }}</div>
          </div>
          <div v-for="def in selectInputs" :key="def.key" class="field">
            <label :for="def.key">{{ def.label }}</label>
            <select :id="def.key" v-model="form[def.key]" class="input">
              <option v-for="opt in def.options" :key="opt" :value="opt">{{ opt }}</option>
            </select>
          </div>
        </div>

        <div v-if="submitError" class="notice error-notice submit-error">{{ submitError }}</div>
        <div class="submit-row">
          <div class="submit-info"><span class="live-dot"></span>自动选择队列最短、可用显存最多的 GPU</div>
          <button type="button" class="btn btn-lg" :disabled="!!(submitting || uploadingKey)" @click="submit">
            {{ submitting ? '正在创建…' : '开始生成' }}
          </button>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup>
import { computed, reactive, ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'

const router = useRouter()
const templates = ref([])
const selectedTpl = ref(null)
const form = reactive({})
const fileLists = reactive({})
const inputEls = reactive({})
const loading = ref(true)
const loadError = ref('')
const submitError = ref('')
const submitting = ref(false)
const uploadingKey = ref('')

const resPresets = [
  { label: '480P', w: 832, h: 480 },
  { label: '720P', w: 1280, h: 704 },
  { label: '1080P', w: 1920, h: 1088 },
  { label: '2K', w: 2560, h: 1440 },
  { label: '竖屏', w: 768, h: 1344 },
  { label: '方形', w: 1024, h: 1024 }
]

const parsedInputs = computed(() => {
  if (!selectedTpl.value) return []
  try { return JSON.parse(selectedTpl.value.inputs_json) } catch (_) { return [] }
})
const promptDef = computed(() => parsedInputs.value.find((d) => d.type === 'prompt'))
const fileInputs = computed(() => parsedInputs.value.filter((d) => ['image', 'images', 'video', 'videos', 'audio', 'audios'].includes(d.type)))
const widthDef = computed(() => parsedInputs.value.find((d) => d.key === 'width'))
const heightDef = computed(() => parsedInputs.value.find((d) => d.key === 'height'))
const numberInputs = computed(() => parsedInputs.value.filter((d) => ['int', 'float'].includes(d.type) && !['width', 'height'].includes(d.key)))
const selectInputs = computed(() => parsedInputs.value.filter((d) => d.type === 'select'))
const frameCount = computed(() => Math.round((form.duration || 5) * (form.fps || 24)))

function templateIcon(code) {
  if (code.includes('first_last')) return '⇄'
  if (code.includes('ref2v')) return '◎'
  if (code.includes('t2v')) return 'Aa'
  return '◫'
}
// 每个工作流对应一种多彩渐变主题，让模板卡片一眼可区分
function tplTheme(code) {
  if (code.includes('t2v')) return 'theme-purple'
  if (code.includes('ref2v')) return 'theme-teal'
  if (code.includes('first_last')) return 'theme-sunset'
  return 'theme-blue'
}
function materialLabel(t) {
  if (t.code.includes('first_last')) return '2 张图片'
  if (t.code.includes('ref2v')) return '多模态参考'
  if (t.code.includes('t2v')) return '无需素材'
  return '1 张图片'
}
function fileIcon(type) {
  const normalized = type.replace(/s$/, '')
  return { image: '◫', video: '▶', audio: '♪' }[normalized] || '•'
}
function acceptOf(type) {
  return { image: 'image/*', video: 'video/*', audio: 'audio/*' }[type.replace(/s$/, '')] || ''
}
function isMultiple(def) { return def.type.endsWith('s') || (def.max_count || 1) > 1 }
function uploadHint(def) {
  const kind = { image: '图片', video: '视频', audio: '音频' }[def.type.replace(/s$/, '')] || '文件'
  return isMultiple(def) ? `最多 ${def.max_count} 个${kind}` : `选择 1 个${kind}`
}

function resetRecord(record) {
  for (const key of Object.keys(record)) delete record[key]
}
function selectTemplate(template) {
  selectedTpl.value = template
  submitError.value = ''
  resetRecord(form)
  resetRecord(fileLists)
  for (const def of parsedInputs.value) {
    if (def.default !== undefined) form[def.key] = def.default
    if (['image', 'images', 'video', 'videos', 'audio', 'audios'].includes(def.type)) fileLists[def.key] = []
  }
}
function setRes(width, height) { form.width = width; form.height = height }
function setInputEl(key, el) { if (el) inputEls[key] = el }

async function onPick(event, def) {
  const files = [...(event.target.files || [])]
  event.target.value = ''
  await uploadFiles(def, files)
}
async function onDrop(event, def) { await uploadFiles(def, [...event.dataTransfer.files]) }
async function uploadFiles(def, files) {
  const remaining = (def.max_count || 1) - (fileLists[def.key]?.length || 0)
  const selected = files.slice(0, remaining)
  if (!selected.length) return
  uploadingKey.value = def.key
  submitError.value = ''
  try {
    const type = def.type.replace(/s$/, '')
    for (const file of selected) {
      if (file.type && !file.type.startsWith(`${type}/`)) throw new Error(`${file.name} 不是有效的${uploadHint({ ...def, max_count: 1 })}`)
      const { data } = await api.upload(file, type, 'draft')
      fileLists[def.key].push({ name: data.name, task_id: data.task_id })
    }
  } catch (err) {
    submitError.value = `素材上传失败：${err.response?.data?.error || err.message}`
  } finally {
    uploadingKey.value = ''
  }
}
function removeFile(def, index) { fileLists[def.key].splice(index, 1) }

function validate() {
  for (const def of parsedInputs.value) {
    if (def.type === 'prompt' && def.required && !String(form[def.key] || '').trim()) return `请输入${def.label}`
    if (fileInputs.value.includes(def) && def.required && !(fileLists[def.key]?.length)) return `请上传${def.label}`
    if (['int', 'float'].includes(def.type)) {
      const value = Number(form[def.key])
      if (!Number.isFinite(value)) return `请输入有效的${def.label}`
      if (def.min !== undefined && value < def.min) return `${def.label}不能小于 ${def.min}`
      if (def.max !== undefined && value > def.max) return `${def.label}不能大于 ${def.max}`
      // 尺寸类参数须对齐 step（视频模型要求），否则 ComfyUI 会拒绝
      if (def.step && Number.isInteger(def.step) && Number.isInteger(value) && value % def.step !== 0) {
        return `${def.label}须为 ${def.step} 的倍数`
      }
    }
  }
  return ''
}

async function submit() {
  submitError.value = validate()
  if (submitError.value) return
  const files = {}
  for (const def of fileInputs.value) if (fileLists[def.key]?.length) files[def.key] = fileLists[def.key]
  const params = {}
  for (const def of parsedInputs.value) {
    if (!['prompt', 'image', 'images', 'video', 'videos', 'audio', 'audios'].includes(def.type)) params[def.key] = form[def.key]
  }
  submitting.value = true
  try {
    const { data } = await api.createTask({
      template_id: selectedTpl.value.id,
      prompt: String(form[promptDef.value?.key] || '').trim(),
      params,
      files
    })
    router.push(`/tasks/${data.task_id}`)
  } catch (err) {
    submitError.value = `创建失败：${err.response?.data?.error || err.message}`
  } finally {
    submitting.value = false
  }
}

async function loadTemplates() {
  loading.value = true
  loadError.value = ''
  try {
    const { data } = await api.templates()
    templates.value = data.filter((t) => t.code.startsWith('minimax_h3_'))
    if (!templates.value.length) throw new Error('未找到 MiniMax H3 模板')
    selectTemplate(templates.value[0])
  } catch (err) {
    loadError.value = `模板加载失败：${err.response?.data?.error || err.message}`
  } finally {
    loading.value = false
  }
}

onMounted(loadTemplates)
</script>

<style scoped>
.create-page { max-width: 1220px; margin: 0 auto; padding: 0 28px 80px; }
.hero { padding: 72px 0 58px; text-align: center; }
.eyebrow { color: var(--accent); font-size: 13px; font-weight: 700; letter-spacing: .12em; margin-bottom: 14px; }
.hero h1 { font-size: clamp(40px, 6vw, 68px); line-height: 1.04; letter-spacing: -.055em; font-weight: 750; }
.hero p { margin-top: 18px; color: var(--text-secondary); font-size: clamp(17px, 2vw, 21px); }
.flow-section { margin-bottom: 54px; }
.section-heading { display: flex; gap: 14px; align-items: flex-start; margin-bottom: 20px; }
.section-heading h2 { font-size: 26px; line-height: 1.2; letter-spacing: -.025em; }
.section-heading p { color: var(--text-secondary); margin-top: 4px; }
.step-num { display: inline-flex; align-items: center; justify-content: center; width: 30px; height: 30px; border-radius: 50%; background: var(--gradient); color: #fff; font-size: 13px; font-weight: 700; box-shadow: 0 4px 12px rgba(10, 132, 255, 0.3); }
.tpl-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 14px; }
.tpl-card { min-height: 270px; display: flex; flex-direction: column; text-align: left; padding: 24px; border: 1px solid var(--border); border-radius: 24px; background: rgba(255,255,255,.8); color: var(--text); font-family: var(--font); cursor: pointer; box-shadow: var(--shadow); transition: transform .25s ease, box-shadow .25s ease, border-color .25s ease; }
.tpl-card:hover { transform: translateY(-4px); box-shadow: var(--shadow-hover); }
.tpl-card.selected { border-color: rgba(0,113,227,.55); box-shadow: 0 0 0 4px var(--accent-soft), var(--shadow-hover); }
.tpl-top { display: flex; justify-content: space-between; align-items: flex-start; }
.tpl-icon { width: 48px; height: 48px; display: grid; place-items: center; border-radius: 14px; background: var(--grad-blue); color: #fff; font-size: 19px; font-weight: 700; box-shadow: 0 6px 16px rgba(10, 132, 255, 0.25); }
.tpl-icon.theme-purple { background: var(--grad-purple); box-shadow: 0 6px 16px rgba(191, 90, 242, 0.25); }
.tpl-icon.theme-teal { background: var(--grad-teal); box-shadow: 0 6px 16px rgba(64, 200, 224, 0.25); }
.tpl-icon.theme-sunset { background: var(--grad-sunset); box-shadow: 0 6px 16px rgba(255, 159, 10, 0.25); }
.tpl-check { width: 24px; height: 24px; display: grid; place-items: center; border-radius: 50%; background: var(--accent); color: #fff; font-size: 12px; }
.tpl-name { margin-top: 24px; font-size: 18px; font-weight: 700; letter-spacing: -.015em; }
.tpl-desc { margin-top: 8px; color: var(--text-secondary); font-size: 13px; line-height: 1.55; flex: 1; }
.tpl-meta { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 16px; }
.tpl-meta span { padding: 4px 8px; border-radius: 999px; background: rgba(0,0,0,.045); color: var(--text-secondary); font-size: 11px; }
.template-skeletons { display: grid; grid-template-columns: repeat(4, 1fr); gap: 14px; }
.template-skeleton { height: 270px; border-radius: 24px; background: linear-gradient(100deg, #ececef 30%, #f7f7f9 50%, #ececef 70%); background-size: 300% 100%; animation: shimmer 1.6s infinite; }
.upload-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); gap: 14px; }
.upload-box, .no-material, .config-card { border: 1px solid var(--border); background: rgba(255,255,255,.82); backdrop-filter: saturate(180%) blur(24px); border-radius: 24px; box-shadow: var(--shadow); }
.upload-box { padding: 20px; }
.upload-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 14px; }
.upload-head > div { display: flex; gap: 8px; align-items: baseline; }
.upload-count { color: var(--text-tertiary); font-size: 12px; }
.drop-zone { width: 100%; min-height: 112px; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 3px; border: 1px dashed rgba(0,113,227,.34); border-radius: 16px; background: var(--accent-soft); color: var(--accent); font: inherit; cursor: pointer; transition: .2s ease; }
.drop-zone:hover { border-color: var(--accent); background: rgba(0,113,227,.14); }
.drop-zone.uploading { cursor: wait; }
.drop-symbol { font-size: 25px; line-height: 1; }
.drop-zone small { color: var(--text-tertiary); font-size: 11px; }
.file-chip { display: flex; gap: 9px; align-items: center; margin-bottom: 8px; padding: 9px 10px; border-radius: 12px; background: rgba(0,0,0,.04); font-size: 13px; }
.file-icon { color: var(--accent); }
.file-name { min-width: 0; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.file-x { border: 0; background: transparent; color: var(--text-tertiary); font-size: 19px; cursor: pointer; }
.no-material { display: flex; align-items: center; gap: 16px; padding: 22px; }
.no-material-icon { width: 46px; height: 46px; display: grid; place-items: center; border-radius: 14px; color: #fff; background: var(--gradient); font-weight: 700; }
.no-material p { color: var(--text-secondary); margin-top: 2px; }
.config-card { padding: 28px; }
.prompt-field label, .group-label { display: block; font-size: 14px; font-weight: 650; margin-bottom: 9px; }
.req, .optional { margin-left: 6px; font-size: 11px; font-weight: 600; }
.req { color: var(--red); }.optional { color: var(--text-tertiary); }
.config-group { margin-top: 24px; }
.res-presets { display: flex; flex-wrap: wrap; gap: 7px; margin-bottom: 10px; }
.chip { border: 1px solid var(--border); background: #fff; border-radius: 999px; padding: 7px 14px; color: var(--text-secondary); font: inherit; font-size: 12px; cursor: pointer; }
.chip.active { color: #fff; background: var(--gradient); border-color: transparent; box-shadow: 0 3px 10px rgba(10, 132, 255, 0.25); }
.res-inputs { max-width: 420px; display: flex; align-items: center; gap: 9px; color: var(--text-tertiary); }
.res-inputs .input { text-align: center; }
.param-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: 16px; margin-top: 24px; }
.field { margin: 0; }
.field-hint { margin-top: 5px; color: var(--text-tertiary); font-size: 11px; }
.submit-row { display: flex; justify-content: space-between; align-items: center; gap: 20px; margin-top: 28px; padding-top: 22px; border-top: 1px solid var(--border); }
.submit-info { display: flex; align-items: center; gap: 8px; color: var(--text-secondary); font-size: 13px; }
.live-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--green); box-shadow: 0 0 0 4px rgba(52,199,89,.13); }
.notice { display: flex; justify-content: space-between; align-items: center; gap: 16px; padding: 14px 16px; border-radius: 14px; }
.error-notice { background: rgba(255,59,48,.08); border: 1px solid rgba(255,59,48,.18); color: #b42318; }
.submit-error { margin-top: 20px; }
@keyframes shimmer { from { background-position: 100% 0; } to { background-position: 0 0; } }
@media (max-width: 1050px) { .tpl-grid, .template-skeletons { grid-template-columns: repeat(2, 1fr); } .param-grid { grid-template-columns: repeat(2, 1fr); } }
@media (max-width: 640px) { .create-page { padding: 0 16px 56px; } .hero { padding: 48px 0 42px; text-align: left; } .tpl-grid, .template-skeletons, .param-grid { grid-template-columns: 1fr; } .tpl-card { min-height: 230px; } .config-card { padding: 20px; } .submit-row { align-items: stretch; flex-direction: column; } .submit-row .btn { width: 100%; } }
</style>
