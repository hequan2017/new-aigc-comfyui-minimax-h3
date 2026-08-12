package service

import (
	"embed"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"comfyui-console/internal/models"
)

// ============================================================
// short-drama 方法论集成（https://github.com/0xsline/short-drama）
// 两阶段制作：
//   阶段 1 GeneratePlan    —— 按方法论生成创作方案（剧名/三幕/节奏/付费卡点/爽感矩阵/角色/分集目录）
//   阶段 2 GenerateScript  —— 依据创作方案渲染分镜场景（供文生图 + 图生视频流水线）
// ============================================================

//go:embed shortdrama
var shortDramaFS embed.FS

// readRef 读取方法论参考文档（嵌入二进制，按需组装进 prompt）
func readRef(name string) string {
	data, err := shortDramaFS.ReadFile("shortdrama/references/" + name)
	if err != nil {
		return ""
	}
	return string(data)
}

// 创作方案 JSON 结构（阶段 1 输出）
type dramaPlan struct {
	Title   string `json:"title"`   // 剧名
	Logline string `json:"logline"` // 一句话故事线
	Core    string `json:"core"`    // 核心冲突
	Acts    []struct {
		Name  string `json:"name"` // 幕名：入局/纠缠/决战
		Range string `json:"range"`
		Event string `json:"event"`
	} `json:"acts"` // 三幕结构
	Rhythm       string `json:"rhythm"`       // 节奏曲线要点
	Paywall      string `json:"paywall"`      // 付费卡点规划
	Satisfaction string `json:"satisfaction"` // 爽感矩阵
	Characters   []struct {
		Name  string `json:"name"`
		Role  string `json:"role"`  // 身份
		Arc   string `json:"arc"`   // 弧光
		Trait string `json:"trait"` // 外貌特征（用于画面一致性）
		Style string `json:"style"` // 服装/造型
	} `json:"characters"` // 主要角色（含外貌，供分镜画面一致）
	Villains []struct {
		Layer string `json:"layer"` // 小/中/大/隐藏反派
		Name  string `json:"name"`
		Motif string `json:"motif"`
	} `json:"villains"` // 四层反派
	Episodes []struct {
		N     int    `json:"n"`
		Title string `json:"title"`
		Brief string `json:"brief"` // 核心冲突/爽点一句话
		Hook  string `json:"hook"`  // 钩子类型
		Tag   string `json:"tag"`   // 🔥关键集 💰付费卡点
	} `json:"episodes"` // 分集目录
}

// planSystemPrompt 阶段 1 系统提示词：注入方法论要点（精选规则控制 token，不全文注入）
func planSystemPrompt() string {
	genre := readRef("genre-guide.md")
	rhythm := readRef("rhythm-curve.md")
	hooks := readRef("hook-design.md")
	paywall := readRef("paywall-design.md")
	satisfaction := readRef("satisfaction-matrix.md")
	villain := readRef("villain-design.md")

	return `你是专业的微短剧编剧，精通短视频平台爆款短剧创作方法论。请根据用户提供的选题信息，产出完整的创作方案。

创作方法论（必须遵循）：
【题材要点】
` + genre + `
【节奏曲线】
` + rhythm + `
【钩子设计】
` + hooks + `
【付费卡点】
` + paywall + `
【爽感矩阵】
` + satisfaction + `
【反派体系】
` + villain + `

输出要求：
1. 只输出一个合法的 JSON 对象，不要输出任何解释、Markdown 代码块标记或其它文字。
2. JSON 结构固定为：
{
  "title": "剧名（3-8字，有网感）",
  "logline": "一句话故事线",
  "core": "核心冲突",
  "acts": [{"name": "第一幕·入局", "range": "第1-{N}集", "event": "核心事件"}],
  "rhythm": "全剧节奏曲线设计（起势/攀升/风暴/决战配比）",
  "paywall": "付费卡点规划（占全集10-15%，标注卡点集数与悬念设计）",
  "satisfaction": "爽感矩阵配比（打脸/逆袭/甜宠/虐心/悬疑/燃/搞笑/感动）",
  "characters": [{"name": "角色名", "role": "身份", "arc": "人物弧光", "trait": "外貌特征（发型/五官/体型，供画面生成保持一致）", "style": "服装造型"}],
  "villains": [{"layer": "小反派/中反派/大反派/隐藏反派", "name": "名字", "motif": "动机与行为模式"}],
  "episodes": [{"n": 1, "title": "集标题", "brief": "核心冲突或爽点一句话", "hook": "钩子类型（悬念钩/反转钩/情绪钩/信息钩/危机钩）", "tag": "🔥或💰或空"}]
}
3. episodes 必须覆盖全集数（与用户配置的集数一致），体现三幕节奏；前10集至少3个🔥和2个💰；🔥占比25-35%，💰占比10-15%。
4. 每个主要角色必须给出 trait（外貌特征）与 style（服装造型），后续分镜画面需要保持人物一致。`
}

// scriptFromPlanSystemPrompt 阶段 2 系统提示词：依据创作方案渲染分镜场景
func scriptFromPlanSystemPrompt() string {
	return `你是专业的漫剧编剧与分镜师。根据给定的创作方案，把故事转化为一条适合"文生图 + 图生视频"流水线的分镜序列。
要求：
1. 只输出一个合法的 JSON 对象，不要输出任何解释、Markdown 代码块标记或其它文字。
2. JSON 结构固定为：
{
  "script": "本集/本段剧本正文（按场景分段，含动作描写与对白）",
  "visual_bible": "汇总创作方案中主要角色的固定外貌、服装、色彩与全片统一画风",
  "scenes": [
    {
      "title": "场景1：概括性标题",
      "content": "该场景的视频提示词：描述画面动作、镜头运动（如推近/摇镜）、人物表情与对白，现在时态，1~3 句",
      "image_prompt": "该场景的静态画面提示词（用于文生图）：包含主体人物外貌特征、服装、场景环境、光影氛围、构图与画风描述",
      "duration": 5,
      "characters": ["出场角色名1", "角色名2"],
      "dialogues": [{"character": "角色名", "text": "台词"}, {"character": "", "text": "旁白"}]
    }
  ]
}
3. 输出 6~10 个场景，每个场景是一段 3~8 秒的独立短视频片段；根据对白长度与动作复杂度设置 duration。
4. 人物一致性至关重要：同一角色在多个场景出现时，image_prompt 必须严格沿用创作方案中该角色的 trait（外貌特征）与 style（服装造型），且所有场景画风描述保持一致。
5. 每个场景必须在 characters 数组中列出该场出场的角色名（须与创作方案中的角色名完全一致；无出场角色则为空数组）。
6. 每个场景必须在 dialogues 数组中列出该场的对白与旁白（character 为说话人角色名，空字符串表示旁白；用于配音与字幕）。无对白则为空数组。
7. 第一个场景尽量给出大场景/环境交代，后续场景聚焦人物动作与剧情推进。
8. 剧情节奏参考创作方案中的节奏曲线：开头要有钩子，中段冲突升级，结尾留悬念。`
}

// GeneratePlan 阶段 1：按 short-drama 方法论生成创作方案（存 project.plan）
func (s *ProjectService) GeneratePlan(p *models.Project) (*models.Project, error) {
	var user strings.Builder
	user.WriteString("故事创意：" + p.Synopsis + "\n")
	if p.Genre != "" {
		user.WriteString("题材：" + p.Genre + "\n")
	}
	if p.Audience != "" {
		user.WriteString("目标受众：" + p.Audience + "\n")
	}
	if p.Tone != "" {
		user.WriteString("故事基调：" + p.Tone + "\n")
	}
	if p.Ending != "" {
		user.WriteString("结局类型：" + p.Ending + "\n")
	}
	if p.Episodes > 0 {
		user.WriteString(fmt.Sprintf("总集数：%d 集\n", p.Episodes))
	}
	if p.Style != "" {
		user.WriteString("画风：" + p.Style + "\n")
	}
	user.WriteString("请按系统要求输出创作方案 JSON。")

	raw, err := s.volc.Chat(planSystemPrompt(), user.String())
	if err != nil {
		return nil, err
	}
	res, err := parsePlanJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("方案解析失败（可重试）: %w", err)
	}
	planJSON, _ := json.Marshal(res)
	status := "plan_done"
	if p.AutoGenerate {
		status = "producing"
	}
	if err := s.db.Model(p).Updates(map[string]any{
		"plan": string(planJSON), "status": status, "error": "",
	}).Error; err != nil {
		return nil, err
	}
	s.pushProject(nil)
	var fresh models.Project
	if err := s.db.First(&fresh, p.ID).Error; err != nil {
		return nil, err
	}
	// 抽取创作方案中的角色为独立资产（保留已编辑角色与标准像）
	s.upsertCharactersFromPlan(&fresh, res)
	return &fresh, nil
}

// PlanEpisodeUpdate 每集可编辑字段（标题 / 剧情提示词）
type PlanEpisodeUpdate struct {
	N     int    `json:"n"`
	Title string `json:"title"`
	Brief string `json:"brief"`
}

// UpdatePlanEpisodes 修改创作方案中分集目录的标题与剧情提示词（按集号 n 匹配）。
// 保存后重新生成剧本，修改后的提示词将生效。
func (s *ProjectService) UpdatePlanEpisodes(p *models.Project, updates []PlanEpisodeUpdate) (*models.Project, error) {
	if strings.TrimSpace(p.Plan) == "" {
		return nil, fmt.Errorf("暂无创作方案，请先生成创作方案")
	}
	var plan dramaPlan
	if err := json.Unmarshal([]byte(p.Plan), &plan); err != nil {
		return nil, fmt.Errorf("创作方案解析失败: %w", err)
	}
	byN := map[int]PlanEpisodeUpdate{}
	for _, u := range updates {
		if u.N > 0 {
			byN[u.N] = u
		}
	}
	changed := false
	for i := range plan.Episodes {
		u, ok := byN[plan.Episodes[i].N]
		if !ok {
			continue
		}
		if u.Title != "" && u.Title != plan.Episodes[i].Title {
			plan.Episodes[i].Title = u.Title
			changed = true
		}
		if u.Brief != "" && u.Brief != plan.Episodes[i].Brief {
			plan.Episodes[i].Brief = u.Brief
			changed = true
		}
	}
	if !changed {
		return nil, fmt.Errorf("没有需要更新的分集内容")
	}
	planJSON, _ := json.Marshal(plan)
	if err := s.db.Model(p).Update("plan", string(planJSON)).Error; err != nil {
		return nil, err
	}
	s.pushProject(nil)
	var fresh models.Project
	if err := s.db.First(&fresh, p.ID).Error; err != nil {
		return nil, err
	}
	return &fresh, nil
}

// parsePlanJSON 从模型输出中提取创作方案 JSON（剥离 markdown 包裹与杂文）
func parsePlanJSON(raw string) (*dramaPlan, error) {
	text := strings.TrimSpace(raw)
	if i := strings.Index(text, "```"); i >= 0 {
		rest := text[i+3:]
		if j := strings.Index(rest, "\n"); j >= 0 {
			rest = rest[j+1:]
		}
		if k := strings.LastIndex(rest, "```"); k >= 0 {
			rest = rest[:k]
		}
		text = strings.TrimSpace(rest)
	}
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("输出中未找到 JSON 对象")
	}
	var res dramaPlan
	if err := json.Unmarshal([]byte(text[start:end+1]), &res); err != nil {
		return nil, err
	}
	if res.Logline == "" || len(res.Episodes) == 0 {
		return nil, fmt.Errorf("方案缺少必要字段（logline/episodes）")
	}
	return &res, nil
}
