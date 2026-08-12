package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ComfyClient 与单个 ComfyUI 实例通信
type ComfyClient struct {
	Host string
	Port int
	HTTP *http.Client
}

func NewComfyClient(host string, port int) *ComfyClient {
	return &ComfyClient{
		Host: host,
		Port: port,
		HTTP: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *ComfyClient) baseURL() string {
	return fmt.Sprintf("http://%s:%d", c.Host, c.Port)
}

type InstanceLoad struct {
	QueueRunning int   `json:"queue_running"`
	QueuePending int   `json:"queue_pending"`
	VRAMFree     int64 `json:"vram_free"`
	VRAMTotal    int64 `json:"vram_total"`
}

func (c *ComfyClient) Ping() error {
	resp, err := c.HTTP.Get(c.baseURL() + "/system_stats")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

// GetLoad 查询队列与显存状态
func (c *ComfyClient) GetLoad() (*InstanceLoad, error) {
	q, err := c.getJSON("/queue", map[string]any{})
	if err != nil {
		return nil, err
	}
	load := &InstanceLoad{}
	if qr, ok := q["queue_running"].([]any); ok {
		load.QueueRunning = len(qr)
	}
	if qp, ok := q["queue_pending"].([]any); ok {
		load.QueuePending = len(qp)
	}

	s, err := c.getJSON("/system_stats", map[string]any{})
	if err != nil {
		return nil, err
	}
	if devs, ok := s["devices"].([]any); ok && len(devs) > 0 {
		dev := devs[0].(map[string]any)
		if v, ok := dev["vram_free"].(float64); ok {
			load.VRAMFree = int64(v)
		}
		if v, ok := dev["vram_total"].(float64); ok {
			load.VRAMTotal = int64(v)
		}
	}
	return load, nil
}

// SubmitPrompt 提交工作流
func (c *ComfyClient) SubmitPrompt(workflow map[string]any, clientID string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"prompt":    workflow,
		"client_id": clientID,
	})
	resp, err := c.HTTP.Post(c.baseURL()+"/prompt", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("submit prompt failed %d: %s", resp.StatusCode, string(data))
	}
	var out struct {
		PromptID string `json:"prompt_id"`
		Error    any    `json:"error"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", err
	}
	if out.Error != nil {
		return "", fmt.Errorf("comfy error: %v", out.Error)
	}
	return out.PromptID, nil
}

// Interrupt 中断当前任务
func (c *ComfyClient) Interrupt() error {
	resp, err := c.HTTP.Post(c.baseURL()+"/interrupt", "application/json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// DeletePrompt 从队列删除任务
func (c *ComfyClient) DeletePrompt(promptID string) error {
	body, _ := json.Marshal(map[string]string{"delete": promptID})
	req, _ := http.NewRequest(http.MethodPost, c.baseURL()+"/queue", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// GetHistory 查询任务结果
func (c *ComfyClient) GetHistory(promptID string) (map[string]any, error) {
	return c.getJSON("/history/"+promptID, map[string]any{})
}

// GetObjectInfo 节点定义缓存
func (c *ComfyClient) GetObjectInfo() (map[string]any, error) {
	return c.getJSON("/object_info", map[string]any{})
}

func (c *ComfyClient) getJSON(path string, fallback map[string]any) (map[string]any, error) {
	resp, err := c.HTTP.Get(c.baseURL() + path)
	if err != nil {
		return fallback, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fallback, fmt.Errorf("GET %s: status %d", path, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fallback, err
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return fallback, err
	}
	return out, nil
}
