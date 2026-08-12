package service

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

// ProjectService 漫剧项目：剧本（文生文）→ 分镜画面（文生图）→ 视频（本地 L40）→ 合并成片
type ProjectService struct {
	cfg          *config.Config
	db           *gorm.DB
	volc         *VolcClient
	ali          *AliyunTTS
	tasks        *TaskService
	remote       *RemoteExec
	upload       *UploadManager
	hub          *Hub
	stopped      chan struct{}
	imageSem     chan struct{} // 文生图并发限制（火山 API QPS），默认 3
	materials    *MaterialService
}

func NewProjectService(cfg *config.Config, db *gorm.DB, volc *VolcClient, tasks *TaskService, remote *RemoteExec, upload *UploadManager, hub *Hub, materials *MaterialService) *ProjectService {
	return &ProjectService{
		cfg: cfg, db: db, volc: volc, tasks: tasks,
		remote: remote, upload: upload, hub: hub,
		ali:       NewAliyunTTS(db),
		stopped:   make(chan struct{}),
		imageSem:  make(chan struct{}, 3),
		materials: materials,
	}
}

// ---------- 项目 ----------

// CreateProject 创建项目（仅记录创意，剧本待生成）
func (s *ProjectService) CreateProject(p models.Project) (*models.Project, error) {
	if p.Title == "" {
		p.Title = "未命名项目"
	}
	if p.Status == "" {
		p.Status = "draft"
	}
	if p.AspectRatio == "" {
		p.AspectRatio = "16:9"
	}
	if err := s.db.Create(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *ProjectService) GetProject(id uint) (*models.Project, []models.Scene, error) {
	var p models.Project
	if err := s.db.First(&p, id).Error; err != nil {
		return nil, nil, err
	}
	var scenes []models.Scene
	s.db.Where("project_id = ?", id).Order("`order`").Find(&scenes)
	return &p, scenes, nil
}

func (s *ProjectService) ListProjects() ([]models.Project, error) {
	var list []models.Project
	s.db.Order("id desc").Find(&list)
	return list, nil
}

// StartPipeline 幂等启动完整生成流水线。实际工作在后台执行，状态持久化后可在重启时恢复。
// stopAfterScript=true 时（创建项目自动触发），流水线生成完创作方案+角色+第一集剧本后停止，后续由人工处理。
func (s *ProjectService) StartPipeline(p *models.Project, episodeN int, stopAfterScript bool) (*models.Project, error) {
	current, err := s.claimPipeline(p, episodeN, stopAfterScript)
	if err != nil {
		return nil, err
	}
	go s.advancePipeline(current.ID)
	return current, nil
}

// claimPipeline 原子认领并持久化本次流水线目标，独立于后台执行，便于重启恢复。
func (s *ProjectService) claimPipeline(p *models.Project, episodeN int, stopAfterScript bool) (*models.Project, error) {
	if episodeN <= 0 {
		episodeN = 1
	}
	startStage := "plan"
	if strings.TrimSpace(p.Plan) != "" {
		startStage = "script"
	}
	active := []string{"plan", "plan_running", "script", "script_running", "images", "videos", "merge"}
	res := s.db.Model(&models.Project{}).Where("id = ? AND COALESCE(pipeline_stage, '') NOT IN ?", p.ID, active).
		Updates(map[string]any{
			"pipeline_stage": startStage, "pipeline_episode": episodeN,
			"auto_generate": true, "stop_after_script": stopAfterScript,
			"status": "producing", "error": "",
		})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, fmt.Errorf("项目生成流程正在进行中")
	}
	var current models.Project
	if err := s.db.First(&current, p.ID).Error; err != nil {
		return nil, err
	}
	return &current, nil
}

// ClaimManualScript 防止手工剧本生成被重复提交。
func (s *ProjectService) ClaimManualScript(p *models.Project) error {
	active := []string{"plan", "plan_running", "script", "script_running", "script_manual", "images", "videos", "merge"}
	res := s.db.Model(&models.Project{}).Where("id = ? AND COALESCE(pipeline_stage, '') NOT IN ?", p.ID, active).
		Updates(map[string]any{"pipeline_stage": "script_manual", "auto_generate": false, "status": "producing", "error": ""})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("项目已有生成任务正在进行")
	}
	return s.db.First(p, p.ID).Error
}

func (s *ProjectService) cancelProjectTasks(projectID uint) {
	if s.tasks == nil {
		return
	}
	var scenes []models.Scene
	if err := s.db.Where("project_id = ?", projectID).Find(&scenes).Error; err != nil {
		return
	}
	for _, sc := range scenes {
		if sc.VideoTaskID != "" {
			if err := s.tasks.CancelTask(sc.VideoTaskID); err != nil && !strings.Contains(err.Error(), "已结束") {
				log.Printf("[project %d] cancel task %s failed: %v", projectID, sc.VideoTaskID, err)
			}
		}
	}
}

func (s *ProjectService) cleanupStaleSceneFiles(projectID uint, scenes []models.Scene) {
	if s.cfg == nil || s.remote == nil || s.upload == nil {
		return
	}
	for _, sc := range scenes {
		paths := make([]string, 0, 2)
		if sc.ImageFile != "" {
			paths = append(paths, filepath.Join(s.upload.InputDir(), fmt.Sprintf("%d", projectID), filepath.Base(sc.ImageFile)))
		}
		if sc.VideoFile != "" && sc.VideoGPU != nil {
			paths = append(paths, filepath.Join(s.cfg.Comfy.ComfyDir, "output_workers", fmt.Sprintf("gpu%d", *sc.VideoGPU), sc.VideoFile))
		}
		for _, path := range paths {
			if _, err := s.remote.Run("rm -f -- " + shellQuote(path)); err != nil {
				log.Printf("[project %d] cleanup stale file %s failed: %v", projectID, path, err)
			}
		}
	}
}

func (s *ProjectService) DeleteProject(id uint) error {
	s.cancelProjectTasks(id)
	var scenesForCleanup []models.Scene
	var mergesForCleanup []models.MergeTask
	s.db.Where("project_id = ?", id).Find(&scenesForCleanup)
	s.db.Where("project_id = ?", id).Find(&mergesForCleanup)
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		var scenes []models.Scene
		if err := tx.Where("project_id = ?", id).Find(&scenes).Error; err != nil {
			return err
		}
		for _, sc := range scenes {
			if sc.VideoTaskID != "" {
				tx.Where("task_id = ?", sc.VideoTaskID).Delete(&models.Event{})
				tx.Where("task_id = ?", sc.VideoTaskID).Delete(&models.Task{})
			}
		}
		if err := tx.Where("project_id = ?", id).Delete(&models.Material{}).Error; err != nil {
			return err
		}
		if err := tx.Where("task_id = ?", fmt.Sprintf("%d", id)).Delete(&models.UploadFile{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&models.Scene{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&models.Character{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&models.Dialogue{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&models.MergeTask{}).Error; err != nil {
			return err
		}
		return tx.Delete(&models.Project{}, id).Error
	}); err != nil {
		return err
	}
	// 数据库提交后再清理远程产物；路径仅由数字项目 ID 和数据库记录组成并经过 shell 转义。
	if s.cfg != nil && s.remote != nil && s.upload != nil {
		paths := []string{filepath.Join(s.upload.InputDir(), fmt.Sprintf("%d", id))}
		for _, sc := range scenesForCleanup {
			if sc.VideoFile != "" && sc.VideoGPU != nil {
				paths = append(paths, filepath.Join(s.cfg.Comfy.ComfyDir, "output_workers", fmt.Sprintf("gpu%d", *sc.VideoGPU), sc.VideoFile))
			}
		}
		for _, mt := range mergesForCleanup {
			if mt.OutputFile != "" {
				paths = append(paths, filepath.Join(s.cfg.Comfy.ComfyDir, "output_workers", "gpu0", mt.OutputFile))
			}
		}
		for _, path := range paths {
			if _, err := s.remote.Run("rm -rf -- " + shellQuote(path)); err != nil {
				log.Printf("[project %d] cleanup %s failed: %v", id, path, err)
			}
		}
	}
	return nil
}

// UpdateProject 编辑项目信息（标题/题材/画风/创意）
func (s *ProjectService) UpdateProject(p *models.Project, req models.Project) error {
	updates := map[string]any{"genre": req.Genre, "style": req.Style}
	if req.Title != "" {
		updates["title"] = req.Title
	}
	if req.Genre != "" {
		updates["genre"] = req.Genre
	}
	if req.Style != "" {
		updates["style"] = req.Style
	}
	if req.Synopsis != "" {
		updates["synopsis"] = req.Synopsis
	}
	if req.Audience != "" {
		updates["audience"] = req.Audience
	}
	if req.Tone != "" {
		updates["tone"] = req.Tone
	}
	if req.Ending != "" {
		updates["ending"] = req.Ending
	}
	if req.Episodes > 0 {
		updates["episodes"] = req.Episodes
	}
	if req.AspectRatio != "" {
		updates["aspect_ratio"] = req.AspectRatio
	}
	if len(updates) == 0 {
		return nil
	}
	return s.db.Model(p).Updates(updates).Error
}

// UpdateScene 编辑场景文案。修改 image_prompt 会清空已生成画面与视频（需重新生成）；
// 仅修改 content/title 时保留画面、清空已生成视频（需重新生成视频）。
func (s *ProjectService) UpdateScene(sc *models.Scene, title, content, imagePrompt string) error {
	updates := map[string]any{}
	if title != "" {
		updates["title"] = title
	}
	if content != "" {
		updates["content"] = content
	}
	imageChanged := false
	if imagePrompt != "" {
		updates["image_prompt"] = imagePrompt
		imageChanged = sc.ImagePrompt != imagePrompt
	}
	if imageChanged {
		updates["image_file"] = ""
		updates["video_task_id"] = ""
		updates["video_file"] = ""
		updates["video_gpu"] = nil
		updates["image_retries"] = 0
		updates["video_retries"] = 0
		updates["image_token"] = ""
		updates["status"] = "pending"
		updates["error"] = ""
	} else if content != "" && sc.Content != content {
		// 只改正文：保留画面，视频需重新生成
		updates["video_task_id"] = ""
		updates["video_file"] = ""
		updates["video_gpu"] = nil
		updates["video_retries"] = 0
		if sc.VideoFile != "" || sc.VideoTaskID != "" {
			updates["status"] = "image_ready"
		}
		updates["error"] = ""
	}
	if len(updates) == 0 {
		return nil
	}
	if err := s.db.Model(sc).Updates(updates).Error; err != nil {
		return err
	}
	if (imageChanged || (content != "" && sc.Content != content)) && sc.VideoTaskID != "" && s.tasks != nil {
		if err := s.tasks.CancelTask(sc.VideoTaskID); err != nil && !strings.Contains(err.Error(), "已结束") {
			log.Printf("[scene %d] cancel stale video task %s failed: %v", sc.ID, sc.VideoTaskID, err)
		}
	}
	return nil
}

// ---------- 剧本生成（文生文） ----------

type scriptDialogue struct {
	Character string `json:"character"`
	Text      string `json:"text"`
}

type scriptScene struct {
	Title       string           `json:"title"`
	Content     string           `json:"content"`
	ImagePrompt string           `json:"image_prompt"`
	Duration    float64          `json:"duration"`
	Characters  []string         `json:"characters"`
	Dialogues   []scriptDialogue `json:"dialogues"`
}

type scriptResult struct {
	Script      string        `json:"script"`
	VisualBible string        `json:"visual_bible"`
	Scenes      []scriptScene `json:"scenes"`
}

const scriptSystemPrompt = `你是一位专业的漫剧编剧与分镜师。根据用户提供的故事创意，创作一部完整的漫剧剧本，并拆分为多个分镜场景，供后续"文生图 + 图生视频"流水线使用。
要求：
1. 只输出一个合法的 JSON 对象，不要输出任何解释、Markdown 代码块标记或其它文字。
2. JSON 结构固定为：
{
  "script": "完整剧本正文（按场景分段，含动作描写与对白）",
  "visual_bible": "主要角色的固定外貌、服装、色彩与全片统一画风；后续每个画面提示词都必须遵守",
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
3. 拆分为 6~10 个场景，每个场景是一段 3~8 秒的独立短视频片段；根据对白长度与动作复杂度设置 duration。
4. 人物一致性至关重要：同一角色在多个场景出现时，image_prompt 必须重复其外貌特征（发型、服装颜色、体型），且所有场景画风描述保持一致。
5. 每个场景必须在 characters 数组中列出该场出场的角色名（须与角色卡或创作方案中的角色名完全一致；无出场角色则为空数组）。
6. 每个场景必须在 dialogues 数组中列出该场的对白与旁白（character 为说话人角色名，空字符串表示旁白；用于配音与字幕）。无对白则为空数组。
7. 第一个场景尽量给出大场景/环境交代，后续场景聚焦人物动作与剧情推进。`

// GenerateScript 生成分镜剧本：已有创作方案时按方案渲染（两阶段流程），否则直接文生文。
// episodeN 指定当前制作集数（默认 1），只生成该集的分镜场景并替换该集旧场景。
func (s *ProjectService) GenerateScript(p *models.Project, episodeN int) (*models.Project, []models.Scene, error) {
	if episodeN <= 0 {
		episodeN = 1
	}
	var user strings.Builder
	user.WriteString("故事创意：" + p.Synopsis + "\n")
	if p.Genre != "" {
		user.WriteString("题材：" + p.Genre + "\n")
	}
	if p.Style != "" {
		user.WriteString("画风：" + p.Style + "\n")
	}

	system := scriptSystemPrompt
	if strings.TrimSpace(p.Plan) != "" {
		// 阶段 2：依据创作方案渲染分镜场景（人物外观沿用方案中的 trait/style 保持一致）
		user.WriteString("\n\n=== 创作方案 ===\n" + p.Plan + "\n")
		// 注入当前集提示词（用户可在创作方案中修改每集标题与剧情提示词）
		epTitle, epBrief := s.planEpisodeOf(p, episodeN)
		user.WriteString(fmt.Sprintf("\n=== 当前制作集 ===\n第 %d 集「%s」\n本集剧情提示词：%s\n", episodeN, epTitle, epBrief))
		user.WriteString("请基于创作方案，重点围绕「当前制作集」的剧情提示词，输出该集的分镜剧本 JSON。")
		system = scriptFromPlanSystemPrompt()
	} else {
		user.WriteString("请按系统要求输出剧本 JSON。")
	}

	raw, err := s.volc.Chat(system, user.String())
	if err != nil {
		return nil, nil, err
	}
	return s.generateScriptCore(p, episodeN, raw, resScriptHandler(true))
}

// resScriptHandler 决定落库的剧本正文来源：true 使用 LLM 输出，false 使用用户定稿剧本
func resScriptHandler(useLLM bool) func(string, string) string {
	if useLLM {
		return func(episodePrefix, resScript string) string {
			if episodePrefix != "" {
				return episodePrefix + resScript
			}
			return resScript
		}
	}
	return func(episodePrefix, userScript string) string {
		if episodePrefix != "" {
			return episodePrefix + userScript
		}
		return userScript
	}
}

// GenerateScriptFromText 用户编辑剧本定稿后，重新渲染该集分镜场景（剧本正文以用户文本为准）
func (s *ProjectService) GenerateScriptFromText(p *models.Project, episodeN int, scriptText string) (*models.Project, []models.Scene, error) {
	if strings.TrimSpace(scriptText) == "" {
		return nil, nil, fmt.Errorf("剧本内容不能为空")
	}
	if episodeN <= 0 {
		episodeN = 1
	}
	var user strings.Builder
	user.WriteString("=== 剧本正文（用户已定稿，请勿改写剧情） ===\n" + scriptText + "\n")
	if strings.TrimSpace(p.Plan) != "" {
		user.WriteString("\n\n=== 创作方案 ===\n" + p.Plan + "\n")
		user.WriteString("请保持创作方案中主要角色的人物外貌（trait）与服装（style）一致、全片画风统一，将上面的剧本正文拆分为该集的分镜场景 JSON，输出格式遵循系统要求。")
	}
	system := scriptFromPlanSystemPrompt()
	if strings.TrimSpace(p.Plan) == "" {
		system = scriptSystemPrompt
	}

	raw, err := s.volc.Chat(system, user.String())
	if err != nil {
		return nil, nil, err
	}
	return s.generateScriptCore(p, episodeN, raw, resScriptHandler(false))
}

// ExpandScript 用 AI 根据已有剧本正文扩写（丰富场景/对白/冲突），返回扩写后文本。
// 不落库，由前端回填编辑框，用户确认后再「保存并 AI 重新生成分镜」。
func (s *ProjectService) ExpandScript(p *models.Project, episodeN int, scriptText string) (string, error) {
	if strings.TrimSpace(scriptText) == "" {
		return "", fmt.Errorf("剧本内容不能为空")
	}
	epTitle, _ := s.planEpisodeOf(p, episodeN)
	if epTitle == "" {
		epTitle = p.Title
	}
	system := "你是专业的漫剧编剧。根据用户提供的本集剧本正文进行扩写：丰富场景环境描写、人物动作与表情、对白与冲突细节，保持原有剧情走向、人物关系与核心冲突不变，节奏更紧凑有张力。只输出扩写后的完整剧本正文（纯文本，按场景分段，含动作描写与对白），不要输出 JSON、Markdown 标记或任何解释。"
	user := fmt.Sprintf("第 %d 集「%s」剧本正文，请扩写：\n\n%s", episodeN, epTitle, scriptText)
	return s.volc.Chat(system, user)
}

// generateScriptCore 解析 LLM 输出并落库（替换指定集的分镜场景）
func (s *ProjectService) generateScriptCore(p *models.Project, episodeN int, raw string, scriptTextFn func(prefix, script string) string) (*models.Project, []models.Scene, error) {
	res, err := parseScriptJSON(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("剧本解析失败（可重试）: %w", err)
	}
	if err := validateScriptResult(res); err != nil {
		return nil, nil, fmt.Errorf("剧本结构不完整（可重试）: %w", err)
	}

	// 新版本落库前停止旧视频任务；旧图片请求使用令牌和版本隔离，无法回写新场景。
	var oldScenes []models.Scene
	s.db.Where("project_id = ? AND episode_n = ?", p.ID, episodeN).Find(&oldScenes)
	s.cancelProjectTasks(p.ID)
	newGeneration := p.Generation + 1

	// 事务写入剧本与场景（仅替换当前集场景）
	err = s.db.Transaction(func(tx *gorm.DB) error {
		for _, old := range oldScenes {
			if old.VideoTaskID != "" {
				if err := tx.Where("task_id = ?", old.VideoTaskID).Delete(&models.Event{}).Error; err != nil {
					return err
				}
				if err := tx.Where("task_id = ?", old.VideoTaskID).Delete(&models.Task{}).Error; err != nil {
					return err
				}
			}
		}
		if len(oldScenes) > 0 {
			oldIDs := make([]uint, 0, len(oldScenes))
			for _, o := range oldScenes {
				oldIDs = append(oldIDs, o.ID)
			}
			if err := tx.Where("project_id = ? AND source = ? AND scene_id IN ?", p.ID, "scene", oldIDs).Delete(&models.Material{}).Error; err != nil {
				return err
			}
			if err := tx.Where("scene_id IN ?", oldIDs).Delete(&models.Dialogue{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("project_id = ? AND episode_n = ?", p.ID, episodeN).Delete(&models.Scene{}).Error; err != nil {
			return err
		}
		for i, sc := range res.Scenes {
			scene := models.Scene{
				ProjectID: p.ID, EpisodeN: episodeN, Order: i + 1, Generation: newGeneration,
				Title: sc.Title, Content: sc.Content,
				ImagePrompt: sc.ImagePrompt, Duration: normalizeSceneDuration(sc.Duration), Status: "pending",
				Characters: joinSceneCharacters(sc.Characters),
			}
			if scene.Title == "" {
				scene.Title = fmt.Sprintf("场景 %d", i+1)
			}
			if err := tx.Create(&scene).Error; err != nil {
				return err
			}
			for j, d := range sc.Dialogues {
				if strings.TrimSpace(d.Text) == "" {
					continue
				}
				if err := tx.Create(&models.Dialogue{
					SceneID: scene.ID, ProjectID: p.ID, Order: j + 1,
					Character: strings.TrimSpace(d.Character), Text: strings.TrimSpace(d.Text), Status: "pending",
				}).Error; err != nil {
					return err
				}
			}
		}
		status, pipelineStage := "script_done", ""
		if p.AutoGenerate && !p.StopAfterScript {
			status, pipelineStage = "producing", "images"
		}
		if len(res.Scenes) == 0 {
			status, pipelineStage = "draft", ""
		}
		scriptText := scriptTextFn(episodePrefix(p, episodeN), res.Script)
		// 按集保存剧本：scripts map 只替换当前集
		scripts := map[int]string{}
		if strings.TrimSpace(p.Scripts) != "" {
			_ = json.Unmarshal([]byte(p.Scripts), &scripts)
		}
		scripts[episodeN] = scriptText
		scriptsJSON, _ := json.Marshal(scripts)
		return tx.Model(p).Updates(map[string]any{
			"script": scriptText, "scripts": string(scriptsJSON), "visual_bible": res.VisualBible, "status": status, "error": "", "generation": newGeneration,
			"pipeline_stage": pipelineStage,
		}).Error
	})
	if err != nil {
		return nil, nil, err
	}
	s.cleanupStaleSceneFiles(p.ID, oldScenes)

	s.pushProject(p)
	return s.GetProject(p.ID)
}

// episodePrefix 有创作方案时剧本落库带集号前缀
func episodePrefix(p *models.Project, episodeN int) string {
	if strings.TrimSpace(p.Plan) != "" {
		return fmt.Sprintf("第%d集：", episodeN)
	}
	return ""
}

// planEpisodeOf 从创作方案中提取指定集的标题与剧情提示词
func (s *ProjectService) planEpisodeOf(p *models.Project, n int) (string, string) {
	if strings.TrimSpace(p.Plan) == "" {
		return "", ""
	}
	var plan dramaPlan
	if err := json.Unmarshal([]byte(p.Plan), &plan); err != nil {
		return "", ""
	}
	for _, ep := range plan.Episodes {
		if ep.N == n {
			return ep.Title, ep.Brief
		}
	}
	return "", ""
}

func normalizeSceneDuration(duration float64) float64 {
	if duration == 0 {
		return 5
	}
	if duration < 3 {
		return 3
	}
	if duration > 8 {
		return 8
	}
	return duration
}

// aspectVideoSize 画幅 + 分辨率档位对应的视频尺寸（i2v，须 step 32 对齐）
// resolution: "720p"/"1080p"/"2k"，默认 720p；按短边定档，16:9 横屏 / 9:16 竖屏宽高互换
func aspectVideoSize(aspect, resolution string) (int, int) {
	w, h := 1280, 704 // 720p 横屏默认
	switch resolution {
	case "1080p":
		w, h = 1920, 1088
	case "2k":
		w, h = 2560, 1440
	}
	switch aspect {
	case "9:16": // 竖屏：宽高互换
		return h, w
	case "1:1":
		switch resolution {
		case "1080p":
			return 1920, 1920
		case "2k":
			return 2560, 2560
		default:
			return 1024, 1024
		}
	default: // 16:9 横屏
		return w, h
	}
}

// videoResolution 读取平台设置的视频分辨率档位（默认 720p）
func (s *ProjectService) videoResolution() string {
	var st models.Setting
	if err := s.db.Where("key = ?", "video_resolution").First(&st).Error; err == nil && st.Value != "" {
		return st.Value
	}
	return "720p"
}

// aspectImageSize 画幅对应的文生图尺寸（须满足 seedream 5.0 像素数 ≥ 3686400，且比例与视频一致避免首帧变形）
func aspectImageSize(aspect string) string {
	switch aspect {
	case "9:16":
		return "1440x2560"
	case "1:1":
		return "1920x1920"
	default:
		return "2560x1440"
	}
}

// projectImageSize 取项目画幅对应的文生图尺寸
func (s *ProjectService) projectImageSize(projectID uint) string {
	var p models.Project
	if err := s.db.First(&p, projectID).Error; err == nil {
		return aspectImageSize(p.AspectRatio)
	}
	return aspectImageSize("")
}

// sceneCharacterPortraits 收集场景出场角色的标准像（仅含已生成 Portrait 的角色，按出场顺序）
func (s *ProjectService) sceneCharacterPortraits(sc *models.Scene) []models.Character {
	names := parseSceneCharacters(sc.Characters)
	if len(names) == 0 {
		return nil
	}
	var chars []models.Character
	s.db.Where("project_id = ? AND name IN ? AND portrait != ''", sc.ProjectID, names).Find(&chars)
	if len(chars) == 0 {
		return nil
	}
	byName := make(map[string]models.Character, len(chars))
	for _, c := range chars {
		byName[c.Name] = c
	}
	ordered := make([]models.Character, 0, len(chars))
	for _, n := range names {
		if c, ok := byName[n]; ok {
			ordered = append(ordered, c)
		}
	}
	return ordered
}

// buildSceneVideoSpec 构造场景视频的模板/prompt/素材：
// 固定使用 i2v（仅首帧）：MiniMax H3 参考视频生成（ref2v）多参考图易跑偏，不再使用。
// 场景对白直接注入 prompt，让 H3 同步生成带人声的音轨（无需后期 TTS 配音）；
// 同时强制禁止画面中出现文字/字幕（字幕由合并阶段统一烧录）。
func (s *ProjectService) buildSceneVideoSpec(sc *models.Scene, pid string) (tplCode, promptText string, files map[string][]FileMeta) {
	// 强制约束（开头置顶 + 结尾重申，中英双语）：i2v 画面纯净，禁止任何文字/字幕
	const noTextPrefix = "【强制要求·画面纯净】画面中禁止出现任何文字、字幕、对话气泡、水印、台标、UI 元素或任何语言的字符；所有对白仅通过人声表达，绝不在画面上显示任何文字。\nStrict rule: NO text, subtitles, captions, speech bubbles, watermarks, logos, on-screen UI, or written characters of any language may appear in the video. All dialogue must be conveyed through voice/audio only, never rendered as on-screen text.\n\n"
	prompt := noTextPrefix
	var p models.Project
	if err := s.db.First(&p, sc.ProjectID).Error; err == nil {
		if desc := styleDescriptor(p.Style); desc != "" {
			prompt += desc
		}
	}
	if content := strings.TrimSpace(sc.Content); content != "" {
		prompt += "\n" + content
	}
	// 注入该场景对白，让 H3 生成对应人声
	var dubs []models.Dialogue
	s.db.Where("scene_id = ?", sc.ID).Order("`order`").Find(&dubs)
	if len(dubs) > 0 {
		var lines []string
		for _, d := range dubs {
			who := strings.TrimSpace(d.Character)
			if who == "" {
				who = "旁白"
			}
			lines = append(lines, who+"说："+strings.TrimSpace(d.Text))
		}
		prompt += "\n\n对白（请按顺序自然说出以下台词，生成清晰的人声音轨）：\n" + strings.Join(lines, "\n")
	}
	// 结尾再次强调无字幕约束
	prompt += "\n\n再次强调：严禁画面出现任何文字/字幕/水印/对话框，对白只用声音表达（Do NOT render any text or subtitles on screen）"

	// 固定使用 i2v：仅首帧（ref2v 多参考图易跑偏，弃用）
	return "minimax_h3_i2v", prompt, map[string][]FileMeta{
		"first_frame": {{TaskID: pid, Name: sc.ImageFile}},
	}
}

func validateScriptResult(res *scriptResult) error {
	if strings.TrimSpace(res.Script) == "" {
		return fmt.Errorf("缺少完整剧本正文")
	}
	if strings.TrimSpace(res.VisualBible) == "" {
		return fmt.Errorf("缺少角色与画风视觉基准")
	}
	if len(res.Scenes) < 6 || len(res.Scenes) > 10 {
		return fmt.Errorf("分镜数量必须为 6~10 个，实际为 %d 个", len(res.Scenes))
	}
	for i, sc := range res.Scenes {
		if strings.TrimSpace(sc.Content) == "" || strings.TrimSpace(sc.ImagePrompt) == "" {
			return fmt.Errorf("场景 %d 缺少视频或画面提示词", i+1)
		}
	}
	return nil
}

// parseScriptJSON 从模型输出中提取 JSON（剥离 markdown 代码块与前后杂文）
func parseScriptJSON(raw string) (*scriptResult, error) {
	text := strings.TrimSpace(raw)
	// 剥离 ```json ... ``` 包裹
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
	// 取第一个 { 到最后一个 }
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("输出中未找到 JSON 对象")
	}
	var res scriptResult
	if err := json.Unmarshal([]byte(text[start:end+1]), &res); err != nil {
		return nil, err
	}
	if res.Script == "" && len(res.Scenes) == 0 {
		return nil, fmt.Errorf("JSON 中缺少 script/scenes 字段")
	}
	return &res, nil
}

// ---------- 角色资产（Character Bible） ----------
//
// 把角色从创作方案文本提升为独立资产：项目内可复用的人物设定 + 标准参考像。
// 文生图时按场景出场角色注入权威 trait/style，纠偏 LLM 描述漂移，保证跨场景人物一致。

// ListCharacters 项目内角色列表
func (s *ProjectService) ListCharacters(projectID uint) ([]models.Character, error) {
	var list []models.Character
	s.db.Where("project_id = ?", projectID).Order("id").Find(&list)
	return list, nil
}

// CharacterSceneCounts 统计每个角色在场景中的出场次数（按 scene.characters 逗号分隔匹配）
func (s *ProjectService) CharacterSceneCounts(projectID uint) (map[uint]int, error) {
	var chars []models.Character
	if err := s.db.Where("project_id = ?", projectID).Find(&chars).Error; err != nil {
		return nil, err
	}
	var scenes []models.Scene
	s.db.Where("project_id = ?", projectID).Find(&scenes)
	count := map[uint]int{}
	for _, sc := range scenes {
		nameSet := map[string]bool{}
		for _, n := range parseSceneCharacters(sc.Characters) {
			nameSet[n] = true
		}
		if len(nameSet) == 0 {
			continue
		}
		for i := range chars {
			if nameSet[chars[i].Name] {
				count[chars[i].ID]++
			}
		}
	}
	return count, nil
}

// CreateCharacter 手动新建角色
func (s *ProjectService) CreateCharacter(ch models.Character) (*models.Character, error) {
	ch.Name = strings.TrimSpace(ch.Name)
	if ch.Name == "" {
		return nil, fmt.Errorf("角色名不能为空")
	}
	ch.Source = "manual"
	ch.Portrait = "" // 新建无标准像
	if err := s.db.Create(&ch).Error; err != nil {
		return nil, fmt.Errorf("角色名已存在或创建失败: %w", err)
	}
	s.pushProject(nil)
	return &ch, nil
}

// UpdateCharacter 编辑角色（改名需保证项目内唯一）；不影响已生成的标准像
func (s *ProjectService) UpdateCharacter(ch *models.Character, req models.Character) error {
	updates := map[string]any{}
	newName := strings.TrimSpace(req.Name)
	if newName != "" && newName != ch.Name {
		var cnt int64
		s.db.Model(&models.Character{}).Where("project_id = ? AND name = ? AND id != ?", ch.ProjectID, newName, ch.ID).Count(&cnt)
		if cnt > 0 {
			return fmt.Errorf("该角色名已存在")
		}
		updates["name"] = newName
	}
	updates["role"] = strings.TrimSpace(req.Role)
	updates["trait"] = strings.TrimSpace(req.Trait)
	updates["style"] = strings.TrimSpace(req.Style)
	if err := s.db.Model(ch).Updates(updates).Error; err != nil {
		return err
	}
	s.pushProject(nil)
	return nil
}

// DeleteCharacter 删除角色（同时清理远程标准像文件）
func (s *ProjectService) DeleteCharacter(projectID, id uint) error {
	var ch models.Character
	if err := s.db.Where("id = ? AND project_id = ?", id, projectID).First(&ch).Error; err != nil {
		return err
	}
	if ch.Portrait != "" && s.cfg != nil && s.remote != nil && s.upload != nil {
		path := filepath.Join(s.upload.InputDir(), fmt.Sprintf("%d", projectID), ch.Portrait)
		if _, err := s.remote.Run("rm -f -- " + shellQuote(path)); err != nil {
			log.Printf("[character %d] cleanup portrait %s failed: %v", ch.ID, path, err)
		}
	}
	if err := s.db.Where("id = ?", id).Delete(&models.Character{}).Error; err != nil {
		return err
	}
	s.pushProject(nil)
	return nil
}

// upsertCharactersFromPlan 从创作方案抽取角色为独立资产。
// 已存在同名：仅补全空字段，保留用户手动编辑与已生成标准像；新角色以 source=auto 入库。
func (s *ProjectService) upsertCharactersFromPlan(p *models.Project, plan *dramaPlan) {
	if plan == nil || len(plan.Characters) == 0 {
		return
	}
	for _, pc := range plan.Characters {
		name := strings.TrimSpace(pc.Name)
		if name == "" {
			continue
		}
		var existing models.Character
		err := s.db.Where("project_id = ? AND name = ?", p.ID, name).First(&existing).Error
		if err == nil {
			updates := map[string]any{}
			if strings.TrimSpace(existing.Role) == "" && strings.TrimSpace(pc.Role) != "" {
				updates["role"] = pc.Role
			}
			if strings.TrimSpace(existing.Trait) == "" && strings.TrimSpace(pc.Trait) != "" {
				updates["trait"] = pc.Trait
			}
			if strings.TrimSpace(existing.Style) == "" && strings.TrimSpace(pc.Style) != "" {
				updates["style"] = pc.Style
			}
			if len(updates) > 0 {
				s.db.Model(&existing).Updates(updates)
			}
			continue
		}
		s.db.Create(&models.Character{
			ProjectID: p.ID, Name: name, Role: pc.Role, Trait: pc.Trait, Style: pc.Style, Source: "auto",
		})
	}
	s.pushProject(nil)
}

// GenerateAllPortraits 一键生成全部缺少标准像的角色
func (s *ProjectService) GenerateAllPortraits(p *models.Project) (int, error) {
	var chars []models.Character
	s.db.Where("project_id = ? AND portrait = ''", p.ID).Find(&chars)
	for i := range chars {
		s.StartCharacterPortrait(&chars[i])
	}
	return len(chars), nil
}

// StartCharacterPortrait 异步生成角色标准像（文生图，限流并发）
func (s *ProjectService) StartCharacterPortrait(ch *models.Character) error {
	go func() {
		if err := s.generateCharacterPortrait(ch); err != nil {
			log.Printf("[character %d] portrait failed: %v", ch.ID, err)
		}
	}()
	return nil
}

func (s *ProjectService) generateCharacterPortrait(ch *models.Character) error {
	if s.volc == nil || s.upload == nil {
		return fmt.Errorf("角色标准像生成依赖文生图与存储服务")
	}
	select {
	case s.imageSem <- struct{}{}:
		defer func() { <-s.imageSem }()
	case <-s.stopped:
		return fmt.Errorf("服务已停止")
	}
	var p models.Project
	if err := s.db.First(&p, ch.ProjectID).Error; err != nil {
		return err
	}
	data, err := s.volc.GenerateImage(buildPortraitPrompt(&p, ch), "")
	if err != nil {
		return err
	}
	ext := detectImageExt(data)
	name := fmt.Sprintf("char_%d_%d%s", ch.ID, time.Now().UnixNano(), ext)
	path, _, err := s.upload.SaveFile(fmt.Sprintf("%d", ch.ProjectID), "image", name, data)
	if err != nil {
		return err
	}
	if err := s.db.Model(&models.Character{}).Where("id = ?", ch.ID).Update("portrait", filepath.Base(path)).Error; err != nil {
		return err
	}
	s.pushProject(nil)
	return nil
}

// styleDescriptor 把项目画风转换为强约束描述，用于角色标准像/分镜画面/视频 prompt 统一注入，
// 保证全链路风格一致（真人风格不会被稀释成动漫/插画）。
func styleDescriptor(style string) string {
	s := strings.TrimSpace(style)
	if s == "" {
		return ""
	}
	lower := strings.ToLower(s)
	switch {
	case strings.Contains(lower, "真人") || strings.Contains(lower, "写实") || strings.Contains(lower, "real"):
		return "超写实真人照片风格：真实摄影质感、自然皮肤纹理与光影、人物与场景均为真实照片，严禁任何动漫/卡通/插画/绘画风格"
	case strings.Contains(lower, "国漫"):
		return "国漫插画风格：国风动漫插画质感、线条与色彩统一、角色为动漫形象，严禁真实照片风格"
	case strings.Contains(lower, "日漫"):
		return "日式动漫风格：日式动画插画质感、大眼睛角色、统一线条与上色，严禁真实照片风格"
	case strings.Contains(lower, "韩漫"):
		return "韩漫风格：精致唯美漫画插画、柔和光感、统一画风，严禁真实照片风格"
	case strings.Contains(lower, "3d") || strings.Contains(lower, "三维"):
		return "3D动画电影风格：皮克斯式三维渲染质感、柔和光影、统一材质，严禁真实照片与二维插画风格"
	case strings.Contains(lower, "水墨"):
		return "中国水墨画风格：写意山水笔触、墨色晕染、留白构图，严禁写实照片风格"
	case strings.Contains(lower, "像素"):
		return "像素复古游戏风格：8-bit 像素块质感、统一调色板，严禁写实照片风格"
	case strings.Contains(lower, "赛博") || strings.Contains(lower, "cyber"):
		return "赛博朋克风格：霓虹灯光、未来都市、高对比色调，画面为数字插画质感，严禁真实照片风格"
	default:
		return "全片统一采用「" + s + "」画风：所有角色、场景、光影严格保持该风格一致，严禁混入其他画风"
	}
}

func buildPortraitPrompt(p *models.Project, ch *models.Character) string {
	parts := make([]string, 0, 4)
	if desc := styleDescriptor(p.Style); desc != "" {
		parts = append(parts, desc)
	}
	if strings.TrimSpace(ch.Trait) != "" {
		parts = append(parts, ch.Trait)
	}
	if strings.TrimSpace(ch.Style) != "" {
		parts = append(parts, ch.Style)
	}
	parts = append(parts, "角色标准像，半身肖像，正面，中性纯色背景，高质量，居中构图")
	return strings.Join(parts, "，")
}

// characterContextForScene 拼装场景出场角色的权威设定文本，前置注入以纠偏 LLM 描述漂移
func (s *ProjectService) characterContextForScene(sc *models.Scene) string {
	names := parseSceneCharacters(sc.Characters)
	if len(names) == 0 {
		return ""
	}
	var chars []models.Character
	s.db.Where("project_id = ? AND name IN ?", sc.ProjectID, names).Find(&chars)
	if len(chars) == 0 {
		return ""
	}
	byName := make(map[string]models.Character, len(chars))
	for _, c := range chars {
		byName[c.Name] = c
	}
	lines := make([]string, 0, len(names))
	for _, n := range names {
		c, ok := byName[n]
		if !ok {
			continue
		}
		// 注入完整外貌：trait（五官/发型/体型）+ style（默认服装/特征），保证人物一致。
		// 图生图模式下角色标准像已锁定主体形象，文本描述进一步强化五官与服装一致性。
		desc := strings.TrimSpace(c.Trait)
		if st := strings.TrimSpace(c.Style); st != "" {
			if desc != "" {
				desc += "；"
			}
			desc += st
		}
		if desc == "" {
			continue
		}
		lines = append(lines, "- "+c.Name+"："+desc)
	}
	if len(lines) == 0 {
		return ""
	}
	return "【角色设定（必须严格遵守，保证人物一致）】\n" + strings.Join(lines, "\n")
}

// buildSceneImagePrompt 拼装场景文生图提示词：强画风约束 → 画风/视觉基准 → 角色设定 → 当前分镜
func (s *ProjectService) buildSceneImagePrompt(sc *models.Scene) string {
	parts := make([]string, 0, 5)
	var p models.Project
	if err := s.db.First(&p, sc.ProjectID).Error; err == nil {
		if desc := styleDescriptor(p.Style); desc != "" {
			parts = append(parts, desc)
		}
		if p.Style != "" {
			parts = append(parts, "画风："+p.Style)
		}
		if vb := strings.TrimSpace(p.VisualBible); vb != "" {
			parts = append(parts, "全片视觉基准："+vb)
		}
	}
	if charCtx := s.characterContextForScene(sc); charCtx != "" {
		parts = append(parts, charCtx)
	}
	if prompt := strings.TrimSpace(sc.ImagePrompt); prompt != "" {
		parts = append(parts, "当前分镜："+prompt)
	}
	return strings.Join(parts, "\n")
}

// parseSceneCharacters 解析场景出场角色（逗号分隔）
func parseSceneCharacters(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// joinSceneCharacters 角色名切片 → 逗号分隔字符串
func joinSceneCharacters(names []string) string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return strings.Join(out, ",")
}

// ---------- 分镜画面生成（文生图） ----------

// GenerateSceneImage 生成单个场景画面：文生图 → 保存到 input/<project_id>/scene_N.png
func (s *ProjectService) GenerateSceneImage(sc *models.Scene) error {
	token, err := s.claimSceneImage(sc)
	if err != nil {
		return err
	}
	return s.generateClaimedSceneImage(sc, token)
}

// StartSceneImage 在返回 API 响应前完成原子占位，避免重复请求都收到成功响应。
func (s *ProjectService) StartSceneImage(sc *models.Scene) error {
	token, err := s.claimSceneImage(sc)
	if err != nil {
		return err
	}
	go func() {
		if err := s.generateClaimedSceneImage(sc, token); err != nil {
			log.Printf("[project %d] scene %d image failed: %v", sc.ProjectID, sc.Order, err)
		}
	}()
	return nil
}

func (s *ProjectService) claimSceneImage(sc *models.Scene) (string, error) {
	token := fmt.Sprintf("%d-%d", sc.ID, time.Now().UnixNano())
	claim := s.db.Model(&models.Scene{}).
		Where("id = ? AND project_id = ? AND generation = ? AND image_token = '' AND status IN ?",
			sc.ID, sc.ProjectID, sc.Generation, []string{"pending", "failed", "image_ready", "video_ready"}).
		Updates(map[string]any{
			"status": "image_pending", "error": "", "image_token": token,
			"image_retries": gorm.Expr("image_retries + 1"),
			"video_task_id": "", "video_file": "", "video_gpu": nil,
		})
	if claim.Error != nil {
		return "", claim.Error
	}
	if claim.RowsAffected == 0 {
		return "", fmt.Errorf("场景 %d 已在生成或状态已变化", sc.Order)
	}
	sc.ImageToken = token
	s.pushProject(nil)
	return token, nil
}

func (s *ProjectService) generateClaimedSceneImage(sc *models.Scene, token string) error {
	// 并发限制：火山文生图 QPS 有限，超出会触发限流
	select {
	case s.imageSem <- struct{}{}:
		defer func() { <-s.imageSem }()
	case <-s.stopped:
		return fmt.Errorf("服务已停止")
	}

	prompt := s.buildSceneImagePrompt(sc)
	if prompt == "" {
		prompt = sc.Content
	}
	if prompt == "" {
		s.failSceneImage(sc, token, "场景缺少提示词")
		return fmt.Errorf("场景缺少提示词")
	}

	// 注入出场角色标准像作为图生图底图（subject 主体参考，多角色传多张），强制人物形象一致
	refs, names, err := s.scenePortraitRefs(sc)
	if err != nil {
		log.Printf("[project %d] scene %d portrait refs failed: %v", sc.ProjectID, sc.Order, err)
	}
	if len(refs) > 0 {
		prompt += "\n参考图说明：\n" + strings.Join(names, "\n") +
			"\n请严格保持图1至图" + fmt.Sprintf("%d", len(refs)) + " 中各人物的外貌完全一致（五官、发型、体型、服装），仅按描述调整场景、构图与动作，禁止改变人物形象。"
	}

	data, err := s.volc.GenerateImageRefs(prompt, s.projectImageSize(sc.ProjectID), refs)
	if err != nil {
		s.failSceneImage(sc, token, err.Error())
		return err
	}
	ext := detectImageExt(data)
	name := fmt.Sprintf("scene_g%d_%d_%d%s", sc.Generation, sc.Order, time.Now().UnixNano(), ext)
	taskID := fmt.Sprintf("%d", sc.ProjectID)
	path, size, err := s.upload.SaveFile(taskID, "image", name, data)
	if err != nil {
		s.failSceneImage(sc, token, "保存图片失败: "+err.Error())
		return err
	}
	updated := s.db.Model(&models.Scene{}).Where("id = ? AND image_token = ? AND generation = ?", sc.ID, token, sc.Generation).
		Updates(map[string]any{
			"image_file": filepath.Base(path), "status": "image_ready", "error": "", "image_token": "",
		})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected == 0 {
		return fmt.Errorf("场景 %d 生成结果已过期", sc.Order)
	}
	// 自动入库素材库
	if s.materials != nil {
		s.materials.SaveGeneratedImage(sc, path, size)
	}
	s.updateProjectStatus(sc.ProjectID)
	s.pushProject(nil)
	return nil
}

// GenerateAllImages 生成项目内所有 pending 场景画面（限流并发）
func (s *ProjectService) GenerateAllImages(p *models.Project, episodeN int) (int, error) {
	var scenes []models.Scene
	query := s.db.Where("project_id = ? AND status = ?", p.ID, "pending")
	if episodeN > 0 {
		query = query.Where("episode_n = ?", episodeN)
	}
	query.Order("`order`").Find(&scenes)
	if len(scenes) > 0 {
		s.db.Model(p).Updates(map[string]any{"status": "producing", "error": ""})
	}
	started := 0
	for i := range scenes {
		if err := s.StartSceneImage(&scenes[i]); err != nil {
			log.Printf("[project %d] scene %d image failed: %v", p.ID, scenes[i].Order, err)
			continue
		}
		started++
	}
	return started, nil
}

// scenePortraitRefs 加载场景出场角色的标准像，转为图生图参考图列表（方舟 image 字段，多角色传多张）。
// 返回 (参考图列表, 与参考图顺序一一对应的「参考图 N → 角色名」描述行)，供提示词关联角色。
func (s *ProjectService) scenePortraitRefs(sc *models.Scene) ([]ImageRef, []string, error) {
	chars := s.sceneCharacterPortraits(sc)
	if len(chars) == 0 {
		return nil, nil, nil
	}
	refs := make([]ImageRef, 0, len(chars))
	lines := make([]string, 0, len(chars))
	for _, ch := range chars {
		if ch.Portrait == "" {
			continue
		}
		abs := filepath.Join(s.cfg.Comfy.ComfyDir, "input", fmt.Sprintf("%d", sc.ProjectID), ch.Portrait)
		f, err := s.remote.Open(abs)
		if err != nil {
			log.Printf("[project %d] open portrait %s: %v", sc.ProjectID, abs, err)
			continue
		}
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			continue
		}
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(ch.Portrait)), ".")
		if ext == "jpg" {
			ext = "jpeg"
		}
		if ext != "jpeg" && ext != "png" && ext != "webp" {
			ext = "jpeg"
		}
		refs = append(refs, ImageRef{URL: fmt.Sprintf("data:image/%s;base64,%s", ext, base64.StdEncoding.EncodeToString(data))})
		lines = append(lines, fmt.Sprintf("- 图%d：%s（%s）", len(refs), ch.Name, strings.TrimSpace(ch.Trait)))
	}
	return refs, lines, nil
}

func (s *ProjectService) failScene(sc *models.Scene, errMsg string) {
	s.db.Model(sc).Updates(map[string]any{"status": "failed", "error": errMsg})
	s.updateProjectStatus(sc.ProjectID)
	s.pushProject(nil)
}

func (s *ProjectService) failSceneImage(sc *models.Scene, token, errMsg string) {
	// 重新生成失败时保留旧画面（回退 image_ready）；首次生成失败则 failed
	status := "failed"
	if sc.ImageFile != "" {
		status = "image_ready"
	}
	s.db.Model(&models.Scene{}).Where("id = ? AND image_token = ?", sc.ID, token).
		Updates(map[string]any{"status": status, "error": errMsg, "image_token": ""})
	s.updateProjectStatus(sc.ProjectID)
	s.pushProject(nil)
}

// ---------- 场景视频生成（本地 L40 / MiniMax H3 i2v） ----------

// GenerateSceneVideo 创建场景视频任务：首帧图 = 文生图画面，提示词 = 场景正文
func (s *ProjectService) GenerateSceneVideo(p *models.Project, sc *models.Scene) error {
	if sc.ImageFile == "" {
		return fmt.Errorf("场景 %d 尚未生成画面", sc.Order)
	}
	claim := s.db.Model(&models.Scene{}).
		Where("id = ? AND project_id = ? AND generation = ? AND status IN ? AND image_file != ''",
			sc.ID, p.ID, sc.Generation, []string{"image_ready", "video_ready", "failed"}).
		Updates(map[string]any{"status": "video_creating", "error": "", "video_task_id": "", "video_file": "", "video_gpu": nil})
	if claim.Error != nil {
		return claim.Error
	}
	if claim.RowsAffected == 0 {
		return fmt.Errorf("场景 %d 已在生成或状态已变化", sc.Order)
	}
	videoW, videoH := aspectVideoSize(p.AspectRatio, s.videoResolution())
	pid := fmt.Sprintf("%d", p.ID)
	tplCode, promptText, videoFiles := s.buildSceneVideoSpec(sc, pid)
	var tpl models.Template
	if err := s.db.Where("code = ?", tplCode).First(&tpl).Error; err != nil {
		return fmt.Errorf("未找到视频模板 %s，请检查系统模板", tplCode)
	}
	task, err := s.tasks.CreateTask(CreateTaskReq{
		TemplateID: tpl.ID,
		Prompt:     promptText,
		Params: map[string]any{
			"width": videoW, "height": videoH, "duration": normalizeSceneDuration(sc.Duration),
			"steps": 20, "cfg": 1.0, "fps": 24, "seed": -1, "ref_image_size": "match",
		},
		Files: videoFiles,
	})
	if err != nil {
		s.failScene(sc, "创建视频任务失败: "+err.Error())
		return err
	}
	updated := s.db.Model(&models.Scene{}).Where("id = ? AND generation = ? AND status = ?", sc.ID, sc.Generation, "video_creating").Updates(map[string]any{
		"video_task_id": task.TaskID, "status": "video_pending", "error": "",
	})
	if updated.Error != nil || updated.RowsAffected == 0 {
		_ = s.tasks.CancelTask(task.TaskID)
		if updated.Error != nil {
			return updated.Error
		}
		return fmt.Errorf("场景 %d 视频任务已过期", sc.Order)
	}
	go func() {
		// 视频生成并发由 TaskService 全局限流（video_concurrency 平台设置），此处直接提交
		if err := s.tasks.Execute(task.TaskID); err != nil {
			log.Printf("[project %d] scene %d video execute failed: %v", p.ID, sc.Order, err)
		}
	}()
	s.pushProject(nil)
	return nil
}

// GenerateAllVideos 为所有画面就绪的场景创建视频任务
func (s *ProjectService) GenerateAllVideos(p *models.Project, episodeN int) (int, error) {
	var scenes []models.Scene
	query := s.db.Where("project_id = ? AND status IN ?", p.ID, []string{"image_ready", "failed"})
	if episodeN > 0 {
		query = query.Where("episode_n = ?", episodeN)
	}
	query.Order("`order`").Find(&scenes)
	if len(scenes) > 0 {
		s.db.Model(p).Updates(map[string]any{"status": "producing", "error": ""})
	}
	created := 0
	for i := range scenes {
		if err := s.GenerateSceneVideo(p, &scenes[i]); err != nil {
			log.Printf("[project %d] scene %d video failed: %v", p.ID, scenes[i].Order, err)
			continue
		}
		created++
	}
	return created, nil
}

// CancelSceneVideo 停止场景视频生成：取消 ComfyUI 任务并回退到画面就绪（不会自动重试）
func (s *ProjectService) CancelSceneVideo(p *models.Project, sc *models.Scene) error {
	if sc.VideoTaskID != "" {
		_ = s.tasks.CancelTask(sc.VideoTaskID)
	}
	updated := s.db.Model(&models.Scene{}).
		Where("id = ? AND project_id = ? AND generation = ? AND status IN ?",
			sc.ID, p.ID, sc.Generation, []string{"video_creating", "video_pending", "video_running"}).
		Updates(map[string]any{
			"status": "image_ready", "error": "用户停止生成",
			"video_task_id": "", "video_file": "", "video_gpu": nil,
		})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected == 0 {
		return fmt.Errorf("场景 %d 当前无进行中的视频生成", sc.Order)
	}
	s.updateProjectStatus(p.ID)
	s.pushProject(nil)
	return nil
}

// WatchSceneVideos 后台轮询场景视频任务状态（3s），任务完成后回写场景
func (s *ProjectService) WatchSceneVideos() {
	s.recoverInterruptedProjects()
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-s.stopped:
				return
			case <-ticker.C:
				s.syncSceneVideos()
				s.advanceAutoPipelines()
			}
		}
	}()
}

func (s *ProjectService) recoverInterruptedProjects() {
	s.db.Model(&models.Scene{}).Where("status = ?", "image_pending").
		Updates(map[string]any{"status": "pending", "image_token": "", "error": "服务重启，画面任务已重新排队"})
	s.db.Model(&models.Scene{}).Where("status = ?", "video_creating").
		Updates(map[string]any{"status": "image_ready", "error": "服务重启，视频任务已重新排队"})
	// video_pending/video_running 在重启后其 ComfyUI 任务已失活（进程内 goroutine 不会恢复），回退到 image_ready 重新排队
	s.db.Model(&models.Scene{}).Where("status IN ?", []string{"video_pending", "video_running"}).
		Updates(map[string]any{"status": "image_ready", "video_task_id": "", "video_file": "", "video_gpu": nil, "error": "服务重启，视频任务已重新排队"})
	s.db.Model(&models.MergeTask{}).Where("status = ?", "running").Update("status", "pending")
	s.db.Model(&models.Project{}).Where("pipeline_stage = ?", "script_running").Update("pipeline_stage", "script")
	s.db.Model(&models.Project{}).Where("pipeline_stage = ?", "plan_running").Update("pipeline_stage", "plan")
	s.db.Model(&models.Project{}).Where("pipeline_stage = ?", "script_manual").
		Updates(map[string]any{"pipeline_stage": "", "auto_generate": false, "error": "服务重启，剧本生成已中断，请重试"})
}

func (s *ProjectService) advanceAutoPipelines() {
	var projects []models.Project
	if err := s.db.Where("auto_generate = ? AND pipeline_stage IN ?", true,
		[]string{"plan", "script", "images", "videos", "merge"}).Find(&projects).Error; err != nil {
		return
	}
	for _, p := range projects {
		go s.advancePipeline(p.ID)
	}
}

func (s *ProjectService) advancePipeline(projectID uint) {
	var p models.Project
	if err := s.db.First(&p, projectID).Error; err != nil || !p.AutoGenerate {
		return
	}
	episodeN := p.PipelineEpisode
	if episodeN <= 0 {
		episodeN = 1
	}
	switch p.PipelineStage {
	case "plan":
		claim := s.db.Model(&models.Project{}).Where("id = ? AND pipeline_stage = ?", p.ID, "plan").
			Update("pipeline_stage", "plan_running")
		if claim.Error != nil || claim.RowsAffected == 0 {
			return
		}
		p.PipelineStage = "plan_running"
		fresh, err := s.GeneratePlan(&p)
		if err != nil {
			s.failPipeline(p.ID, err, true)
			return
		}
		// 触发角色标准像生成（异步，供后续 ref2v 视频锁定人物）
		s.GenerateAllPortraits(fresh)
		s.db.Model(&models.Project{}).Where("id = ? AND pipeline_stage = ?", p.ID, "plan_running").Update("pipeline_stage", "script")
		go s.advancePipeline(fresh.ID)
	case "script":
		claim := s.db.Model(&models.Project{}).Where("id = ? AND pipeline_stage = ?", p.ID, "script").
			Update("pipeline_stage", "script_running")
		if claim.Error != nil || claim.RowsAffected == 0 {
			return
		}
		p.PipelineStage = "script_running"
		if _, _, err := s.GenerateScript(&p, episodeN); err != nil {
			s.failPipeline(p.ID, err, true)
			return
		}
		if p.StopAfterScript {
			// 创建项目自动触发：只生成第一集剧本，后续画面/视频/合并由人工处理
			s.db.Model(&models.Project{}).Where("id = ?", p.ID).Updates(map[string]any{
				"pipeline_stage": "", "auto_generate": false,
				"status": "script_done", "error": "",
			})
			s.pushProject(nil)
			return
		}
		go s.advancePipeline(p.ID)
	case "images":
		var scenes []models.Scene
		if err := s.db.Where("project_id = ? AND episode_n = ? AND generation = ?", p.ID, episodeN, p.Generation).Order("`order`").Find(&scenes).Error; err != nil || len(scenes) == 0 {
			s.failPipeline(p.ID, fmt.Errorf("没有可生成的分镜场景"), true)
			return
		}
		// 文生图失败自动重试一次；达到上限后停止流水线并保留逐场景重试入口。
		s.db.Model(&models.Scene{}).Where("project_id = ? AND episode_n = ? AND generation = ? AND status = ? AND image_file = '' AND image_retries < ?",
			p.ID, episodeN, p.Generation, "failed", 2).Updates(map[string]any{"status": "pending", "error": ""})
		var failed int64
		s.db.Model(&models.Scene{}).Where("project_id = ? AND episode_n = ? AND generation = ? AND status = ?", p.ID, episodeN, p.Generation, "failed").Count(&failed)
		if failed > 0 {
			s.failPipeline(p.ID, fmt.Errorf("%d 个分镜画面生成失败，请修正后重试", failed), true)
			return
		}
		var waiting int64
		s.db.Model(&models.Scene{}).Where("project_id = ? AND episode_n = ? AND generation = ? AND status IN ?", p.ID, episodeN, p.Generation,
			[]string{"pending", "image_pending"}).Count(&waiting)
		if waiting > 0 {
			_, _ = s.GenerateAllImages(&p, episodeN)
			return
		}
		s.db.Model(&models.Project{}).Where("id = ? AND pipeline_stage = ?", p.ID, "images").Update("pipeline_stage", "videos")
		go s.advancePipeline(p.ID)
	case "videos":
		// 确保角色标准像就绪（ref2v 锁角色依赖）：未就绪则触发并等待下一轮，避免系统性退化为 i2v
		var pendingPortraits int64
		s.db.Model(&models.Character{}).Where("project_id = ? AND portrait = ''", p.ID).Count(&pendingPortraits)
		if pendingPortraits > 0 {
			s.GenerateAllPortraits(&p)
			return
		}
		var failed int64
		s.db.Model(&models.Scene{}).Where("project_id = ? AND episode_n = ? AND generation = ? AND status = ?", p.ID, episodeN, p.Generation, "failed").Count(&failed)
		if failed > 0 {
			s.failPipeline(p.ID, fmt.Errorf("%d 个场景视频生成失败，请修正后重试", failed), true)
			return
		}
		var ready, total, waiting int64
		s.db.Model(&models.Scene{}).Where("project_id = ? AND episode_n = ? AND generation = ?", p.ID, episodeN, p.Generation).Count(&total)
		s.db.Model(&models.Scene{}).Where("project_id = ? AND episode_n = ? AND generation = ? AND status = ?", p.ID, episodeN, p.Generation, "video_ready").Count(&ready)
		if total > 0 && ready == total {
			// 触发对白配音（异步，不阻塞合并）
			s.GenerateProjectDubs(&p)
			s.db.Model(&models.Project{}).Where("id = ? AND pipeline_stage = ?", p.ID, "videos").Update("pipeline_stage", "merge")
			go s.advancePipeline(p.ID)
			return
		}
		s.db.Model(&models.Scene{}).Where("project_id = ? AND episode_n = ? AND generation = ? AND status IN ?", p.ID, episodeN, p.Generation,
			[]string{"video_creating", "video_pending", "video_running"}).Count(&waiting)
		if waiting == 0 {
			_, _ = s.GenerateAllVideos(&p, episodeN)
		}
	case "merge":
		var mt models.MergeTask
		err := s.db.Where("project_id = ? AND episode_n = ? AND generation = ?", p.ID, episodeN, p.Generation).Order("id desc").First(&mt).Error
		if err == gorm.ErrRecordNotFound {
			var scenes []models.Scene
			if err := s.db.Where("project_id = ? AND episode_n = ? AND generation = ? AND status = ?", p.ID, episodeN, p.Generation, "video_ready").Order("`order`").Find(&scenes).Error; err != nil {
				s.failPipeline(p.ID, err, true)
				return
			}
			ids := make([]uint, len(scenes))
			for i := range scenes {
				ids[i] = scenes[i].ID
			}
			if _, err := s.CreateMergeTask(&p, ids, true, true); err != nil {
				s.failPipeline(p.ID, err, true)
			}
			return
		}
		if err != nil {
			s.failPipeline(p.ID, err, true)
			return
		}
		switch mt.Status {
		case "pending":
			go s.runMerge(&p, &mt, true, true)
		case "failed":
			s.failPipeline(p.ID, fmt.Errorf("合并成片失败: %s", mt.Error), true)
		case "success":
			s.db.Model(&models.Project{}).Where("id = ?", p.ID).Updates(map[string]any{
				"status": "finished", "pipeline_stage": "finished", "auto_generate": false, "error": "",
			})
		}
	}
}

func (s *ProjectService) failPipeline(projectID uint, err error, auto bool) {
	updates := map[string]any{"error": err.Error(), "auto_generate": false}
	if auto {
		updates["status"] = "failed"
		updates["pipeline_stage"] = "failed"
	} else {
		updates["pipeline_stage"] = ""
		// 手工重新生成失败时保留原有分镜的可用状态。
		var count int64
		s.db.Model(&models.Scene{}).Where("project_id = ?", projectID).Count(&count)
		if count == 0 {
			updates["status"] = "failed"
		}
	}
	s.db.Model(&models.Project{}).Where("id = ?", projectID).Updates(updates)
	s.pushProject(nil)
}

func (s *ProjectService) syncSceneVideos() {
	var scenes []models.Scene
	if err := s.db.Where("status IN ?", []string{"video_pending", "video_running"}).
		Find(&scenes).Error; err != nil || len(scenes) == 0 {
		return
	}
	changed := false
	for i := range scenes {
		sc := &scenes[i]
		if sc.VideoTaskID == "" {
			s.db.Model(sc).Updates(map[string]any{"status": "image_ready", "error": "视频任务缺失"})
			changed = true
			continue
		}
		var task models.Task
		if err := s.db.Where("task_id = ?", sc.VideoTaskID).First(&task).Error; err != nil {
			continue
		}
		switch task.Status {
		case "running", "queued":
			if sc.Status != "video_running" {
				s.db.Model(sc).Updates(map[string]any{"status": "video_running"})
				changed = true
			}
		case "success":
			file, gpu := resultVideoOf(&task)
			if file == "" || gpu == nil {
				s.retryOrFailVideo(sc, "任务成功但未返回视频文件或 GPU 信息")
				changed = true
				continue
			}
			s.db.Model(sc).Updates(map[string]any{
				"status": "video_ready", "error": "", "video_file": file, "video_gpu": *gpu,
			})
			changed = true
		case "failed", "cancelled":
			msg := task.Error
			if msg == "" {
				msg = task.Status
			}
			s.retryOrFailVideo(sc, msg)
			changed = true
		}
	}
	if changed {
		s.pushProject(nil)
		s.updateProjectStatusForAll()
	}
}

func (s *ProjectService) retryOrFailVideo(sc *models.Scene, msg string) {
	if sc.VideoRetries < 2 {
		retries := sc.VideoRetries + 1
		s.db.Model(sc).Updates(map[string]any{
			"status": "image_ready", "video_retries": retries, "error": "",
			"video_task_id": "", "video_file": "", "video_gpu": nil,
		})
		go func(scene models.Scene) {
			var p models.Project
			if err := s.db.First(&p, scene.ProjectID).Error; err == nil {
				scene.Status = "image_ready"
				_ = s.GenerateSceneVideo(&p, &scene)
			}
		}(*sc)
		log.Printf("[project %d] scene %d video 失败自动重试 %d/2: %s", sc.ProjectID, sc.Order, retries, msg)
		return
	}
	s.db.Model(sc).Updates(map[string]any{
		"status": "failed", "error": "视频生成失败: " + msg,
	})
}

// updateProjectStatusForAll 扫描含活动场景的项目并推进状态
func (s *ProjectService) updateProjectStatusForAll() {
	var projects []models.Project
	if err := s.db.Where("status IN ?", []string{"draft", "script_done", "producing", "ready", "failed"}).
		Find(&projects).Error; err != nil {
		return
	}
	for _, p := range projects {
		s.updateProjectStatus(p.ID)
	}
}

// updateProjectStatus 根据场景完成度推进项目状态：
// 全部视频就绪 → ready；有正在进行的制作 → producing；全部失败 → failed
func (s *ProjectService) updateProjectStatus(projectID uint) {
	var scenes []models.Scene
	if err := s.db.Where("project_id = ?", projectID).Find(&scenes).Error; err != nil || len(scenes) == 0 {
		return
	}
	ready, failed, active := 0, 0, 0
	for _, sc := range scenes {
		switch sc.Status {
		case "video_ready":
			ready++
		case "failed":
			failed++
		default:
			active++
		}
	}
	status := ""
	switch {
	case ready == len(scenes):
		status = "ready"
	case failed == len(scenes):
		status = "failed"
	case active > 0 || ready > 0:
		status = "producing"
	}
	if status != "" {
		s.db.Model(&models.Project{}).Where("id = ?", projectID).
			Where("status != ? AND COALESCE(pipeline_stage, '') != ?", "finished", "failed").Update("status", status)
	}
}

// resultVideoOf 从任务结果中提取第一个视频文件（subfolder/filename）与 GPU
// isVideoExt 判断视频扩展名（minimax SaveVideo 输出 type 标为 images，但文件实为 mp4）
func isVideoExt(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp4", ".webm", ".mov", ".mkv", ".gif":
		return true
	}
	return false
}

// resultVideoOf 从任务结果中提取第一个视频文件（subfolder/filename）与 GPU。
// minimax H3 的 SaveVideo 在 ComfyUI outputs 里 type 标为 "images"（文件实为 mp4），故按扩展名兼容。
func resultVideoOf(task *models.Task) (string, *int) {
	var files []map[string]string
	if err := json.Unmarshal([]byte(task.ResultFiles), &files); err != nil {
		return "", nil
	}
	for _, f := range files {
		if f["type"] == "videos" || (f["type"] == "images" && isVideoExt(f["filename"])) {
			sub, name := f["subfolder"], f["filename"]
			if sub != "" {
				return sub + "/" + name, task.GPUIndex
			}
			return name, task.GPUIndex
		}
	}
	return "", nil
}

// ---------- 视频合并（ffmpeg concat） ----------

// CreateMergeTask 创建合并任务
func (s *ProjectService) CreateMergeTask(p *models.Project, sceneIDs []uint, dub, subtitles bool) (*models.MergeTask, error) {
	if len(sceneIDs) < 2 {
		return nil, fmt.Errorf("请至少选择 2 个场景进行合并")
	}
	// 防同代重复创建（advancePipeline 并发兜底，避免双 goroutine 各建一条 MergeTask）
	var dup int64
	s.db.Model(&models.MergeTask{}).Where("project_id = ? AND generation = ? AND status IN ?", p.ID, p.Generation, []string{"pending", "running"}).Count(&dup)
	if dup > 0 {
		return nil, fmt.Errorf("已有进行中的合并任务，请等待完成")
	}
	// 校验所有场景视频就绪
	for _, id := range sceneIDs {
		var sc models.Scene
		if err := s.db.First(&sc, id).Error; err != nil {
			return nil, fmt.Errorf("场景 %d 不存在", id)
		}
		if sc.ProjectID != p.ID {
			return nil, fmt.Errorf("场景 %d 不属于该项目", id)
		}
		if sc.Status != "video_ready" || sc.VideoFile == "" {
			return nil, fmt.Errorf("场景 %d 的视频尚未完成，请先生成视频", sc.Order)
		}
	}
	ids := make([]string, len(sceneIDs))
	for i, id := range sceneIDs {
		ids[i] = fmt.Sprintf("%d", id)
	}
	// 记录所属集号（取第一个场景的集号）
	episodeN := 1
	if len(sceneIDs) > 0 {
		var first models.Scene
		if err := s.db.First(&first, sceneIDs[0]).Error; err == nil && first.EpisodeN > 0 {
			episodeN = first.EpisodeN
		}
	}
	mt := models.MergeTask{
		ProjectID: p.ID, EpisodeN: episodeN, Title: fmt.Sprintf("第%d集 · %s", episodeN, p.Title),
		SceneOrder: strings.Join(ids, ","), Status: "pending", Generation: p.Generation,
	}
	if err := s.db.Create(&mt).Error; err != nil {
		return nil, err
	}
	go s.runMerge(p, &mt, dub, subtitles)
	s.pushProject(p)
	return &mt, nil
}

// runMerge 后台执行合并：远程 ffmpeg filter_complex concat → output_workers/gpu0/merged/
// dub=true 保留各场景 H3 原音轨（对白已在生成时同步进视频），subtitles=true 按场景时长均分生成 SRT 字幕并烧录进画面。
func (s *ProjectService) runMerge(p *models.Project, mt *models.MergeTask, dub, subtitles bool) {
	claim := s.db.Model(&models.MergeTask{}).Where("id = ? AND status = ?", mt.ID, "pending").Update("status", "running")
	if claim.Error != nil || claim.RowsAffected == 0 {
		return
	}

	ids := strings.Split(mt.SceneOrder, ",")
	inputs := make([]string, 0, len(ids))
	filters := make([]string, 0, len(ids))
	audioCount := 0
	videoDurs := make([]sceneVideo, 0, len(ids))
	scenes := make([]models.Scene, 0, len(ids))
	for i, idStr := range ids {
		id, _ := strconv.Atoi(idStr)
		var sc models.Scene
		if err := s.db.First(&sc, id).Error; err != nil || sc.VideoFile == "" || sc.VideoGPU == nil {
			s.failMerge(mt, p, fmt.Sprintf("场景 %d 视频信息缺失", id))
			return
		}
		abs := filepath.Join(s.cfg.Comfy.ComfyDir, "output_workers", fmt.Sprintf("gpu%d", *sc.VideoGPU), sc.VideoFile)
		inputs = append(inputs, "-i", shellQuote(abs))
		hasAudio, err := s.remoteFileHasAudio(abs)
		if err != nil {
			s.failMerge(mt, p, err.Error())
			return
		}
		if hasAudio {
			audioCount++
		}
		filters = append(filters, fmt.Sprintf("[%d:v]", i))
		dur := normalizeSceneDuration(sc.Duration)
		if mi, err := s.remote.ProbeMedia(abs); err == nil && mi.Duration > 0 {
			dur = mi.Duration
		}
		videoDurs = append(videoDurs, sceneVideo{abs: abs, dur: dur})
		scenes = append(scenes, sc)
	}

	outName := fmt.Sprintf("merged/%s_merged_%d.mp4", projectFileTag(p), mt.ID)
	outAbs := filepath.Join(s.cfg.Comfy.ComfyDir, "output_workers", "gpu0", outName)
	srtName := strings.TrimSuffix(outName, ".mp4") + ".srt"
	srtAbs := filepath.Join(s.cfg.Comfy.ComfyDir, "output_workers", "gpu0", srtName)

	// 1) 生成 SRT 字幕：按场景顺序与时长均分对白（无 TTS 依赖）
	sceneIDs := make([]uint, len(ids))
	for i, idStr := range ids {
		id, _ := strconv.Atoi(idStr)
		sceneIDs[i] = uint(id)
	}
	if subtitles {
		if err := s.writeMergeSRT(p, mt, srtAbs, scenes); err != nil {
			s.failMerge(mt, p, "生成字幕失败: "+err.Error())
			return
		}
	}

	// 2) 组装 ffmpeg：视频 concat（保留原音轨）+ 烧录字幕
	// 视频流 concat
	concatHead := ""
	for i := range ids {
		concatHead += fmt.Sprintf("[%d:v]", i)
	}
	ffilters := []string{concatHead + fmt.Sprintf("concat=n=%d:v=1:a=0[vc]", len(ids))}
	// 统一缩放到首个场景尺寸（兼容不同分辨率）
	scale := ""
	if len(ids) > 0 {
		if mi, err := s.remote.ProbeMedia(videoDurs[0].abs); err == nil && mi.Width > 0 && mi.Height > 0 {
			scale = fmt.Sprintf(",scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2", mi.Width, mi.Height, mi.Width, mi.Height)
		}
	}
	// subtitles filter 烧录 SRT（仅 subtitles 开关开启时）；不使用单引号（避免与 shellQuote 冲突），转义特殊字符
	subFilter := ""
	if subtitles {
		subFilter = fmt.Sprintf(",subtitles=filename=%s:force_style=FontName\\=Noto Sans CJK SC\\,FontSize\\=14\\,MarginV\\=28", escapeFilterPath(srtAbs))
	}
	ffilters = append(ffilters, fmt.Sprintf("[vc]fps=24%s%s[v]", scale, subFilter))

	mapArgs := `-map "[v]"`
	if dub && audioCount == len(ids) && audioCount > 0 {
		// 所有场景都有原音轨（H3 同步人声）：音频一并 concat 保留
		audioHead := ""
		for i := range ids {
			audioHead += fmt.Sprintf("[%d:a]", i)
		}
		ffilters = append(ffilters, audioHead+fmt.Sprintf("concat=n=%d:v=0:a=1[a]", len(ids)))
		mapArgs = `-map "[v]" -map "[a]" -c:a aac -b:a 192k`
	}
	filterComplex := strings.Join(ffilters, ";")

	cmd := fmt.Sprintf(
		"%s; mkdir -p %s; \"$FF\" -y %s -filter_complex %s %s -c:v libx264 -crf 18 -preset medium -pix_fmt yuv420p -movflags +faststart %s",
		ffmpegResolveCmd, shellQuote(filepath.Dir(outAbs)),
		strings.Join(inputs, " "), shellQuote(filterComplex), mapArgs, shellQuote(outAbs),
	)

	log.Printf("[merge %d] %s", mt.ID, cmd)
	if _, err := s.remote.RunTimeout(cmd, 30*time.Minute); err != nil {
		s.failMerge(mt, p, err.Error())
		return
	}
	s.db.Model(mt).Updates(map[string]any{
		"status": "success", "output_file": outName, "subtitle": subtitles, "error": "",
	})
	s.finishMergeProject(p)
	log.Printf("[merge %d] 完成: %s", mt.ID, outName)
}

// writeMergeSRT 按场景顺序/时长均分对白生成 SRT 字幕（无 TTS 依赖，字幕对齐场景时间轴）
func (s *ProjectService) writeMergeSRT(p *models.Project, mt *models.MergeTask, srtAbs string, scenes []models.Scene) error {
	var sb strings.Builder
	idx, global := 0, 0.0
	for _, sc := range scenes {
		var dubs []models.Dialogue
		s.db.Where("scene_id = ?", sc.ID).Order("`order`").Find(&dubs)
		dur := normalizeSceneDuration(sc.Duration)
		if sc.VideoFile != "" && sc.VideoGPU != nil {
			abs := filepath.Join(s.cfg.Comfy.ComfyDir, "output_workers", fmt.Sprintf("gpu%d", *sc.VideoGPU), sc.VideoFile)
			if mi, err := s.remote.ProbeMedia(abs); err == nil && mi.Duration > 0 {
				dur = mi.Duration
			}
		}
		if len(dubs) == 0 {
			global += dur
			continue
		}
		seg := dur / float64(len(dubs))
		for _, d := range dubs {
			if strings.TrimSpace(d.Text) == "" {
				continue
			}
			idx++
			start := global + seg
			sb.WriteString(fmt.Sprintf("%d\n%s --> %s\n", idx,
				formatSRTTime(start), formatSRTTime(start+seg)))
			line := strings.TrimSpace(d.Text)
			if strings.TrimSpace(d.Character) != "" {
				line = d.Character + "：" + line
			}
			sb.WriteString(line + "\n\n")
			global = start + seg
		}
	}
	data := []byte{0xEF, 0xBB, 0xBF}
	data = append(data, []byte(sb.String())...)
	if err := s.remote.WriteFile(srtAbs, data, 0o644); err != nil {
		return err
	}
	log.Printf("[merge %d] 字幕已生成: %s (%d 条)", mt.ID, srtAbs, idx)
	return nil
}

// finishMergeProject 成片完成后标记项目收尾
func (s *ProjectService) finishMergeProject(p *models.Project) {
	s.db.Model(&models.Project{}).Where("id = ? AND generation = ?", p.ID, p.Generation).Updates(map[string]any{
		"status": "finished", "pipeline_stage": "finished", "auto_generate": false,
	})
	s.pushProject(p)
}

// sceneVideo 场景视频本地信息（合并时间轴计算用）
type sceneVideo struct {
	abs string
	dur float64
}

// dubSegment 单句对白在成片全局时间轴上的位置
type dubSegment struct {
	DialogueID uint
	SceneID    uint
	Start      float64 // 全局秒
	End        float64
	Character  string
	Text       string
	AudioAbs   string // TTS 音频绝对路径（空表示无音频）
}

// buildDubTimeline 构建该集对白时间轴：
// 场景按顺序排布（起点 = 前序视频时长累加），场景内对白按序排列（句间 0.35s 间隔）。
// 全部对白均 ready 时返回 dubReady=true；存在未合成对白时返回 false（回退原音轨合并）。
func (s *ProjectService) buildDubTimeline(p *models.Project, episodeN int, sceneIDs []uint, videoDurs []sceneVideo) ([]dubSegment, float64, bool, error) {
	var dubs []models.Dialogue
	if err := s.db.Where("scene_id IN ?", sceneIDs).Order("scene_id, `order`").Find(&dubs).Error; err != nil {
		return nil, 0, false, err
	}
	if len(dubs) == 0 {
		return nil, 0, false, nil
	}
	// 校验全部 ready（存在 pending/failed/synthesizing 则回退）
	for _, d := range dubs {
		if d.Status != "ready" {
			log.Printf("[merge] 对白 %d 状态 %s，回退原音轨合并", d.ID, d.Status)
			return nil, 0, false, nil
		}
	}
	byScene := map[uint][]models.Dialogue{}
	sceneOrder := map[uint]int{}
	for i, sid := range sceneIDs {
		sceneOrder[sid] = i
	}
	for _, d := range dubs {
		byScene[d.SceneID] = append(byScene[d.SceneID], d)
	}

	const gap = 0.35
	segs := make([]dubSegment, 0, len(dubs))
	global := 0.0
	sceneDur := make([]float64, len(sceneIDs))
	for i := range sceneIDs {
		if i < len(videoDurs) {
			sceneDur[i] = videoDurs[i].dur
		}
	}
	for i, sid := range sceneIDs {
		list := byScene[sid]
		sceneStart := global
		if len(list) == 0 {
			global += sceneDur[i]
			continue
		}
		cursor := sceneStart + 0.4
		for _, d := range list {
			audioAbs := ""
			dur := 2.0 // 无音频时的兜底时长
			if d.AudioFile != "" {
				audioAbs = filepath.Join(s.cfg.Comfy.ComfyDir, "input", fmt.Sprintf("%d", p.ID), d.AudioFile)
				if ad, err := s.remoteMediaDuration(audioAbs); err == nil && ad > 0 {
					dur = ad
				}
			}
			segs = append(segs, dubSegment{
				DialogueID: d.ID, SceneID: d.SceneID,
				Start: cursor, End: cursor + dur,
				Character: d.Character, Text: d.Text, AudioAbs: audioAbs,
			})
			cursor += dur + gap
		}
		if cursor > sceneStart+sceneDur[i] {
			global = cursor // 对白超出场景视频时长时顺延时间轴
		} else {
			global = sceneStart + sceneDur[i]
		}
	}
	return segs, global, true, nil
}

// remoteMediaDuration 远程探测音频/视频时长（秒），用 ffmpeg -i 解析
func (s *ProjectService) remoteMediaDuration(abs string) (float64, error) {
	cmd := fmt.Sprintf(`%s; "$FF" -i %s 2>&1 | grep -oP 'Duration: \K[0-9:.]+' | head -1`, ffmpegResolveCmd, shellQuote(abs))
	out, err := s.remote.RunTimeout(cmd, 30*time.Second)
	if err != nil {
		return 0, err
	}
	t := strings.TrimSpace(out)
	if t == "" {
		return 0, fmt.Errorf("无法解析时长: %s", abs)
	}
	parts := strings.Split(t, ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("时长格式异常: %s", t)
	}
	h, _ := strconv.ParseFloat(parts[0], 64)
	m, _ := strconv.ParseFloat(parts[1], 64)
	sec, _ := strconv.ParseFloat(parts[2], 64)
	return h*3600 + m*60 + sec, nil
}

// escapeFilterPath ffmpeg filter 内路径转义（冒号/引号/逗号）
func escapeFilterPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "\\\\")
	p = strings.ReplaceAll(p, ":", "\\:")
	p = strings.ReplaceAll(p, "'", "\\'")
	p = strings.ReplaceAll(p, ",", "\\,")
	return p
}

// writeEpisodeSRT 按真实音频时间轴写出 SRT 字幕（UTF-8 BOM 兼容 Windows）
func (s *ProjectService) writeEpisodeSRT(mt *models.MergeTask, srtAbs string, segs []dubSegment) error {
	var sb strings.Builder
	idx := 0
	for _, seg := range segs {
		if strings.TrimSpace(seg.Text) == "" {
			continue
		}
		idx++
		sb.WriteString(fmt.Sprintf("%d\n", idx))
		sb.WriteString(fmt.Sprintf("%s --> %s\n", formatSRTTime(seg.Start), formatSRTTime(seg.End)))
		line := strings.TrimSpace(seg.Text)
		if strings.TrimSpace(seg.Character) != "" {
			line = seg.Character + "：" + line
		}
		sb.WriteString(line + "\n\n")
	}
	data := []byte{0xEF, 0xBB, 0xBF}
	data = append(data, []byte(sb.String())...)
	if err := s.remote.WriteFile(srtAbs, data, 0o644); err != nil {
		return err
	}
	log.Printf("[merge %d] 字幕已生成: %s (%d 条)", mt.ID, srtAbs, idx)
	return nil
}

const ffmpegResolveCmd = `FF=$(command -v ffmpeg 2>/dev/null); if [ -z "$FF" ]; then for PY in python /opt/miniconda3/envs/comfyenv/bin/python /opt/miniconda3/envs/comfyenv/bin/python /opt/miniconda3/envs/wan22/bin/python; do FF=$($PY -c "import imageio_ffmpeg,sys;sys.stdout.write(imageio_ffmpeg.get_ffmpeg_exe())" 2>/dev/null); [ -n "$FF" ] && break; done; fi; if [ -z "$FF" ]; then for F in /usr/bin/ffmpeg /usr/local/bin/ffmpeg /opt/miniconda3/envs/*/lib/python3.*/site-packages/imageio_ffmpeg/binaries/ffmpeg-*; do [ -x "$F" ] && { FF=$F; break; }; done; fi; [ -z "$FF" ] && FF=ffmpeg`

func (s *ProjectService) remoteFileHasAudio(path string) (bool, error) {
	cmd := fmt.Sprintf(`%s; if ! command -v "$FF" >/dev/null 2>&1 && [ ! -x "$FF" ]; then echo unknown; elif "$FF" -i %s 2>&1 | grep -q "Audio:"; then echo yes; else echo no; fi`, ffmpegResolveCmd, shellQuote(path))
	out, err := s.remote.RunTimeout(cmd, 30*time.Second)
	if err != nil {
		return false, fmt.Errorf("检测场景音轨失败: %w", err)
	}
	switch strings.TrimSpace(out) {
	case "yes":
		return true, nil
	case "no":
		return false, nil
	default:
		return false, fmt.Errorf("无法找到 ffmpeg，不能安全检测并保留音轨")
	}
}

func projectFileTag(p *models.Project) string {
	// 纯数字安全标签，避免文件名注入
	now := time.Now().Format("20060102")
	return fmt.Sprintf("p%d_%s", p.ID, now)
}

func (s *ProjectService) failMerge(mt *models.MergeTask, p *models.Project, errMsg string) {
	s.db.Model(mt).Updates(map[string]any{"status": "failed", "error": errMsg})
	s.pushProject(p)
	log.Printf("[merge %d] 失败: %s", mt.ID, errMsg)
}

func (s *ProjectService) ListMerges(projectID uint) ([]models.MergeTask, error) {
	var list []models.MergeTask
	s.db.Where("project_id = ?", projectID).Order("id desc").Find(&list)
	return list, nil
}

// CreateAllMerges 整剧一键合并：对所有含就绪视频的集各创建一个合并任务
func (s *ProjectService) CreateAllMerges(p *models.Project) (int, error) {
	var episodes []int
	if err := s.db.Model(&models.Scene{}).Where("project_id = ? AND status = ?", p.ID, "video_ready").
		Distinct("episode_n").Order("episode_n").Pluck("episode_n", &episodes).Error; err != nil {
		return 0, err
	}
	count := 0
	for _, ep := range episodes {
		var scenes []models.Scene
		if err := s.db.Where("project_id = ? AND episode_n = ? AND status = ?", p.ID, ep, "video_ready").
			Order("`order`").Find(&scenes).Error; err != nil {
			continue
		}
		if len(scenes) < 2 {
			continue
		}
		ids := make([]uint, len(scenes))
		for i := range scenes {
			ids[i] = scenes[i].ID
		}
		if _, err := s.CreateMergeTask(p, ids, true, true); err == nil {
			count++
		}
	}
	return count, nil
}

// ---------- 对白配音与字幕（Dialogue） ----------

// ListSceneDialogues 场景对白列表
func (s *ProjectService) ListSceneDialogues(sceneID uint) ([]models.Dialogue, error) {
	var list []models.Dialogue
	s.db.Where("scene_id = ?", sceneID).Order("`order`").Find(&list)
	return list, nil
}

// EditorData 剪辑台数据：场景（含视频时长与资源 URL）+ 对白 + 按真实音频对齐的字幕时间轴
func (s *ProjectService) EditorData(p *models.Project, episodeN int) (map[string]any, error) {
	var scenes []models.Scene
	if err := s.db.Where("project_id = ? AND episode_n = ? AND generation = ?", p.ID, episodeN, p.Generation).
		Order("`order`").Find(&scenes).Error; err != nil {
		return nil, err
	}
	type editorScene struct {
		models.Scene
		VideoURL  string  `json:"video_url"`
		ImageURL  string  `json:"image_url"`
		VideoDur  float64 `json:"video_dur"`
		AudioURLs []string `json:"audio_urls,omitempty"`
	}
	outScenes := make([]editorScene, 0, len(scenes))
	sceneIDs := make([]uint, 0, len(scenes))
	for _, sc := range scenes {
		es := editorScene{Scene: sc}
		if sc.VideoFile != "" && sc.VideoGPU != nil {
			es.VideoURL = fmt.Sprintf("/api/output/%d/%s", *sc.VideoGPU, sc.VideoFile)
			abs := filepath.Join(s.cfg.Comfy.ComfyDir, "output_workers", fmt.Sprintf("gpu%d", *sc.VideoGPU), sc.VideoFile)
			if mi, err := s.remote.ProbeMedia(abs); err == nil && mi.Duration > 0 {
				es.VideoDur = mi.Duration
			}
		}
		if sc.ImageFile != "" {
			es.ImageURL = fmt.Sprintf("/api/input/%d/%s", p.ID, sc.ImageFile)
		}
		outScenes = append(outScenes, es)
		sceneIDs = append(sceneIDs, sc.ID)
	}

	var dubs []models.Dialogue
	if len(sceneIDs) > 0 {
		s.db.Where("scene_id IN ?", sceneIDs).Order("scene_id, `order`").Find(&dubs)
	}
	for i := range dubs {
		if dubs[i].AudioFile != "" {
			dubs[i].AudioFile = fmt.Sprintf("/api/input/%d/%s", p.ID, dubs[i].AudioFile)
		}
	}

	// 字幕时间轴（真实音频对齐；配音未就绪时按场景时长均分占位）
	// 注意：dubs 的 AudioFile 已转为 URL，时间轴探测需用原名（在调用前保存）
	subs := s.EditorSubtitleTimelineRaw(p, scenes, dubs)

	return map[string]any{
		"project":   p,
		"scenes":    outScenes,
		"dialogues": dubs,
		"subtitles": subs,
	}, nil
}

// EditorSubtitleTimelineRaw 计算该集字幕时间轴（秒），供剪辑台展示：
// 对白配音 ready 时按真实音频时长排布；否则按场景时长均分占位。
func (s *ProjectService) EditorSubtitleTimelineRaw(p *models.Project, scenes []models.Scene, dubs []models.Dialogue) []map[string]any {
	byScene := map[uint][]models.Dialogue{}
	for _, d := range dubs {
		byScene[d.SceneID] = append(byScene[d.SceneID], d)
	}
	ready := true
	for _, d := range dubs {
		if d.Status != "ready" {
			ready = false
			break
		}
	}
	out := make([]map[string]any, 0, len(dubs))
	global := 0.0
	sceneDur := make([]float64, len(scenes))
	for i, sc := range scenes {
		d := normalizeSceneDuration(sc.Duration)
		if sc.VideoFile != "" && sc.VideoGPU != nil {
			abs := filepath.Join(s.cfg.Comfy.ComfyDir, "output_workers", fmt.Sprintf("gpu%d", *sc.VideoGPU), sc.VideoFile)
			if mi, err := s.remote.ProbeMedia(abs); err == nil && mi.Duration > 0 {
				d = mi.Duration
			}
		}
		sceneDur[i] = d
	}
	const gap = 0.35
	for i, sc := range scenes {
		list := byScene[sc.ID]
		if len(list) == 0 {
			global += sceneDur[i]
			continue
		}
		cursor := global + 0.4
		if !ready {
			seg := sceneDur[i] / float64(len(list))
			for _, d := range list {
				out = append(out, map[string]any{
					"dialogue_id": d.ID, "scene_id": d.SceneID, "scene_order": sc.Order,
					"character": d.Character, "text": d.Text,
					"start": cursor, "end": cursor + seg, "status": d.Status,
				})
				cursor += seg
			}
			global += sceneDur[i]
			continue
		}
		for _, d := range list {
			dur := 2.0
			if d.AudioFile != "" {
				// AudioFile 可能为 URL（/api/input/pid/name），取文件名后拼真实路径
				name := filepath.Base(strings.TrimPrefix(d.AudioFile, "/api/input/"))
				abs := filepath.Join(s.cfg.Comfy.ComfyDir, "input", fmt.Sprintf("%d", p.ID), name)
				if ad, err := s.remoteMediaDuration(abs); err == nil && ad > 0 {
					dur = ad
				}
			}
			out = append(out, map[string]any{
				"dialogue_id": d.ID, "scene_id": d.SceneID, "scene_order": sc.Order,
				"character": d.Character, "text": d.Text,
				"start": cursor, "end": cursor + dur, "status": d.Status,
			})
			cursor += dur + gap
		}
		if cursor > global+sceneDur[i] {
			global = cursor
		} else {
			global += sceneDur[i]
		}
	}
	return out
}

// UpdateDialogue 编辑对白文本/音色/角色；修改后重置为 pending 以便重新合成
func (s *ProjectService) UpdateDialogue(p *models.Project, did uint, text, voice, character string) (*models.Dialogue, error) {
	var d models.Dialogue
	if err := s.db.Where("id = ? AND project_id = ?", did, p.ID).First(&d).Error; err != nil {
		return nil, fmt.Errorf("对白不存在")
	}
	updates := map[string]any{}
	if text != "" {
		updates["text"] = text
	}
	if voice != "" {
		updates["voice"] = voice
	}
	if character != "" {
		updates["character"] = character
	}
	if len(updates) > 0 {
		updates["status"] = "pending"
		updates["audio_file"] = ""
		updates["error"] = ""
	}
	if err := s.db.Model(&d).Updates(updates).Error; err != nil {
		return nil, err
	}
	s.pushProject(p)
	s.db.First(&d, did)
	return &d, nil
}

// ReorderScenes 调整场景顺序：scene_ids 为该集新顺序（同代内重写 order）
func (s *ProjectService) ReorderScenes(p *models.Project, sceneIDs []uint) error {
	if len(sceneIDs) < 2 {
		return fmt.Errorf("至少需要 2 个场景")
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		// 第一步：全部先移出唯一索引冲突区（-1000-i 保证唯一）
		for i, sid := range sceneIDs {
			if err := tx.Model(&models.Scene{}).Where("id = ? AND project_id = ? AND generation = ?", sid, p.ID, p.Generation).
				Update("`order`", -1000-i).Error; err != nil {
				return err
			}
		}
		// 第二步：写入最终顺序
		for i, sid := range sceneIDs {
			if err := tx.Model(&models.Scene{}).Where("id = ?", sid).
				Update("`order`", i+1).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// dubVoiceFor 对白音色：Dialogue.Voice 优先，否则取阿里云 TTS 默认音色
func (s *ProjectService) dubVoiceFor(d *models.Dialogue) string {
	if strings.TrimSpace(d.Voice) != "" {
		return d.Voice
	}
	if s.ali != nil {
		return s.ali.Config().VoiceFor(d.Character)
	}
	return ""
}

// StartDialogueTTS 异步合成单条对白（占位防并发）
func (s *ProjectService) StartDialogueTTS(d *models.Dialogue) error {
	claim := s.db.Model(&models.Dialogue{}).Where("id = ? AND status IN ?", d.ID, []string{"pending", "failed"}).
		Updates(map[string]any{"status": "synthesizing", "error": ""})
	if claim.Error != nil {
		return claim.Error
	}
	if claim.RowsAffected == 0 {
		return fmt.Errorf("对白已在合成或已完成")
	}
	go func() {
		if err := s.synthesizeDialogue(d); err != nil {
			log.Printf("[dialogue %d] tts failed: %v", d.ID, err)
		}
	}()
	return nil
}

func (s *ProjectService) synthesizeDialogue(d *models.Dialogue) error {
	if s.upload == nil {
		s.db.Model(&models.Dialogue{}).Where("id = ?", d.ID).Updates(map[string]any{"status": "failed", "error": "存储未配置"})
		return fmt.Errorf("存储未配置")
	}
	if strings.TrimSpace(d.Text) == "" {
		s.db.Model(&models.Dialogue{}).Where("id = ?", d.ID).Updates(map[string]any{"status": "ready", "audio_file": "", "error": ""})
		return nil
	}
	// 后期配音：阿里云 TTS（DashScope）
	var data []byte
	var err error
	voice := s.dubVoiceFor(d)
	if s.ali != nil && s.ali.Config().Configured() {
		data, err = s.ali.TextToSpeech(d.Text, d.Character, voice)
	} else {
		err = fmt.Errorf("TTS 未配置（请到平台设置配置阿里云语音服务）")
	}
	if err != nil {
		s.db.Model(&models.Dialogue{}).Where("id = ?", d.ID).Updates(map[string]any{"status": "failed", "error": err.Error()})
		return err
	}
	name := fmt.Sprintf("dub_%d_%d.mp3", d.ID, time.Now().UnixNano())
	path, _, err := s.upload.SaveFile(fmt.Sprintf("%d", d.ProjectID), "audio", name, data)
	if err != nil {
		s.db.Model(&models.Dialogue{}).Where("id = ?", d.ID).Updates(map[string]any{"status": "failed", "error": err.Error()})
		return err
	}
	s.db.Model(&models.Dialogue{}).Where("id = ?", d.ID).Updates(map[string]any{"status": "ready", "audio_file": filepath.Base(path), "error": ""})
	s.pushProject(nil)
	return nil
}

// GenerateSceneDubs 合成场景全部待合成对白
func (s *ProjectService) GenerateSceneDubs(sc *models.Scene) (int, error) {
	var dubs []models.Dialogue
	s.db.Where("scene_id = ? AND status IN ?", sc.ID, []string{"pending", "failed"}).Find(&dubs)
	for i := range dubs {
		_ = s.StartDialogueTTS(&dubs[i])
	}
	return len(dubs), nil
}

// GenerateProjectDubs 一键合成项目全部待合成对白
func (s *ProjectService) GenerateProjectDubs(p *models.Project) (int, error) {
	var dubs []models.Dialogue
	s.db.Where("project_id = ? AND status IN ?", p.ID, []string{"pending", "failed"}).Find(&dubs)
	for i := range dubs {
		_ = s.StartDialogueTTS(&dubs[i])
	}
	return len(dubs), nil
}

// EpisodeSRT 生成指定集的 SRT 字幕（按场景顺序、场内对白均分时长），返回字幕正文与条数
func (s *ProjectService) EpisodeSRT(p *models.Project, episodeN int) (string, int) {
	var scenes []models.Scene
	s.db.Where("project_id = ? AND episode_n = ?", p.ID, episodeN).Order("`order`").Find(&scenes)
	if len(scenes) == 0 {
		return "", 0
	}
	sceneIDs := make([]uint, 0, len(scenes))
	for _, sc := range scenes {
		sceneIDs = append(sceneIDs, sc.ID)
	}
	var dubs []models.Dialogue
	s.db.Where("scene_id IN ?", sceneIDs).Order("scene_id, `order`").Find(&dubs)
	byScene := map[uint][]models.Dialogue{}
	for _, d := range dubs {
		byScene[d.SceneID] = append(byScene[d.SceneID], d)
	}
	var sb strings.Builder
	idx, cursor, total := 0, 0.0, 0
	for _, sc := range scenes {
		list := byScene[sc.ID]
		total += len(list)
		if len(list) == 0 {
			cursor += normalizeSceneDuration(sc.Duration)
			continue
		}
		seg := normalizeSceneDuration(sc.Duration) / float64(len(list))
		for _, d := range list {
			idx++
			writeSRTEntry(&sb, idx, cursor, cursor+seg, d.Character, d.Text)
			cursor += seg
		}
	}
	return sb.String(), total
}

// writeSRTEntry 写一条 SRT 字幕块
func writeSRTEntry(sb *strings.Builder, idx int, start, end float64, character, text string) {
	sb.WriteString(fmt.Sprintf("%d\n", idx))
	sb.WriteString(fmt.Sprintf("%s --> %s\n", formatSRTTime(start), formatSRTTime(end)))
	line := strings.TrimSpace(text)
	if strings.TrimSpace(character) != "" {
		line = character + "：" + line
	}
	sb.WriteString(line + "\n\n")
}

// formatSRTTime 秒 → SRT 时间码 HH:MM:SS,mmm
func formatSRTTime(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	totalMs := int(sec * 1000)
	h := totalMs / 3600000
	m := (totalMs % 3600000) / 60000
	se := (totalMs % 60000) / 1000
	ms := totalMs % 1000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, se, ms)
}

// ---------- 进度推送 ----------

func (s *ProjectService) pushProject(p *models.Project) {
	// 项目状态变化由前端在轮询/刷新时获取，这里广播轻量通知
	if s.hub != nil {
		s.hub.Broadcast("project_update", map[string]any{"t": time.Now().UnixMilli()})
	}
}

// PushProject 供其他组件触发项目状态广播
func (s *ProjectService) PushProject(p *models.Project) {
	s.pushProject(p)
}

func (s *ProjectService) Stop() {
	close(s.stopped)
}

// detectImageExt 根据魔数判断图片扩展名
func detectImageExt(data []byte) string {
	switch {
	case len(data) >= 8 && data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47:
		return ".png"
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return ".jpg"
	case len(data) >= 12 && data[0] == 'R' && data[1] == 'I' && data[2] == 'F' && data[3] == 'F' && data[8] == 'W' && data[9] == 'E' && data[10] == 'B' && data[11] == 'P':
		return ".webp"
	default:
		return ".png"
	}
}
