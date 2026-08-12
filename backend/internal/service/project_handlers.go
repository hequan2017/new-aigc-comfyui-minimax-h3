package service

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"comfyui-console/internal/models"
)

// ---------- 平台设置 ----------

// HandleGetSettings 读取平台设置（API Key 打码返回）
func (s *Service) HandleGetSettings(c *gin.Context) {
	c.JSON(200, s.Volc.AllSettings())
}

// HandleUpdateSettings 保存平台设置
func (s *Service) HandleUpdateSettings(c *gin.Context) {
	var req map[string]string
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	s.Volc.UpdateSettings(req)
	c.JSON(200, gin.H{"ok": true})
}

// HandleTestText 测试文生文接口
func (s *Service) HandleTestText(c *gin.Context) {
	msg, err := s.Volc.TestText()
	if err != nil {
		c.JSON(500, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": "文生文连通正常: " + msg})
}

// HandleTestImage 测试文生图接口
func (s *Service) HandleTestImage(c *gin.Context) {
	msg, err := s.Volc.TestImage()
	if err != nil {
		c.JSON(500, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": "文生图连通正常: " + msg})
}

// HandleTestTTS 测试阿里云 TTS 配音接口
func (s *Service) HandleTestTTS(c *gin.Context) {
	ali := NewAliyunTTS(s.DB)
	msg, err := ali.TestTTS()
	if err != nil {
		c.JSON(500, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": "配音连通正常: " + msg})
}

// ---------- 漫剧项目 ----------

func (s *Service) HandleListProjects(c *gin.Context) {
	list, err := s.Projects.ListProjects()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	// 附加场景统计
	out := make([]gin.H, 0, len(list))
	for _, p := range list {
		var total, ready, videos int64
		s.DB.Where("project_id = ?", p.ID).Count(&total)
		s.DB.Where("project_id = ? AND status = ?", p.ID, "video_ready").Count(&ready)
		s.DB.Where("project_id = ? AND video_file != ''", p.ID).Count(&videos)
		out = append(out, gin.H{
			"project": p, "scene_total": total,
			"scene_video_ready": ready, "scene_videos": videos,
		})
	}
	c.JSON(200, out)
}

func (s *Service) HandleCreateProject(c *gin.Context) {
	var req models.Project
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if req.Synopsis == "" {
		c.JSON(400, gin.H{"error": "请填写故事创意"})
		return
	}
	p, err := s.Projects.CreateProject(req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, p)
}

func (s *Service) HandleGetProject(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid id"})
		return
	}
	p, scenes, err := s.Projects.GetProject(uint(id))
	if err != nil {
		c.JSON(404, gin.H{"error": "project not found"})
		return
	}
	merges, _ := s.Projects.ListMerges(uint(id))
	chars, _ := s.Projects.ListCharacters(uint(id))
	counts, _ := s.Projects.CharacterSceneCounts(uint(id))
	var dialogues []models.Dialogue
	s.DB.Where("project_id = ?", uint(id)).Order("scene_id, `order`").Find(&dialogues)
	c.JSON(200, gin.H{"project": p, "scenes": scenes, "merges": merges, "characters": chars, "character_counts": counts, "dialogues": dialogues})
}

func (s *Service) HandleDeleteProject(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid id"})
		return
	}
	if err := s.Projects.DeleteProject(uint(id)); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// HandleUpdateProject 编辑项目信息
func (s *Service) HandleUpdateProject(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req models.Project
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if strings.TrimSpace(req.Synopsis) == "" {
		c.JSON(400, gin.H{"error": "故事创意不能为空"})
		return
	}
	if err := s.Projects.UpdateProject(p, req); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	np, _, _ := s.Projects.GetProject(p.ID)
	c.JSON(200, np)
}

// HandleUpdateScene 编辑场景文案（修改画面提示词会重置画面与视频）
func (s *Service) HandleUpdateScene(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		Title       string `json:"title"`
		Content     string `json:"content"`
		ImagePrompt string `json:"image_prompt"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if strings.TrimSpace(req.Content) == "" || strings.TrimSpace(req.ImagePrompt) == "" {
		c.JSON(400, gin.H{"error": "场景正文和画面提示词不能为空"})
		return
	}
	if err := s.Projects.UpdateScene(sc, req.Title, req.Content, req.ImagePrompt); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	var updated models.Scene
	s.DB.First(&updated, sc.ID)
	c.JSON(200, updated)
}

// HandleUpdatePlanEpisodes 修改创作方案中每集的标题与剧情提示词
func (s *Service) HandleUpdatePlanEpisodes(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		Episodes []PlanEpisodeUpdate `json:"episodes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if len(req.Episodes) == 0 {
		c.JSON(400, gin.H{"error": "episodes 不能为空"})
		return
	}
	np, err := s.Projects.UpdatePlanEpisodes(p, req.Episodes)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"project": np})
}

// HandleGeneratePlan 阶段 1：按 short-drama 方法论生成创作方案
func (s *Service) HandleGeneratePlan(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	if err := s.Projects.ClaimManualScript(p); err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	fresh, err := s.Projects.GeneratePlan(p)
	if err != nil {
		s.Projects.failPipeline(p.ID, err, false)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"project": fresh})
}

// HandleRenderScriptFromText 用户编辑剧本后重新渲染该集分镜场景（剧本正文以用户文本为准）
func (s *Service) HandleRenderScriptFromText(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	if err := s.Projects.ClaimManualScript(p); err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	var req struct {
		EpisodeN int    `json:"episode_n"`
		Script   string `json:"script"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	p, scenes, err := s.Projects.GenerateScriptFromText(p, req.EpisodeN, req.Script)
	if err != nil {
		s.Projects.failPipeline(p.ID, err, false)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"project": p, "scenes": scenes})
}

// HandleExpandScript AI 扩写剧本正文（不落库，返回扩写后文本由前端回填）
func (s *Service) HandleExpandScript(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		EpisodeN int    `json:"episode_n"`
		Script   string `json:"script"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if strings.TrimSpace(req.Script) == "" {
		c.JSON(400, gin.H{"error": "剧本内容不能为空"})
		return
	}
	out, err := s.Projects.ExpandScript(p, req.EpisodeN, req.Script)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"script": out})
}

// HandleGenerateScript 生成剧本（有创作方案时按方案渲染分镜）。
// query 参数 episode_n 指定当前制作集（默认 1），只生成并替换该集分镜场景。
func (s *Service) HandleGenerateScript(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	if err := s.Projects.ClaimManualScript(p); err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	episodeN, _ := strconv.Atoi(c.DefaultQuery("episode_n", "1"))
	p, scenes, err := s.Projects.GenerateScript(p, episodeN)
	if err != nil {
		s.Projects.failPipeline(p.ID, err, false)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"project": p, "scenes": scenes})
}

// HandleGenerateProject 启动剧本→画面→视频→合并的持久化一键生成流程。
// query auto=true（创建项目自动触发）：生成完创作方案+角色+第一集剧本后停止，后续由人工处理。
func (s *Service) HandleGenerateProject(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	stopAfterScript := c.Query("auto") == "1" || c.Query("auto") == "true"
	episodeN, _ := strconv.Atoi(c.DefaultQuery("episode_n", "1"))
	if episodeN <= 0 {
		episodeN = 1
	}
	current, err := s.Projects.StartPipeline(p, episodeN, stopAfterScript)
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	msg := fmt.Sprintf("第%d集完整生成流程已启动", episodeN)
	if stopAfterScript {
		msg = "已启动自动生成：创作方案、角色与第一集剧本，完成后由人工处理后续制作"
	}
	c.JSON(http.StatusAccepted, gin.H{"ok": true, "project": current, "message": msg})
}

// HandleGenerateSceneImage 生成单个场景画面
func (s *Service) HandleGenerateSceneImage(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	if err := s.Projects.StartSceneImage(sc); err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": fmt.Sprintf("场景 %d 画面生成中", sc.Order)})
}

// HandleGenerateAllImages 一键生成当前集全部画面（query episode_n 过滤，默认全部）
func (s *Service) HandleGenerateAllImages(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	episodeN, _ := strconv.Atoi(c.DefaultQuery("episode_n", "0"))
	n, err := s.Projects.GenerateAllImages(p, episodeN)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": fmt.Sprintf("已提交 %d 个场景的画面生成", n)})
}

// HandleGenerateSceneVideo 生成单个场景视频（本地 L40）
func (s *Service) HandleGenerateSceneVideo(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	if err := s.Projects.GenerateSceneVideo(p, sc); err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": fmt.Sprintf("场景 %d 视频任务已创建", sc.Order)})
}

// HandleCancelSceneVideo 停止单个场景视频生成
func (s *Service) HandleCancelSceneVideo(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	if err := s.Projects.CancelSceneVideo(p, sc); err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": fmt.Sprintf("场景 %d 视频已停止", sc.Order)})
}

// HandleGenerateAllVideos 一键生成当前集全部视频（query episode_n 过滤，默认全部）
func (s *Service) HandleGenerateAllVideos(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	episodeN, _ := strconv.Atoi(c.DefaultQuery("episode_n", "0"))
	n, err := s.Projects.GenerateAllVideos(p, episodeN)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": fmt.Sprintf("已提交 %d 个场景的视频生成", n)})
}

// HandleCreateMerge 合并视频成片
func (s *Service) HandleCreateMerge(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		SceneIDs  []uint `json:"scene_ids"`
		Dub       bool   `json:"dub"`       // true=保留原音轨(默认), false=静音
		Subtitles bool   `json:"subtitles"` // true=烧录字幕(默认), false=不烧录
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	mt, err := s.Projects.CreateMergeTask(p, req.SceneIDs, req.Dub, req.Subtitles)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, mt)
}

// HandleCreateAllMerges 整剧一键合并（所有含就绪视频的集）
func (s *Service) HandleCreateAllMerges(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	n, err := s.Projects.CreateAllMerges(p)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if n == 0 {
		c.JSON(400, gin.H{"error": "没有可合并的集（每集至少需 2 个视频就绪场景）"})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": fmt.Sprintf("已为 %d 集启动合并", n)})
}

// HandleListMerges 合并任务列表
func (s *Service) HandleListMerges(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid id"})
		return
	}
	list, err := s.Projects.ListMerges(uint(id))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, list)
}

// ---------- 角色资产 ----------

func (s *Service) HandleListCharacters(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	chars, err := s.Projects.ListCharacters(p.ID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	counts, _ := s.Projects.CharacterSceneCounts(p.ID)
	c.JSON(200, gin.H{"characters": chars, "counts": counts})
}

func (s *Service) HandleCreateCharacter(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req models.Character
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	req.ID = 0
	req.ProjectID = p.ID
	ch, err := s.Projects.CreateCharacter(req)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, ch)
}

func (s *Service) HandleUpdateCharacter(c *gin.Context) {
	ch, ok := s.loadCharacter(c)
	if !ok {
		return
	}
	var req models.Character
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if err := s.Projects.UpdateCharacter(ch, req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	var updated models.Character
	s.DB.First(&updated, ch.ID)
	c.JSON(200, updated)
}

func (s *Service) HandleDeleteCharacter(c *gin.Context) {
	ch, ok := s.loadCharacter(c)
	if !ok {
		return
	}
	if err := s.Projects.DeleteCharacter(ch.ProjectID, ch.ID); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Service) HandleGenerateCharacterPortrait(c *gin.Context) {
	ch, ok := s.loadCharacter(c)
	if !ok {
		return
	}
	if err := s.Projects.StartCharacterPortrait(ch); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": fmt.Sprintf("角色「%s」标准像生成中", ch.Name)})
}

// HandleUploadCharacterPortrait 上传角色照片作为标准像（替代文生图）
func (s *Service) HandleUploadCharacterPortrait(c *gin.Context) {
	ch, ok := s.loadCharacter(c)
	if !ok {
		return
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "file required: " + err.Error()})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if len(data) == 0 {
		c.JSON(400, gin.H{"error": "空文件"})
		return
	}
	ext := filepath.Ext(header.Filename)
	if ext == "" {
		ext = detectImageExt(data)
	}
	if ext == "" {
		ext = ".jpg"
	}
	name := fmt.Sprintf("char_%d_%d%s", ch.ID, time.Now().UnixNano(), ext)
	path, _, err := s.Upload.SaveFile(fmt.Sprintf("%d", ch.ProjectID), "image", name, data)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if err := s.DB.Model(&models.Character{}).Where("id = ?", ch.ID).Update("portrait", filepath.Base(path)).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	s.Projects.PushProject(nil)
	c.JSON(200, gin.H{"ok": true, "message": "照片已设为角色标准像", "portrait": filepath.Base(path)})
}

func (s *Service) HandleGenerateAllPortraits(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	n, err := s.Projects.GenerateAllPortraits(p)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": fmt.Sprintf("已提交 %d 个角色的标准像生成", n)})
}

// loadCharacter 从 :cid 加载角色（校验归属 :id 项目）
func (s *Service) loadCharacter(c *gin.Context) (*models.Character, bool) {
	projectID, err := strconv.Atoi(c.Param("id"))
	if err != nil || projectID <= 0 {
		c.JSON(400, gin.H{"error": "invalid project id"})
		return nil, false
	}
	cid, err := strconv.Atoi(c.Param("cid"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid character id"})
		return nil, false
	}
	var ch models.Character
	if err := s.DB.Where("id = ? AND project_id = ?", cid, projectID).First(&ch).Error; err != nil {
		c.JSON(404, gin.H{"error": "character not found"})
		return nil, false
	}
	return &ch, true
}

// ---------- 对白配音与字幕 ----------

// HandleEditorData 剪辑台数据：该集场景（含视频时长/顺序）+ 全部对白 + 字幕时间轴
func (s *Service) HandleEditorData(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	episodeN, _ := strconv.Atoi(c.DefaultQuery("episode_n", "1"))
	if episodeN <= 0 {
		episodeN = 1
	}
	data, err := s.Projects.EditorData(p, episodeN)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, data)
}

// HandleUpdateDialogue 编辑单条对白（文本/音色），编辑后需重新合成配音
func (s *Service) HandleUpdateDialogue(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	did, err := strconv.Atoi(c.Param("did"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid dialogue id"})
		return
	}
	var req struct {
		Text      string `json:"text"`
		Voice     string `json:"voice"`
		Character string `json:"character"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	d, err := s.Projects.UpdateDialogue(p, uint(did), req.Text, req.Voice, req.Character)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, d)
}

// HandleRedubDialogue 重新合成单条对白配音
func (s *Service) HandleRedubDialogue(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	did, err := strconv.Atoi(c.Param("did"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid dialogue id"})
		return
	}
	var d models.Dialogue
	if err := s.DB.Where("id = ? AND project_id = ?", did, p.ID).First(&d).Error; err != nil {
		c.JSON(404, gin.H{"error": "对白不存在"})
		return
	}
	if err := s.Projects.StartDialogueTTS(&d); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": "配音重新合成已提交"})
}

// HandleReorderScenes 调整场景顺序（时间轴拖拽排序）
func (s *Service) HandleReorderScenes(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		SceneIDs []uint `json:"scene_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if err := s.Projects.ReorderScenes(p, req.SceneIDs); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// HandleUpdateSceneDuration 调整场景目标时长（秒，影响时间轴与合并）
func (s *Service) HandleUpdateSceneDuration(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		Duration float64 `json:"duration"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if req.Duration < 3 || req.Duration > 15 {
		c.JSON(400, gin.H{"error": "时长需在 3~15 秒之间"})
		return
	}
	if err := s.DB.Model(&models.Scene{}).Where("id = ?", sc.ID).Update("duration", req.Duration).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "duration": req.Duration})
}

func (s *Service) HandleListSceneDialogues(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	list, err := s.Projects.ListSceneDialogues(sc.ID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, list)
}

func (s *Service) HandleGenerateSceneDub(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	n, err := s.Projects.GenerateSceneDubs(sc)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": fmt.Sprintf("已提交 %d 条对白配音", n)})
}

func (s *Service) HandleGenerateProjectDub(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	n, err := s.Projects.GenerateProjectDubs(p)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": fmt.Sprintf("已提交 %d 条对白配音", n)})
}

// HandleEpisodeSRT 生成并下载该集 SRT 字幕（含 UTF-8 BOM 兼容 Windows 播放器）
func (s *Service) HandleEpisodeSRT(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	episodeN, _ := strconv.Atoi(c.DefaultQuery("episode_n", "1"))
	if episodeN <= 0 {
		episodeN = 1
	}
	srt, count := s.Projects.EpisodeSRT(p, episodeN)
	if count == 0 {
		c.JSON(400, gin.H{"error": "该集暂无对白字幕（需先生成含对白的剧本）"})
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="episode_%d.srt"`, episodeN))
	c.Data(200, "text/plain; charset=utf-8", []byte(srt))
}

// ---------- 素材库 ----------

// HandleListMaterials 素材列表（?type=image|video|audio&project_id=N）
func (s *Service) HandleListMaterials(c *gin.Context) {
	ftype := c.Query("type")
	var projectID *uint
	if v := c.Query("project_id"); v != "" {
		id, err := strconv.ParseUint(v, 10, 32)
		if err == nil {
			pid := uint(id)
			projectID = &pid
		}
	}
	list, err := s.Materials.List(ftype, projectID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, list)
}

// HandleUploadMaterial 上传素材到素材库 (multipart: file/type/project_id)
func (s *Service) HandleUploadMaterial(c *gin.Context) {
	ftype := c.PostForm("type")
	if ftype == "" {
		ftype = "image"
	}
	var projectID *uint
	if v := c.PostForm("project_id"); v != "" {
		if id, err := strconv.ParseUint(v, 10, 32); err == nil {
			pid := uint(id)
			projectID = &pid
		}
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "file required"})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	mat, err := s.Materials.SaveUpload(projectID, ftype, header.Filename, data)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, mat)
}

// HandleDeleteMaterial 删除素材
func (s *Service) HandleDeleteMaterial(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid id"})
		return
	}
	if err := s.Materials.Delete(uint(id)); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// HandleInputFile 场景首帧图预览：/api/input/:taskid/*path → ComfyUI input 目录
func (s *Service) HandleInputFile(c *gin.Context) {
	taskID := c.Param("taskid")
	path := c.Param("path")
	if strings.Contains(path, "..") || strings.Contains(taskID, "..") {
		c.JSON(400, gin.H{"error": "invalid path"})
		return
	}
	full := s.Upload.InputDir() + "/" + taskID + "/" + path
	handleRemoteFile(c, s.Remote, full)
}

// loadProject 从 :id 加载项目
func (s *Service) loadProject(c *gin.Context) (*models.Project, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid id"})
		return nil, false
	}
	var p models.Project
	if err := s.DB.First(&p, id).Error; err != nil {
		c.JSON(404, gin.H{"error": "project not found"})
		return nil, false
	}
	return &p, true
}

// loadScene 从 :sid 加载场景
func (s *Service) loadScene(c *gin.Context) (*models.Scene, bool) {
	projectID, err := strconv.Atoi(c.Param("id"))
	if err != nil || projectID <= 0 {
		c.JSON(400, gin.H{"error": "invalid project id"})
		return nil, false
	}
	id, err := strconv.Atoi(c.Param("sid"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid scene id"})
		return nil, false
	}
	var sc models.Scene
	if err := s.DB.Where("id = ? AND project_id = ?", id, projectID).First(&sc).Error; err != nil {
		c.JSON(404, gin.H{"error": "scene not found"})
		return nil, false
	}
	return &sc, true
}

// handleRemoteFile 通用远程文件响应（校验存在 + MIME）
// 容器已挂载共享目录时优先本地直读（避免 SSH/SFTP 回连宿主机），并支持 ETag/304 缓存。
func handleRemoteFile(c *gin.Context, remote *RemoteExec, full string) {
	// 本地直读：容器挂载共享目录时文件即为本地路径，避免 SSH 回连
	if fi, err := os.Stat(full); err == nil && fi.Mode().IsRegular() {
		serveLocalFile(c, full, fi)
		return
	}
	size, err := remote.Size(full)
	if err != nil {
		c.JSON(404, gin.H{"error": "file not found"})
		return
	}
	f, err := remote.Open(full)
	if err != nil {
		c.JSON(404, gin.H{"error": "file not found"})
		return
	}
	defer f.Close()
	c.Header("Content-Type", mimeTypeOf(full))
	c.Header("Content-Length", strconv.FormatInt(size, 10))
	c.Status(http.StatusOK)
	buf := make([]byte, 64*1024)
	for {
		n, rerr := f.Read(buf)
		if n > 0 {
			if _, werr := c.Writer.Write(buf[:n]); werr != nil {
				return
			}
		}
		if rerr != nil {
			return
		}
	}
}

// serveLocalFile 本地文件响应：ETag/304 缓存 + Range 支持
func serveLocalFile(c *gin.Context, full string, fi os.FileInfo) {
	etag := fmt.Sprintf("\"%x-%x\"", fi.Size(), fi.ModTime().UnixNano())
	c.Header("ETag", etag)
	c.Header("Cache-Control", "public, max-age=3600")
	if inm := c.GetHeader("If-None-Match"); inm != "" && inm == etag {
		c.Status(http.StatusNotModified)
		return
	}
	c.Header("Content-Type", mimeTypeOf(full))
	c.Header("Accept-Ranges", "bytes")
	size := fi.Size()
	if rng := c.GetHeader("Range"); rng != "" {
		start, end, ok := parseRange(rng, size)
		if !ok {
			c.Status(http.StatusRequestedRangeNotSatisfiable)
			c.Header("Content-Range", fmt.Sprintf("bytes */%d", size))
			return
		}
		f, err := os.Open(full)
		if err != nil {
			c.JSON(404, gin.H{"error": "file not found"})
			return
		}
		defer f.Close()
		if _, err := f.Seek(start, io.SeekStart); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusPartialContent)
		c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
		c.Header("Content-Length", strconv.FormatInt(end-start+1, 10))
		_, _ = io.CopyN(c.Writer, f, end-start+1)
		return
	}
	f, err := os.Open(full)
	if err != nil {
		c.JSON(404, gin.H{"error": "file not found"})
		return
	}
	defer f.Close()
	c.Header("Content-Length", strconv.FormatInt(size, 10))
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, f)
}

var _ = gorm.ErrRecordNotFound
