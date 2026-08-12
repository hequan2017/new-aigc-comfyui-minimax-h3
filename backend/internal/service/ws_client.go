package service

import (
	"encoding/json"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"comfyui-console/internal/models"
)

// ComfyWSClient 监听单个 ComfyUI 实例的 WS 事件并转发
type ComfyWSClient struct {
	host     string
	port     int
	clientID string
	onEvent  func(type_ string, data map[string]any)
	done     chan struct{}
	once     sync.Once
}

func NewComfyWSClient(host string, port int, clientID string, onEvent func(string, map[string]any)) *ComfyWSClient {
	return &ComfyWSClient{
		host:     host,
		port:     port,
		clientID: clientID,
		onEvent:  onEvent,
		done:     make(chan struct{}),
	}
}

func (w *ComfyWSClient) Start() {
	go w.run()
}

func (w *ComfyWSClient) Close() {
	w.once.Do(func() { close(w.done) })
}

func (w *ComfyWSClient) run() {
	for {
		select {
		case <-w.done:
			return
		default:
		}
		url := "ws://" + w.host + ":" + strconv.Itoa(w.port) + "/ws?clientId=" + w.clientID
		conn, _, err := websocket.DefaultDialer.Dial(url, nil)
		if err != nil {
			select {
			case <-w.done:
				return
			case <-time.After(3 * time.Second):
				continue
			}
		}
		w.loop(conn)
		select {
		case <-w.done:
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func (w *ComfyWSClient) loop(conn *websocket.Conn) {
	defer conn.Close()
	defer func() {
		if r := recover(); r != nil {
			// 防止单个事件处理 panic 杀死整个服务
		}
	}()
	for {
		select {
		case <-w.done:
			return
		default:
		}
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var evt map[string]any
		if err := json.Unmarshal(msg, &evt); err != nil {
			continue
		}
		t, _ := evt["type"].(string)
		data, _ := evt["data"].(map[string]any)
		if w.onEvent != nil {
			w.onEvent(t, data)
		}
	}
}

// Hub 向前端推送实时消息。
// gorilla/websocket 的写操作非线程安全：多个任务并发广播时若同时 WriteMessage
// 同一连接会触发数据竞争甚至 panic。因此所有写操作由同一把互斥锁串行化。
type Hub struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]struct{}
}

func NewHub() *Hub {
	return &Hub{clients: map[*websocket.Conn]struct{}{}}
}

func (h *Hub) Register(conn *websocket.Conn) {
	h.mu.Lock()
	h.clients[conn] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) Unregister(conn *websocket.Conn) {
	h.mu.Lock()
	delete(h.clients, conn)
	h.mu.Unlock()
}

// writeMsg 在已持锁的前提下写入单个连接。设置写超时避免慢连接阻塞所有广播；
// 写失败（连接已断）则摘除该连接，防止后续广播反复重试失效连接。
func (h *Hub) writeMsg(conn *websocket.Conn, data []byte) {
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		delete(h.clients, conn)
	}
}

// Send 向单个连接推送消息（线程安全）
func (h *Hub) Send(conn *websocket.Conn, type_ string, payload any) {
	data, _ := json.Marshal(map[string]any{"type": type_, "data": payload})
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[conn]; ok {
		h.writeMsg(conn, data)
	}
}

// Broadcast 广播消息到所有前端（线程安全）
func (h *Hub) Broadcast(type_ string, payload any) {
	data, _ := json.Marshal(map[string]any{"type": type_, "data": payload})
	h.mu.Lock()
	defer h.mu.Unlock()
	for conn := range h.clients {
		h.writeMsg(conn, data)
	}
}

// TaskUpdate 任务状态推送
func TaskUpdatePayload(t *models.Task) map[string]any {
	return map[string]any{
		"task_id":      t.TaskID,
		"status":       t.Status,
		"progress":     t.Progress,
		"current_node": t.CurrentNode,
		"error":        t.Error,
		"gpu_index":    t.GPUIndex,
		"port":         t.Port,
		"result_files": t.ResultFiles,
	}
}
