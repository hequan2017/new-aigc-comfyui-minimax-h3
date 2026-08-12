# 漫剧角色资产体系（Character Bible）设计

> 日期：2026-08-06
> 对标参考：启元·灵映「世界观与角色资产库」
> 状态：已批准自主执行

## 1. 背景与对标差距

参考平台「启元·灵映」定位 **AI 导演工作台 · 剧本到交付**，其核心卖点是 **创作资产的统一与一致性**，反复强调「角色、场景、道具，是否终于在同一套世界里」「统一角色 reference，避免风格漂移与信息断层」。

当前项目（ComfyStudio 漫剧工作台）在 **GPU 调度 + 流水线自动化 + 持久化恢复** 上有工程优势，但在 **创作资产组织** 上薄弱：

- `Project.VisualBible` 仅是一段文本，角色外貌靠 LLM 在每个 `image_prompt` 自觉重复 → **人物跨场景不一致（风格漂移）**，这是漫剧制作 #1 痛点。
- `dramaPlan.characters`（name/role/trait/style）已存在于 plan JSON 中，但 **未实体化、无参考图、未与场景绑定**。
- `Material` 是扁平素材列表，无「角色」维度。

## 2. 目标与非目标

### 目标（本次实现）
1. 将角色从 plan 文本**提升为独立实体** `Character`（项目级角色卡）。
2. `GeneratePlan` 时**自动抽取** plan.characters 为 `Character`（upsert，保留用户编辑与手动角色）。
3. `Scene` 绑定**出场角色**；剧本生成时 LLM 输出每场出场角色名。
4. 文生图时按场景出场角色**注入权威 trait/style**，纠偏 LLM 描述漂移。
5. 角色卡支持**标准参考像**（基于 trait 文生图，复用 `VolcClient.GenerateImage`）。
6. `ProjectDetail` 新增**角色面板 UI**：角色卡、编辑、生成/上传参考像、出场统计。

### 非目标（YAGNI，留后续阶段）
- **TTS 配音**（第二阶段，需火山 TTS 集成 + 音轨 mux 进场景视频/合并）。
- **ref2v 图像参考注入**（进阶可选，i2v 改 ref2v 耗时翻倍，本次仅做文本注入）。
- **交付中心**（按项目/集组织成片与素材的集中视图）。
- **首页叙事重设计**（参考平台首页式产品理念叙事）。

## 3. 数据模型变更（`backend/internal/models/models.go`）

### 新增 `Character`
```go
// Character 角色卡：项目内可复用的人物资产，保证跨场景一致性
type Character struct {
    ID        uint      `gorm:"primaryKey" json:"id"`
    ProjectID uint      `gorm:"column:project_id;index;uniqueIndex:idx_character_project_name" json:"project_id"`
    Name      string    `gorm:"uniqueIndex:idx_character_project_name" json:"name"` // 项目内唯一
    Role      string    `json:"role"`            // 身份：主角/女主/反派/配角…
    Trait     string    `gorm:"type:text" json:"trait"` // 外貌特征（发型/五官/体型）
    Style     string    `gorm:"type:text" json:"style"` // 服装造型
    Portrait  string    `json:"portrait"`       // 标准参考像文件名（input/<project_id>/ 下）
    Source    string    `json:"source"`         // auto(方案抽取) / manual(手动新建)
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}
```

### `Scene` 新增字段
```go
Characters string `json:"characters"` // 出场角色名（逗号分隔），空表示不限定
```
> 选逗号分隔字符串而非 JSON：角色名不含逗号，查询/拼接简单，与现有 `SceneOrder` 同风格。

### `database.go` AutoMigrate 追加 `&models.Character{}`。

## 4. 角色抽取与一致性注入

### 4.1 方案抽取（`shortdrama_plan.go` → `project_service.go`）
`GeneratePlan` 落库 plan 后，调用新方法 `upsertCharactersFromPlan(p, plan)`：
- 遍历 `plan.Characters`，按 `(project_id, name)` upsert。
- 已存在同名：**仅补全空 trait/style**，不覆盖用户已编辑值；不触碰 `Portrait`。
- 新角色：`source=auto`。
- 手动角色（`source=manual`）永不被动覆盖。

### 4.2 场景出场角色（`project_service.go` 剧本生成）
- `scriptScene` 结构新增 `Characters []string`。
- `scriptSystemPrompt` / `scriptFromPlanSystemPrompt` 增加要求：「每个场景输出 `characters` 数组，列出该场出场的角色名（须与角色卡/方案角色名一致）」。
- `generateScriptCore` 落库时写入 `scene.Characters = strings.Join(sc.Characters, ",")`。

### 4.3 一致性注入（`generateClaimedSceneImage`）
现有逻辑：`prompt = visualContext + "\n当前分镜：" + imagePrompt`。
新增：若 `scene.Characters` 非空，按名查 `Character`，把每个角色的**权威** `trait`/`style` 拼成「角色设定」块前置注入：
```
【角色设定（必须严格遵守，保证人物一致）】
- 林夏：长发及腰、杏眼、瓜子脸；穿白色风衣
- 陆川：寸头、剑眉、高个子；穿黑色夹克
```
这样即使 LLM 在 `image_prompt` 中描述漂移，权威设定前置纠偏。

## 5. 角色标准像生成（`project_service.go`）
新增 `GenerateCharacterPortrait(ch *Character)`：
- `prompt = 画风 + ch.Trait + ch.Style + "，角色标准像，半身肖像，正面，中性背景，高质量"`。
- 复用 `VolcClient.GenerateImage`，保存到 `input/<project_id>/`，更新 `ch.Portrait`。
- 复用 `imageSem`（文生图并发限制）与 token 模式防并发回写。
- `GenerateAllPortraits(p)`：一键生成全部 `Portrait=""` 的角色标准像。

## 6. API 设计（REST，遵循现有风格）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/projects/:id/characters` | 角色列表（含出场统计） |
| POST | `/api/projects/:id/characters` | 新建角色（手动，source=manual） |
| PUT | `/api/projects/:id/characters/:cid` | 编辑角色（trait/style/role/name） |
| DELETE | `/api/projects/:id/characters/:cid` | 删除角色 |
| POST | `/api/projects/:id/characters/:cid/portrait` | 生成/重新生成标准像 |
| POST | `/api/projects/:id/characters/portraits` | 一键生成全部缺图标准像 |

> 上传参考像复用现有 `POST /api/upload`（task_id=project_id），再 `PUT` 写 `portrait` 字段，无需新端点。

`GET /api/projects/:id` 的返回体增加 `characters` 字段，前端一次加载。

## 7. 前端 UI（`frontend/src/views/ProjectDetail.vue`）

在「创作方案」与「剧本」之间插入 **角色与人物** 区块：
- 区块头：「CHARACTERS · 角色资产」+「＋ 新建角色」+「一键生成标准像」。
- 角色卡网格（`character-grid`）：头像（标准像或占位首字）、姓名、身份徽标、trait/style 摘要、出场 N 场、操作（编辑 ✎ / 生成头像 / 删除）。
- 编辑/新建角色弹窗（复用 `.modal` 模式）：姓名、身份、trait、style。
- 角色标准像走 `inputUrl(project.id, ch.portrait)` 预览。

`api/index.js` 增加对应客户端方法。

## 8. 实现步骤
1. `models.go`：`Character` + `Scene.Characters`。
2. `database.go`：AutoMigrate。
3. `project_service.go`：character CRUD + 抽取 + 注入 + portrait 生成 + `scriptScene.Characters` 落库。
4. `shortdrama_plan.go`：`GeneratePlan` 末尾调用抽取（无需改 `dramaPlan` 结构）。
5. `project_handlers.go`：character handlers；`HandleGetProject` 返回 characters。
6. `router.go`：注册路由。
7. `api/index.js`：客户端方法。
8. `ProjectDetail.vue`：角色面板 UI。
9. 单测：角色 upsert 幂等、注入拼装、出场角色解析。

## 9. 测试策略
- **单测（不依赖 DB/GPU）**：
  - `upsertCharactersFromPlan` 幂等：重复抽取不覆盖手动编辑与 portrait。
  - 注入拼装：给定 scene.Characters 与角色表，生成正确的前置「角色设定」文本。
  - `scene.Characters` 字符串解析。
- **编译验证**：`go build ./...` 与 `go vet`。
- **前端**：`npm run build` 通过。

## 10. 后续路线图（不在本次）
- **阶段 2 · 配音**：火山 TTS 对每场对白/旁白合成 → mux 进场景视频音轨 → 合并天然支持（`runMerge` 已检测音轨）。
- **阶段 3 · 交付中心**：按项目/集聚合成片、分镜图、角色像，支持版本与批量下载。
- **阶段 4 · 首页叙事**：参考平台式产品理念首页。
