package service

import (
	"fmt"
	"log"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

// InstanceManager 管理 ComfyUI 实例生命周期（启动/停止/状态）
// 调度模式由 comfy.mode 决定:
//   - docker: 通过 docker CLI 控制 comfyui-gpu{N} 容器 (compose 一键部署)
//   - ssh:    通过 SSH 在算力节点上启停裸进程 (旧部署)
//   - local:  本机启停裸进程
type InstanceManager struct {
	cfg    *config.Config
	db     *gorm.DB
	remote *RemoteExec
	mu     sync.Mutex
}

func NewInstanceManager(cfg *config.Config, db *gorm.DB, remote *RemoteExec) *InstanceManager {
	return &InstanceManager{cfg: cfg, db: db, remote: remote}
}

func (m *InstanceManager) PortOf(gpu int) int { return m.cfg.Comfy.BasePort + gpu }

// containerName 返回 docker 模式下的容器名, 如 comfyui-gpu0
func (m *InstanceManager) containerName(gpu int) string {
	return fmt.Sprintf("%s%d", m.cfg.Comfy.ContainerPrefix, gpu)
}

// dockerMode 是否使用 docker 容器调度
func (m *InstanceManager) dockerMode() bool {
	return m.cfg.Comfy.Mode == "docker"
}

// comfyHostOf 返回端口对应实例的可达主机:
//   - docker 模式: 容器网络内的容器名 (compose 网络 DNS 解析)
//   - 其他模式: 算力节点 IP / 本机
func (m *InstanceManager) comfyHostOf(port int) string {
	if m.dockerMode() {
		return m.containerName(port - m.cfg.Comfy.BasePort)
	}
	return m.remote.Host()
}

// ensureRows 确保 8 个实例记录存在
func (m *InstanceManager) ensureRows() {
	var count int64
	m.db.Model(&models.Instance{}).Count(&count)
	if count >= int64(m.cfg.Comfy.GPUCount) {
		return
	}
	for gpu := 0; gpu < m.cfg.Comfy.GPUCount; gpu++ {
		var inst models.Instance
		err := m.db.Where("gpu_index = ?", gpu).First(&inst).Error
		if err == gorm.ErrRecordNotFound {
			inst = models.Instance{
				GPUIndex:      gpu,
				Port:          m.PortOf(gpu),
				Status:        "stopped",
				EnableManager: m.cfg.Comfy.EnableManager && gpu == 0,
			}
			m.db.Create(&inst)
		}
	}
}

func (m *InstanceManager) List() []models.Instance {
	m.ensureRows()
	var list []models.Instance
	m.db.Order("gpu_index").Find(&list)
	return list
}

func (m *InstanceManager) ByPort(port int) (models.Instance, error) {
	var inst models.Instance
	err := m.db.Where("port = ?", port).First(&inst).Error
	return inst, err
}

// FindPID 通过命令行匹配找到实例进程 PID（docker 模式返回 1 表示容器运行中）
func (m *InstanceManager) FindPID(port int) int {
	if m.dockerMode() {
		out, err := m.remote.Run(fmt.Sprintf(
			"docker inspect -f '{{.State.Running}}' %s 2>/dev/null || echo false",
			m.containerName(port-m.cfg.Comfy.BasePort)))
		if err == nil && strings.Contains(strings.TrimSpace(out), "true") {
			return 1
		}
		return 0
	}
	cmd := fmt.Sprintf("pgrep -f 'main.py.*--port %d( |$)'", port)
	out, err := m.remote.Run(cmd)
	if err != nil {
		return 0
	}
	pidStr := strings.TrimSpace(strings.Split(strings.TrimSpace(out), "\n")[0])
	pid, _ := strconv.Atoi(pidStr)
	return pid
}

// Start 启动单个 GPU 实例
func (m *InstanceManager) Start(gpu int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	inst, err := m.byGPU(gpu)
	if err != nil {
		return err
	}
	port := m.PortOf(gpu)

	if pid := m.FindPID(port); pid > 0 {
		m.db.Model(&inst).Updates(map[string]any{
			"status": "running", "pid": pid,
		})
		return fmt.Errorf("GPU %d 实例已在运行 (端口 %d)", gpu, port)
	}

	// docker 模式: 启动已由 compose 创建好的容器
	if m.dockerMode() {
		if _, err := m.remote.Run(fmt.Sprintf("docker start %s", m.containerName(gpu))); err != nil {
			return fmt.Errorf("start container %s: %w", m.containerName(gpu), err)
		}
		// 等待容器就绪 (最多 60s)
		ok := false
		for i := 0; i < 60; i++ {
			time.Sleep(time.Second)
			c := NewComfyClient(m.comfyHostOf(port), port)
			if c.Ping() == nil {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("container %s 启动超时", m.containerName(gpu))
		}
		m.db.Model(&inst).Updates(map[string]any{"status": "running", "pid": 1})
		return nil
	}

	comfyDir := m.cfg.Comfy.ComfyDir
	userDir := filepath.Join(comfyDir, "user_workers", fmt.Sprintf("gpu%d", gpu))
	tempDir := filepath.Join(comfyDir, "temp_workers", fmt.Sprintf("gpu%d", gpu))
	outDir := filepath.Join(comfyDir, "output_workers", fmt.Sprintf("gpu%d", gpu))
	logDir := filepath.Join(comfyDir, "logs")
	for _, d := range []string{userDir, tempDir, outDir, logDir} {
		if err := m.remote.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}

	python := m.remote.PythonBin()

	// 组装启动命令（远程节点以 nohup 后台运行，日志落盘）
	args := []string{
		python, "main.py",
		"--listen", "0.0.0.0",
		"--port", strconv.Itoa(port),
		"--reserve-vram", strconv.Itoa(m.cfg.Comfy.ReserveVRAM),
		"--user-directory", userDir,
		"--temp-directory", tempDir,
		"--output-directory", outDir,
		"--database-url", fmt.Sprintf("sqlite:///%s/comfyui.db", userDir),
	}
	if m.cfg.Comfy.ForceFP16 {
		args = append(args, "--force-fp16")
	}
	if inst.EnableManager {
		args = append(args, "--enable-manager")
	}
	logFile := filepath.Join(logDir, fmt.Sprintf("comfyui-gpu%d.log", gpu))

	var cmdStr string
	if m.remote.Enabled() {
		cmdStr = fmt.Sprintf(
			"cd %s && CUDA_VISIBLE_DEVICES=%d CUDA_DEVICE_ORDER=PCI_BUS_ID nohup %s >%s 2>&1 &",
			shellQuote(comfyDir), gpu, strings.Join(shellQuoteAll(args), " "), shellQuote(logFile))
	} else {
		cmdStr = fmt.Sprintf(
			"cd %s && CUDA_VISIBLE_DEVICES=%d CUDA_DEVICE_ORDER=PCI_BUS_ID nohup %s >%s 2>&1 &",
			shellQuote(comfyDir), gpu, strings.Join(shellQuoteAll(args), " "), shellQuote(logFile))
	}

	if err := m.remote.StartDaemon(cmdStr); err != nil {
		return fmt.Errorf("start comfyui gpu%d: %w", gpu, err)
	}

	// 启动后探测 PID（SSH 远程启动进程可能需要几秒才注册）
	pid := 0
	for i := 0; i < 12; i++ {
		time.Sleep(500 * time.Millisecond)
		if pid = m.FindPID(port); pid > 0 {
			break
		}
	}

	m.db.Model(&inst).Updates(map[string]any{
		"status": "starting", "pid": pid,
	})
	return nil
}

// shellQuote 简单单引号包裹（路径不含单引号）
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func shellQuoteAll(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = shellQuote(a)
	}
	return out
}

// Stop 停止单个实例
func (m *InstanceManager) Stop(gpu int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	inst, err := m.byGPU(gpu)
	if err != nil {
		return err
	}
	port := m.PortOf(gpu)

	// docker 模式: 停止容器
	if m.dockerMode() {
		if pid := m.FindPID(port); pid > 0 {
			log.Printf("[instance] stopping gpu%d (container %s)", gpu, m.containerName(gpu))
			_, _ = m.remote.Run(fmt.Sprintf("docker stop %s", m.containerName(gpu)))
			time.Sleep(3 * time.Second)
			if m.FindPID(port) > 0 {
				log.Printf("[instance] force kill gpu%d (container %s)", gpu, m.containerName(gpu))
				_, _ = m.remote.Run(fmt.Sprintf("docker kill %s", m.containerName(gpu)))
				time.Sleep(1 * time.Second)
			}
		}
		m.db.Model(&inst).Updates(map[string]any{
			"status": "stopped", "pid": 0, "queue_len": 0, "vram_free": 0,
		})
		return nil
	}

	pattern := fmt.Sprintf("main.py.*--port %d( |$)", port)

	// 仅当实例进程存在时才执行终止；无进程时直接清理状态，避免每个实例空等 3s
	if pid := m.FindPID(port); pid > 0 {
		log.Printf("[instance] stopping gpu%d (port %d, pid %d)", gpu, port, pid)
		_, _ = m.remote.Run(fmt.Sprintf("pkill -f '%s'", pattern))
		time.Sleep(3 * time.Second)
		// 验证是否已停止，未停止则强制 kill -9
		if m.FindPID(port) > 0 {
			log.Printf("[instance] force kill gpu%d (port %d)", gpu, port)
			_, _ = m.remote.Run(fmt.Sprintf("pkill -9 -f '%s'", pattern))
			time.Sleep(1 * time.Second)
		}
	}

	m.db.Model(&inst).Updates(map[string]any{
		"status": "stopped", "pid": 0, "queue_len": 0, "vram_free": 0,
	})
	return nil
}

// StopAll 停止全部实例（并发受限，避免同时停止互相影响）
func (m *InstanceManager) StopAll(concurrency int) error {
	if concurrency < 1 {
		concurrency = 2
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for gpu := 0; gpu < m.cfg.Comfy.GPUCount; gpu++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			_ = m.Stop(g)
		}(gpu)
	}
	wg.Wait()
	return nil
}

// StartAll 启动全部实例（并发受限：显存/带宽竞争，避免同时加载模型；单个实例失败不中断）
func (m *InstanceManager) StartAll(concurrency int) error {
	if concurrency < 1 {
		concurrency = 2
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []string
	for gpu := 0; gpu < m.cfg.Comfy.GPUCount; gpu++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := m.Start(g); err != nil {
				log.Printf("[instance] start gpu%d failed: %v", g, err)
				mu.Lock()
				errs = append(errs, fmt.Sprintf("gpu %d: %v", g, err))
				mu.Unlock()
			}
		}(gpu)
	}
	wg.Wait()
	if len(errs) > 0 {
		return fmt.Errorf("部分实例启动失败: %s", strings.Join(errs, "; "))
	}
	return nil
}

// RestartAll 重启全部实例（先停后启，停失败不中断）
func (m *InstanceManager) RestartAll(concurrency int) error {
	if err := m.StopAll(concurrency); err != nil {
		log.Printf("[instance] restart-all stop phase: %v", err)
	}
	time.Sleep(3 * time.Second)
	return m.StartAll(concurrency)
}

func (m *InstanceManager) byGPU(gpu int) (models.Instance, error) {
	m.ensureRows()
	var inst models.Instance
	err := m.db.Where("gpu_index = ?", gpu).First(&inst).Error
	return inst, err
}
