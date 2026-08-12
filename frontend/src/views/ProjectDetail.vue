<template>
  <div class="page fade-up" v-if="project">
    <!-- 项目头 -->
    <div class="project-head">
      <div>
        <div class="head-links">
          <router-link to="/projects" class="back">← 全部项目</router-link>
        </div>
        <h1>{{ project.title }}</h1>
        <p class="synopsis">{{ project.synopsis }}</p>
        <div class="meta-tags">
          <span v-if="project.genre" class="tag">{{ project.genre }}</span>
          <span v-if="project.style" class="tag tag-orange">{{ project.style }}</span>
          <span v-if="project.aspect_ratio" class="tag tag-gray">{{ aspectLabel(project.aspect_ratio) }}</span>
        </div>
      </div>
      <div class="head-actions">
        <span class="badge" :class="projectBadgeClass">{{ projectStatusText }}</span>
        <button class="btn btn-ghost btn-sm" :disabled="busy" @click="openEditProject">✎ 编辑信息</button>
        <button v-if="!project.plan" class="btn btn-secondary btn-sm" :disabled="busy || generatingPlan || !project.synopsis" @click="generatePlan">
          {{ generatingPlan ? '方案生成中…' : '📋 生成创作方案' }}
        </button>
        <button class="btn btn-ghost btn-sm" :disabled="busy || !project.synopsis" @click="regenerateScript">
          {{ generatingScript ? '剧本生成中…' : ('🔄 重生第' + activeEpN + '集剧本') }}
        </button>
        <button class="btn btn-sm" :disabled="busy || pipelineActive || !project.synopsis" @click="startPipeline">
          {{ pipelineActive ? `第${project.pipeline_episode || activeEpN}集生成中…` : `⚡ 一键生成(第${activeEpN}集全流程)` }}
        </button>
        <button class="btn btn-ghost btn-sm" :disabled="busy" @click="dubAll">🎤 全集配音</button>
        <router-link :to="`/projects/${id()}/editor`" class="btn btn-secondary btn-sm">🎬 剪辑台</router-link>
        <a class="btn btn-ghost btn-sm" :href="api.srtUrl(id(), activeEpN)" target="_blank">📜 第{{ activeEpN }}集字幕</a>
        <button class="btn btn-danger btn-sm" @click="removeProject">🗑 删除</button>
      </div>
    </div>

    <!-- 创作方案（short-drama 方法论两阶段：先方案后分镜） -->
    <section class="section" v-if="project.plan || !project.plan === false">
      <div class="section-head">
        <div>
          <span class="overline">CREATIVE PLAN</span>
          <h2>创作方案</h2>
          <p class="sub" v-if="plan">剧名「{{ plan.title }}」· {{ plan.logline }}</p>
        </div>
        <div v-if="project.plan" class="section-actions">
          <button class="btn btn-ghost btn-sm" @click="showPlan = !showPlan">{{ showPlan ? '收起' : '展开' }}</button>
        </div>
      </div>
      <div class="card plan-card" v-if="showPlan || !project.plan">
        <template v-if="plan">
          <div class="plan-grid">
            <div class="plan-block">
              <h4>三幕结构</h4>
              <p v-for="a in plan.acts" :key="a.name" class="plan-line"><b>{{ a.name }}</b>（{{ a.range }}）：{{ a.event }}</p>
            </div>
            <div class="plan-block">
              <h4>主要角色</h4>
              <p v-for="ch in plan.characters" :key="ch.name" class="plan-line"><b>{{ ch.name }}</b>（{{ ch.role }}）：{{ ch.arc }}</p>
            </div>
            <div class="plan-block">
              <h4>四层反派</h4>
              <p v-for="v in plan.villains" :key="v.layer + v.name" class="plan-line"><b>{{ v.layer }}</b> {{ v.name }}：{{ v.motif }}</p>
            </div>
            <div class="plan-block">
              <h4>节奏 / 卡点 / 爽点</h4>
              <p class="plan-line">{{ plan.rhythm }}</p>
              <p class="plan-line">{{ plan.paywall }}</p>
              <p class="plan-line">{{ plan.satisfaction }}</p>
            </div>
            <div class="plan-block plan-episodes">
              <h4>分集目录（{{ epEdits.length }} 集）
                <button class="btn btn-sm btn-secondary" :disabled="epSaving || epDirtyCount === 0" @click="saveEpisodes">
                  {{ epSaving ? '保存中…' : '保存分集修改' }}
                </button>
                <span v-if="epDirtyCount > 0" class="ep-dirty-hint">{{ epDirtyCount }} 集未保存</span>
              </h4>
              <p class="plan-hint">点击集数或「进入」切换该集分镜场景；可修改每集标题与剧情提示词，保存后点击「重新生成剧本」使新提示词生效</p>
              <div v-for="e in epEdits" :key="e.n" class="plan-episode-row"
                :class="{ 'ep-active': activeEpN === e.n, 'ep-dirty-row': isEpDirty(e) }">
                <span class="ep-n ep-link" :title="'查看第' + e.n + '集分镜'" @click="selectEp(e.n)">第{{ e.n }}集</span>
                <input v-model="e.title" class="input input-sm ep-title" :class="{ 'ep-dirty': isEpDirty(e) }" placeholder="集标题" @click.stop />
                <span v-if="e.tag" class="ep-tag">{{ e.tag }}</span>
                <textarea v-model="e.brief" rows="1" class="textarea textarea-sm ep-brief" :class="{ 'ep-dirty': isEpDirty(e) }" placeholder="剧情提示词" @click.stop />
                <button class="btn btn-sm btn-ghost ep-enter" @click.stop="selectEp(e.n)">进入 ▶</button>
              </div>
            </div>
          </div>
        </template>
        <div v-else class="plan-empty">
          点击「生成创作方案」，AI 将按专业短剧方法论（题材指南/节奏曲线/钩子设计/付费卡点/爽感矩阵/四层反派）产出剧名、三幕结构、角色档案与分集目录，随后「重新生成剧本」将基于方案渲染分镜场景。
        </div>
      </div>
    </section>

    <!-- 角色与人物资产（Character Bible）：跨场景一致性） -->
    <section class="section" v-if="characters.length || project.plan !== undefined">
      <div class="section-head">
        <div>
          <span class="overline">CHARACTERS</span>
          <h2>角色与人物 <span class="count">{{ characters.length }}</span></h2>
          <p class="sub">统一角色外貌与服装，分镜画面生成时自动注入角色设定，保证跨场景人物一致</p>
        </div>
        <div class="section-actions">
          <button class="btn btn-ghost btn-sm" :disabled="busy || charsWithoutPortrait === 0" @click="allPortraits">
            一键生成标准像 ({{ charsWithoutPortrait }})
          </button>
          <button class="btn btn-secondary btn-sm" @click="openCreateCharacter">＋ 新建角色</button>
        </div>
      </div>
      <div v-if="characters.length" class="character-grid">
        <div v-for="ch in characters" :key="ch.id" class="card character-card">
          <div class="char-portrait" @click="viewCharPortrait(ch)">
            <img v-if="ch.portrait" :src="charPortraitUrl(ch)" alt="角色标准像" />
            <div v-else class="char-portrait-ph">{{ (ch.name || '?').slice(0, 1) }}</div>
          </div>
          <div class="char-body">
            <div class="char-name-row">
              <span class="char-name">{{ ch.name }}</span>
              <span v-if="ch.role" class="char-role">{{ ch.role }}</span>
              <span v-if="ch.source === 'auto'" class="tag tag-gray">方案抽取</span>
            </div>
            <p v-if="ch.trait" class="char-trait">🎨 {{ ch.trait }}</p>
            <p v-if="ch.style" class="char-style">👔 {{ ch.style }}</p>
            <span class="char-appear">出场 {{ characterCounts[ch.id] || 0 }} 场</span>
            <div class="char-actions">
              <button class="btn btn-sm btn-secondary" :disabled="busy" @click="genPortrait(ch)">
                {{ ch.portrait ? '重生成标准像' : '生成标准像' }}
              </button>
              <button class="btn btn-sm btn-ghost" :disabled="busy || ch._uploading" @click="uploadPortrait(ch)">
                {{ ch._uploading ? '上传中…' : '上传图片替换' }}
              </button>
              <button class="btn btn-sm btn-ghost" @click="openEditCharacter(ch)">编辑</button>
              <button class="btn btn-sm btn-danger" @click="removeCharacter(ch)">删除</button>
            </div>
          </div>
        </div>
      </div>
      <div v-else class="card empty-inline">
        暂无角色。生成创作方案后会自动抽取角色，也可点击「新建角色」手动添加。
      </div>
    </section>

    <!-- 流程步骤条 -->
    <div class="steps-bar card">
      <div class="step" :class="{ done: !!project.plan, active: !project.plan }">
        <span class="step-n">1</span><span>创作方案</span>
      </div>
      <span class="step-arrow">→</span>
      <div class="step" :class="{ done: allPortraitsReady, active: !!project.plan && !allPortraitsReady }" :title="allPortraitsReady ? '角色标准像就绪，视频将锁定人物' : '建议先生成角色标准像，视频才能锁定人物形象'">
        <span class="step-n">2</span><span>角色标准像</span>
      </div>
      <span class="step-arrow">→</span>
      <div class="step" :class="{ done: scenes.length > 0 && imageCount === scenes.length }">
        <span class="step-n">3</span><span>分镜画面</span>
      </div>
      <span class="step-arrow">→</span>
      <div class="step" :class="{ done: scenes.length > 0 && videoReadyCount === scenes.length }">
        <span class="step-n">4</span><span>场景视频</span>
      </div>
      <span class="step-arrow">→</span>
      <div class="step" :class="{ done: merges.some(m => m.status === 'success') }">
        <span class="step-n">5</span><span>合并成片</span>
      </div>
      <span class="step-status" v-if="project.status === 'ready' || project.status === 'finished'">
        <span class="dot green"></span>{{ project.status === 'finished' ? '成片已完成' : '全部视频就绪，可合并成片' }}
      </span>
    </div>

    <!-- 剧本（当前集，可编辑后 AI 重新生成分镜） -->
    <section class="section" v-if="project.script || project.scripts">
      <div class="section-head">
        <div>
          <span class="overline">SCENARIO</span>
          <h2>第{{ activeEpN }}集 剧本 <span v-if="currentEpTitle && !currentEpTitle.includes('第')">· {{ currentEpTitle }}</span></h2>
          <p class="sub">修改本集剧本正文后点击「保存并 AI 重新生成分镜」，将按新剧本重建该集的分镜场景</p>
        </div>
        <div class="section-actions">
          <button class="btn btn-secondary btn-sm" :disabled="busy || expandingScript || !scriptDraft.trim()" @click="aiExpand">
            {{ expandingScript ? 'AI 扩写中…' : '✨ AI 扩写' }}
          </button>
          <button class="btn btn-secondary btn-sm" :disabled="busy || scriptRendering || !scriptDraft.trim()" @click="saveAndRender">
            {{ scriptRendering ? 'AI 生成中…' : '💾 保存并 AI 重新生成分镜' }}
          </button>
          <button class="btn btn-ghost btn-sm" @click="showScript = !showScript">
            {{ showScript ? '收起' : '展开' }}
          </button>
        </div>
      </div>
      <div class="card script-card" v-if="showScript">
        <textarea v-model="scriptDraft" class="textarea script-edit" rows="10"
          :placeholder="'编辑第' + activeEpN + '集剧本正文…'" @input="scriptDirty = true" />
        <div class="script-edit-bar">
          <span v-if="scriptDirty" class="script-dirty-hint">已修改，未保存</span>
          <span v-else-if="!scriptDraft.trim()" class="script-dirty-hint">该集剧本尚未生成，可直接编写或点击「重新生成剧本」</span>
          <button class="btn btn-sm btn-secondary" :disabled="busy || scriptRendering || !scriptDirty" @click="saveAndRender">
            {{ scriptRendering ? 'AI 生成中…' : '💾 保存并 AI 重新生成分镜' }}
          </button>
        </div>
      </div>
    </section>

    <!-- 场景工作区（按当前集显示） -->
    <section class="section" v-if="currentScenes.length">
      <div class="section-head">
        <div>
          <span class="overline">STORYBOARD</span>
          <h2>第{{ activeEpN }}集 · {{ currentEpTitle }} <span class="count">{{ curReadyText }}</span></h2>
        </div>
        <div class="section-actions">
          <button class="btn btn-secondary" :disabled="busy || curPendingScenes === 0" @click="allImages">
            <span class="live-dot"></span> 生成该集全部画面 ({{ curPendingScenes }})
          </button>
          <button class="btn" :disabled="busy || curImageReadyScenes === 0" @click="allVideos">
            🎬 生成该集全部视频 ({{ curImageReadyScenes }})
          </button>
          <button class="btn btn-primary" :disabled="busy || curVideoReadyScenes === 0" @click="autoMergeEpisode">
            ⚡ 自动合并该集成片
          </button>
        </div>
      </div>

      <div class="scene-grid">
        <div v-for="sc in pagedScenes" :key="sc.id" class="card scene-card" :class="{ 'scene-active': isWorking(sc) }">
          <div class="scene-head">
            <span class="scene-order" :class="orderColor(sc.order)">{{ sc.order }}</span>
            <div class="scene-title">
              <div>{{ sc.title || '场景 ' + sc.order }}</div>
              <span class="badge" :class="sceneBadgeClass(sc)">{{ sceneStatusText(sc) }}</span>
            </div>
            <button class="icon-btn" title="编辑场景" @click="openEditScene(sc)">✎</button>
          </div>

          <div class="scene-preview">
            <img v-if="sc.image_file" :src="imageUrl(sc)" class="scene-img" alt="分镜画面"
              @click="viewImage(sc)" />
            <div v-else class="scene-placeholder">
              <span class="ph-icon">🎨</span>
              <span v-if="sc.status === 'image_pending'">画面生成中…（约 30 秒）</span>
              <span v-else-if="sc.status === 'failed' && sc.error && sc.error.includes('画面')">画面生成失败</span>
              <span v-else>{{ sc.content ? '等待生成画面' : '等待生成剧本' }}</span>
              <span v-if="sc.status === 'image_pending'" class="ph-bar">
                <span class="ph-bar-fill"></span>
              </span>
            </div>
          </div>

          <p class="scene-content">{{ sc.content }}</p>
          <p v-if="sc.image_prompt" class="scene-prompt">🎨 {{ sc.image_prompt }}</p>
          <div v-if="sceneDialogues(sc).length" class="scene-dialogues">
            <div v-for="d in sceneDialogues(sc)" :key="d.id" class="dialogue-row">
              <span class="dl-char">{{ d.character || '旁白' }}</span>
              <span class="dl-text">{{ d.text }}</span>
              <audio v-if="d.audio_file && d.status === 'ready'" :src="dubAudioUrl(d)" controls preload="none" class="dl-audio" />
              <span v-else-if="d.status === 'synthesizing'" class="dl-state">合成中…</span>
              <span v-else-if="d.status === 'failed'" class="dl-state dl-fail" :title="d.error">失败</span>
            </div>
          </div>

          <div class="scene-actions">
            <!-- 画面：无图生成，有图重新生成（始终可单独触发） -->
            <template v-if="!sc.image_file">
              <span v-if="sc.status === 'image_pending'" class="working">
                <span class="dot blue pulse"></span>画面生成中…
              </span>
              <button v-else class="btn btn-sm btn-secondary" :disabled="busy || sc._working" @click="genImage(sc)">
                {{ sc._working ? '生成中…' : (sc.status === 'failed' && sc.image_retries > 0 ? '重试画面' : '生成画面') }}
              </button>
            </template>
            <template v-else>
              <button class="btn btn-sm btn-secondary" :disabled="busy || sc._working || isVideoWorking(sc)" @click="genImage(sc)">
                {{ sc._working ? '生成中…' : '重新生成画面' }}
              </button>
              <!-- 视频：画面就绪即可单独触发；就绪后仍可重新生成 -->
              <span v-if="isVideoWorking(sc)" class="working">
                <span class="dot blue pulse"></span>{{ videoProgress(sc) }}
                <button class="btn btn-sm btn-danger stop-video" :disabled="sc._stopping" @click="stopVideo(sc)">
                  {{ sc._stopping ? '停止中…' : '⏹ 停止' }}
                </button>
              </span>
              <button v-else class="btn btn-sm" :class="{ 'btn-ghost': sc.status === 'video_ready' }"
                :disabled="busy || sc._working" @click="genVideo(sc)">
                {{ sc.status === 'video_ready' ? '重新生成视频' : (sc.video_retries > 0 ? '重试视频' : '生成视频') }}
              </button>
              <button class="btn btn-sm btn-ghost" @click="viewImage(sc)">查看画面</button>
            </template>
            <span v-if="sc.status === 'failed' && sc.error" class="fail-msg">{{ sc.error }}</span>
          </div>

          <div v-if="sc.video_file && sc.video_gpu !== null && sc.video_gpu !== undefined" class="video-box">
            <video :src="videoUrl(sc)" controls preload="metadata" class="scene-video"></video>
            <a class="btn btn-sm btn-ghost download" :href="videoUrl(sc) + '?download=1'">下载</a>
          </div>
        </div>
      </div>

      <!-- 场景分页 -->
      <div v-if="currentScenes.length > scenePageSize" class="pager">
        <button class="btn btn-sm btn-ghost" :disabled="scenePage <= 1" @click="scenePage--">← 上一页</button>
        <span class="pager-info">第 {{ scenePage }} / {{ scenePageCount }} 页 · 共 {{ currentScenes.length }} 个场景</span>
        <button class="btn btn-sm btn-ghost" :disabled="scenePage >= scenePageCount" @click="scenePage++">下一页 →</button>
      </div>
    </section>

    <!-- 合并成片（当前集） -->
    <section class="section" v-if="curVideoReadyCount > 0">
      <div class="section-head">
        <div>
          <span class="overline">MERGE & EDIT</span>
          <h2>第{{ activeEpN }}集 · 合并成片</h2>
          <p class="sub">按该集场景序号顺序拼接，输出完整成片，可下载</p>
        </div>
        <div class="section-actions">
          <button class="btn btn-ghost btn-sm" :disabled="busy" @click="mergeAll">🎬 整剧一键合并</button>
          <label class="merge-opt"><input type="checkbox" v-model="mergeSub" />烧录字幕</label>
          <label class="merge-opt"><input type="checkbox" v-model="mergeDub" />保留原声</label>
          <button class="btn btn-lg" :disabled="busy || curVideoReadyCount < 2 || curMerging" @click="autoMergeEpisode">
            ⚡ 合并第{{ activeEpN }}集 {{ curVideoReadyCount }} 个场景
          </button>
        </div>
      </div>

      <div class="card merge-card">
        <div class="merge-select">
          <span class="merge-hint">合并时自动烧录字幕；各场景 H3 视频已同步对白配音，成片保留原音轨</span>
          <button v-for="sc in curReadyScenes" :key="sc.id" class="merge-chip active" disabled>
            <span class="chip-n">{{ sc.order }}</span>
            <span class="chip-title">{{ sc.title || '场景' }}</span>
            <span class="chip-check">✓</span>
          </button>
          <span class="merge-hint" v-if="curReadyScenes.length >= 2">合并顺序：场景 {{ curReadyScenes.map(s => s.order).join(' → ') }}</span>
        </div>

        <div v-if="merges.length" class="merge-list">
          <div v-for="m in visibleMerges" :key="m.id" class="merge-item">
            <div class="merge-info">
              <span class="badge" :class="mergeBadgeClass(m.status)">{{ mergeStatusText(m) }}</span>
              <span class="merge-time">第{{ m.episode_n || 1 }}集 · {{ m.created_at?.slice(0, 16).replace('T', ' ') }}</span>
              <span v-if="m.error" class="fail-msg">{{ m.error }}</span>
            </div>
            <div v-if="m.status === 'success' && m.output_file" class="merge-result">
              <video :src="outputUrl(0, m.output_file)" controls preload="metadata" class="merge-video"></video>
              <div class="merge-links">
                <a class="btn btn-sm btn-ghost" :href="outputUrl(0, m.output_file) + '?download=1'">下载成片</a>
                <a class="btn btn-sm btn-ghost" :href="outputUrl(0, m.output_file.replace(/\.mp4$/, '.srt')) + '?download=1'" v-if="m.subtitle">下载字幕</a>
              </div>
            </div>
            <div v-else-if="m.status === 'running' || m.status === 'pending'" class="merge-working">
              <span class="dot blue pulse"></span>正在拼接视频，请稍候…
            </div>
          </div>
          <button v-if="merges.length > mergeShowLimit" class="btn btn-sm btn-ghost merge-more" @click="showAllMerges = !showAllMerges">
            {{ showAllMerges ? '收起合并历史' : `展开全部合并历史（${merges.length} 条）` }}
          </button>
        </div>
      </div>
    </section>

    <!-- 场景编辑弹窗 -->
    <div v-if="editingScene" class="modal-mask" @click.self="editingScene = null">
      <div class="modal card">
        <h2>编辑场景 {{ editingScene.order }}</h2>
        <div class="field">
          <label>场景标题</label>
          <input v-model="sceneForm.title" class="input" />
        </div>
        <div class="field">
          <label>场景正文（视频提示词）</label>
          <textarea v-model="sceneForm.content" class="textarea" rows="3"
            placeholder="描述画面动作、镜头运动、对白…" />
        </div>
        <div class="field">
          <label>画面提示词（图生图，参考人像图为主）</label>
          <textarea v-model="sceneForm.image_prompt" class="textarea" rows="3"
            placeholder="人物外貌特征、场景环境、画风…" />
          <div class="field-hint">以出场角色的标准人像图为底图生成画面（多角色传多张参考图锁人物）；修改画面提示词会清空已生成的画面与视频，需要重新生成</div>
        </div>
        <div v-if="sceneError" class="notice error-notice">{{ sceneError }}</div>
        <div class="modal-actions">
          <button class="btn btn-ghost" @click="editingScene = null">取消</button>
          <button class="btn" :disabled="savingScene" @click="saveScene">
            {{ savingScene ? '保存中…' : '保存' }}
          </button>
        </div>
      </div>
    </div>

    <!-- 项目信息编辑弹窗 -->
    <div v-if="editingProject" class="modal-mask" @click.self="editingProject = false">
      <div class="modal card">
        <h2>编辑项目</h2>
        <div class="field">
          <label>项目名称</label>
          <input v-model="projectForm.title" class="input" />
        </div>
        <div class="field-row">
          <div class="field">
            <label>题材</label>
            <input v-model="projectForm.genre" class="input" placeholder="如：科幻+悬疑" />
          </div>
          <div class="field">
            <label>画风</label>
            <input v-model="projectForm.style" class="input" placeholder="如：国漫 / 赛博朋克 / 水墨" />
          </div>
        </div>
        <div class="field-row">
          <div class="field">
            <label>目标受众</label>
            <select v-model="projectForm.audience" class="input">
              <option value="">不限</option>
              <option>女频</option>
              <option>男频</option>
              <option>全龄</option>
            </select>
          </div>
          <div class="field">
            <label>故事基调</label>
            <select v-model="projectForm.tone" class="input">
              <option value="">不限</option>
              <option>爽</option>
              <option>甜</option>
              <option>虐</option>
              <option>燃</option>
              <option>搞笑</option>
              <option>悬疑</option>
            </select>
          </div>
        </div>
        <div class="field-row">
          <div class="field">
            <label>结局类型</label>
            <select v-model="projectForm.ending" class="input">
              <option value="">不限</option>
              <option value="HE">HE（大团圆）</option>
              <option value="BE">BE（悲剧）</option>
              <option value="OE">OE（开放式）</option>
            </select>
          </div>
          <div class="field">
            <label>目标集数</label>
            <select v-model="projectForm.episodes" class="input">
              <option :value="0">不指定</option>
              <option :value="5">5 集</option>
              <option :value="10">10 集</option>
              <option :value="20">20 集</option>
              <option :value="40">40 集</option>
              <option :value="60">60 集</option>
            </select>
          </div>
        </div>
        <div class="field">
          <label>画幅 <span class="optional">影响视频与画面比例</span></label>
          <select v-model="projectForm.aspect_ratio" class="input">
            <option value="16:9">横屏 16:9（YouTube / 横屏）</option>
            <option value="9:16">竖屏 9:16（短剧 / 抖音 / 快手）</option>
            <option value="1:1">方形 1:1</option>
          </select>
          <div class="field-hint">切换画幅后需重新生成画面与视频</div>
        </div>
        <div class="field">
          <label>故事创意</label>
          <textarea v-model="projectForm.synopsis" class="textarea" rows="3" />
          <div class="field-hint">修改创意后请先「生成创作方案」，再「重新生成剧本」更新分镜</div>
        </div>
        <div v-if="projectError" class="notice error-notice">{{ projectError }}</div>
        <div class="modal-actions">
          <button class="btn btn-ghost" @click="editingProject = false">取消</button>
          <button class="btn" :disabled="savingProject" @click="saveProject">
            {{ savingProject ? '保存中…' : '保存' }}
          </button>
        </div>
      </div>
    </div>

    <!-- 角色新建/编辑弹窗 -->
    <div v-if="editingCharacter" class="modal-mask" @click.self="editingCharacter = null">
      <div class="modal card">
        <h2>{{ editingCharacter === 'new' ? '新建角色' : '编辑角色' }}</h2>
        <div class="field">
          <label>角色名 <span class="req">必填</span></label>
          <input v-model="charForm.name" class="input" placeholder="如：林夏" />
        </div>
        <div class="field">
          <label>身份 <span class="optional">可选</span></label>
          <input v-model="charForm.role" class="input" placeholder="主角 / 女主 / 反派 / 配角…" />
        </div>
        <div class="field">
          <label>外貌特征 <span class="optional">用于保证人物一致</span></label>
          <textarea v-model="charForm.trait" class="textarea" rows="3"
            placeholder="发型、五官、体型、年龄感、肤色…" />
        </div>
        <div class="field">
          <label>服装造型</label>
          <textarea v-model="charForm.style" class="textarea" rows="2"
            placeholder="标志性服装、配饰、主色调…" />
        </div>
        <div class="modal-actions">
          <button class="btn btn-ghost" @click="editingCharacter = null">取消</button>
          <button class="btn" :disabled="busy || !charForm.name.trim()" @click="saveCharacter">
            {{ busy ? '保存中…' : '保存' }}
          </button>
        </div>
      </div>
    </div>

    <!-- 图片查看弹窗 -->
    <div v-if="viewer" class="modal-mask viewer-mask" tabindex="-1" ref="viewerMask" @click.self="closeViewer" @keydown.esc="closeViewer">
      <div class="viewer-panel" role="dialog" aria-modal="true" aria-label="图片预览">
        <div class="viewer-head">
          <span>图片预览</span>
          <button class="viewer-close" type="button" @click="closeViewer" aria-label="关闭图片预览" title="关闭 (Esc)">✕</button>
        </div>
        <div class="viewer-body">
          <img :src="viewer" alt="预览图片" class="viewer-img" />
        </div>
        <div class="viewer-foot">
          <span class="viewer-hint">点击遮罩或按 Esc 关闭</span>
          <div class="viewer-actions">
            <a :href="viewer + '?download=1'" class="btn btn-sm btn-download" @click.stop>下载原图</a>
            <button class="btn btn-sm btn-ghost btn-close" type="button" @click="closeViewer">关闭</button>
          </div>
        </div>
      </div>
    </div>
  </div>

  <div v-else-if="loadError" class="page empty">
    <div class="empty-icon">!</div>
    <div>{{ loadError }}</div>
    <button class="btn btn-sm" @click="load">重新加载</button>
  </div>
  <div v-else class="page empty">
    <div class="empty-icon">✦</div>
    <div>加载中…</div>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../api'
import { useAppStore } from '../stores/app'
import { useToastStore } from '../stores/toast'

const route = useRoute()
const router = useRouter()
const store = useAppStore()
const toast = useToastStore()

const project = ref(null)
const scenes = ref([])
const merges = ref([])
const characters = ref([])
const characterCounts = ref({})
const dialogues = ref([])
const dubMergeOn = ref(true)
const mergeSub = ref(true)
const mergeDub = ref(true)
const curEpDubReady = computed(() => {
  const ids = currentScenes.value.map(s => s.id)
  const dubs = dialogues.value.filter(d => ids.includes(d.scene_id))
  return dubs.length > 0 && dubs.every(d => d.status === 'ready')
})
const editingCharacter = ref(null)
const charForm = reactive({ name: '', role: '', trait: '', style: '' })
const showScript = ref(false)
const showPlan = ref(false)
const viewer = ref(null)
const viewerMask = ref(null)
const busy = ref(false)
const generatingScript = ref(false)
const generatingPlan = ref(false)
const scriptRendering = ref(false)
const expandingScript = ref(false)
const scriptDraft = ref('')
const scriptDirty = ref(false)
const videoProgressMap = ref({})
const editingScene = ref(null)
const editingProject = ref(false)
const savingScene = ref(false)
const savingProject = ref(false)
const sceneError = ref('')
const projectError = ref('')
const loadError = ref('')
const sceneForm = reactive({ title: '', content: '', image_prompt: '' })
const projectForm = reactive({ title: '', genre: '', style: '', synopsis: '', audience: '', tone: '', ending: '', episodes: 10, aspect_ratio: '16:9' })
let timer = null
let wsTimer = null
let pollTimer = null

const id = () => route.params.id

const pendingScenes = computed(() => scenes.value.filter(s => s.status === 'pending').length)
const imageReadyScenes = computed(() => scenes.value.filter(s => s.status === 'image_ready').length)
const imageCount = computed(() => scenes.value.filter(s => s.image_file).length)
const videoReadyCount = computed(() => scenes.value.filter(s => s.status === 'video_ready').length)
const allPortraitsReady = computed(() => characters.value.length > 0 && characters.value.every(c => c.portrait))
const readyText = computed(() => `${videoReadyCount.value}/${scenes.value.length} 视频就绪`)
const pipelineActive = computed(() => ['plan', 'plan_running', 'script', 'script_running', 'script_manual', 'images', 'videos', 'merge'].includes(project.value?.pipeline_stage))
const projectStatusText = computed(() => ({
  draft: '草稿', plan_done: '方案就绪', script_done: '剧本就绪', producing: '制作中', ready: '全部就绪', finished: '成片完成', failed: '异常'
}[project.value.status] || project.value.status))
const projectBadgeClass = computed(() => ({
  draft: 'badge-gray', plan_done: 'badge-purple', script_done: 'badge-blue', producing: 'badge-orange', ready: 'badge-green', finished: 'badge-green', failed: 'badge-red'
}[project.value.status] || 'badge-gray'))
const plan = computed(() => {
  if (!project.value?.plan) return null
  try { return JSON.parse(project.value.plan) } catch (_) { return null }
})

const epEdits = ref([])
const epSaving = ref(false)
const epDirtyCount = computed(() => epEdits.value.filter(isEpDirty).length)
function trackEpisodes() {
  const p = plan.value
  if (!p || !p.episodes || p.episodes.length === 0) return
  epEdits.value = p.episodes.map(e => ({
    n: e.n, title: e.title || '', brief: e.brief || '', tag: e.tag || '',
    origTitle: e.title || '', origBrief: e.brief || ''
  }))
  if (!activeEpN.value || !p.episodes.some(e => e.n === activeEpN.value)) {
    activeEpN.value = p.episodes[0].n
    syncEpisodeQuery(activeEpN.value)
  }
}
function isEpDirty(e) {
  return e.origTitle !== e.title || e.origBrief !== e.brief
}
async function saveEpisodes() {
  if (epEdits.value.length === 0) return
  const eps = epEdits.value.map(e => ({ n: e.n, title: e.title.trim(), brief: e.brief.trim() }))
  epSaving.value = true
  try {
    const { data } = await api.updatePlanEpisodes(id(), eps)
    project.value = data.project
    trackEpisodes()
    toast.show('分集提示词已保存，重新生成剧本后生效')
  } catch (e) {
    toast.show(e.response?.data?.error || '保存失败')
  } finally {
    epSaving.value = false
  }
}

// ---------- 按集工作区 ----------
const queryEpisode = Number.parseInt(String(route.query.episode || ''), 10)
const activeEpN = ref(Number.isInteger(queryEpisode) && queryEpisode > 0 ? queryEpisode : 1)
const curMerging = ref(false)
function selectEp(n) {
  if (n === activeEpN.value) {
    syncEpisodeQuery(n)
    return
  }
  if (scriptDirty.value && !confirm(`第${activeEpN.value}集剧本有未保存修改，切换后将丢弃？`)) return
  activeEpN.value = n
  syncEpisodeQuery(n)
  scriptDirty.value = false
  scenePage.value = 1
  load()
  document.querySelector('.scene-grid')?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}
function syncEpisodeQuery(n) {
  if (String(route.query.episode || '') === String(n)) return
  router.replace({ query: { ...route.query, episode: String(n) } })
}
const currentScenes = computed(() => scenes.value
  .filter(s => (s.episode_n || 1) === activeEpN.value)
  .sort((a, b) => a.order - b.order))
// 场景分页
const scenePageSize = 6
const scenePage = ref(1)
const scenePageCount = computed(() => Math.max(1, Math.ceil(currentScenes.value.length / scenePageSize)))
const pagedScenes = computed(() => {
  const start = (scenePage.value - 1) * scenePageSize
  return currentScenes.value.slice(start, start + scenePageSize)
})
// 合并历史折叠
const mergeShowLimit = 3
const showAllMerges = ref(false)
const visibleMerges = computed(() => showAllMerges.value ? merges.value : merges.value.slice(0, mergeShowLimit))
const currentEpTitle = computed(() => {
  const e = epEdits.value.find(x => x.n === activeEpN.value)
  return e ? (e.title || `第${e.n}集`) : `第${activeEpN.value}集`
})
const curPendingScenes = computed(() => currentScenes.value.filter(s => s.status === 'pending').length)
const curImageReadyScenes = computed(() => currentScenes.value.filter(s => s.status === 'image_ready').length)
const curVideoReadyCount = computed(() => currentScenes.value.filter(s => s.status === 'video_ready').length)
const curReadyText = computed(() => `${curVideoReadyCount.value}/${currentScenes.value.length} 视频就绪`)
const curReadyScenes = computed(() => currentScenes.value.filter(s => s.status === 'video_ready'))
async function autoMergeEpisode() {
  const ids = curReadyScenes.value.map(s => s.id)
  if (ids.length < 2) {
    toast.show('该集至少需要 2 个视频就绪的场景才能合并')
    return
  }
  curMerging.value = true
  try {
    await api.mergeScenes(id(), { scene_ids: ids, dub: mergeDub.value, subtitles: mergeSub.value })
    await load()
    toast.show(`第${activeEpN.value}集合并任务已启动，完成后可下载成片`)
  } catch (e) {
    toast.show('合并失败：' + (e.response?.data?.error || e.message))
  } finally {
    curMerging.value = false
  }
}

function isWorking(sc) {
  return sc.status === 'video_creating' || sc.status === 'video_pending' || sc.status === 'video_running' || sc.status === 'image_pending'
}
function isVideoWorking(sc) {
  return sc.status === 'video_creating' || sc.status === 'video_pending' || sc.status === 'video_running'
}
function sceneStatusText(sc) {
  return {
    pending: '待画面', image_pending: '画面生成中', image_ready: '画面就绪', video_pending: '视频排队',
    video_creating: '创建视频任务', video_running: '视频生成中', video_ready: '视频就绪', failed: '失败'
  }[sc.status] || sc.status
}
function sceneBadgeClass(sc) {
  return {
    pending: 'badge-gray', image_pending: 'badge-blue', image_ready: 'badge-blue', video_pending: 'badge-orange',
    video_creating: 'badge-orange', video_running: 'badge-orange', video_ready: 'badge-green', failed: 'badge-red'
  }[sc.status] || 'badge-gray'
}
function mergeStatusText(m) {
  return { pending: '等待中', running: '合并中', success: '成片完成', failed: '失败' }[m.status] || m.status
}
function mergeBadgeClass(m) {
  return { pending: 'badge-gray', running: 'badge-orange', success: 'badge-green', failed: 'badge-red' }[m.status] || 'badge-gray'
}
function orderColor(n) {
  return ['', 'icon-blue', 'icon-purple', 'icon-teal', 'icon-orange', 'icon-green', 'icon-pink', 'icon-indigo', 'icon-brown'][n % 8 + 1]
}
function videoProgress(sc) {
  const p = videoProgressMap.value[sc.video_task_id]
  return p !== undefined ? `视频生成中 ${Math.min(99, Math.floor(p))}%` : '视频生成中…'
}
function imageUrl(sc) {
  return api.inputUrl(project.value.id, sc.image_file)
}
function videoUrl(sc) {
  return api.outputUrl(sc.video_gpu, sc.video_file)
}
function outputUrl(gpu, path) {
  return api.outputUrl(gpu, path)
}
function viewImage(sc) {
  viewer.value = imageUrl(sc)
  nextTick(() => viewerMask.value?.focus())
}
function closeViewer() {
  viewer.value = null
}

async function load() {
  try {
    const { data } = await api.project(id())
    loadError.value = ''
    project.value = data.project
    if (epDirtyCount.value === 0) trackEpisodes()
    const cur = (data.project.scripts ? JSON.parse(data.project.scripts) : {})[activeEpN.value] || ''
    if (!scriptDirty.value) {
      scriptDraft.value = cur
    }
    merges.value = data.merges || []
    characters.value = data.characters || []
    characterCounts.value = data.character_counts || {}
    dialogues.value = data.dialogues || []
    scenes.value = (data.scenes || []).map(s => {
      s._working = isWorking(s)
      return s
    })
    if (data.scenes?.some(s => isVideoWorking(s))) {
      fetchProgress()
    }
  } catch (e) {
    loadError.value = e.response?.data?.error || '项目加载失败，请检查服务连接'
  }
}

async function fetchProgress() {
  try {
    const { data } = await api.tasks({ size: 50 })
    const map = {}
    ;(data.items || []).forEach(t => { map[t.task_id] = t.progress })
    videoProgressMap.value = map
  } catch (_) {
  }
}

function refreshSoon() {
  clearTimeout(timer)
  timer = setTimeout(load, 400)
}

async function regenerateScript() {
  busy.value = true
  generatingScript.value = true
  try {
    await api.generateScript(id(), activeEpN.value)
    await load()
  } catch (e) {
    toast.show('生成剧本失败：' + (e.response?.data?.error || e.message))
  } finally {
    busy.value = false
    generatingScript.value = false
  }
}

// 保存编辑后的剧本，并调用 AI 重新渲染该集分镜场景
async function saveAndRender() {
  if (!scriptDraft.value.trim()) return
  scriptRendering.value = true
  try {
    await api.renderScriptFromText(id(), activeEpN.value, scriptDraft.value.trim())
    scriptDirty.value = false
    await load()
    toast.show(`第${activeEpN.value}集分镜场景已按新剧本重新生成，可在下方生成画面`)
  } catch (e) {
    toast.show('重新生成分镜失败：' + (e.response?.data?.error || e.message))
  } finally {
    scriptRendering.value = false
  }
}

// AI 扩写剧本正文：根据当前内容丰富场景/对白/冲突，回填编辑框（不自动保存，用户确认后可保存重生分镜）
async function aiExpand() {
  if (!scriptDraft.value.trim()) return
  expandingScript.value = true
  try {
    const { data } = await api.expandScript(id(), activeEpN.value, scriptDraft.value.trim())
    if (data.script && data.script.trim()) {
      scriptDraft.value = data.script
      scriptDirty.value = true
      toast.success('AI 扩写完成，确认满意后点「保存并 AI 重新生成分镜」')
    } else {
      toast.show('AI 扩写返回为空，请重试')
    }
  } catch (e) {
    toast.error('扩写失败：' + (e.response?.data?.error || e.message))
  } finally {
    expandingScript.value = false
  }
}

async function generatePlan() {
  busy.value = true
  generatingPlan.value = true
  try {
    const { data } = await api.generatePlan(id())
    project.value = data.project
    showPlan.value = true
    await load() // 刷新角色列表（方案抽取的角色）
  } catch (e) {
    toast.show('生成方案失败：' + (e.response?.data?.error || e.message))
  } finally {
    busy.value = false
    generatingPlan.value = false
  }
}

// ---------- 角色资产 ----------
const charsWithoutPortrait = computed(() => characters.value.filter(c => !c.portrait).length)
function charPortraitUrl(ch) {
  return api.inputUrl(project.value.id, ch.portrait)
}
function viewCharPortrait(ch) {
  if (ch.portrait) {
    viewer.value = charPortraitUrl(ch)
    nextTick(() => viewerMask.value?.focus())
  }
}
async function allPortraits() {
  busy.value = true
  try {
    const { data } = await api.generateAllPortraits(id())
    toast.show(data.message || '已提交')
    refreshSoon()
  } catch (e) {
    toast.show(e.response?.data?.error || '生成失败')
  } finally {
    busy.value = false
  }
}
async function genPortrait(ch) {
  try {
    await api.generateCharacterPortrait(id(), ch.id)
    refreshSoon()
  } catch (e) {
    toast.show(e.response?.data?.error || '生成失败')
  }
}
async function uploadPortrait(ch) {
  const input = document.createElement('input')
  input.type = 'file'
  input.accept = 'image/*'
  input.onchange = async () => {
    const f = input.files && input.files[0]
    if (!f) return
    if (!/^image\/(jpeg|png|webp)$/.test(f.type)) {
      toast.show('请上传 JPG/PNG/WebP 图片')
      return
    }
    if (f.size > 10 * 1024 * 1024) {
      toast.show('图片不能超过 10MB')
      return
    }
    ch._uploading = true
    try {
      await api.uploadCharacterPortrait(id(), ch.id, f)
      toast.show('照片已设为角色标准像')
      await load()
    } catch (e) {
      toast.show(e.response?.data?.error || '上传失败')
    } finally {
      ch._uploading = false
    }
  }
  input.click()
}
function openCreateCharacter() {
  Object.assign(charForm, { name: '', role: '', trait: '', style: '' })
  editingCharacter.value = 'new'
}
function openEditCharacter(ch) {
  Object.assign(charForm, { name: ch.name, role: ch.role, trait: ch.trait, style: ch.style })
  editingCharacter.value = ch
}
async function saveCharacter() {
  if (!charForm.name.trim()) return
  busy.value = true
  try {
    if (editingCharacter.value === 'new') {
      await api.createCharacter(id(), { ...charForm })
    } else {
      await api.updateCharacter(id(), editingCharacter.value.id, { ...charForm })
    }
    editingCharacter.value = null
    await load()
  } catch (e) {
    toast.show(e.response?.data?.error || '保存失败')
  } finally {
    busy.value = false
  }
}
async function removeCharacter(ch) {
  if (!confirm(`确定删除角色「${ch.name}」？已生成标准像将被移除（不影响已有分镜）。`)) return
  try {
    await api.deleteCharacter(id(), ch.id)
    await load()
  } catch (e) {
    toast.show(e.response?.data?.error || '删除失败')
  }
}

// ---------- 对白配音与字幕 ----------
function sceneDialogues(sc) {
  return dialogues.value.filter(d => d.scene_id === sc.id)
}
function dubAudioUrl(d) {
  return api.dubAudioUrl(project.value.id, d.audio_file)
}
async function dubScene(sc) {
  try {
    const { data } = await api.generateSceneDub(id(), sc.id)
    toast.show(data.message || '配音已提交')
    refreshSoon()
  } catch (e) {
    toast.error(e.response?.data?.error || '配音失败')
  }
}
async function dubAll() {
  busy.value = true
  try {
    const { data } = await api.generateProjectDub(id())
    toast.show(data.message || '配音已提交')
    refreshSoon()
  } catch (e) {
    toast.error(e.response?.data?.error || '配音失败')
  } finally {
    busy.value = false
  }
}

async function mergeAll() {
  busy.value = true
  try {
    const { data } = await api.mergeAllScenes(id())
    toast.show(data.message || '整剧合并已启动')
    refreshSoon()
  } catch (e) {
    toast.error(e.response?.data?.error || '整剧合并失败')
  } finally {
    busy.value = false
  }
}

async function stopVideo(sc) {
  if (!confirm(`确定停止场景 ${sc.order} 的视频生成？已生成画面会保留，可稍后重新生成视频。`)) return
  sc._stopping = true
  try {
    await api.cancelSceneVideo(id(), sc.id)
    refreshSoon()
  } catch (e) {
    toast.show('停止失败：' + (e.response?.data?.error || e.message))
    sc._stopping = false
  }
}

async function startPipeline() {
  busy.value = true
  try {
    const { data } = await api.generateProject(id(), activeEpN.value)
    toast.show(data.message || '完整生成流程已启动')
    await load()
  } catch (e) {
    toast.show('启动完整生成失败：' + (e.response?.data?.error || e.message))
  } finally {
    busy.value = false
  }
}

async function genImage(sc) {
  sc._working = true
  try {
    await api.generateSceneImage(id(), sc.id)
    refreshSoon()
  } catch (e) {
    toast.show('生成画面失败：' + (e.response?.data?.error || e.message))
    sc._working = false
  }
}

async function genVideo(sc) {
  sc._working = true
  try {
    await api.generateSceneVideo(id(), sc.id)
    refreshSoon()
  } catch (e) {
    toast.show('创建视频任务失败：' + (e.response?.data?.error || e.message))
    sc._working = false
  }
}

async function allImages() {
  busy.value = true
  try {
    const { data } = await api.generateAllImages(id(), activeEpN.value)
    currentScenes.value.forEach(s => {
      if (s.status === 'pending') s._working = true
    })
    toast.show(`第${activeEpN.value}集：` + (data.message || '已提交'))
  } catch (e) {
    toast.show(e.response?.data?.error || '提交失败')
  } finally {
    busy.value = false
    refreshSoon()
  }
}

async function allVideos() {
  busy.value = true
  try {
    const { data } = await api.generateAllVideos(id(), activeEpN.value)
    toast.show(`第${activeEpN.value}集：` + (data.message || '已提交'))
  } catch (e) {
    toast.show(e.response?.data?.error || '提交失败')
  } finally {
    busy.value = false
    refreshSoon()
  }
}

async function removeProject() {
  if (!confirm(`确定删除项目「${project.value.title}」？该操作不可恢复。`)) return
  await api.deleteProject(id())
  router.push('/projects')
}

// ---------- 编辑 ----------

function openEditScene(sc) {
  sceneError.value = ''
  editingScene.value = sc
  Object.assign(sceneForm, {
    title: sc.title, content: sc.content, image_prompt: sc.image_prompt
  })
}

async function saveScene() {
  savingScene.value = true
  sceneError.value = ''
  try {
    await api.updateScene(id(), editingScene.value.id, {
      title: sceneForm.title,
      content: sceneForm.content,
      image_prompt: sceneForm.image_prompt
    })
    const promptChanged = sceneForm.image_prompt !== editingScene.value.image_prompt
    editingScene.value = null
    await load()
    if (promptChanged) toast.show('画面提示词已修改，场景已重置，请重新生成画面')
  } catch (e) {
    sceneError.value = e.response?.data?.error || '保存失败'
  } finally {
    savingScene.value = false
  }
}

function aspectLabel(a) {
  return { '16:9': '横屏 16:9', '9:16': '竖屏 9:16', '1:1': '方形 1:1' }[a] || a || '横屏 16:9'
}
function openEditProject() {
  projectError.value = ''
  editingProject.value = true
  Object.assign(projectForm, {
    title: project.value.title,
    genre: project.value.genre,
    style: project.value.style,
    synopsis: project.value.synopsis,
    audience: project.value.audience || '',
    tone: project.value.tone || '',
    ending: project.value.ending || '',
    episodes: project.value.episodes || 0,
    aspect_ratio: project.value.aspect_ratio || '16:9'
  })
}

async function saveProject() {
  savingProject.value = true
  projectError.value = ''
  try {
    const { data } = await api.updateProject(id(), projectForm)
    project.value = data
    editingProject.value = false
  } catch (e) {
    projectError.value = e.response?.data?.error || '保存失败'
  } finally {
    savingProject.value = false
  }
}

// ---------- 实时刷新 ----------

watch(
  () => store.lastEvent,
  (t) => {
    if (t && scenes.value.some(s => s.video_task_id === t.task_id)) {
      videoProgressMap.value[t.task_id] = t.progress
    }
    refreshSoon()
  }
)

watch(
  () => store.lastProjectUpdate,
  () => refreshSoon()
)

onMounted(() => {
  load()
  wsTimer = setInterval(() => {
    if (store.lastEvent || store.lastProjectUpdate) refreshSoon()
  }, 1000)
  pollTimer = setInterval(load, 5000)
})

onBeforeUnmount(() => {
  clearInterval(wsTimer)
  clearInterval(pollTimer)
  clearTimeout(timer)
})
</script>

<style scoped>
.project-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 20px;
  padding: 14px 0;
  position: sticky;
  top: var(--nav-h);
  z-index: 50;
  background: var(--bg);
  border-bottom: 1px solid var(--border);
}
.head-links .back { color: var(--text-secondary); font-size: 13px; text-decoration: none; }
.back:hover { color: var(--accent); }
.project-head h1 { margin: 10px 0 6px; font-size: 30px; }
.synopsis { margin: 0 0 8px; color: var(--text-secondary); font-size: 13px; max-width: 720px; line-height: 1.5; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
.meta-tags { display: flex; gap: 8px; }
.tag {
  font-size: 11px; font-weight: 600; padding: 3px 10px; border-radius: 980px;
  background: var(--accent-soft); color: var(--accent);
}
.tag-orange { background: rgba(255, 159, 10, 0.14); color: #c47f00; }
.head-actions { display: flex; align-items: center; gap: 10px; flex: 0 0 auto; flex-wrap: wrap; }
.steps-bar { display: flex; align-items: center; gap: 12px; padding: 14px 20px; margin: 18px 0 26px; flex-wrap: wrap; }
.step { display: flex; align-items: center; gap: 8px; font-size: 13px; font-weight: 600; color: var(--text-secondary); }
.step-n {
  width: 22px; height: 22px; border-radius: 50%; background: rgba(0, 0, 0, 0.08);
  display: flex; align-items: center; justify-content: center; font-size: 12px; color: var(--text-tertiary);
}
.step.done { color: var(--text); }
.step.done .step-n { background: var(--gradient); color: #fff; }
.step-arrow { color: var(--text-tertiary); }
.step-status { margin-left: auto; display: inline-flex; align-items: center; gap: 6px; font-size: 12px; font-weight: 700; color: var(--green); }
.section { margin-bottom: 30px; }
.section-head { display: flex; justify-content: space-between; align-items: flex-end; margin-bottom: 14px; gap: 16px; flex-wrap: wrap; }
.overline { font-size: 11px; letter-spacing: 1.4px; color: var(--text-tertiary); font-weight: 700; }
.section-head h2 { margin: 2px 0 0; font-size: 21px; }
.section-head .count { font-size: 13px; color: var(--text-secondary); font-weight: 500; margin-left: 6px; }
.section-head .sub { margin: 4px 0 0; font-size: 13px; color: var(--text-secondary); }
.section-actions { display: flex; gap: 10px; }
.live-dot { display: inline-block; width: 7px; height: 7px; border-radius: 50%; background: var(--accent); margin-right: 2px; }
.script-card { padding: 20px; }
.script-text { white-space: pre-wrap; font-family: inherit; font-size: 13.5px; line-height: 1.8; color: var(--text); margin: 0; }
.plan-card { padding: 20px; }
.plan-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: 18px; }
.plan-block h4 { margin: 0 0 8px; font-size: 13px; color: var(--accent); }
.plan-line { margin: 0 0 6px; font-size: 13px; line-height: 1.6; color: var(--text-secondary); }
.plan-line b { color: var(--text); }
.plan-episodes { grid-column: 1 / -1; }
.plan-empty { color: var(--text-tertiary); font-size: 13px; line-height: 1.7; }
.plan-hint { margin: 0 0 10px; font-size: 12px; color: var(--text-tertiary); }
.script-edit { font-size: 13px; line-height: 1.7; color: var(--text); min-height: 220px; }
.script-edit-bar { display: flex; align-items: center; gap: 10px; margin-top: 8px; }
.script-dirty-hint { font-size: 12px; color: var(--accent); }
.plan-episode-row { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; padding: 4px 8px; border-radius: 8px; border: 1px solid transparent; cursor: default; }
.plan-episode-row:hover { background: var(--accent-soft); }
.plan-episode-row.ep-active { border-color: var(--accent); background: var(--accent-soft); }
.plan-episode-row.ep-dirty-row { border-style: dashed; border-color: var(--accent); }
.plan-episode-row .ep-n { flex: 0 0 52px; font-size: 13px; color: var(--text-secondary); font-weight: 600; }
.plan-episode-row .ep-link { cursor: pointer; }
.plan-episode-row .ep-link:hover { color: var(--accent); text-decoration: underline; }
.plan-episode-row .ep-title { flex: 0 0 180px; }
.plan-episode-row .ep-brief { flex: 1; }
.plan-episode-row .ep-tag { flex: 0 0 auto; font-size: 12px; }
.plan-episode-row .ep-enter { flex: 0 0 auto; padding: 2px 10px; font-size: 12px; }
.input-sm { padding: 5px 8px; font-size: 13px; min-height: 30px; }
.textarea-sm { padding: 5px 8px; font-size: 13px; min-height: 30px; resize: vertical; }
.ep-dirty { border-color: var(--accent) !important; }
.ep-dirty-hint { margin-left: 8px; font-size: 12px; color: var(--accent); }
.plan-episodes h4 { display: flex; align-items: center; }
.plan-episodes h4 .btn { margin-left: 10px; }
.stop-video { margin-left: 8px; padding: 2px 10px; font-size: 12px; }
.character-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 16px; }
.character-card { padding: 16px; display: flex; gap: 14px; align-items: flex-start; }
.char-portrait { flex: 0 0 88px; width: 88px; height: 88px; border-radius: 14px; overflow: hidden; background: rgba(0, 0, 0, 0.04); cursor: zoom-in; }
.char-portrait img { width: 100%; height: 100%; object-fit: cover; display: block; }
.char-portrait-ph { width: 100%; height: 100%; display: flex; align-items: center; justify-content: center; font-size: 34px; font-weight: 700; color: #fff; background: var(--gradient); }
.char-body { flex: 1; min-width: 0; display: flex; flex-direction: column; }
.char-name-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.char-name { font-weight: 700; font-size: 15px; }
.char-role { font-size: 11px; padding: 2px 9px; border-radius: 980px; background: var(--accent-soft); color: var(--accent); }
.char-trait, .char-style { margin: 4px 0 0; font-size: 12px; color: var(--text-secondary); line-height: 1.5; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
.char-appear { margin-top: 6px; font-size: 12px; color: var(--text-tertiary); }
.char-actions { display: flex; gap: 6px; flex-wrap: wrap; margin-top: 8px; }
.empty-inline { padding: 18px; color: var(--text-tertiary); font-size: 13px; text-align: center; }
.field .req { color: var(--red); margin-left: 4px; font-weight: 400; }
.field .optional { color: var(--text-tertiary); font-weight: 400; margin-left: 4px; }
.tag-gray { background: rgba(0, 0, 0, 0.06); color: var(--text-secondary); }
.scene-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(340px, 1fr)); gap: 16px; }
.scene-card { padding: 18px; display: flex; flex-direction: column; }
.scene-active { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
.scene-head { display: flex; align-items: center; gap: 12px; margin-bottom: 12px; }
.scene-order {
  width: 30px; height: 30px; border-radius: 9px; color: #fff; font-weight: 700; font-size: 14px;
  display: flex; align-items: center; justify-content: center; flex: 0 0 auto;
}
.icon-blue { background: var(--accent); }
.icon-purple { background: var(--purple); }
.icon-teal { background: var(--teal); }
.icon-orange { background: var(--orange); }
.icon-green { background: var(--green); }
.icon-pink { background: #ff6482; }
.icon-indigo { background: #5856d6; }
.icon-brown { background: #a2845e; }
.scene-title { flex: 1; display: flex; align-items: center; justify-content: space-between; gap: 8px; min-width: 0; }
.scene-title div { font-weight: 700; font-size: 14px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.icon-btn {
  border: none; background: transparent; color: var(--text-tertiary); font-size: 14px; cursor: pointer;
  padding: 4px 6px; border-radius: 6px; transition: all 0.2s; flex: 0 0 auto;
}
.icon-btn:hover { color: var(--accent); background: var(--accent-soft); }
.scene-preview { border-radius: 12px; overflow: hidden; background: rgba(0, 0, 0, 0.04); margin-bottom: 10px; aspect-ratio: 16/9; }
.scene-img { width: 100%; height: 100%; object-fit: cover; display: block; cursor: zoom-in; }
.scene-placeholder {
  height: 100%; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 8px;
  color: var(--text-tertiary); font-size: 13px;
}
.ph-icon { font-size: 26px; }
.ph-bar { width: 120px; height: 4px; border-radius: 2px; background: rgba(0, 0, 0, 0.08); overflow: hidden; }
.ph-bar-fill {
  display: block; width: 40%; height: 100%; border-radius: 2px;
  background: var(--gradient); animation: slide 1.2s ease-in-out infinite;
}
@keyframes slide { 0% { transform: translateX(-100%); } 100% { transform: translateX(320%); } }
.scene-content {
  margin: 0 0 6px; font-size: 13px; color: var(--text-secondary); line-height: 1.6;
  display: -webkit-box; -webkit-line-clamp: 4; -webkit-box-orient: vertical; overflow: hidden;
}
.scene-prompt {
  margin: 0 0 10px; font-size: 12px; color: var(--text-tertiary); line-height: 1.6;
  display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden;
}
.scene-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin-top: auto; }
.scene-dialogues { margin: 0 0 10px; display: flex; flex-direction: column; gap: 5px; }
.dialogue-row { display: flex; align-items: center; gap: 8px; font-size: 12px; padding: 5px 9px; border-radius: 8px; background: rgba(0, 0, 0, 0.03); }
.dl-char { flex: 0 0 auto; font-weight: 700; color: var(--accent); }
.dl-text { flex: 1; min-width: 0; color: var(--text-secondary); line-height: 1.4; }
.dl-audio { flex: 0 0 auto; height: 26px; max-width: 180px; }
.dl-state { flex: 0 0 auto; font-size: 11px; color: var(--text-tertiary); }
.dl-fail { color: var(--red); }
.working { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; font-weight: 600; color: var(--accent); }
.ready { font-size: 12px; font-weight: 700; color: var(--green); }
.fail-msg { font-size: 12px; color: var(--red); }
.video-box { position: relative; margin-top: 12px; border-radius: 12px; overflow: hidden; background: #000; }
.scene-video { width: 100%; display: block; max-height: 260px; }
.download { position: absolute; right: 8px; bottom: 8px; background: rgba(0, 0, 0, 0.55); color: #fff; border-color: transparent; }
.merge-card { padding: 20px; }
.merge-select { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin-bottom: 16px; }
.merge-hint { font-size: 12px; color: var(--text-tertiary); margin-left: 6px; }
.merge-chip {
  display: inline-flex; align-items: center; gap: 8px; padding: 7px 12px; border-radius: 980px;
  border: 1.5px solid var(--border); background: transparent; font-size: 13px; cursor: pointer;
  transition: all 0.2s; color: var(--text-secondary);
}
.merge-chip .chip-check { opacity: 0; color: var(--green); font-weight: 700; }
.merge-chip.active { border-color: var(--accent); background: var(--accent-soft); color: var(--text); }
.merge-chip.active .chip-check { opacity: 1; }
.chip-n {
  width: 20px; height: 20px; border-radius: 50%; background: rgba(0, 0, 0, 0.08);
  display: flex; align-items: center; justify-content: center; font-size: 11px; font-weight: 700;
}
.merge-chip.active .chip-n { background: var(--accent); color: #fff; }
.merge-list { border-top: 1px solid var(--border); padding-top: 14px; display: flex; flex-direction: column; gap: 16px; }
.merge-more { align-self: flex-start; margin-top: 4px; }
.pager { display: flex; align-items: center; justify-content: center; gap: 14px; margin-top: 16px; }
.pager-info { font-size: 13px; color: var(--text-secondary); }
.merge-info { display: flex; align-items: center; gap: 10px; margin-bottom: 8px; }
.merge-time { font-size: 12px; color: var(--text-tertiary); }
.merge-result { display: flex; gap: 12px; align-items: flex-start; }
.merge-video { width: min(480px, 100%); border-radius: 12px; }
.merge-working { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--text-secondary); }
.merge-opt { display: inline-flex; align-items: center; gap: 4px; font-size: 13px; color: var(--text-secondary); cursor: pointer; white-space: nowrap; }
.merge-opt input { width: 14px; height: 14px; cursor: pointer; margin: 0; }
.modal-mask {
  position: fixed; inset: 0; background: rgba(0, 0, 0, 0.6); backdrop-filter: blur(10px);
  display: flex; align-items: center; justify-content: center; z-index: 200; padding: 24px;
}
/* 图片查看：整图可见、无需滚动 */
.viewer-mask { overflow: hidden; padding: 32px; }
.viewer-panel {
  width: min(860px, calc(100vw - 64px)); max-height: min(82vh, 700px);
  display: grid; grid-template-rows: auto minmax(0, 1fr) auto;
  overflow: hidden; border-radius: 18px; background: var(--card-solid);
  box-shadow: 0 28px 80px rgba(0, 0, 0, 0.42);
}
.viewer-head {
  min-height: 52px; padding: 0 12px 0 18px; display: flex; align-items: center; justify-content: space-between;
  color: var(--text); font-size: 14px; font-weight: 650; border-bottom: 1px solid var(--border);
}
.viewer-body { min-height: 0; padding: 16px; display: flex; align-items: center; justify-content: center; background: #111; }
.viewer-img {
  display: block; max-width: 100%; max-height: min(62vh, 560px); border-radius: 10px; object-fit: contain;
}
.viewer-close {
  width: 36px; height: 36px; flex: 0 0 36px; border-radius: 10px;
  border: 1px solid var(--border); background: var(--bg); color: var(--text);
  font-size: 18px; cursor: pointer; display: flex; align-items: center; justify-content: center; transition: 0.2s;
}
.viewer-close:hover { background: var(--red); border-color: var(--red); color: #fff; }
.viewer-foot {
  min-height: 58px; padding: 10px 14px 10px 18px; display: flex; align-items: center; justify-content: space-between; gap: 16px;
  border-top: 1px solid var(--border);
}
.viewer-hint { font-size: 12px; color: var(--text-tertiary); }
.viewer-actions { display: flex; align-items: center; gap: 10px; }
.viewer-foot .btn-download { background: var(--accent); color: #fff; border-color: transparent; }
.viewer-foot .btn-close { min-width: 68px; }
.modal { width: min(520px, calc(100vw - 32px)); padding: 28px; animation: pop 0.25s ease; max-height: 90vh; overflow-y: auto; }
.modal h2 { margin: 0 0 18px; font-size: 19px; }
.field label { display: block; font-size: 13px; font-weight: 600; margin-bottom: 6px; }
.field + .field { margin-top: 14px; }
.field-row { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; margin-top: 14px; }
.field-row + .field { margin-top: 14px; }
.field-hint { font-size: 12px; color: var(--text-tertiary); margin-top: 5px; }
.notice { border-radius: 12px; padding: 10px 14px; font-size: 13px; margin-top: 14px; }
.error-notice { background: rgba(255, 69, 58, 0.1); color: var(--red); }
.modal-actions { display: flex; justify-content: flex-end; gap: 10px; margin-top: 22px; }
@keyframes pop { from { transform: scale(0.94); opacity: 0; } to { transform: scale(1); opacity: 1; } }
@media (max-width: 780px) {
  .project-head { flex-direction: column; }
  .scene-grid { grid-template-columns: 1fr; }
  .viewer-mask { padding: 12px; }
  .viewer-panel { width: calc(100vw - 24px); max-height: calc(100vh - 24px); }
  .viewer-img { max-height: calc(100vh - 176px); }
  .viewer-hint { display: none; }
}
</style>
