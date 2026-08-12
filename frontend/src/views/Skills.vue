<template>
  <div class="page fade-up">
    <section class="skills-hero">
      <div class="hero-copy">
        <span class="hero-eyebrow">DRAMA SKILLS</span>
        <h1>漫剧生成 Skill</h1>
        <p>平台集成的漫剧创作全链路 skill：每个环节的方法论、模型、输入输出、关键参数与触发方式。点击任意环节查看详细说明。</p>
      </div>
      <div class="hero-orb" aria-hidden="true"><span>🧩</span></div>
    </section>

    <div class="pipeline-bar card">
      <template v-for="(s, i) in skills" :key="s.key">
        <div class="pipe-node" @click="openSkill(s)" :title="'查看' + s.name">
          <span class="pipe-n">{{ i + 1 }}</span>
          <span>{{ s.short }}</span>
        </div>
        <span v-if="i < skills.length - 1" class="pipe-arrow">→</span>
      </template>
    </div>

    <div class="skill-grid">
      <article v-for="s in skills" :key="s.key" class="card skill-card" @click="openSkill(s)">
        <div class="skill-head">
          <span class="skill-icon">{{ s.icon }}</span>
          <div>
            <h2>{{ s.name }}</h2>
            <span class="skill-stage">{{ s.stage }}</span>
          </div>
          <span class="skill-open">详情 ›</span>
        </div>
        <p class="skill-what">{{ s.what }}</p>
        <div class="skill-flow">
          <div class="flow-row"><span class="flow-k">输入</span><span class="flow-v">{{ s.input }}</span></div>
          <div class="flow-row"><span class="flow-k">输出</span><span class="flow-v">{{ s.output }}</span></div>
        </div>
        <div class="skill-meta">
          <span class="meta-row"><b>模型/工具</b>{{ s.model }}</span>
          <span class="meta-row"><b>关键参数</b>{{ s.params }}</span>
          <span class="meta-row"><b>怎么触发</b>{{ s.how }}</span>
        </div>
      </article>
    </div>

    <div class="card method-card">
      <h2>📖 创作方法论（short-drama skill）</h2>
      <p>创作方案环节集成的专业短剧方法论，<code>readRef()</code> 按阶段组装进系统提示词：</p>
      <div class="method-grid">
        <div class="method-item"><b>三幕结构</b>入局 / 纠缠 / 决战，规划全剧骨架与集数分配</div>
        <div class="method-item"><b>节奏曲线</b>起势 / 攀升 / 风暴 / 决战的节奏配比</div>
        <div class="method-item"><b>钩子设计</b>悬念钩 / 反转钩 / 情绪钩 / 信息钩 / 危机钩</div>
        <div class="method-item"><b>付费卡点</b>占全集 10-15%，标注卡点集与悬念设计</div>
        <div class="method-item"><b>爽感矩阵</b>打脸 / 逆袭 / 甜宠 / 虐心 / 燃 / 搞笑 / 感动</div>
        <div class="method-item"><b>四层反派</b>小反派 / 中反派 / 大反派 / 隐藏反派</div>
      </div>
      <p class="method-note">来源：<code>0xsline/short-drama</code>（<code>backend/internal/service/shortdrama/references/*.md</code>）。</p>
    </div>

    <div class="card method-card">
      <h2>🔑 一致性与调度机制</h2>
      <div class="method-grid">
        <div class="method-item"><b>角色一致</b>Character Bible 权威 trait 注入画面提示词 + i2v 首帧锁定（首帧含角色）</div>
        <div class="method-item"><b>画幅一致</b>项目级 16:9 / 9:16 / 1:1，画面与视频按画幅生成，避免首帧变形</div>
        <div class="method-item"><b>GPU 独占</b>每个视频任务独占一个 GPU，避免抢占；并发数可配（默认 4）</div>
        <div class="method-item"><b>持久化</b>流水线状态机 + 重启恢复（卡死任务自动 reconcile）</div>
      </div>
    </div>

    <!-- 详情弹窗 -->
    <div v-if="detail" class="modal-mask" @click.self="detail = null">
      <div class="modal card skill-detail">
        <div class="detail-head">
          <span class="skill-icon">{{ detail.icon }}</span>
          <div>
            <h2>{{ detail.name }}</h2>
            <span class="skill-stage">{{ detail.stage }}</span>
          </div>
          <button class="btn btn-sm btn-ghost close" @click="detail = null">✕</button>
        </div>

        <p class="detail-what">{{ detail.what }}</p>

        <div class="detail-section">
          <h4>输入</h4>
          <p>{{ detail.input }}</p>
        </div>
        <div class="detail-section">
          <h4>输出</h4>
          <p>{{ detail.output }}</p>
        </div>
        <div class="detail-section">
          <h4>模型 / 工具</h4>
          <p>{{ detail.model }}</p>
        </div>
        <div class="detail-section">
          <h4>关键参数</h4>
          <p>{{ detail.params }}</p>
        </div>
        <div class="detail-section">
          <h4>怎么触发</h4>
          <p>{{ detail.how }}</p>
        </div>

        <div class="detail-section" v-if="detail.steps && detail.steps.length">
          <h4>流程步骤</h4>
          <ol class="steps-list">
            <li v-for="(st, i) in detail.steps" :key="i">{{ st }}</li>
          </ol>
        </div>

        <div class="detail-section" v-if="detail.apis && detail.apis.length">
          <h4>相关 API</h4>
          <div v-for="a in detail.apis" :key="a.method + a.path" class="api-row">
            <span class="api-method" :class="a.method.toLowerCase()">{{ a.method }}</span>
            <code>{{ a.path }}</code>
          </div>
        </div>

        <div class="detail-section" v-if="detail.fields && detail.fields.length">
          <h4>数据结构 / 状态</h4>
          <table class="detail-table">
            <thead><tr><th>字段</th><th>说明</th></tr></thead>
            <tbody>
              <tr v-for="f in detail.fields" :key="f.k"><td><code>{{ f.k }}</code></td><td>{{ f.v }}</td></tr>
            </tbody>
          </table>
        </div>

        <div class="modal-actions">
          <button class="btn" @click="detail = null">知道了</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'

const skills = [
  {
    key: 'plan', short: '创作方案', icon: '📐', stage: '第 1 步 · 策划',
    what: '按 short-drama 方法论生成剧名、三幕结构、角色档案、分集目录、节奏/卡点/爽感矩阵。',
    input: '故事创意、题材、画风、受众、基调、结局、目标集数',
    output: '创作方案 JSON（剧名/三幕/角色 trait+style/分集目录/节奏）+ 自动抽取角色卡',
    model: '火山文生文 deepseek-v4（Responses API）',
    params: '系统提示词注入 6 份方法论参考文档；角色数量 3-5；分集 5-60',
    how: '项目详情 → 生成创作方案',
    steps: ['读取 short-drama 方法论参考文档（三幕/节奏/钩子/卡点/爽感/反派）', 'LLM 输出创作方案 JSON', '解析并抽取角色卡（幂等，不覆盖手动编辑）', '自动触发角色标准像生成（异步）'],
    apis: [
      { method: 'POST', path: '/api/projects/:id/plan' },
      { method: 'PUT', path: '/api/projects/:id/plan/episodes' },
      { method: 'GET', path: '/api/projects/:id' },
    ],
    fields: [
      { k: 'plan', v: '创作方案 JSON 文本' },
      { k: 'status', v: 'draft → plan_done' },
      { k: 'characters', v: '从方案抽取的角色卡（trait/style/source=auto）' },
    ],
  },
  {
    key: 'character', short: '角色资产', icon: '🧑‍🎨', stage: '第 2 步 · 角色',
    what: '角色卡（trait/style/标准像），跨场景注入权威设定保证人物一致；重复抽取幂等不覆盖编辑。',
    input: '角色的外貌 trait、服装 style',
    output: '角色标准像 jpg（input/<pid>/）+ 角色卡',
    model: '火山文生图 doubao-seedream-5-0',
    params: '画幅匹配（与项目一致）、限流 3 并发',
    how: '角色区块 → 一键生成标准像',
    steps: ['从创作方案抽取角色（幂等）', '按项目画幅与角色 trait/style 拼文生图提示词', '标准像写入 input/<pid>/char_*.jpg'],
    apis: [
      { method: 'GET', path: '/api/projects/:id/characters' },
      { method: 'POST', path: '/api/projects/:id/characters' },
      { method: 'POST', path: '/api/projects/:id/characters/portraits' },
    ],
    fields: [
      { k: 'trait', v: '外貌特征（发型/五官/体型）' },
      { k: 'style', v: '服装造型' },
      { k: 'portrait', v: '标准像文件名' },
    ],
  },
  {
    key: 'script', short: '剧本分镜', icon: '📜', stage: '第 3 步 · 剧本',
    what: '依据创作方案渲染分镜：6-10 场景，每场含画面/视频提示词、对白、出场角色。',
    input: '创作方案 + 当前集剧情提示词',
    output: '分镜场景（title/content/image_prompt/duration/characters/dialogues）',
    model: '火山文生文 deepseek-v4（Responses API）',
    params: '场景数 6-10、时长 3-8s（normalize 3~8）',
    how: '重新生成剧本 / ✨ AI 扩写',
    steps: ['读取当前集剧情提示词', 'LLM 按剧本 System Prompt 输出分镜 JSON（含对白 dialogues）', '校验：场景数 6-10、每场必含 content 与 image_prompt', '替换该集旧场景（保留已完成画面/视频）'],
    apis: [
      { method: 'POST', path: '/api/projects/:id/script?episode_n=N' },
      { method: 'POST', path: '/api/projects/:id/script/render' },
      { method: 'POST', path: '/api/projects/:id/script/expand' },
    ],
    fields: [
      { k: 'content', v: '场景正文（视频提示词）' },
      { k: 'image_prompt', v: '图生图画面提示词' },
      { k: 'duration', v: '目标时长 3-8s' },
      { k: 'dialogues', v: '对白数组（character/text/voice）' },
    ],
  },
  {
    key: 'image', short: '分镜画面', icon: '🖼️', stage: '第 4 步 · 画面',
    what: '按画幅比例图生图：以出场角色标准人像图为底（多角色传多张参考图锁人物），作为视频首帧。',
    input: '场景 image_prompt + 角色标准人像图 + 画风',
    output: '分镜画面 jpg（input/<pid>/）',
    model: '火山图生图 doubao-seedream-5-0',
    params: '横 2560×1440 / 竖 1440×2560 / 方 1920²；图生图失败自动重试 1 次',
    how: '场景区 → 生成全部画面',
    steps: ['按项目画幅计算图像尺寸', '注入出场角色标准像 subject 主体参考（多角色多图）', '调用 seedream 图生图生成，重试兜底', '画面就绪 → 可触发视频'],
    apis: [
      { method: 'POST', path: '/api/projects/:id/images' },
      { method: 'POST', path: '/api/projects/:id/scenes/:sid/image' },
    ],
    fields: [
      { k: 'image_file', v: '首帧图文件名' },
      { k: 'status', v: 'image_pending → image_ready' },
      { k: 'image_retries', v: '失败重试次数' },
    ],
  },
  {
    key: 'video', short: '场景视频', icon: '🎬', stage: '第 5 步 · 视频',
    what: 'i2v 图生视频：首帧=场景画面强制锁定起点；对白注入 prompt 由 H3 同步生成人声音轨；画面禁止出现字幕。',
    input: '首帧图（场景画面）+ 视频 prompt（场景正文 + 对白）',
    output: '场景视频 mp4（output_workers/gpuN/，带人声音轨）',
    model: '本地 L40 · MiniMax H3 i2v',
    params: '画幅分辨率、时长、steps=20、cfg=1.0、fps=24、GPU 独占、并发可配（默认 4）',
    how: '场景区 → 生成全部视频',
    steps: ['读取场景对白并注入 prompt（角色说：台词）', 'i2v 生成视频（含同步音频）', 'GPU 独占 + 并发限流（video_concurrency）', '失败自动重试（≤2 次），仍失败可手动重试'],
    apis: [
      { method: 'POST', path: '/api/projects/:id/videos' },
      { method: 'POST', path: '/api/projects/:id/scenes/:sid/video' },
      { method: 'POST', path: '/api/projects/:id/scenes/:sid/video/cancel' },
    ],
    fields: [
      { k: 'video_file', v: '输出相对路径' },
      { k: 'video_gpu', v: '生成的 GPU 编号' },
      { k: 'video_task_id', v: 'ComfyUI 任务 ID' },
      { k: 'status', v: 'video_pending → video_running → video_ready' },
    ],
  },
  {
    key: 'dub', short: '后期配音', icon: '🎤', stage: '第 6 步 · 配音（可选）',
    what: '逐条对白 TTS 合成 mp3（阿里云 TTS 优先，火山引擎兜底），支持按角色配置不同音色，失败不阻塞流水线。',
    input: '对白文本 + 角色音色（角色音色映射/默认音色）',
    output: '对白音频 mp3（input/<pid>/）',
    model: '阿里云 qwen3-tts-flash（DashScope）',
    params: '音色：Cherry(女)/Ethan(男)/Serena/Chelsie；角色音色映射 JSON；extra 参数（speed 等）',
    how: '头部 → 🎤 一键配音 / 剪辑台 → 单条重新合成',
    steps: ['读取平台设置中的阿里云 TTS 配置', '按角色选择音色（映射优先，否则默认）', 'DashScope multimodal-generation 合成 → 下载 mp3', '写入 input/<pid>/dub_*.mp3，状态 ready'],
    apis: [
      { method: 'POST', path: '/api/projects/:id/dub' },
      { method: 'POST', path: '/api/projects/:id/scenes/:sid/dub' },
      { method: 'PUT', path: '/api/projects/:id/dialogues/:did' },
      { method: 'POST', path: '/api/projects/:id/dialogues/:did/dub' },
    ],
    fields: [
      { k: 'voice', v: '音色（角色映射优先）' },
      { k: 'audio_file', v: '合成音频文件名' },
      { k: 'status', v: 'pending → synthesizing → ready / failed' },
    ],
  },
  {
    key: 'srt', short: '字幕', icon: '💬', stage: '第 7 步 · 字幕',
    what: '对白文本直出 SRT（按场景时长均分时间轴），无需 ASR，按集下载；合并时自动烧录进画面。',
    input: '该集所有场景对白 + 场景时长',
    output: 'SRT 字幕文件（含 UTF-8 BOM，可外挂/烧录）',
    model: '文本直出',
    params: '时间轴按场景 duration 均分；字幕样式：Noto Sans CJK SC 14px、MarginV 28',
    how: '头部 → 📜 下载字幕 / 合并时自动烧录',
    steps: ['按场景顺序累加时长', '场内对白均分时间片', '生成 SRT（角色：台词格式）', '合并时烧录（subtitles filter）'],
    apis: [
      { method: 'GET', path: '/api/projects/:id/srt?episode_n=N' },
    ],
    fields: [
      { k: 'start/end', v: '毫秒级时间码' },
      { k: 'character', v: '说话人前缀' },
    ],
  },
  {
    key: 'editor', short: '剪辑台', icon: '🎬', stage: '第 8 步 · 剪辑（可选）',
    what: '类剪映简化版：时间轴拖拽排序、场景预览、时长调整、按场景编辑对白配音与字幕，从剪辑台直接合并。',
    input: '该集场景视频 + 对白 + 字幕时间轴',
    output: '排序后的场景顺序 / 编辑后的对白 / 成片',
    model: '本地工具',
    params: '场景顺序、目标时长 3-15s、对白文本/音色、字幕时间微调',
    how: '项目页 → 🎬 剪辑台',
    steps: ['加载该集场景（含视频时长）与对白', '时间轴拖拽/按钮排序（保存顺序）', '选中场景预览 + 编辑对白（文本/音色）', '字幕时间轴展示 + 微调', '一键合并（配音+字幕）'],
    apis: [
      { method: 'GET', path: '/api/projects/:id/editor?episode_n=N' },
      { method: 'PUT', path: '/api/projects/:id/editor/order' },
      { method: 'PATCH', path: '/api/projects/:id/scenes/:sid/duration' },
    ],
    fields: [
      { k: 'subtitles', v: '字幕时间轴（start/end/text）' },
      { k: 'video_dur', v: '场景视频真实时长' },
    ],
  },
  {
    key: 'merge', short: '合并成片', icon: '✂️', stage: '第 9 步 · 成片',
    what: '按场景顺序 ffmpeg concat 拼接（保留 H3 人声音轨）+ 生成 SRT 字幕并烧录进画面，输出成片。',
    input: '就绪的场景视频列表（各带 H3 原声）',
    output: '成片 mp4（output_workers/gpu0/merged/）+ 同名 .srt',
    model: '远程 ffmpeg libx264/AAC',
    params: 'CRF 18、preset medium、faststart；字幕烧录（Noto Sans CJK SC）',
    how: '合并区 → 合并该集 / 整剧一键合并 / 剪辑台合并',
    steps: ['校验场景视频就绪', '生成 SRT（场景均分时间轴）', 'ffmpeg concat 视频流 + 保留原音轨', 'subtitles filter 烧录字幕', '成片完成 → 项目 finished'],
    apis: [
      { method: 'POST', path: '/api/projects/:id/merge' },
      { method: 'POST', path: '/api/projects/:id/merge-all' },
      { method: 'GET', path: '/api/projects/:id/merges' },
    ],
    fields: [
      { k: 'output_file', v: 'merged/xxx.mp4' },
      { k: 'subtitle', v: '是否生成并烧录字幕' },
      { k: 'status', v: 'pending → running → success / failed' },
    ],
  },
]

const detail = ref(null)
function openSkill(s) {
  detail.value = s
}
</script>

<style scoped>
.skills-hero { display: flex; align-items: center; justify-content: space-between; gap: 24px; padding: 36px 0 18px; }
.hero-eyebrow { font-size: 12px; font-weight: 700; letter-spacing: 1.5px; color: var(--accent); }
.skills-hero h1 { margin: 8px 0 8px; font-size: 32px; }
.skills-hero p { color: var(--text-secondary); margin: 0; font-size: 14px; max-width: 660px; line-height: 1.6; }
.hero-orb { width: 96px; height: 96px; border-radius: 24px; background: var(--accent-soft); display: flex; align-items: center; justify-content: center; flex: 0 0 auto; }
.hero-orb span { font-size: 36px; }
.pipeline-bar { display: flex; align-items: center; gap: 8px; padding: 14px 18px; margin: 16px 0 24px; flex-wrap: wrap; }
.pipe-node { display: inline-flex; align-items: center; gap: 7px; font-size: 13px; font-weight: 600; padding: 6px 12px; border-radius: 980px; background: var(--accent-soft); color: var(--accent); cursor: pointer; transition: all 0.2s; }
.pipe-node:hover { background: var(--accent); color: #fff; }
.pipe-n { width: 18px; height: 18px; border-radius: 50%; background: var(--accent); color: #fff; display: inline-flex; align-items: center; justify-content: center; font-size: 11px; }
.pipe-node:hover .pipe-n { background: rgba(255, 255, 255, 0.25); }
.pipe-arrow { color: var(--text-tertiary); }
.skill-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(340px, 1fr)); gap: 16px; }
.skill-card { padding: 20px; cursor: pointer; transition: all 0.2s; }
.skill-card:hover { transform: translateY(-2px); box-shadow: 0 8px 24px rgba(0, 0, 0, 0.08); border-color: var(--accent); }
.skill-head { display: flex; align-items: center; gap: 12px; margin-bottom: 12px; }
.skill-icon { width: 42px; height: 42px; border-radius: 12px; background: var(--accent-soft); display: flex; align-items: center; justify-content: center; font-size: 20px; flex: 0 0 auto; }
.skill-head h2 { margin: 0; font-size: 16px; }
.skill-stage { font-size: 11px; color: var(--text-tertiary); font-weight: 600; }
.skill-open { margin-left: auto; font-size: 12px; color: var(--accent); font-weight: 600; flex: 0 0 auto; }
.skill-what { margin: 0 0 12px; font-size: 13px; color: var(--text-secondary); line-height: 1.6; }
.skill-flow { display: flex; flex-direction: column; gap: 5px; margin-bottom: 12px; padding: 10px 12px; border-radius: 10px; background: rgba(0, 0, 0, 0.03); }
.flow-row { display: flex; gap: 8px; font-size: 12px; line-height: 1.5; }
.flow-k { flex: 0 0 36px; color: var(--accent); font-weight: 700; }
.flow-v { color: var(--text-secondary); }
.skill-meta { display: flex; flex-direction: column; gap: 6px; padding-top: 12px; border-top: 1px solid var(--border); }
.meta-row { font-size: 12px; color: var(--text-secondary); line-height: 1.5; }
.meta-row b { display: inline-block; min-width: 70px; color: var(--text-tertiary); font-weight: 600; }
.method-card { padding: 24px; margin-top: 20px; }
.method-card h2 { margin: 0 0 8px; font-size: 18px; }
.method-card > p { margin: 0 0 16px; font-size: 13px; color: var(--text-secondary); }
.method-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 12px; }
.method-item { font-size: 13px; color: var(--text-secondary); padding: 10px 12px; border-radius: 10px; background: rgba(0, 0, 0, 0.035); line-height: 1.5; }
.method-item b { color: var(--accent); margin-right: 4px; }
.method-note { margin: 16px 0 0; font-size: 12px; color: var(--text-tertiary); }
.method-note code { background: rgba(0, 0, 0, 0.05); padding: 1px 6px; border-radius: 4px; font-size: 11px; }

/* 详情弹窗 */
.skill-detail { max-width: 560px; max-height: 85vh; overflow-y: auto; }
.detail-head { display: flex; align-items: center; gap: 12px; margin-bottom: 8px; }
.detail-head h2 { margin: 0; font-size: 18px; }
.detail-head .close { margin-left: auto; }
.detail-what { font-size: 13px; color: var(--text-secondary); line-height: 1.6; margin: 0 0 16px; padding-bottom: 14px; border-bottom: 1px solid var(--border); }
.detail-section { margin-bottom: 14px; }
.detail-section h4 { margin: 0 0 6px; font-size: 12px; color: var(--text-tertiary); font-weight: 700; letter-spacing: 0.5px; }
.detail-section p { margin: 0; font-size: 13px; color: var(--text-secondary); line-height: 1.6; }
.steps-list { margin: 0; padding-left: 18px; font-size: 13px; color: var(--text-secondary); line-height: 1.8; }
.api-row { display: flex; align-items: center; gap: 10px; margin-bottom: 6px; }
.api-method { font-size: 11px; font-weight: 700; padding: 2px 8px; border-radius: 6px; flex: 0 0 auto; }
.api-method.get { background: rgba(16, 185, 129, 0.12); color: #059669; }
.api-method.post { background: rgba(59, 130, 246, 0.12); color: #2563eb; }
.api-method.put { background: rgba(245, 158, 11, 0.14); color: #d97706; }
.api-method.patch { background: rgba(139, 92, 246, 0.12); color: #7c3aed; }
.api-method.delete { background: rgba(239, 68, 68, 0.12); color: #dc2626; }
.api-row code { font-size: 12px; color: var(--text-secondary); }
.detail-table { width: 100%; border-collapse: collapse; font-size: 12px; }
.detail-table th, .detail-table td { text-align: left; padding: 6px 8px; border-bottom: 1px solid var(--border); }
.detail-table th { color: var(--text-tertiary); font-weight: 600; }
.detail-table td { color: var(--text-secondary); }
.detail-table code { background: rgba(0, 0, 0, 0.05); padding: 1px 5px; border-radius: 4px; font-size: 11px; }
.modal-actions { display: flex; justify-content: flex-end; margin-top: 16px; }
@media (max-width: 780px) { .skills-hero { flex-direction: column; align-items: flex-start; } }
</style>
