package service

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

// errNoFreeGPU GPU 全忙时的调度错误：任务保持 pending 排队等待，不判定失败
var errNoFreeGPU = errors.New("所有 GPU 都在执行任务，任务排队等候中")

// errVideoConcurrencyLimit 视频生成并发达到平台设置上限：任务保持 pending 排队等待
var errVideoConcurrencyLimit = errors.New("视频生成并发已达上限，排队等候中")

// TaskService 任务创建、调度、提交、进度跟踪
type TaskService struct {
	cfg         *config.Config
	db          *gorm.DB
	manager     *InstanceManager
	gpuMon      *GPUMonitor
	hub         *Hub
	remote      *RemoteExec
	mu          sync.Mutex
	scheduleMu  sync.Mutex                    // 保证选卡与 GPU 占用登记原子化
	executing   map[string]bool               // task_id -> 执行中标记（防同任务并发执行）
	wsMu        sync.Mutex                    // 保护 wsActive/nodeClasses，避免与 mu 嵌套死锁
	wsActive    map[string]chan struct{}      // task_id -> stop
	nodeClasses map[string]map[string]float64 // task_id -> node_id -> 进度权重
	retryMu     sync.Mutex                    // 保护 retrying
	retrying    map[string]bool               // task_id -> GPU 排队重试循环运行中标记
}

func NewTaskService(cfg *config.Config, db *gorm.DB, manager *InstanceManager, gpuMon *GPUMonitor, hub *Hub, remote *RemoteExec) *TaskService {
	return &TaskService{
		cfg:         cfg,
		db:          db,
		manager:     manager,
		gpuMon:      gpuMon,
		hub:         hub,
		remote:      remote,
		executing:   map[string]bool{},
		wsActive:    map[string]chan struct{}{},
		nodeClasses: map[string]map[string]float64{},
		retrying:    map[string]bool{},
	}
}

// comfyHost 返回 ComfyUI 实例所在主机（docker 模式为容器名，远程模式为算力节点 IP，本地模式为本机）
func (s *TaskService) comfyHost() string {
	return s.manager.comfyHostOf(0)
}

// comfyHostForPort 按端口返回实例所在主机（docker 模式下每个实例是独立容器）
func (s *TaskService) comfyHostForPort(port int) string {
	return s.manager.comfyHostOf(port)
}

// ---------- 任务 ID ----------

func (s *TaskService) NewTaskID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return time.Now().Format("20060102-150405-") + hex.EncodeToString(b)
}

// ---------- 模板 ----------

type TemplateInput struct {
	Key      string   `json:"key"`
	Type     string   `json:"type"` // int/float/string/image/images/video/videos/audio/audios/prompt/select
	Label    string   `json:"label"`
	Required bool     `json:"required"`
	Default  any      `json:"default,omitempty"`
	Min      any      `json:"min,omitempty"`
	Max      any      `json:"max,omitempty"`
	Step     any      `json:"step,omitempty"`
	Options  []string `json:"options,omitempty"`
	MaxCount int      `json:"max_count,omitempty"`
	Help     string   `json:"help,omitempty"`
}

var fileInputTypes = map[string]bool{
	"image": true, "images": true,
	"video": true, "videos": true,
	"audio": true, "audios": true,
}

// normalizeTemplateFiles 根据模板输入定义校验素材，并将复数素材展开为工作流占位符使用的索引参数。
// 例如 ref_images 会同时保留原始数组，并展开为 ref_image_0、ref_image_1。
func normalizeTemplateFiles(tpl *models.Template, params map[string]any, files map[string][]FileMeta) error {
	var inputs []TemplateInput
	if err := json.Unmarshal([]byte(tpl.InputsJSON), &inputs); err != nil {
		return fmt.Errorf("模板输入定义解析失败: %w", err)
	}

	for _, input := range inputs {
		if !fileInputTypes[input.Type] {
			continue
		}
		items := files[input.Key]
		if input.Required && len(items) == 0 {
			return fmt.Errorf("缺少必填素材: %s", input.Label)
		}
		limit := input.MaxCount
		if limit == 0 && !strings.HasSuffix(input.Type, "s") {
			limit = 1
		}
		if limit > 0 && len(items) > limit {
			return fmt.Errorf("%s 最多允许 %d 个素材", input.Label, limit)
		}
		if len(items) == 0 {
			continue
		}

		refs := make([]map[string]any, 0, len(items))
		for i, file := range items {
			ref := map[string]any{"task_id": file.TaskID, "name": file.Name}
			refs = append(refs, ref)
			if strings.HasSuffix(input.Type, "s") {
				params[fmt.Sprintf("%s_%d", strings.TrimSuffix(input.Key, "s"), i)] = ref
			}
		}
		if strings.HasSuffix(input.Type, "s") {
			params[input.Key] = refs
		} else {
			params[input.Key] = refs[0]
		}
	}
	return nil
}

// ---------- 工作流渲染 ----------

var (
	placeholderRe = regexp.MustCompile(`\{\{([^}]+)\}\}`)
	fileKeyRe     = regexp.MustCompile(`^(img|vid|aud):(.+)$`)
)

// RenderWorkflow 用参数渲染模板工作流，返回可提交的 workflow map
func (s *TaskService) RenderWorkflow(tpl *models.Template, params map[string]any) (map[string]any, error) {
	var nodes map[string]any
	if err := json.Unmarshal([]byte(tpl.WorkflowJSON), &nodes); err != nil {
		return nil, fmt.Errorf("模板 JSON 解析失败: %w", err)
	}

	// 时长(秒) -> 帧数, 对齐 17k+5 网格 (与 ComfyUI align_frame_count 一致)
	if _, ok := params["length"]; !ok {
		duration := 5.0
		if d, ok := params["duration"].(float64); ok && d > 0 {
			duration = d
		}
		fps := 24.0
		if f, ok := params["fps"].(float64); ok && f > 0 {
			fps = f
		}
		length := int(math.Round(duration * fps))
		if length < 5 {
			length = 5
		}
		for length%17 != 5 {
			length++
		}
		params["length"] = length
	}
	if _, ok := params["seed"]; !ok {
		params["seed"] = 0
	}
	// seed=-1 表示随机
	if seed, ok := params["seed"].(float64); ok && seed == -1 {
		b := make([]byte, 8)
		_, _ = rand.Read(b)
		params["seed"] = int64(int(seed)) + int64(binary.BigEndian.Uint64(b)%1_000_000_000)
	}

	// 第一轮: 替换标量占位符。缺参数时收集首个错误，统一向外抛出，
	// 避免被后续 "模板为空" 判断掩盖真实原因。
	var missingErr error
	walkJSON(nodes, func(path string, key string, val any) any {
		str, ok := val.(string)
		if !ok {
			return val
		}
		replaced := placeholderRe.ReplaceAllStringFunc(str, func(m string) string {
			inner := m[2 : len(m)-2]
			if fileKeyRe.FindStringSubmatch(inner) != nil {
				return m // 文件占位符第二轮处理
			}
			if inner == "task_id" {
				return fmt.Sprint(params["_task_id"])
			}
			v, ok := params[inner]
			if !ok {
				if missingErr == nil {
					missingErr = fmt.Errorf("缺少参数 %s", inner)
				}
				return m
			}
			return fmt.Sprint(v)
		})
		return replaced
	})
	if missingErr != nil {
		return nil, missingErr
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("模板为空")
	}

	// 第二轮: 处理文件占位符 {{img:xxx}} / {{vid:xxx}} / {{aud:xxx}}
	// 收集要删除的节点
	removeNodes := map[string]bool{}
	for nodeID, raw := range nodes {
		node, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		inputs, _ := node["inputs"].(map[string]any)
		for k, v := range inputs {
			str, ok := v.(string)
			if !ok {
				continue
			}
			keys := placeholderRe.FindAllString(str, -1)
			for _, m := range keys {
				inner := m[2 : len(m)-2]
				km := fileKeyRe.FindStringSubmatch(inner)
				if km == nil {
					continue
				}
				paramKey := km[2]
				fileName := s.fileNameOf(params, paramKey)
				if fileName == "" {
					// 文件未提供: 删除该节点
					removeNodes[nodeID] = true
					delete(inputs, k)
					continue
				}
				inputs[k] = strings.ReplaceAll(str, m, fileName)
			}
		}
	}
	// 删除不可用的节点引用输入
	s.pruneRefs(nodes, removeNodes)

	// 处理动态数量参数: images/videos/audios
	// 以 {{img:ref_image_0}} 形式内嵌在 ref_images map 中,由模板写死 max 数量,未提供的自动移除
	return nodes, nil
}

func (s *TaskService) fileNameOf(params map[string]any, key string) string {
	v, ok := params[key]
	if !ok || v == nil {
		return ""
	}
	var taskID, name string
	switch t := v.(type) {
	case string:
		name = t
	case map[string]any:
		taskID, _ = t["task_id"].(string)
		name, _ = t["name"].(string)
	case []any:
		if len(t) > 0 {
			if m, ok := t[0].(map[string]any); ok {
				taskID, _ = m["task_id"].(string)
				name, _ = m["name"].(string)
			}
		}
	}
	if name == "" {
		return ""
	}
	if taskID != "" {
		return taskID + "/" + name
	}
	return name
}

// pruneRefs 删除引用已被移除节点的输入项
func (s *TaskService) pruneRefs(nodes map[string]any, removed map[string]bool) {
	for _, raw := range nodes {
		node, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		inputs, _ := node["inputs"].(map[string]any)
		for k, v := range inputs {
			switch t := v.(type) {
			case []any:
				if len(t) == 2 {
					if id, ok := t[0].(string); ok && removed[id] {
						delete(inputs, k)
					}
				}
			case map[string]any:
				for kk, vv := range t {
					if arr, ok := vv.([]any); ok && len(arr) == 2 {
						if id, ok := arr[0].(string); ok && removed[id] {
							delete(t, kk)
						}
					}
				}
			}
		}
	}
	for id := range removed {
		delete(nodes, id)
	}
}

// walkJSON 深度遍历 JSON 树，替换字符串占位符
func walkJSON(v any, fn func(path, key string, val any) any) {
	switch t := v.(type) {
	case map[string]any:
		for k, vv := range t {
			t[k] = fn(k, k, vv)
			walkJSON(t[k], fn)
		}
	case []any:
		for i := range t {
			t[i] = fn("", fmt.Sprint(i), t[i])
			walkJSON(t[i], fn)
		}
	}
}

// ---------- 调度 ----------

type instanceLoad struct {
	inst     models.Instance
	queueLen int
	vramFree int64
	up       bool
}

// pickInstance 只从平台与 ComfyUI 都确认空闲的实例中选择。
// 空闲显存仅用于多个空闲实例之间的排序，不能作为实例是否有任务的判断依据。
func (s *TaskService) pickInstance(forcePort *int) (*models.Instance, error) {
	var insts []models.Instance
	s.db.Order("gpu_index").Find(&insts)
	if len(insts) == 0 {
		return nil, fmt.Errorf("没有可用实例")
	}
	if forcePort != nil {
		for i := range insts {
			if insts[i].Port == *forcePort {
				if insts[i].Status != "running" {
					return nil, fmt.Errorf("实例端口 %d 未运行", *forcePort)
				}
				if s.platformPortBusy(insts[i].Port) {
					return nil, errNoFreeGPU
				}
				load := s.probeInstanceLoad(insts[i])
				if !load.up {
					return nil, fmt.Errorf("实例端口 %d 无法连接", *forcePort)
				}
				if load.queueLen > 0 {
					return nil, errNoFreeGPU
				}
				return &insts[i], nil
			}
		}
		return nil, fmt.Errorf("实例端口 %d 不存在", *forcePort)
	}

	// 收集运行中的实例，并发探测负载。串行探测 8 实例会累加单次 HTTP 延迟，
	// 并发后总耗时由最慢的一个实例决定。
	var running []models.Instance
	for i := range insts {
		if insts[i].Status == "running" {
			running = append(running, insts[i])
		}
	}
	if len(running) == 0 {
		return nil, fmt.Errorf("没有运行中的 ComfyUI 实例，请先启动实例")
	}

	// 平台任务绑定是第一层占用判断。按端口识别具体 ComfyUI 进程，避免只依赖显存缓存或 GPU 序号。
	var busyPorts []int
	s.db.Model(&models.Task{}).
		Where("status IN ? AND port IS NOT NULL", []string{"running", "queued"}).
		Distinct("port").Pluck("port", &busyPorts)
	busy := make(map[int]bool, len(busyPorts))
	for _, port := range busyPorts {
		busy[port] = true
	}
	free := make([]models.Instance, 0, len(running))
	for _, inst := range running {
		if !busy[inst.Port] {
			free = append(free, inst)
		}
	}
	if len(free) == 0 {
		return nil, errNoFreeGPU
	}
	running = free

	loads := make([]instanceLoad, len(running))
	var wg sync.WaitGroup
	for i := range running {
		wg.Add(1)
		go func(idx int, inst models.Instance) {
			defer wg.Done()
			loads[idx] = s.probeInstanceLoad(inst)
		}(i, running[i])
	}
	wg.Wait()

	// ComfyUI 队列是第二层占用判断，用于拦截绕过平台直接提交到进程的任务。
	// 只有平台无活动任务且 ComfyUI 队列为零的实例才进入显存排序。
	idle := make([]instanceLoad, 0, len(loads))
	anyUp := false
	for _, load := range loads {
		if !load.up {
			continue
		}
		anyUp = true
		if load.queueLen == 0 {
			idle = append(idle, load)
		}
	}
	if len(idle) == 0 {
		if anyUp {
			return nil, errNoFreeGPU
		}
		return nil, fmt.Errorf("没有可用的 ComfyUI 实例")
	}
	rankInstanceLoads(idle)
	return &idle[0].inst, nil
}

func (s *TaskService) platformPortBusy(port int) bool {
	var active int64
	s.db.Model(&models.Task{}).
		Where("status IN ? AND port = ?", []string{"running", "queued"}, port).
		Count(&active)
	return active > 0
}

func (s *TaskService) probeInstanceLoad(inst models.Instance) instanceLoad {
	c := NewComfyClient(s.comfyHostForPort(inst.Port), inst.Port)
	result := instanceLoad{inst: inst}
	// ComfyUI 未就绪或瞬时网络故障时重试，避免把短暂探测失败当成空闲。
	for attempt := 0; attempt < 3; attempt++ {
		load, err := c.GetLoad()
		if err == nil {
			result.up = true
			result.queueLen = load.QueueRunning + load.QueuePending
			result.vramFree = load.VRAMFree
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	return result
}

// reserveInstance 在同一调度临界区内完成并发限流、选卡与占用登记。
// queued 状态在真正提交 ComfyUI 前写入，使后续并发任务能立即避开已预占 GPU。
func (s *TaskService) reserveInstance(task *models.Task, video bool, forcePort *int) (*models.Instance, error) {
	s.scheduleMu.Lock()
	defer s.scheduleMu.Unlock()

	if video && s.runningVideoTaskCount() >= s.videoConcurrency() {
		return nil, errVideoConcurrencyLimit
	}
	inst, err := s.pickInstance(forcePort)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	claim := s.db.Model(&models.Task{}).
		Where("id = ? AND status = ?", task.ID, "pending").
		Updates(map[string]any{
			"instance_id": inst.ID, "gpu_index": inst.GPUIndex, "port": inst.Port,
			"status": "queued", "started_at": now,
		})
	if claim.Error != nil {
		return nil, claim.Error
	}
	if claim.RowsAffected == 0 {
		return nil, fmt.Errorf("任务状态已变化，无法分配 GPU")
	}

	task.InstanceID = &inst.ID
	task.GPUIndex = &inst.GPUIndex
	task.Port = &inst.Port
	task.Status = "queued"
	task.StartedAt = &now
	return inst, nil
}

func rankInstanceLoads(loads []instanceLoad) {
	sort.SliceStable(loads, func(a, b int) bool {
		if !loads[a].up {
			return false
		}
		if !loads[b].up {
			return true
		}
		if loads[a].queueLen != loads[b].queueLen {
			return loads[a].queueLen < loads[b].queueLen
		}
		return loads[a].vramFree > loads[b].vramFree
	})
}

// ---------- 任务创建与执行 ----------

type CreateTaskReq struct {
	TemplateID uint                  `json:"template_id"`
	Prompt     string                `json:"prompt"`
	Params     map[string]any        `json:"params"`
	Files      map[string][]FileMeta `json:"files"` // param_key -> 文件列表
	ForcePort  *int                  `json:"force_port,omitempty"`
	Seed       *int64                `json:"seed,omitempty"`
}

type FileMeta struct {
	TaskID string `json:"task_id"`
	Name   string `json:"name"`
}

func (s *TaskService) CreateTask(req CreateTaskReq) (*models.Task, error) {
	var tpl models.Template
	if err := s.db.First(&tpl, req.TemplateID).Error; err != nil {
		return nil, fmt.Errorf("模板不存在")
	}
	if !tpl.Enabled {
		return nil, fmt.Errorf("模板已禁用")
	}

	taskID := s.NewTaskID()
	params := req.Params
	if params == nil {
		params = map[string]any{}
	}
	if err := normalizeTemplateFiles(&tpl, params, req.Files); err != nil {
		return nil, err
	}
	params["_task_id"] = taskID
	// prompt 始终写入参数；图生视频与首尾帧模板允许空提示词。
	params["prompt"] = req.Prompt
	if req.Seed != nil {
		params["seed"] = *req.Seed
	}

	paramsJSON, _ := json.Marshal(params)
	task := models.Task{
		TaskID:       taskID,
		TemplateID:   tpl.ID,
		TemplateName: tpl.Name,
		Prompt:       req.Prompt,
		ParamsJSON:   string(paramsJSON),
		Status:       "pending",
	}
	if err := s.db.Create(&task).Error; err != nil {
		return nil, err
	}
	s.push(&task)
	return &task, nil
}

// Execute 调度并提交任务 (异步执行)
func (s *TaskService) Execute(taskID string) error {
	// 短临界区：校验状态并登记执行中，随后的实例探测/工作流渲染/提交均在锁外，
	// 避免某个实例探测或提交超时阻塞后续所有任务的调度。
	s.mu.Lock()
	var task models.Task
	if err := s.db.Where("task_id = ?", taskID).First(&task).Error; err != nil {
		s.mu.Unlock()
		return err
	}
	if task.Status != "pending" {
		s.mu.Unlock()
		return fmt.Errorf("任务状态不允许执行: %s", task.Status)
	}
	if s.executing[taskID] {
		s.mu.Unlock()
		return fmt.Errorf("任务已在执行中")
	}
	s.executing[taskID] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.executing, taskID)
		s.mu.Unlock()
	}()

	var tpl models.Template
	if err := s.db.First(&tpl, task.TemplateID).Error; err != nil {
		return err
	}

	var params map[string]any
	_ = json.Unmarshal([]byte(task.ParamsJSON), &params)

	// 模拟模式：不连接 ComfyUI，按模板参考耗时模拟进度推进直至成功
	if s.cfg.Simulate {
		go s.simulateExecute(&task, &tpl)
		return nil
	}

	// 全局视频生成并发限制：读平台设置 video_concurrency（默认 4），
	// 对视频模板任务（i2v/t2v/ref2v/first_last）统一生效（含任务页手动创建的任务）。
	// 超限时任务保持 pending 排队，由 queueRetryWhenFree 等待后再提交。
	if isVideoTemplateCode(tpl.Code) && s.runningVideoTaskCount() >= s.videoConcurrency() {
		s.queueRetryWhenFree(taskID)
		return errVideoConcurrencyLimit
	}

	// 渲染工作流
	workflow, err := s.RenderWorkflow(&tpl, params)
	if err != nil {
		s.failTask(&task, "工作流渲染失败: "+err.Error())
		return err
	}

	// 原子完成选卡与 GPU 预占；并发批量任务会依次拿到不同的空闲 GPU。
	forcePort := s.forcePortOf(params)
	inst, err := s.reserveInstance(&task, isVideoTemplateCode(tpl.Code), forcePort)
	if err != nil {
		if errors.Is(err, errNoFreeGPU) || errors.Is(err, errVideoConcurrencyLimit) {
			s.queueRetryWhenFree(taskID)
			return err
		}
		s.failTask(&task, err.Error())
		return err
	}
	s.push(&task)

	// 提交前二次校验：任务可能已被用户取消/清空
	s.mu.Lock()
	var cur models.Task
	if err := s.db.Where("task_id = ?", taskID).First(&cur).Error; err == nil && cur.Status != "queued" {
		s.mu.Unlock()
		return fmt.Errorf("任务状态已变化: %s", cur.Status)
	}
	s.mu.Unlock()

	clientID := "console-" + taskID
	c := NewComfyClient(s.comfyHostForPort(inst.Port), inst.Port)
	promptID, err := c.SubmitPrompt(workflow, clientID)
	if err != nil {
		s.failTask(&task, "提交失败: "+err.Error())
		return err
	}

	now := time.Now()
	s.db.Model(&task).Updates(map[string]any{
		"comfy_prompt_id": promptID, "started_at": now,
	})
	task.ComfyPromptID = promptID
	task.StartedAt = &now
	s.push(&task)

	s.listenWS(&task, c)
	return nil
}

// queueRetryWhenFree GPU 全忙时启动后台重试循环：每 10s 探测一次空闲实例，
// 有空闲则重新 Execute 提交。任务取消/状态变化或失败时退出循环。
// 通过 retrying 标记保证每个任务只有一个重试循环，避免重试风暴。
func (s *TaskService) queueRetryWhenFree(taskID string) {
	s.retryMu.Lock()
	if s.retrying[taskID] {
		s.retryMu.Unlock()
		return
	}
	s.retrying[taskID] = true
	s.retryMu.Unlock()

	go func() {
		defer func() {
			s.retryMu.Lock()
			delete(s.retrying, taskID)
			s.retryMu.Unlock()
		}()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			var cur models.Task
			if err := s.db.Where("task_id = ?", taskID).First(&cur).Error; err != nil {
				return
			}
			if cur.Status != "pending" {
				// 已取消/被重新执行/已结束，退出重试
				return
			}
			err := s.Execute(taskID)
			if err == nil {
				return
			}
			if !errors.Is(err, errNoFreeGPU) && !errors.Is(err, errVideoConcurrencyLimit) {
				// 其他错误已由 Execute failTask，退出
				return
			}
		}
	}()
}

// simulateExecute 模拟任务执行：queued→running→按节点推进进度→success。
// 耗时参考真实成功任务（按模板典型耗时），用于无 GPU 环境验证前端全流程与进度展示。
func (s *TaskService) simulateExecute(task *models.Task, tpl *models.Template) {
	gpu := 0
	port := s.cfg.Comfy.BasePort + gpu
	started := time.Now()
	s.db.Model(task).Updates(map[string]any{
		"gpu_index": gpu, "port": port, "status": "queued", "started_at": started,
	})
	task.GPUIndex = &gpu
	task.Port = &port
	task.Status = "queued"
	task.StartedAt = &started
	s.push(task)

	time.Sleep(800 * time.Millisecond)
	task.Status = "running"
	task.CurrentNode = "UNETLoader"
	s.db.Model(task).Updates(map[string]any{"status": "running", "current_node": "UNETLoader"})
	s.push(task)

	// 按节点分配进度比例，KSampler 扩散采样占大头（符合真实任务耗时分布）
	stages := []struct {
		node  string
		share float64
	}{
		{"UNETLoader", 0.05},
		{"CLIPLoader", 0.08},
		{"KSampler", 0.70},
		{"VAEDecode", 0.10},
		{"VAEDecodeAudio", 0.04},
		{"CreateVideo", 0.03},
	}
	total := simulateDuration(tpl.Code)
	tick := 500 * time.Millisecond
	progress := 0.0
	for _, st := range stages {
		task.CurrentNode = st.node
		stageSteps := int(total.Seconds() * st.share / tick.Seconds())
		if stageSteps < 1 {
			stageSteps = 1
		}
		for i := 0; i < stageSteps; i++ {
			time.Sleep(tick)
			progress += st.share / float64(stageSteps) * 100
			if progress > 99 {
				progress = 99
			}
			task.Progress = progress
			s.db.Model(task).Updates(map[string]any{"progress": progress, "current_node": st.node})
			s.push(task)
		}
	}

	resultFiles := "[]"
	if strings.Contains(tpl.Code, "2v") {
		subfolder := "simulated"
		filename := task.TaskID + ".mp4"
		outAbs := filepath.Join(s.cfg.Comfy.ComfyDir, "output_workers", "gpu0", subfolder, filename)
		cmd := fmt.Sprintf(`%s; mkdir -p %s; "$FF" -y -f lavfi -i color=c=0x1c1c1e:s=1280x704:d=1 -f lavfi -i anullsrc=r=48000:cl=stereo -shortest -c:v libx264 -pix_fmt yuv420p -c:a aac -movflags +faststart %s`,
			ffmpegResolveCmd, shellQuote(filepath.Dir(outAbs)), shellQuote(outAbs))
		if _, err := s.remote.RunTimeout(cmd, 2*time.Minute); err != nil {
			s.failTask(task, "模拟视频生成失败: "+err.Error())
			return
		}
		filesJSON, _ := json.Marshal([]map[string]string{{"type": "videos", "filename": filename, "subfolder": subfolder}})
		resultFiles = string(filesJSON)
	}
	finished := time.Now()
	s.db.Model(task).Updates(map[string]any{
		"status": "success", "progress": 100, "current_node": "", "finished_at": finished,
		"result_files": resultFiles,
	})
	task.Status = "success"
	task.Progress = 100
	task.CurrentNode = ""
	task.FinishedAt = &finished
	task.ResultFiles = resultFiles
	s.push(task)
	log.Printf("[simulate] task %s (%s) 完成，耗时 %v", task.TaskID, tpl.Code, time.Since(started))
}

// simulateDuration 按模板返回模拟总耗时（参考真实成功任务的典型耗时）
func simulateDuration(code string) time.Duration {
	switch {
	case strings.Contains(code, "t2v"):
		return 90 * time.Second
	case strings.Contains(code, "ref2v"):
		return 120 * time.Second
	default: // i2v / first_last
		return 60 * time.Second
	}
}

// forcePortOf 从参数中提取强制端口 (params["_force_port"])
func (s *TaskService) forcePortOf(params map[string]any) *int {
	if v, ok := params["_force_port"]; ok {
		switch t := v.(type) {
		case float64:
			p := int(t)
			return &p
		case int:
			return &t
		}
	}
	return nil
}

// isVideoTemplateCode 判断模板是否为视频生成模板（i2v/t2v/ref2v/first_last）
func isVideoTemplateCode(code string) bool {
	c := strings.ToLower(code)
	return strings.Contains(c, "i2v") || strings.Contains(c, "t2v") ||
		strings.Contains(c, "ref2v") || strings.Contains(c, "first_last")
}

// runningVideoTaskCount 当前执行中/排队中的视频任务数（全局，跨项目与手动任务）
func (s *TaskService) runningVideoTaskCount() int {
	var tpls []models.Template
	if err := s.db.Find(&tpls).Error; err != nil {
		return 0
	}
	var ids []uint
	for _, t := range tpls {
		if isVideoTemplateCode(t.Code) {
			ids = append(ids, t.ID)
		}
	}
	if len(ids) == 0 {
		return 0
	}
	var count int64
	s.db.Model(&models.Task{}).Where("status IN ? AND template_id IN ?",
		[]string{"running", "queued"}, ids).Count(&count)
	return int(count)
}

// videoConcurrency 视频生成并发上限（平台设置 video_concurrency，默认 4）
func (s *TaskService) videoConcurrency() int {
	var st models.Setting
	if err := s.db.Where("key = ?", "video_concurrency").First(&st).Error; err != nil || st.Value == "" {
		return 4
	}
	v, err := strconv.Atoi(st.Value)
	if err != nil || v <= 0 {
		return 4
	}
	return v
}

// listenWS 连接 ComfyUI WS 跟踪任务
func (s *TaskService) listenWS(task *models.Task, c *ComfyClient) {
	stop := make(chan struct{})
	s.wsMu.Lock()
	s.wsActive[task.TaskID] = stop
	s.wsMu.Unlock()

	ws := NewComfyWSClient(s.comfyHostForPort(*task.Port), *task.Port, "console-"+task.TaskID, func(t string, d map[string]any) {
		s.handleWSEvent(task.TaskID, t, d)
	})
	ws.Start()

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		defer ws.Close()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				// 超时保护: 若任务卡住且无进度, 检查 queue
				s.reconcile(task.TaskID)
			}
		}
	}()
}

// handleWSEvent 处理 ComfyUI WS 事件 (ComfyUI 0.30+ 事件格式)
func (s *TaskService) handleWSEvent(taskID string, type_ string, data map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var task models.Task
	if err := s.db.Where("task_id = ?", taskID).First(&task).Error; err != nil {
		return
	}

	switch type_ {
	case "execution_start":
		if task.Status != "running" {
			task.Status = "running"
			s.db.Model(&task).Update("status", "running")
		}
		s.push(&task)
	case "executing":
		node, _ := data["node"].(string)
		if node != "" {
			task.CurrentNode = node
			if task.Status != "running" {
				task.Status = "running"
			}
			s.db.Model(&task).Updates(map[string]any{
				"status": task.Status, "current_node": node,
			})
		}
		s.push(&task)
	case "progress_state":
		// 按模板节点权重加权汇总所有节点进度，避免"小节点完成即跳到 99%"。
		// 节点权重: 加载类低、KSampler 扩散采样最高、解码/合成次之。
		nodes, _ := data["nodes"].(map[string]any)
		weights := s.nodeWeights(&task)
		if len(weights) == 0 {
			break
		}
		// nodes 只包含非 pending 节点：已完成节点 value==max 贡献满权重，
		// 未开始节点不在其中贡献 0，加权后整体进度平滑推进。
		var weighted, total float64
		for id, w := range weights {
			total += w
			raw, ok := nodes[id]
			if !ok {
				continue
			}
			n, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			value, _ := n["value"].(float64)
			max, _ := n["max"].(float64)
			if max > 0 {
				if value >= max {
					weighted += w
				} else {
					weighted += w * value / max
				}
			}
		}
		if total > 0 {
			pct := weighted / total * 100
			// 生成中封顶 99%，成功完成后才显示 100%
			if pct >= 100 {
				pct = 99
			}
			// 进度只增不减，避免节点间权重重算导致回退
			if pct > task.Progress {
				task.Progress = pct
				if task.Status != "running" {
					task.Status = "running"
				}
				s.db.Model(&task).Updates(map[string]any{
					"progress": pct, "status": task.Status,
				})
				s.push(&task)
			}
		}
	case "execution_success":
		s.finishTask(&task)
	case "execution_error":
		msg := s.errorMessage(data)
		s.failTask(&task, "执行错误: "+msg)
	case "execution_interrupted":
		s.db.Model(&task).Updates(map[string]any{"status": "cancelled", "error": "执行被中断"})
		task.Status = "cancelled"
		task.Error = "执行被中断"
		s.push(&task)
		s.stopListener(task.TaskID)
	}
}

func (s *TaskService) errorMessage(data map[string]any) string {
	if msg, ok := data["message"].(string); ok {
		return msg
	}
	b, _ := json.Marshal(data)
	return string(b)
}

// nodeWeightOf 按节点类型分配进度权重（与 simulate 模式的耗时分布一致）
func nodeWeightOf(classType string) float64 {
	switch classType {
	case "UNETLoader", "UNETLoaderGGUF":
		return 0.05
	case "CLIPLoader", "CLIPLoaderSD3", "CLIPLoaderHybrid":
		return 0.08
	case "VAELoader", "VAELoaderSD3":
		return 0.04
	case "LoadImage", "LoadVideo", "LoadAudio", "LoadImagesFromDir":
		return 0.03
	case "MiniMaxH3ImageToVideo", "MiniMaxH3ReferenceToVideo", "MiniMaxH3TextEncode":
		return 0.06
	case "KSampler", "KSamplerAdvanced", "KSamplerCustom":
		return 0.70
	case "VAEDecode", "VAEDecodeAudio":
		return 0.10
	case "CreateVideo", "CreateAudio", "SaveVideo", "SaveAudio", "SaveImage":
		return 0.04
	default:
		// 未知节点（含辅助节点）给一个小的非零权重
		return 0.02
	}
}

// nodeWeights 解析任务模板 workflow 的 node_id -> class_type 权重映射，结果缓存。
// 访问 nodeClasses 时持有 wsMu（调用方 handleWSEvent 持 mu 但不持 wsMu，不会嵌套死锁）。
func (s *TaskService) nodeWeights(task *models.Task) map[string]float64 {
	s.wsMu.Lock()
	cached, ok := s.nodeClasses[task.TaskID]
	s.wsMu.Unlock()
	if ok {
		return cached
	}
	var tpl models.Template
	if err := s.db.First(&tpl, task.TemplateID).Error; err != nil {
		return nil
	}
	var nodes map[string]map[string]any
	if err := json.Unmarshal([]byte(tpl.WorkflowJSON), &nodes); err != nil {
		return nil
	}
	out := map[string]float64{}
	for id, node := range nodes {
		ct, _ := node["class_type"].(string)
		if ct == "" {
			continue
		}
		out[id] = nodeWeightOf(ct)
	}
	if len(out) == 0 {
		return nil
	}
	s.wsMu.Lock()
	s.nodeClasses[task.TaskID] = out
	s.wsMu.Unlock()
	return out
}

// finishTask 查询 history 提取结果
func (s *TaskService) finishTask(task *models.Task) {
	c := NewComfyClient(s.comfyHostForPort(*task.Port), *task.Port)
	hist, err := c.GetHistory(task.ComfyPromptID)
	if err != nil {
		s.failTask(task, "结果查询失败: "+err.Error())
		return
	}
	item, ok := hist[task.ComfyPromptID].(map[string]any)
	if !ok {
		s.failTask(task, "历史记录缺失")
		return
	}
	status, _ := item["status"].(map[string]any)
	statusStr, _ := status["status_str"].(string)
	if statusStr == "error" {
		s.failTask(task, "ComfyUI 执行出错")
		return
	}

	outputs, _ := item["outputs"].(map[string]any)
	var files []map[string]string
	for _, raw := range outputs {
		o, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		for _, ftype := range []string{"videos", "gifs", "images", "audio"} {
			list, ok := o[ftype].([]any)
			if !ok {
				continue
			}
			for _, fr := range list {
				f, ok := fr.(map[string]any)
				if !ok {
					continue
				}
				files = append(files, map[string]string{
					"type":      ftype,
					"filename":  fmt.Sprint(f["filename"]),
					"subfolder": fmt.Sprint(f["subfolder"]),
				})
			}
		}
	}
	filesJSON, _ := json.Marshal(files)
	now := time.Now()
	s.db.Model(task).Updates(map[string]any{
		"status": "success", "progress": 100, "result_files": string(filesJSON),
		"finished_at": now, "error": "",
	})
	task.Status = "success"
	task.Progress = 100
	task.ResultFiles = string(filesJSON)
	s.push(task)
	s.stopListener(task.TaskID)
}

func (s *TaskService) failTask(task *models.Task, errMsg string) {
	now := time.Now()
	s.db.Model(task).Updates(map[string]any{
		"status": "failed", "error": errMsg, "finished_at": now,
	})
	task.Status = "failed"
	task.Error = errMsg
	s.push(task)
	s.stopListener(task.TaskID)
}

// reconcile 定期检查队列中任务是否真实运行
func (s *TaskService) reconcile(taskID string) {
	var task models.Task
	if err := s.db.Where("task_id = ?", taskID).First(&task).Error; err != nil {
		return
	}
	if task.Status != "running" && task.Status != "queued" {
		return
	}
	c := NewComfyClient(s.comfyHostForPort(*task.Port), *task.Port)
	load, err := c.GetLoad()
	if err != nil {
		return
	}
	if load.QueueRunning == 0 && load.QueuePending == 0 {
		// 队列为空但任务还标记为运行: 查 history
		hist, herr := c.GetHistory(task.ComfyPromptID)
		if herr != nil || len(hist) == 0 {
			// ComfyUI 重启后内存 history 丢失：尝试扫描输出目录按任务 ID 恢复结果
			s.recoverFromOutput(&task)
			return
		}
		s.finishTask(&task)
	}
}

// StartRecovery 启动后台恢复循环：扫描所有 running/queued 任务，
// 处理平台重启 / 实例重启后遗留的卡死任务（WS 监听器已丢失的场景）
func (s *TaskService) StartRecovery() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		// 启动后立即扫描一次
		s.recoverStuckTasks()
		for range ticker.C {
			s.recoverStuckTasks()
		}
	}()
}

// recoverStuckTasks 扫描 running/queued 任务并触发 reconcile
func (s *TaskService) recoverStuckTasks() {
	var tasks []models.Task
	if err := s.db.Where("status IN ?", []string{"running", "queued"}).Find(&tasks).Error; err != nil {
		return
	}
	for i := range tasks {
		t := tasks[i]
		go func(task models.Task) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[recover] task %s panic: %v", task.TaskID, r)
				}
			}()
			s.reconcile(task.TaskID)
		}(t)
	}
}

// recoverFromOutput 实例重启丢失 history 时，扫描 output_workers/gpuN 目录按任务 ID 恢复任务结果。
func (s *TaskService) recoverFromOutput(task *models.Task) {
	if task.GPUIndex == nil {
		s.failTask(task, "执行中断: 实例已重启且无法定位输出")
		return
	}
	root := filepath.Join(s.cfg.Comfy.ComfyDir, "output_workers", fmt.Sprintf("gpu%d", *task.GPUIndex))
	matches := s.findTaskOutputs(root, task.TaskID)
	if len(matches) == 0 {
		s.failTask(task, "执行中断: 实例已重启, 未发现输出文件")
		return
	}
	filesJSON, _ := json.Marshal(matches)
	now := time.Now()
	s.db.Model(task).Updates(map[string]any{
		"status": "success", "progress": 100, "result_files": string(filesJSON),
		"finished_at": now, "error": "",
	})
	task.Status = "success"
	task.Progress = 100
	task.ResultFiles = string(filesJSON)
	s.push(task)
	s.stopListener(task.TaskID)
	log.Printf("[task] %s 从输出目录恢复完成: %d 个文件", task.TaskID, len(matches))
}

// findTaskOutputs 递归扫描目录，按任务 ID 匹配输出文件
func (s *TaskService) findTaskOutputs(root, taskID string) []map[string]string {
	var out []map[string]string
	var walk func(dir, sub string)
	walk = func(dir, sub string) {
		entries, err := s.remote.ListDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() {
				walk(filepath.Join(dir, e.Name()), filepath.Join(sub, e.Name()))
				continue
			}
			if strings.HasPrefix(e.Name(), taskID) {
				ext := strings.ToLower(filepath.Ext(e.Name()))
				ftype := "images"
				switch ext {
				case ".mp4", ".webm", ".mov", ".mkv":
					ftype = "videos"
				case ".mp3", ".wav", ".flac":
					ftype = "audio"
				case ".gif":
					ftype = "gifs"
				}
				out = append(out, map[string]string{
					"type": ftype, "filename": e.Name(),
					"subfolder": strings.TrimPrefix(sub, "/"),
				})
			}
		}
	}
	walk(root, "")
	return out
}

func (s *TaskService) stopListener(taskID string) {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	if ch, ok := s.wsActive[taskID]; ok {
		close(ch)
		delete(s.wsActive, taskID)
	}
	delete(s.nodeClasses, taskID)
}

// push 广播任务更新
func (s *TaskService) push(task *models.Task) {
	s.hub.Broadcast("task_update", TaskUpdatePayload(task))
}

// RequeueTask 重新排队：把已绑定 GPU 的 queued 任务从原实例队列摘除，解除 GPU 绑定回 pending，
// 重新走调度（先过 video_concurrency 并发闸门，再经 pickInstance 探测 3 次确定可用 GPU 后分配）。
func (s *TaskService) RequeueTask(taskID string) error {
	var task models.Task
	if err := s.db.Where("task_id = ?", taskID).First(&task).Error; err != nil {
		return err
	}
	if task.Status != "queued" {
		return fmt.Errorf("仅排队中（queued）任务可重新排队")
	}
	// 从原实例队列摘除（不 Interrupt，避免打断同实例正在运行的其他任务）
	if task.Port != nil && task.ComfyPromptID != "" {
		c := NewComfyClient(s.comfyHostForPort(*task.Port), *task.Port)
		if err := c.DeletePrompt(task.ComfyPromptID); err != nil {
			log.Printf("[task] %s requeue delete prompt failed: %v", taskID, err)
		}
	}
	s.stopListener(taskID)
	// 解除 GPU 绑定回 pending，等待重新调度
	if err := s.db.Model(&task).Updates(map[string]any{
		"status": "pending", "instance_id": nil, "gpu_index": nil, "port": nil,
		"comfy_prompt_id": "", "started_at": nil, "progress": 0, "current_node": "", "error": "",
	}).Error; err != nil {
		return err
	}
	task.Status = "pending"
	s.push(&task)
	// 异步重新调度：并发闸门未满且有可用 GPU 时立即分配，否则保持排队等待
	go func() {
		if err := s.Execute(taskID); err != nil {
			log.Printf("[task] %s requeue execute failed: %v", taskID, err)
		}
	}()
	return nil
}

// CancelTask 取消任务
func (s *TaskService) CancelTask(taskID string) error {
	var task models.Task
	if err := s.db.Where("task_id = ?", taskID).First(&task).Error; err != nil {
		return err
	}
	if task.Status == "success" || task.Status == "failed" || task.Status == "cancelled" {
		return fmt.Errorf("任务已结束")
	}
	if task.Port != nil {
		c := NewComfyClient(s.comfyHostForPort(*task.Port), *task.Port)
		if task.ComfyPromptID != "" {
			_ = c.DeletePrompt(task.ComfyPromptID)
		}
		_ = c.Interrupt()
	}
	s.db.Model(&task).Updates(map[string]any{"status": "cancelled", "error": "用户取消"})
	task.Status = "cancelled"
	task.Error = "用户取消"
	s.push(&task)
	s.stopListener(taskID)
	return nil
}

// CancelAllTasks 取消全部未完成任务（pending/queued/running），返回取消数量
func (s *TaskService) CancelAllTasks() (int, error) {
	var tasks []models.Task
	if err := s.db.Where("status IN ?", []string{"pending", "queued", "running"}).Find(&tasks).Error; err != nil {
		return 0, err
	}
	count := 0
	for i := range tasks {
		if err := s.CancelTask(tasks[i].TaskID); err == nil {
			count++
		}
	}
	return count, nil
}
