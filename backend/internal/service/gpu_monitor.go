package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

// GPUStatus nvidia-smi 采集结果
type GPUStatus struct {
	Index     int      `json:"index"`
	Name      string   `json:"name"`
	UUID      string   `json:"uuid"`
	Util      float64  `json:"util"`
	Temp      float64  `json:"temp"`
	Power     float64  `json:"power"`
	VRAMUsed  int64    `json:"vram_used"`
	VRAMTotal int64    `json:"vram_total"`
	Processes []GPUMem `json:"processes"`
	Up        bool     `json:"up"`
}

type GPUMem struct {
	PID     int    `json:"pid"`
	Process string `json:"process"`
	MemMB   int64  `json:"mem_mb"`
}

// HostStatus 宿主机 CPU/内存状态（与 nvidia-smi 同机采集）
type HostStatus struct {
	CPUUtil     float64 `json:"cpu_util"`
	MemUsed     int64   `json:"mem_used"`
	MemTotal    int64   `json:"mem_total"`
	MemPct      float64 `json:"mem_pct"`
	LoadAvg     string  `json:"load_avg"`
	UpSeconds   int64   `json:"up_seconds"`
	Collected   bool    `json:"collected"`
	CollectedAt string  `json:"collected_at"`
}

// GPUMonitor 定时采集 nvidia-smi（远程模式通过 SSH 在算力节点执行）
type GPUMonitor struct {
	cfg    *config.Config
	db     *gorm.DB
	remote *RemoteExec
	mu     sync.RWMutex
	status map[int]GPUStatus
	host   HostStatus
	cancel context.CancelFunc
}

func NewGPUMonitor(cfg *config.Config, db *gorm.DB, remote *RemoteExec) *GPUMonitor {
	return &GPUMonitor{
		cfg:    cfg,
		db:     db,
		remote: remote,
		status: map[int]GPUStatus{},
	}
}

func (m *GPUMonitor) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	go func() {
		m.collect()
		ticker := time.NewTicker(time.Duration(m.cfg.GPU.MonitorInt) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.collect()
			}
		}
	}()
}

func (m *GPUMonitor) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
}

// CollectOnce 手动触发一次采集
func (m *GPUMonitor) CollectOnce() {
	m.collect()
}

func (m *GPUMonitor) Snapshot() []GPUStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]GPUStatus, 0, len(m.status))
	for i := 0; i < len(m.status); i++ {
		if s, ok := m.status[i]; ok {
			out = append(out, s)
		}
	}
	return out
}

// HostSnapshot 返回宿主机 CPU/内存状态
func (m *GPUMonitor) HostSnapshot() HostStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.host
}

func (m *GPUMonitor) collect() {
	query := m.cfg.GPU.NVSMICmd +
		" --query-gpu=index,name,uuid,utilization.gpu,temperature.gpu,power.draw,memory.used,memory.total" +
		" --format=csv,noheader,nounits"
	out, err := m.remote.Run(query)
	if err != nil {
		return
	}

	procQuery := m.cfg.GPU.NVSMICmd +
		" --query-compute-apps=gpu_uuid,pid,process_name,used_memory" +
		" --format=csv,noheader,nounits"
	procOut, _ := m.remote.Run(procQuery)

	procs := map[string][]GPUMem{}
	for _, line := range strings.Split(strings.TrimSpace(string(procOut)), "\n") {
		parts := splitCSV(line)
		if len(parts) < 4 {
			continue
		}
		var pid, mem int64
		fmt.Sscanf(parts[1], "%d", &pid)
		fmt.Sscanf(parts[3], "%d", &mem)
		procs[parts[0]] = append(procs[parts[0]], GPUMem{
			PID: int(pid), Process: parts[2], MemMB: mem,
		})
	}

	newStatus := map[int]GPUStatus{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := splitCSV(line)
		if len(parts) < 8 {
			continue
		}
		var idx int
		var util, temp, power, used, total float64
		// 列顺序: index,name,uuid,utilization.gpu,temperature.gpu,power.draw,memory.used,memory.total
		fmt.Sscanf(parts[0], "%d", &idx)
		fmt.Sscanf(parts[3], "%f", &util)
		fmt.Sscanf(parts[4], "%f", &temp)
		fmt.Sscanf(parts[5], "%f", &power)
		fmt.Sscanf(parts[6], "%f", &used)
		fmt.Sscanf(parts[7], "%f", &total)
		newStatus[idx] = GPUStatus{
			Index:     idx,
			Name:      parts[1],
			UUID:      parts[2],
			Util:      util,
			Temp:      temp,
			Power:     power,
			VRAMUsed:  int64(used * 1024 * 1024),
			VRAMTotal: int64(total * 1024 * 1024),
			Up:        true,
			Processes: procs[parts[2]],
		}
	}
	if len(newStatus) == 0 {
		return
	}

	m.mu.Lock()
	m.status = newStatus
	m.host = m.collectHost()
	m.mu.Unlock()

	m.syncInstances(newStatus)
}

// collectHost 采集宿主机 CPU/内存/负载/运行时间（bash 单次调用，兼容本地/远程）
func (m *GPUMonitor) collectHost() HostStatus {
	cmd := `bash -c 'LC_ALL=C; CORE=$(nproc); U=$(awk -v c="$CORE" "{print 100-\$11/c}" <(head -1 /proc/stat) 2>/dev/null || echo 0); read -r M1 M2 M3 < /proc/loadavg; R=$(awk "/^MemTotal:/{t=\$2}/^MemAvailable:/{a=\$2}END{printf \"%d %d\", t-a, t}" /proc/meminfo); UPT=$(awk "{print int(\$1)}" /proc/uptime); echo "$U $R $M1,$M2,$M3 $UPT"'`
	out, err := m.remote.Run(cmd)
	if err != nil {
		return HostStatus{Collected: false}
	}
	parts := strings.Fields(out)
	if len(parts) < 5 {
		return HostStatus{Collected: false}
	}
	var cpuUtil, memUsed, memTotal, upSec float64
	fmt.Sscanf(parts[0], "%f", &cpuUtil)
	fmt.Sscanf(parts[1], "%f", &memUsed)
	fmt.Sscanf(parts[2], "%f", &memTotal)
	fmt.Sscanf(parts[4], "%f", &upSec)
	host := HostStatus{
		CPUUtil:     cpuUtil,
		MemUsed:     int64(memUsed * 1024),
		MemTotal:    int64(memTotal * 1024),
		LoadAvg:     parts[3],
		UpSeconds:   int64(upSec),
		Collected:   true,
		CollectedAt: time.Now().Format("2006-01-02 15:04:05"),
	}
	if memTotal > 0 {
		host.MemPct = memUsed / memTotal * 100
	}
	return host
}

// syncInstances 把 GPU 状态同步到 Instance 表
func (m *GPUMonitor) syncInstances(status map[int]GPUStatus) {
	var insts []models.Instance
	m.db.Find(&insts)
	for i := range insts {
		g, ok := status[insts[i].GPUIndex]
		if !ok {
			continue
		}
		m.db.Model(&insts[i]).Updates(map[string]any{
			"vram_total":   g.VRAMTotal,
			"vram_free":    g.VRAMTotal - g.VRAMUsed,
			"util":         g.Util,
			"temp":         g.Temp,
			"power":        g.Power,
			"last_checked": time.Now(),
		})
	}
}

func splitCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}
