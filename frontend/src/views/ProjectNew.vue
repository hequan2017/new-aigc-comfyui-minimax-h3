<template>
  <div class="page fade-up new-project-page">
    <div class="new-head">
      <div>
        <div class="head-links">
          <router-link to="/projects" class="back">← 全部项目</router-link>
        </div>
        <h1>新建漫剧项目</h1>
        <p class="sub">填写故事创意与风格设定，AI 自动生成创作方案 → 剧本 → 分镜画面 → 视频 → 合并成片</p>
      </div>
    </div>

    <form class="card form-card" @submit.prevent="create">
      <!-- 基础信息 -->
      <div class="form-section">
        <div class="form-section-title">
          <span class="fs-icon">📋</span>
          <div>
            <h3>基础信息</h3>
            <p>项目名称与整体设定</p>
          </div>
        </div>
        <div class="form-grid">
          <div class="field">
            <label>项目名称 <span class="optional">可选</span></label>
            <input v-model="form.title" class="input" placeholder="如：穿越到明朝当皇帝" />
            <div class="field-hint">留空时系统会按故事自动起名</div>
          </div>
          <div class="field">
            <label>题材 <span class="optional">可选</span></label>
            <input v-model="form.genre" class="input" placeholder="如：科幻+悬疑 / 古风 / 甜宠" />
            <div class="field-hint">可写多个题材用 + 连接，如「甜宠+宫斗」</div>
          </div>
          <div class="field style-field">
            <label>画风 <span class="req">建议填写</span></label>
            <select v-model="form.style" class="input">
              <option value="" disabled>请选择画面风格…</option>
              <optgroup label="真人风格">
                <option value="真人写实">🎥 真人写实（电视剧质感，真实人物）</option>
                <option value="真人电影感">🎬 真人电影感（电影镜头、光影质感）</option>
                <option value="真人都市偶像剧">💄 真人都市偶像剧（现代都市清新）</option>
                <option value="真人古装剧">👘 真人古装剧（汉服宫廷造型）</option>
                <option value="真人纪录片">📹 真人纪录片（自然光线、纪实感）</option>
                <option value="真人复古胶片">🎞 真人复古胶片（80年代胶片颗粒）</option>
              </optgroup>
              <optgroup label="动漫风格">
                <option value="国漫">🎨 国漫（国风动漫插画）</option>
                <option value="日漫">🌸 日漫（日式动画风格）</option>
                <option value="韩漫">💋 韩漫（精致唯美漫画）</option>
                <option value="新国潮动漫">🐉 新国潮动漫（水墨+工笔国风）</option>
                <option value="热血少年漫">🔥 热血少年漫（夸张分镜、高对比）</option>
                <option value="少女漫">🩷 少女漫（柔光、大眼睛、粉彩）</option>
                <option value="Q版卡通">🍭 Q版卡通（可爱大头身）</option>
                <option value="美漫">🦸 美漫（美式肌肉线条、硬朗）</option>
              </optgroup>
              <optgroup label="3D / 动画电影">
                <option value="3D动画">🧊 3D动画（皮克斯/梦工厂质感）</option>
                <option value="3D国漫">🎭 3D国漫（斗罗/斗破质感）</option>
                <option value="3D低多边形">📐 3D低多边形（Low Poly 简约）</option>
                <option value="黏土动画">🧱 黏土动画（定格动画质感）</option>
                <option value="毛绒玩偶风">🧸 毛绒玩偶风（软萌材质）</option>
              </optgroup>
              <optgroup label="艺术画风">
                <option value="古风水墨">🖌 古风水墨（写意山水）</option>
                <option value="水彩插画">💧 水彩插画（通透水色晕染）</option>
                <option value="油画质感">🖼 油画质感（古典笔触）</option>
                <option value="素描手绘">✏️ 素描手绘（黑白铅笔）</option>
                <option value="浮世绘">⛩ 浮世绘（日本江户版画）</option>
                <option value="敦煌壁画">🪷 敦煌壁画（矿物颜料、飞天）</option>
                <option value="皮影戏">🎭 皮影戏（剪纸透光）</option>
                <option value="剪纸动画">✂️ 剪纸动画（平面拼贴）</option>
              </optgroup>
              <optgroup label="潮流科技">
                <option value="赛博朋克">🌆 赛博朋克（霓虹未来感）</option>
                <option value="蒸汽朋克">⚙️ 蒸汽朋克（维多利亚机械）</option>
                <option value="废土末世">🏜 废土末世（荒漠粗粝）</option>
                <option value="像素风">👾 像素风（8-bit 复古游戏）</option>
                <option value="矢量扁平">🎯 矢量扁平（干净色块插画）</option>
                <option value="玻璃拟态">🪟 玻璃拟态（毛玻璃透光）</option>
                <option value="暗黑哥特">🦇 暗黑哥特（幽暗神秘）</option>
              </optgroup>
              <optgroup label="其他">
                <option value="__custom">✍️ 自定义（手动输入画风）</option>
              </optgroup>
            </select>
            <div class="field-hint">决定全片画面风格：角色标准像、分镜画面、视频均按此风格生成。<b>「真人写实」等真人风格可生成真人视频</b>；也可选「自定义」手动输入</div>
            <div class="custom-style" v-if="form.style === '__custom'">
              <input v-model="customStyle" class="input" placeholder="输入自定义画风，如：梵高星空风格 / 昭和时代剧…" />
            </div>
          </div>
          <div class="field">
            <label>画幅 <span class="optional">可选</span></label>
            <select v-model="form.aspect_ratio" class="input">
              <option value="9:16">竖屏 9:16（短剧 / 抖音 / 快手，推荐）</option>
              <option value="16:9">横屏 16:9（长视频 / YouTube）</option>
              <option value="1:1">方形 1:1（社交分享）</option>
            </select>
            <div class="field-hint">短剧平台默认竖屏 9:16</div>
          </div>
        </div>
      </div>

      <!-- 内容偏好 -->
      <div class="form-section">
        <div class="form-section-title">
          <span class="fs-icon">🎯</span>
          <div>
            <h3>内容偏好</h3>
            <p>影响剧本走向与节奏</p>
          </div>
        </div>
        <div class="form-grid">
          <div class="field">
            <label>目标受众 <span class="optional">可选</span></label>
            <select v-model="form.audience" class="input">
              <option value="">不限</option>
              <option>女频</option>
              <option>男频</option>
              <option>全龄</option>
            </select>
            <div class="field-hint">女频偏情感/宫斗/甜宠，男频偏升级/权谋/热血</div>
          </div>
          <div class="field">
            <label>故事基调 <span class="optional">可选</span></label>
            <select v-model="form.tone" class="input">
              <option value="">不限</option>
              <option>爽</option>
              <option>甜</option>
              <option>虐</option>
              <option>燃</option>
              <option>搞笑</option>
              <option>悬疑</option>
            </select>
            <div class="field-hint">全片情绪主基调，可影响卡点设计</div>
          </div>
          <div class="field">
            <label>结局类型 <span class="optional">可选</span></label>
            <select v-model="form.ending" class="input">
              <option value="">不限</option>
              <option value="HE">HE（大团圆）</option>
              <option value="BE">BE（悲剧）</option>
              <option value="OE">OE（开放式）</option>
            </select>
            <div class="field-hint">结局走向，HE 为最受欢迎的完本结局</div>
          </div>
          <div class="field">
            <label>目标集数 <span class="optional">可选</span></label>
            <select v-model="form.episodes" class="input">
              <option :value="5">5 集（试播短剧）</option>
              <option :value="10">10 集</option>
              <option :value="20">20 集</option>
              <option :value="40">40 集</option>
              <option :value="60">60 集（完整长剧）</option>
              <option :value="0">不指定</option>
            </select>
            <div class="field-hint">创作方案按此规划分集目录与付费卡点</div>
          </div>
        </div>
      </div>

      <!-- 故事创意 -->
      <div class="form-section">
        <div class="form-section-title">
          <span class="fs-icon">💡</span>
          <div>
            <h3>故事创意 <span class="req">必填</span></h3>
            <p>一句话或一段话描述你的故事，越具体生成质量越高</p>
          </div>
        </div>
        <div class="field">
          <textarea v-model="form.synopsis" class="textarea story-input" rows="5"
            placeholder="示例：35岁女前端程序员被AI裁员后跳楼，一觉醒来穿越成明朝小宫女，凭借编程思维一路逆袭，最终登基称帝…" />
          <div class="field-hint">建议包含：主角身份与处境 → 核心冲突 → 成长目标。可补充关键配角、世界观等细节</div>
        </div>
      </div>

      <!-- 预览 -->
      <div v-if="form.synopsis" class="form-section preview-section">
        <div class="form-section-title">
          <span class="fs-icon">🔍</span>
          <div>
            <h3>设定预览</h3>
            <p>系统将按以下设定生成创作方案</p>
          </div>
        </div>
        <div class="preview-grid">
          <div class="pv-item"><b>项目名</b><span>{{ form.title || '（自动起名）' }}</span></div>
          <div class="pv-item"><b>画风</b><span>{{ form.style || '（未指定）' }}</span></div>
          <div class="pv-item"><b>题材</b><span>{{ form.genre || '（未指定）' }}</span></div>
          <div class="pv-item"><b>受众</b><span>{{ form.audience || '不限' }}</span></div>
          <div class="pv-item"><b>基调</b><span>{{ form.tone || '不限' }}</span></div>
          <div class="pv-item"><b>结局</b><span>{{ endingLabel }}</span></div>
          <div class="pv-item"><b>集数</b><span>{{ form.episodes ? form.episodes + ' 集' : '不指定' }}</span></div>
          <div class="pv-item"><b>画幅</b><span>{{ form.aspect_ratio === '9:16' ? '竖屏 9:16' : form.aspect_ratio === '1:1' ? '方形 1:1' : '横屏 16:9' }}</span></div>
        </div>
      </div>

      <div v-if="error" class="notice error-notice">{{ error }}</div>
      <div class="form-actions">
        <router-link to="/projects" class="btn btn-ghost">取消</router-link>
        <button class="btn btn-lg" type="submit" :disabled="creating || !form.synopsis.trim()">
          {{ creating ? '创建中…' : '✨ 创建项目' }}
        </button>
      </div>
    </form>
  </div>
</template>

<script setup>
import { computed, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'

const router = useRouter()
const creating = ref(false)
const error = ref('')
const customStyle = ref('')
const form = reactive({ title: '', genre: '', style: '', synopsis: '', audience: '', tone: '', ending: '', episodes: 10, aspect_ratio: '9:16' })

const endingLabel = computed(() => ({ HE: 'HE（大团圆）', BE: 'BE（悲剧）', OE: 'OE（开放式）' }[form.ending] || '不限'))
const effectiveStyle = computed(() => form.style === '__custom' ? customStyle.value.trim() : form.style)

async function create() {
  error.value = ''
  if (!form.synopsis.trim()) {
    error.value = '请填写故事创意'
    return
  }
  creating.value = true
  try {
    const payload = { ...form, synopsis: form.synopsis.trim(), style: effectiveStyle.value }
    const { data } = await api.createProject(payload)
    try {
      await api.generateProject(data.id, 1, true)
    } catch (e) {
      alert('项目已创建，但自动生成启动失败：' + (e.response?.data?.error || e.message))
    }
    router.push(`/projects/${data.id}`)
  } catch (e) {
    error.value = e.response?.data?.error || '创建失败'
    creating.value = false
  }
}
</script>

<style scoped>
.new-project-page { max-width: 860px; }
.new-head { margin-bottom: 20px; }
.new-head h1 { margin: 8px 0 6px; font-size: 28px; }
.new-head .sub { color: var(--text-secondary); margin: 0; }
.form-card { padding: 24px; }
.form-section { padding: 8px 0 20px; border-bottom: 1px dashed var(--border); margin-bottom: 20px; }
.form-section:last-of-type { border-bottom: none; }
.form-section-title { display: flex; gap: 12px; align-items: flex-start; margin-bottom: 16px; }
.fs-icon { font-size: 22px; line-height: 1.4; }
.form-section-title h3 { margin: 0 0 2px; font-size: 16px; }
.form-section-title p { margin: 0; font-size: 12px; color: var(--text-tertiary); }
.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
@media (max-width: 720px) { .form-grid { grid-template-columns: 1fr; } }
.field { display: flex; flex-direction: column; gap: 6px; }
.field label { font-size: 13px; font-weight: 600; }
.field-hint { font-size: 12px; color: var(--text-tertiary); line-height: 1.5; }
.req { color: var(--red); font-weight: 700; }
.optional { color: var(--text-tertiary); font-weight: 400; font-size: 11px; }
.story-input { min-height: 120px; font-size: 14px; line-height: 1.7; }
.preview-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
.pv-item { display: flex; gap: 10px; font-size: 13px; background: rgba(0, 0, 0, 0.03); border-radius: 10px; padding: 10px 12px; }
.pv-item b { flex: 0 0 52px; color: var(--text-tertiary); font-weight: 600; }
.form-actions { display: flex; justify-content: flex-end; gap: 12px; margin-top: 8px; }
</style>
