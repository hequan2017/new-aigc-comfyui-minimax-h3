package service

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

// Service 聚合所有子服务
type Service struct {
	Cfg      *config.Config
	DB       *gorm.DB
	Mgr      *InstanceManager
	Mon      *GPUMonitor
	Tasks    *TaskService
	Hub      *Hub
	Upload   *UploadManager
	Remote   *RemoteExec
	Volc      *VolcClient
	Projects  *ProjectService
	Materials *MaterialService
}

func New(cfg *config.Config, db *gorm.DB) *Service {
	hub := NewHub()
	remote := NewRemoteExec(cfg.Remote)
	// 容器挂载了算力节点共享目录（ComfyDir/DataDir 本机可见）时，文件操作直读本地，跳过 SSH
	remote.SetLocalRoots([]string{cfg.Comfy.ComfyDir, cfg.Storage.DataDir})
	mgr := NewInstanceManager(cfg, db, remote)
	mon := NewGPUMonitor(cfg, db, remote)
	tasks := NewTaskService(cfg, db, mgr, mon, hub, remote)
	upload := NewUploadManager(cfg, db, remote)
	volc := NewVolcClient(db)
	materials := NewMaterialService(cfg, db, remote, upload)
	projects := NewProjectService(cfg, db, volc, tasks, remote, upload, hub, materials)
	return &Service{Cfg: cfg, DB: db, Mgr: mgr, Mon: mon, Tasks: tasks, Hub: hub, Upload: upload, Remote: remote, Volc: volc, Projects: projects, Materials: materials}
}

// comfyHost 返回 ComfyUI 实例所在主机（docker 模式为容器名，远程模式为算力节点 IP，本地模式为本机）
func (s *Service) comfyHost() string {
	return s.Mgr.comfyHostOf(0)
}

// comfyHostForPort 按端口返回实例所在主机（docker 模式下每个实例是独立容器）
func (s *Service) comfyHostForPort(port int) string {
	return s.Mgr.comfyHostOf(port)
}

// Start 启动后台服务
func (s *Service) Start() {
	if err := InitSystemTemplates(s.DB); err != nil {
		log.Printf("[templates] seed failed: %v", err)
	}
	if s.Remote.Enabled() {
		if _, err := s.Remote.Run("echo ok"); err != nil {
			log.Printf("[remote] SSH 连接算力节点 %s 失败: %v", s.Cfg.Remote.Host, err)
		} else {
			log.Printf("[remote] 已连接算力节点 %s (SSH)", s.Cfg.Remote.Host)
		}
	}
	s.Mon.Start()
	s.Tasks.StartRecovery()
	s.Projects.WatchSceneVideos()
}

func (s *Service) Stop() {
	s.Mon.Stop()
	s.Projects.Stop()
	s.Remote.Close()
}

// ---------- 实例 ----------

func (s *Service) HandleListInstances(c *gin.Context) {
	insts := s.Mgr.List()
	// 刷新运行状态
	s.refreshInstances(&insts)
	c.JSON(200, insts)
}

// refreshInstances 探测每个实例实际状态
func (s *Service) refreshInstances(insts *[]models.Instance) {
	for i := range *insts {
		inst := &(*insts)[i]
		client := NewComfyClient(s.comfyHostForPort(inst.Port), inst.Port)
		if err := client.Ping(); err == nil {
			if inst.Status != "running" {
				s.DB.Model(inst).Update("status", "running")
				inst.Status = "running"
			}
			if load, err := client.GetLoad(); err == nil {
				inst.QueueLen = load.QueueRunning + load.QueuePending
				s.DB.Model(inst).Updates(map[string]any{
					"queue_len": inst.QueueLen, "vram_free": load.VRAMFree,
				})
			}
		} else {
			// starting/running/error 都必须以实际端口监听为准，避免保留过期 PID。
			if inst.Status != "stopped" && s.Mgr.FindPID(inst.Port) == 0 {
				s.DB.Model(inst).Updates(map[string]any{"status": "stopped", "pid": 0})
				inst.Status = "stopped"
				inst.PID = 0
			}
		}
	}
}

func (s *Service) HandleInstanceStart(c *gin.Context) {
	gpu, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid id"})
		return
	}
	insts := s.Mgr.List()
	if gpu < 0 || gpu >= len(insts) {
		c.JSON(404, gin.H{"error": "instance not found"})
		return
	}
	if err := s.Mgr.Start(gpu); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": fmt.Sprintf("GPU %d 实例启动中 (端口 %d)", gpu, s.Mgr.PortOf(gpu))})
}

func (s *Service) HandleInstanceStop(c *gin.Context) {
	gpu, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid id"})
		return
	}
	if err := s.Mgr.Stop(gpu); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Service) HandleInstanceRestart(c *gin.Context) {
	gpu, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid id"})
		return
	}
	_ = s.Mgr.Stop(gpu)
	time.Sleep(2 * time.Second)
	if err := s.Mgr.Start(gpu); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// batchConcurrency 实例批量操作并发数：query 优先，默认读平台设置 video_concurrency（默认 4），范围 1~GPU 数
func (s *Service) batchConcurrency(c *gin.Context) int {
	n := 2
	if s.Volc != nil {
		if v := s.Volc.GetSetting("video_concurrency", "4"); v != "" {
			if x, err := strconv.Atoi(v); err == nil && x >= 1 {
				n = x
			}
		}
	}
	if v := c.Query("concurrency"); v != "" {
		if x, err := strconv.Atoi(v); err == nil && x >= 1 {
			n = x
		}
	}
	if max := s.Cfg.Comfy.GPUCount; n > max {
		n = max
	}
	return n
}

// HandleStartAll 一键启动全部实例（异步执行，避免 8 实例启动耗时超过 HTTP 超时；并发数可配）
func (s *Service) HandleStartAll(c *gin.Context) {
	conc := s.batchConcurrency(c)
	go func() {
		if err := s.Mgr.StartAll(conc); err != nil {
			log.Printf("[instance] start-all: %v", err)
		}
	}()
	c.JSON(200, gin.H{"ok": true})
}

// HandleStopAll 一键停止全部实例（异步执行；并发数可配）
func (s *Service) HandleStopAll(c *gin.Context) {
	conc := s.batchConcurrency(c)
	go func() {
		if err := s.Mgr.StopAll(conc); err != nil {
			log.Printf("[instance] stop-all: %v", err)
		}
	}()
	c.JSON(200, gin.H{"ok": true})
}

// HandleRestartAll 一键重启全部实例（异步执行；并发数可配）
func (s *Service) HandleRestartAll(c *gin.Context) {
	conc := s.batchConcurrency(c)
	go func() {
		if err := s.Mgr.RestartAll(conc); err != nil {
			log.Printf("[instance] restart-all: %v", err)
		}
	}()
	c.JSON(200, gin.H{"ok": true})
}

// ---------- GPU ----------

func (s *Service) HandleListGPUs(c *gin.Context) {
	s.Mon.CollectOnce()
	c.JSON(200, gin.H{
		"gpus": s.Mon.Snapshot(),
		"host": s.Mon.HostSnapshot(),
	})
}

// ---------- 模板 ----------

func (s *Service) HandleListTemplates(c *gin.Context) {
	var list []models.Template
	s.DB.Where("enabled = ?", true).Order("id").Find(&list)
	c.JSON(200, list)
}

// ---------- 任务 ----------

func (s *Service) HandleListTasks(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	status := c.Query("status")
	tplName := c.Query("template_name")

	var tasks []models.Task
	query := s.DB.Model(&models.Task{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if tplName != "" {
		query = query.Where("template_name = ?", tplName)
	}
	var total int64
	query.Count(&total)
	query.Order("id desc").Offset((page - 1) * size).Limit(size).Find(&tasks)
	c.JSON(200, gin.H{"total": total, "items": tasks, "page": page, "size": size})
}

// HandleClearTasks 清空已结束的任务记录。存在活动任务时拒绝操作，避免丢失调度状态。
func (s *Service) HandleClearTasks(c *gin.Context) {
	var active int64
	if err := s.DB.Model(&models.Task{}).
		Where("status IN ?", []string{"pending", "queued", "running"}).
		Count(&active).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if active > 0 {
		c.JSON(409, gin.H{"error": fmt.Sprintf("仍有 %d 个活动任务，请等待完成或取消后再清空", active)})
		return
	}

	var count int64
	if err := s.DB.Model(&models.Task{}).Count(&count).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("1 = 1").Delete(&models.Event{}).Error; err != nil {
			return err
		}
		return tx.Where("1 = 1").Delete(&models.Task{}).Error
	}); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "deleted": count})
}

func (s *Service) HandleGetTask(c *gin.Context) {
	var task models.Task
	if err := s.DB.Where("task_id = ?", c.Param("id")).First(&task).Error; err != nil {
		c.JSON(404, gin.H{"error": "task not found"})
		return
	}
	c.JSON(200, task)
}

// HandleCreateTask 创建任务（包含上传的文件引用）
func (s *Service) HandleCreateTask(c *gin.Context) {
	var req struct {
		TemplateID uint                  `json:"template_id"`
		Prompt     string                `json:"prompt"`
		Params     map[string]any        `json:"params"`
		Files      map[string][]FileMeta `json:"files"`
		ForcePort  *int                  `json:"force_port,omitempty"`
		Seed       *int64                `json:"seed,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误: " + err.Error()})
		return
	}
	if req.Params == nil {
		req.Params = map[string]any{}
	}
	task, err := s.Tasks.CreateTask(CreateTaskReq{
		TemplateID: req.TemplateID,
		Prompt:     req.Prompt,
		Params:     req.Params,
		Files:      req.Files,
		ForcePort:  req.ForcePort,
		Seed:       req.Seed,
	})
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	// 异步执行
	go func() {
		if err := s.Tasks.Execute(task.TaskID); err != nil {
			log.Printf("[task] %s execute failed: %v", task.TaskID, err)
		}
	}()
	c.JSON(200, task)
}

func (s *Service) HandleCancelTask(c *gin.Context) {
	if err := s.Tasks.CancelTask(c.Param("id")); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// HandleRequeueTask 重新排队：解除 GPU 绑定后重新调度（重新分配 GPU）
func (s *Service) HandleRequeueTask(c *gin.Context) {
	if err := s.Tasks.RequeueTask(c.Param("id")); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// HandleCancelAllTasks 取消全部未完成任务
func (s *Service) HandleCancelAllTasks(c *gin.Context) {
	count, err := s.Tasks.CancelAllTasks()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "cancelled": count})
}

// HandleRerunTask 重新运行失败/取消的任务
func (s *Service) HandleRerunTask(c *gin.Context) {
	var task models.Task
	if err := s.DB.Where("task_id = ?", c.Param("id")).First(&task).Error; err != nil {
		c.JSON(404, gin.H{"error": "task not found"})
		return
	}
	if task.Status == "pending" || task.Status == "queued" || task.Status == "running" {
		c.JSON(400, gin.H{"error": "任务还在运行中"})
		return
	}
	// 复制为新任务
	var tpl models.Template
	if err := s.DB.First(&tpl, task.TemplateID).Error; err != nil {
		c.JSON(400, gin.H{"error": "模板不存在"})
		return
	}
	newTask := models.Task{
		TaskID:       s.Tasks.NewTaskID(),
		TemplateID:   task.TemplateID,
		TemplateName: task.TemplateName,
		Prompt:       task.Prompt,
		ParamsJSON:   task.ParamsJSON,
		InputsJSON:   task.InputsJSON,
		Status:       "pending",
	}
	if err := s.DB.Create(&newTask).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	go func() {
		if err := s.Tasks.Execute(newTask.TaskID); err != nil {
			log.Printf("[task] %s rerun failed: %v", newTask.TaskID, err)
		}
	}()
	c.JSON(200, newTask)
}

// ---------- 文件上传 ----------

func (s *Service) HandleUpload(c *gin.Context) {
	ftype := c.PostForm("type")
	taskID := c.PostForm("task_id")
	if taskID == "" {
		taskID = "console-upload"
	}
	if ftype == "" {
		ftype = "image"
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "file required: " + err.Error()})
		return
	}
	defer file.Close()

	name := header.Filename
	if name == "" {
		name = "upload_" + fmt.Sprint(time.Now().UnixNano())
	}
	// 生成唯一文件名
	ext := filepath.Ext(name)
	name = fmt.Sprintf("%s_%d%s", strings.TrimSuffix(name, ext), time.Now().UnixNano()%1e6, ext)

	data, err := io.ReadAll(file)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	path, size, err := s.Upload.SaveFile(taskID, ftype, name, data)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{
		"name": name, "path": path, "size": size, "task_id": taskID,
	})
}

// ---------- 结果文件 ----------

// HandleOutputFile 提供任务输出文件静态访问: /api/output/:gpu/*path
// 支持 HTTP Range(视频拖动播放)、正确 MIME 类型与下载附件头（?download=1）
func (s *Service) HandleOutputFile(c *gin.Context) {
	gpu := c.Param("gpu")
	path := c.Param("path")
	if strings.Contains(path, "..") {
		c.JSON(400, gin.H{"error": "invalid path"})
		return
	}
	root := filepath.Join(s.Cfg.Comfy.ComfyDir, "output_workers", "gpu"+gpu)
	full := filepath.Join(root, path)

	size, err := s.Remote.Size(full)
	if err != nil {
		c.JSON(404, gin.H{"error": "file not found"})
		return
	}
	f, err := s.Remote.OpenSeek(full)
	if err != nil {
		c.JSON(404, gin.H{"error": "file not found"})
		return
	}
	defer f.Close()

	c.Header("Accept-Ranges", "bytes")
	c.Header("Content-Type", mimeTypeOf(full))
	if c.Query("download") != "" {
		c.Header("Content-Disposition", "attachment; filename=\""+filepath.Base(full)+"\"")
	} else {
		c.Header("Content-Disposition", "inline; filename=\""+filepath.Base(full)+"\"")
	}

	// Range 请求：206 部分内容
	if rng := c.GetHeader("Range"); rng != "" {
		start, end, ok := parseRange(rng, size)
		if !ok {
			c.Status(http.StatusRequestedRangeNotSatisfiable)
			c.Header("Content-Range", fmt.Sprintf("bytes */%d", size))
			return
		}
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

	c.Header("Content-Length", strconv.FormatInt(size, 10))
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, f)
}

// HandleMediaInfo 返回输出文件的元数据（分辨率/时长等）
func (s *Service) HandleMediaInfo(c *gin.Context) {
	gpu := c.Param("gpu")
	path := c.Param("path")
	if strings.Contains(path, "..") {
		c.JSON(400, gin.H{"error": "invalid path"})
		return
	}
	root := filepath.Join(s.Cfg.Comfy.ComfyDir, "output_workers", "gpu"+gpu)
	full := filepath.Join(root, path)
	info, err := s.Remote.ProbeMedia(full)
	if err != nil {
		c.JSON(404, gin.H{"error": "media not found"})
		return
	}
	c.JSON(200, info)
}

// mimeTypeOf 根据扩展名返回 MIME
func mimeTypeOf(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mov":
		return "video/quicktime"
	case ".mkv":
		return "video/x-matroska"
	case ".gif":
		return "image/gif"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".flac":
		return "audio/flac"
	default:
		return "application/octet-stream"
	}
}

// parseRange 解析 bytes=start-end 请求
func parseRange(rng string, size int64) (start, end int64, ok bool) {
	if !strings.HasPrefix(rng, "bytes=") {
		return 0, 0, false
	}
	spec := strings.TrimPrefix(rng, "bytes=")
	if i := strings.Index(spec, ","); i >= 0 {
		spec = spec[:i] // 仅支持单区间
	}
	dash := strings.Index(spec, "-")
	if dash < 0 {
		return 0, 0, false
	}
	startStr, endStr := spec[:dash], spec[dash+1:]
	if startStr == "" { // suffix range: bytes=-N
		n, err := strconv.ParseInt(endStr, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		if n > size {
			n = size
		}
		return size - n, size - 1, size > 0
	}
	start, err := strconv.ParseInt(startStr, 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, false
	}
	if endStr == "" {
		end = size - 1
	} else if end, err = strconv.ParseInt(endStr, 10, 64); err != nil {
		return 0, 0, false
	}
	if end >= size {
		end = size - 1
	}
	if end < start {
		return 0, 0, false
	}
	return start, end, true
}

// ---------- WS ----------

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (s *Service) HandleWS(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	s.Hub.Register(conn)
	// 连接建立后推送一次实例+任务快照
	snapshot := gin.H{
		"instances": s.Mgr.List(),
		"gpus":      s.Mon.Snapshot(),
	}
	s.send(conn, "snapshot", snapshot)

	go func() {
		defer s.Hub.Unregister(conn)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
}

// send 经 Hub 的写锁安全地推送给单个连接
func (s *Service) send(conn *websocket.Conn, type_ string, data any) {
	s.Hub.Send(conn, type_, data)
}
