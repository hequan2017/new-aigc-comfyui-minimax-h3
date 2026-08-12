package models

import "time"

// Instance 表示一个 ComfyUI 实例（每 GPU 一个）
type Instance struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	GPUIndex      int       `gorm:"column:gpu_index;uniqueIndex" json:"gpu_index"`
	Port          int       `gorm:"column:port" json:"port"`
	Status        string    `gorm:"column:status" json:"status"` // stopped/starting/running/error
	PID           int       `gorm:"column:pid" json:"pid"`
	EnableManager bool      `gorm:"column:enable_manager" json:"enable_manager"`
	QueueLen      int       `gorm:"column:queue_len" json:"queue_len"`
	VRAMFree      int64     `gorm:"column:vram_free" json:"vram_free"`
	VRAMTotal     int64     `gorm:"column:vram_total" json:"vram_total"`
	Util          float64   `gorm:"column:util" json:"util"`
	Temp          float64   `gorm:"column:temp" json:"temp"`
	Power         float64   `gorm:"column:power" json:"power"`
	LastChecked   time.Time `gorm:"column:last_checked" json:"last_checked"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Template 工作流模板
type Template struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Name         string    `json:"name"`
	Code         string    `gorm:"uniqueIndex" json:"code"`
	Description  string    `json:"description"`
	WorkflowJSON string    `gorm:"type:text" json:"workflow_json"`
	InputsJSON   string    `gorm:"type:text" json:"inputs_json"`
	IsSystem     bool      `json:"is_system"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// UploadFile 已上传素材文件
type UploadFile struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	TaskID    string    `gorm:"column:task_id;index" json:"task_id"`
	Type      string    `json:"type"` // image/video/audio
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}

// Task 生成任务
type Task struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	TaskID        string     `gorm:"column:task_id;uniqueIndex" json:"task_id"`
	TemplateID    uint       `gorm:"column:template_id" json:"template_id"`
	TemplateName  string     `gorm:"column:template_name" json:"template_name"`
	Prompt        string     `json:"prompt"`
	ParamsJSON    string     `gorm:"type:text" json:"params_json"`
	InputsJSON    string     `gorm:"type:text" json:"inputs_json"`
	InstanceID    *uint      `gorm:"column:instance_id" json:"instance_id"`
	GPUIndex      *int       `gorm:"column:gpu_index" json:"gpu_index"`
	Port          *int       `gorm:"column:port" json:"port"`
	ComfyPromptID string     `gorm:"column:comfy_prompt_id" json:"comfy_prompt_id"`
	Status        string     `json:"status"` // pending/queued/running/success/failed/cancelled
	Progress      float64    `json:"progress"`
	CurrentNode   string     `gorm:"column:current_node" json:"current_node"`
	Error         string     `json:"error"`
	ResultFiles   string     `gorm:"column:result_files;type:text" json:"result_files"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	StartedAt     *time.Time `gorm:"column:started_at" json:"started_at"`
	FinishedAt    *time.Time `gorm:"column:finished_at" json:"finished_at"`
}

// Event 任务事件日志
type Event struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	TaskID    string    `gorm:"column:task_id;index" json:"task_id"`
	Type      string    `json:"type"` // created/submitted/progress/executing/completed/failed/error
	Node      string    `json:"node"`
	Progress  float64   `json:"progress"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

// Setting 平台设置（key-value，如火山引擎 API Key / 模型 ID 等）
type Setting struct {
	Key       string    `gorm:"primaryKey" json:"key"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Project 漫剧项目：选题 → 创作方案 → 剧本 → 分镜画面 → 视频 → 合并成片
type Project struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	Title         string    `json:"title"`
	Genre         string    `json:"genre"`                         // 题材（可选，支持组合如"科幻+悬疑"）
	Style         string    `json:"style"`                         // 画风（可选）
	Synopsis      string    `json:"synopsis"`                      // 故事创意/一句话梗概
	Audience      string    `json:"audience"`                      // 受众：女频/男频/全龄
	Tone          string    `json:"tone"`                          // 基调：爽/甜/虐/燃/搞笑/悬疑
	Ending        string    `json:"ending"`                        // 结局：HE/BE/OE
	Episodes      int       `json:"episodes"`                      // 目标集数（用于节奏规划）
	AspectRatio   string    `json:"aspect_ratio"`                  // 画幅：16:9 横屏 / 9:16 竖屏 / 1:1 方形（默认 16:9）
	Plan          string    `gorm:"type:text" json:"plan"`         // 创作方案 JSON（short-drama 方法论产物）
	Script        string    `gorm:"type:text" json:"script"`       // 最近一集剧本（兼容旧数据）
	Scripts       string    `gorm:"type:text" json:"scripts"`      // 按集剧本 JSON map[int]string（episode_n -> 剧本正文）
	VisualBible   string    `gorm:"type:text" json:"visual_bible"` // 角色外观与统一画风基准
	Status        string    `json:"status"`                        // draft/plan_done/script_done/producing/ready/finished/failed
	Error         string    `json:"error"`
	Generation    uint      `gorm:"default:0" json:"generation"`                       // 当前生成版本，防止旧任务回写新分镜
	PipelineStage string    `gorm:"column:pipeline_stage;index" json:"pipeline_stage"` // plan/script/images/videos/merge/finished/failed
	PipelineEpisode int       `gorm:"column:pipeline_episode;default:1" json:"pipeline_episode"` // 当前一键生成流水线的目标集数
	AutoGenerate  bool      `gorm:"column:auto_generate" json:"auto_generate"`
	StopAfterScript bool      `gorm:"column:stop_after_script" json:"-"` // 自动流水线生成完第一集剧本后停止（后续由人工处理）
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Scene 分镜场景（项目内顺序片段）
type Scene struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	ProjectID    uint      `gorm:"column:project_id;index;uniqueIndex:idx_scene_project_generation_episode_order" json:"project_id"`
	EpisodeN     int       `gorm:"column:episode_n;default:1;uniqueIndex:idx_scene_project_generation_episode_order" json:"episode_n"` // 所属集数（从 1 开始）
	Order        int       `gorm:"uniqueIndex:idx_scene_project_generation_episode_order" json:"order"`                                // 场景序号（从 1 开始）
	Generation   uint      `gorm:"uniqueIndex:idx_scene_project_generation_episode_order" json:"generation"`
	Title        string    `json:"title"`                                     // 场景标题
	Content      string    `json:"content"`                                   // 场景正文（作为视频提示词）
	ImagePrompt  string    `json:"image_prompt"`                              // 文生图提示词
	Duration     float64   `gorm:"default:5" json:"duration"`                 // 场景目标时长（秒）
	Characters   string    `json:"characters"`                                // 出场角色名（逗号分隔），用于一致性注入
	ImageFile    string    `json:"image_file"`                                // 首帧图文件名（input/<project_id>/ 下）
	ImageToken   string    `gorm:"column:image_token" json:"-"`               // 单次生成令牌，防止并发或过期结果回写
	VideoTaskID  string    `gorm:"column:video_task_id" json:"video_task_id"` // 关联视频生成任务
	VideoGPU     *int      `gorm:"column:video_gpu" json:"video_gpu"`
	VideoFile    string    `gorm:"column:video_file" json:"video_file"` // 输出相对路径（subfolder/filename）
	Status       string    `json:"status"`                              // pending/image_pending/image_ready/video_pending/video_running/video_ready/failed
	Error        string    `json:"error"`
	ImageRetries int       `gorm:"column:image_retries" json:"image_retries"` // 画面生成已重试次数
	VideoRetries int       `gorm:"column:video_retries" json:"video_retries"` // 视频生成已重试次数
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Character 角色卡：项目内可复用的人物资产，统一外貌/服装设定以保证跨场景一致性
type Character struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ProjectID uint      `gorm:"column:project_id;index;uniqueIndex:idx_character_project_name" json:"project_id"`
	Name      string    `gorm:"uniqueIndex:idx_character_project_name" json:"name"` // 项目内唯一
	Role      string    `json:"role"`                                               // 身份：主角/女主/反派/配角…
	Trait     string    `gorm:"type:text" json:"trait"`                             // 外貌特征（发型/五官/体型）
	Style     string    `gorm:"type:text" json:"style"`                             // 服装造型
	Portrait  string    `json:"portrait"`                                           // 标准参考像文件名（input/<project_id>/ 下）
	Source    string    `json:"source"`                                             // auto(方案抽取) / manual(手动新建)
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// MergeTask 视频合并任务（把多个场景视频合并剪辑成片）
type MergeTask struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	ProjectID  uint      `gorm:"column:project_id;index" json:"project_id"`
	EpisodeN   int       `gorm:"column:episode_n;default:1" json:"episode_n"` // 所属集数
	Title      string    `json:"title"`
	SceneOrder string    `json:"scene_order"` // 按序合并的场景 ID（逗号分隔）
	Generation uint      `gorm:"index" json:"generation"`
	Status     string    `json:"status"`      // pending/running/success/failed
	OutputFile string    `json:"output_file"` // 合并输出文件（相对 output_workers/gpu0/ 路径）
	Subtitle   bool      `gorm:"default:false" json:"subtitle"` // 是否生成了配音字幕（SRT 与成片同名）
	Error      string    `json:"error"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Material 素材库：生成的图片/视频自动入库，也支持手动上传管理
type Material struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `json:"name"`
	Type      string    `gorm:"index" json:"type"`                         // image/video/audio
	Source    string    `json:"source"`                                    // scene(场景生成)/upload(手动上传)
	ProjectID *uint     `gorm:"column:project_id;index" json:"project_id"` // 关联项目（可选）
	SceneID   *uint     `gorm:"column:scene_id" json:"scene_id"`           // 关联场景（可选）
	Path      string    `json:"path"`                                      // input 相对路径（task_id/filename）
	Prompt    string    `json:"prompt"`                                    // 生成提示词（可选）
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}

// Dialogue 场景对白：用于 TTS 配音与 SRT 字幕生成
type Dialogue struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	SceneID   uint      `gorm:"column:scene_id;index" json:"scene_id"`
	ProjectID uint      `gorm:"column:project_id;index" json:"project_id"`
	Order     int       `json:"order"`                 // 场景内句序（从 1 开始）
	Character string    `json:"character"`             // 说话人角色名（空表示旁白）
	Text      string    `gorm:"type:text" json:"text"` // 台词正文
	Voice     string    `json:"voice"`                 // TTS 音色（voice_type）
	AudioFile string    `json:"audio_file"`            // 合成音频文件名（input/<pid>/dub/ 下）
	Status    string    `json:"status"`                // pending/synthesizing/ready/failed
	Error     string    `json:"error"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
