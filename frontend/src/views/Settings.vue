<template>
  <div class="page fade-up">
    <div class="settings-hero">
      <div class="settings-hero-icon">⚡</div>
      <div>
        <span class="hero-eyebrow">PLATFORM SETTINGS</span>
        <h1>火山引擎 · 在线模型</h1>
        <p>平台将使用火山引擎 Ark 的<strong>文生文</strong>模型生成剧本、<strong>图生图</strong>模型生成分镜画面（以角色标准人像图为底）；视频生成仍由本地 L40 算力完成。</p>
      </div>
    </div>

    <div class="card settings-card">
      <div class="section-head">
        <div>
          <h2>接口配置</h2>
          <p class="hint">前往 <a href="https://console.volcengine.com/ark" target="_blank" rel="noopener">火山方舟控制台</a> 创建 API Key，并在「开通管理」中开通所需模型</p>
        </div>
        <span class="badge" :class="saved ? 'badge-green' : 'badge-gray'">{{ saved ? '已保存' : '未保存' }}</span>
      </div>

      <div class="form-grid">
        <div class="field span-2">
          <label for="api_key">API Key <span class="req">必填</span></label>
          <div class="key-row">
            <input id="api_key" v-model="form.volc_api_key" type="password" class="input" autocomplete="off"
              :placeholder="apiKeyPlaceholder" />
            <button type="button" class="btn btn-sm btn-ghost" @click="showKey = !showKey">
              {{ showKey ? '隐藏' : '显示' }}
            </button>
          </div>
          <div class="field-hint">打码显示时保存不会覆盖原 Key</div>
        </div>
        <div class="field span-2">
          <label for="base_url">接口地址</label>
          <input id="base_url" v-model="form.volc_base_url" type="text" class="input" />
        </div>
        <div class="field">
          <label for="text_model">文生文模型</label>
          <input id="text_model" v-model="form.volc_text_model" type="text" class="input" placeholder="deepseek-v4-flash-260425" />
          <div class="field-hint">用于剧本 / 分镜生成</div>
        </div>
        <div class="field">
          <label for="image_model">文生图模型</label>
          <input id="image_model" v-model="form.volc_image_model" type="text" class="input" placeholder="doubao-seedream-5-0-260128" />
          <div class="field-hint">用于分镜画面生成</div>
        </div>
        <div class="field">
          <label for="image_size">画面尺寸</label>
          <select id="image_size" v-model="form.volc_image_size" class="input">
            <option value="1920x1920">1920x1920（最低要求，默认）</option>
            <option value="2k">2K（高清，慢）</option>
            <option value="3k">3K（更清晰）</option>
            <option value="4k">4K（超清，最慢）</option>
          </select>
        </div>
        <div class="field">
          <label for="video_concurrency">视频生成并发数</label>
          <input id="video_concurrency" v-model.number="form.video_concurrency" type="number" min="1" max="8" class="input" placeholder="4" />
          <div class="field-hint">同时生成的视频任务数（默认 4，动态生效）</div>
        </div>
        <div class="field">
          <label for="video_resolution">视频分辨率</label>
          <select id="video_resolution" v-model="form.video_resolution" class="input">
            <option value="720p">720p（默认，快）</option>
            <option value="1080p">1080p（更清晰）</option>
            <option value="2k">2K（最清晰，慢）</option>
          </select>
          <div class="field-hint">场景视频生成分辨率；越高越慢、显存越大（2K 在 L40 上可能明显变慢）</div>
        </div>
      </div>

      <div v-if="error" class="notice error-notice">{{ error }}</div>
      <div v-if="success" class="notice success-notice">{{ success }}</div>

      <div class="actions-row">
        <button class="btn btn-lg" :disabled="saving" @click="save">
          {{ saving ? '保存中…' : '保存配置' }}
        </button>
        <button class="btn btn-secondary" :disabled="testing || saving" @click="test('text')">
          {{ testing === 'text' ? '测试中…' : '测试文生文' }}
        </button>
        <button class="btn btn-secondary" :disabled="testing || saving" @click="test('image')">
          {{ testing === 'image' ? '测试中…' : '测试文生图' }}
        </button>
        <button class="btn btn-danger" :disabled="restarting" @click="restartAll">
          {{ restarting ? '重启中…' : '重启全部' }}
        </button>
        <div v-if="testResult" class="test-result" :class="{ ok: testResultOk }">
          <span class="dot" :class="testResultOk ? 'dot-green' : 'dot-red'"></span>{{ testResult }}
        </div>
      </div>
    </div>

    <!-- 阿里云 TTS 后期配音 -->
    <div class="card settings-card ali-tts-card">
      <div class="section-head">
        <div>
          <h2>后期配音 · 阿里云 TTS</h2>
          <p class="hint">使用阿里云百炼语音合成（qwen3-tts-flash 等）为对白生成配音，支持按角色配置不同音色</p>
        </div>
        <span class="badge" :class="aliConfigured ? 'badge-green' : 'badge-gray'">{{ aliConfigured ? '已配置' : '未配置' }}</span>
      </div>

      <div class="form-grid">
        <div class="field span-2">
          <label for="ali_api_key">阿里云 API Key</label>
          <div class="key-row">
            <input id="ali_api_key" v-model="form.ali_api_key" type="password" class="input" autocomplete="off"
              :placeholder="aliKeyPlaceholder" />
            <button type="button" class="btn btn-sm btn-ghost" @click="showAliKey = !showAliKey">
              {{ showAliKey ? '隐藏' : '显示' }}
            </button>
          </div>
          <div class="field-hint">打码显示时保存不会覆盖原 Key（示例：sk-xxxxxxxx）</div>
        </div>
        <div class="field span-2">
          <label for="ali_base_url">接口地址</label>
          <input id="ali_base_url" v-model="form.ali_base_url" type="text" class="input" />
        </div>
        <div class="field">
          <label for="ali_tts_model">TTS 模型</label>
          <input id="ali_tts_model" v-model="form.ali_tts_model" type="text" class="input" placeholder="qwen3-tts-flash" />
          <div class="field-hint">可用：qwen3-tts-flash / qwen3-tts-instruct-flash / qwen-tts</div>
        </div>
        <div class="field">
          <label for="ali_tts_voice">默认音色</label>
          <select id="ali_tts_voice" v-model="form.ali_tts_voice" class="input">
            <option value="" disabled>请选择默认音色…</option>
            <optgroup label="👩 女声（推荐）">
              <option value="Cherry">Cherry · 甜美清澈（推荐女主）</option>
              <option value="Serena">Serena · 温柔成熟</option>
              <option value="Chelsie">Chelsie · 清爽知性</option>
            </optgroup>
            <optgroup label="👨 男声（推荐）">
              <option value="Ethan">Ethan · 沉稳磁性（推荐男主）</option>
            </optgroup>
          </select>
          <div class="field-hint">对白未指定角色音色时使用；角色卡可单独设置音色</div>
        </div>
        <div class="field">
          <label for="ali_voice_male">男声默认音色</label>
          <select id="ali_voice_male" v-model="form.ali_voice_male" class="input">
            <option value="" disabled>请选择男声音色…</option>
            <option value="Ethan">Ethan · 沉稳磁性（推荐）</option>
          </select>
          <div class="field-hint">男角色（男主/皇帝/太子等）自动使用，无需逐个配置</div>
        </div>
        <div class="field span-2">
          <label>角色音色映射（JSON，可为空）</label>
          <textarea id="ali_tts_style" v-model="aliStyleText" class="textarea" rows="3"
            placeholder='{"林晓":"Cherry","柳如烟":"Serena"}'></textarea>
          <div class="field-hint">可选：为指定角色固定音色（优先级最高）；未配置时男角色自动用男声、女角色自动用女声默认音色</div>
        </div>
        <div class="field span-2">
          <label>额外参数（JSON，可为空）</label>
          <textarea id="ali_tts_extra" v-model="aliExtraText" class="textarea" rows="2"
            placeholder='{"speed":1.0}'></textarea>
          <div class="field-hint">如语速 speed、情感等模型支持参数</div>
        </div>
      </div>

      <div class="actions-row">
        <button class="btn btn-secondary" :disabled="testing || saving" @click="test('tts')">
          {{ testing === 'tts' ? '测试中…' : '🔊 测试配音' }}
        </button>
        <div v-if="testResult" class="test-result" :class="{ ok: testResultOk }">
          <span class="dot" :class="testResultOk ? 'dot-green' : 'dot-red'"></span>{{ testResult }}
        </div>
      </div>
    </div>

    <div class="card info-card">
      <h2>模型分工</h2>
      <div class="pipeline">
        <div class="pipe-step">
          <span class="pipe-icon icon-purple">✍</span>
          <div><strong>文生文</strong><p>剧本生成<br>场景拆分</p></div>
        </div>
        <span class="pipe-arrow">→</span>
        <div class="pipe-step">
          <span class="pipe-icon icon-orange">🖼</span>
          <div><strong>图生图</strong><p>分镜画面<br>（角色人像图为底）</p></div>
        </div>
        <span class="pipe-arrow">→</span>
        <div class="pipe-step">
          <span class="pipe-icon icon-teal">🎬</span>
          <div><strong>图生视频</strong><p>本地 L40<br>MiniMax H3</p></div>
        </div>
        <span class="pipe-arrow">→</span>
        <div class="pipe-step">
          <span class="pipe-icon icon-green">✂</span>
          <div><strong>合并成片</strong><p>ffmpeg 拼接<br>输出成片</p></div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { api } from '../api'

const form = ref({})
const saved = ref(false)
const saving = ref(false)
const testing = ref('')
const error = ref('')
const success = ref('')
const showKey = ref(false)
const showAliKey = ref(false)
const testResult = ref('')
const testResultOk = ref(false)
const restarting = ref(false)
const aliStyleText = ref('{}')
const aliExtraText = ref('{}')
const aliKeyPlaceholder = ref('')

const apiKeyPlaceholder = ref('')

const aliConfigured = computed(() => !!(form.value.ali_api_key || aliKeyPlaceholder.value))

onMounted(async () => {
  try {
    const { data } = await api.settings()
    form.value = { ...data }
    aliStyleText.value = data.ali_tts_style || '{}'
    aliExtraText.value = data.ali_tts_extra || '{}'
    if (data.volc_api_key && data.volc_api_key.includes('****')) {
      apiKeyPlaceholder.value = data.volc_api_key
      form.value.volc_api_key = ''
    }
    if (data.ali_api_key && data.ali_api_key.includes('****')) {
      aliKeyPlaceholder.value = data.ali_api_key
      form.value.ali_api_key = ''
    }
  } catch (e) {
    error.value = e.response?.data?.error || '读取设置失败'
  }
})

async function save() {
  error.value = ''
  success.value = ''
  saving.value = true
  try {
    const payload = { ...form.value }
    if (!payload.volc_api_key) delete payload.volc_api_key
    if (!payload.ali_api_key) delete payload.ali_api_key
    if (payload.video_concurrency != null) payload.video_concurrency = String(payload.video_concurrency)
    // 校验 JSON
    try { JSON.parse(aliStyleText.value) } catch { throw new Error('角色音色映射不是合法 JSON') }
    try { JSON.parse(aliExtraText.value) } catch { throw new Error('额外参数不是合法 JSON') }
    payload.ali_tts_style = aliStyleText.value
    payload.ali_tts_extra = aliExtraText.value
    await api.saveSettings(payload)
    saved.value = true
    success.value = '配置已保存，立即生效'
    setTimeout(() => { success.value = '' }, 3000)
    if (form.value.volc_api_key) apiKeyPlaceholder.value = ''
    if (form.value.ali_api_key) aliKeyPlaceholder.value = ''
  } catch (e) {
    error.value = e.response?.data?.error || e.message || '保存失败'
  } finally {
    saving.value = false
  }
}

async function test(type) {
  error.value = ''
  success.value = ''
  testResult.value = ''
  testing.value = type
  try {
    const { data } = type === 'text' ? await api.testText() : type === 'tts' ? await api.testTTS() : await api.testImage()
    testResultOk.value = data.ok
    testResult.value = data.message
  } catch (e) {
    testResultOk.value = false
    testResult.value = e.response?.data?.error || '测试失败'
  } finally {
    testing.value = ''
  }
}

async function restartAll() {
  if (!confirm('确定重启全部 ComfyUI 实例？正在执行的任务会被中断，重启约需数分钟。')) return
  error.value = ''
  success.value = ''
  restarting.value = true
  try {
    await api.restartAll()
    success.value = '重启指令已下发，全部实例正在重启（异步执行，可在实例页查看状态）'
    setTimeout(() => { success.value = '' }, 5000)
  } catch (e) {
    error.value = e.response?.data?.error || '重启指令下发失败'
  } finally {
    restarting.value = false
  }
}
</script>

<style scoped>
.settings-hero {
  display: flex;
  align-items: center;
  gap: 20px;
  padding: 28px 0 20px;
}
.settings-hero-icon {
  width: 64px;
  height: 64px;
  border-radius: 18px;
  background: var(--gradient);
  color: #fff;
  font-size: 28px;
  display: flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 12px 30px rgba(0, 122, 255, 0.25);
  flex: 0 0 auto;
}
.hero-eyebrow {
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 1.5px;
  color: var(--accent);
}
.settings-hero h1 { margin: 4px 0 6px; font-size: 28px; }
.settings-hero p { color: var(--text-secondary); margin: 0; font-size: 14px; }
.settings-card { padding: 28px; margin-bottom: 20px; }
.section-head { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 22px; gap: 16px; }
.section-head h2 { margin: 0 0 4px; }
.section-head .hint { margin: 0; color: var(--text-secondary); font-size: 13px; }
.section-head .hint a { color: var(--accent); }
.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 18px; }
.span-2 { grid-column: span 2; }
.field label { display: block; font-size: 13px; font-weight: 600; margin-bottom: 6px; color: var(--text); }
.field .req { color: var(--red, #ff3b30); margin-left: 4px; }
.field-hint { font-size: 12px; color: var(--text-tertiary); margin-top: 5px; }
.key-row { display: flex; gap: 8px; }
.key-row .input { flex: 1; }
.actions-row { display: flex; align-items: center; gap: 10px; margin-top: 24px; flex-wrap: wrap; }
.notice { border-radius: 12px; padding: 12px 16px; font-size: 13px; margin-top: 16px; }
.error-notice { background: rgba(255, 69, 58, 0.1); color: var(--red); }
.success-notice { background: rgba(48, 209, 88, 0.12); color: #1d9e45; }
.test-result { display: inline-flex; align-items: center; gap: 6px; font-size: 13px; font-weight: 500; color: var(--text-secondary); }
.dot { width: 8px; height: 8px; border-radius: 50%; }
.dot-green { background: var(--green); }
.dot-red { background: var(--red, #ff3b30); }
.test-result.ok { color: var(--green); }
.info-card { padding: 24px 28px; }
.info-card h2 { margin: 0 0 20px; font-size: 18px; }
.pipeline { display: flex; align-items: center; gap: 14px; flex-wrap: wrap; }
.pipe-step { display: flex; align-items: center; gap: 12px; background: rgba(0, 0, 0, 0.035); border-radius: 14px; padding: 12px 16px; }
.pipe-icon { width: 40px; height: 40px; border-radius: 11px; display: flex; align-items: center; justify-content: center; font-size: 18px; color: #fff; }
.icon-purple { background: var(--purple, #af52de); }
.icon-orange { background: #ff9500; }
.icon-teal { background: #30b0c7; }
.icon-green { background: var(--green); }
.pipe-step strong { font-size: 14px; }
.pipe-step p { margin: 3px 0 0; font-size: 12px; color: var(--text-secondary); }
.pipe-arrow { color: var(--text-tertiary); font-size: 18px; }
@media (max-width: 780px) {
  .form-grid { grid-template-columns: 1fr; }
  .span-2 { grid-column: span 1; }
  .pipeline { flex-direction: column; align-items: flex-start; }
  .pipe-arrow { transform: rotate(90deg); }
}
</style>
