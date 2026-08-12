package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"comfyui-console/internal/models"
)

// 火山引擎 Ark 平台配置键
const (
	SettingVolcAPIKey    = "volc_api_key"
	SettingVolcBaseURL   = "volc_base_url"
	SettingVolcTextModel = "volc_text_model"
	SettingVolcImgModel  = "volc_image_model"
	SettingVolcImgSize   = "volc_image_size"
)

// 阿里云百炼 TTS（后期配音）配置键
const (
	SettingAliAPIKey    = "ali_api_key"
	SettingAliBaseURL   = "ali_base_url"
	SettingAliTTSModel  = "ali_tts_model"
	SettingAliTTSVoice  = "ali_tts_voice"  // 默认音色（角色未单独配置时）
	SettingAliVoiceMale = "ali_voice_male" // 男声默认音色（男角色自动使用）
	SettingAliTTSStyle  = "ali_tts_style"  // 角色音色映射 JSON：{"角色名":"音色"}
	SettingAliTTSExtra  = "ali_tts_extra"  // 额外参数 JSON（instruction 等）
)

const (
	DefaultVolcBaseURL   = "https://ark.cn-beijing.volces.com/api/v3"
	DefaultVolcTextModel = "deepseek-v4-flash-ga-260731"
	DefaultVolcImgModel  = "doubao-seedream-5-0-260128"
	// seedream 5.0 要求 size 为 'WIDTHxHEIGHT'/'2k'/'3k'/'4k'，且像素数 ≥ 3686400（即 1920x1920）
	DefaultVolcImgSize = "1920x1920"
)

// normalizeVolcSize 兼容旧配置值: 1K/2K/3K/4K -> 1920x1920/2k/3k/4k
func normalizeVolcSize(size string) string {
	switch strings.ToLower(size) {
	case "1k":
		return "1920x1920"
	case "2k", "3k", "4k":
		return strings.ToLower(size)
	}
	return size
}

// VolcConfig 火山引擎配置
type VolcConfig struct {
	APIKey    string `json:"api_key"`
	BaseURL   string `json:"base_url"`
	TextModel string `json:"text_model"`
	ImgModel  string `json:"image_model"`
	ImgSize   string `json:"image_size"`
}

func (c VolcConfig) Endpoint(api string) string {
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = DefaultVolcBaseURL
	}
	return base + api
}

func (c VolcConfig) Configured() bool {
	return c.APIKey != ""
}

// VolcClient 火山引擎 Ark 在线模型客户端（文生文 / 文生图）
type VolcClient struct {
	db    *gorm.DB
	httpc *http.Client
}

func NewVolcClient(db *gorm.DB) *VolcClient {
	return &VolcClient{
		db:    db,
		httpc: &http.Client{Timeout: 120 * time.Second},
	}
}

// Config 从平台设置读取火山引擎配置（实时读取，改配置立即生效）
func (v *VolcClient) Config() VolcConfig {
	get := func(key, def string) string {
		var s models.Setting
		if v.db != nil && v.db.Where("key = ?", key).First(&s).Error == nil && s.Value != "" {
			return s.Value
		}
		return def
	}
	return VolcConfig{
		APIKey:    get(SettingVolcAPIKey, ""),
		BaseURL:   get(SettingVolcBaseURL, DefaultVolcBaseURL),
		TextModel: get(SettingVolcTextModel, DefaultVolcTextModel),
		ImgModel:  get(SettingVolcImgModel, DefaultVolcImgModel),
		ImgSize:   get(SettingVolcImgSize, DefaultVolcImgSize),
	}
}

// ---------- 文生文（Responses API） ----------

type volcResponseMessage struct {
	Type    string                `json:"type"`
	Role    string                `json:"role"`
	Content []volcResponseContent `json:"content"`
}

type volcResponseContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type volcResponsesReq struct {
	Model  string              `json:"model"`
	Stream bool                `json:"stream"`
	Input  []volcResponseInput `json:"input"`
}

type volcResponseInput struct {
	Role    string                `json:"role"`
	Content []volcResponseContent `json:"content"`
}

type volcResponsesResp struct {
	Output []volcResponseMessage `json:"output"`
	Error  *volcAPIError         `json:"error"`
}

type volcAPIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Chat 文生文：system 为系统提示词，user 为用户输入，返回模型文本
// 模型偶发只返回 reasoning 无文本，自动重试一次
func (v *VolcClient) Chat(system, user string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Second)
		}
		text, err := v.chatOnce(system, user)
		if err == nil {
			return text, nil
		}
		lastErr = err
	}
	return "", lastErr
}

func (v *VolcClient) chatOnce(system, user string) (string, error) {
	cfg := v.Config()
	if !cfg.Configured() {
		return "", fmt.Errorf("尚未配置火山引擎 API Key，请到「平台设置」中填写")
	}
	input := []volcResponseInput{}
	if system != "" {
		input = append(input, volcResponseInput{Role: "system", Content: []volcResponseContent{{Type: "input_text", Text: system}}})
	}
	input = append(input, volcResponseInput{Role: "user", Content: []volcResponseContent{{Type: "input_text", Text: user}}})
	body, _ := json.Marshal(volcResponsesReq{Model: cfg.TextModel, Stream: false, Input: input})

	req, err := http.NewRequest("POST", cfg.Endpoint("/responses"), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.httpc.Do(req)
	if err != nil {
		return "", fmt.Errorf("调用文生文接口失败: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("文生文接口返回 %d: %s", resp.StatusCode, truncateStr(string(data), 300))
	}

	var out volcResponsesResp
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("解析文生文响应失败: %w", err)
	}
	if out.Error != nil {
		return "", fmt.Errorf("文生文接口错误: %s %s", out.Error.Code, out.Error.Message)
	}
	var sb strings.Builder
	for _, msg := range out.Output {
		if msg.Role == "assistant" {
			for _, c := range msg.Content {
				if c.Type == "output_text" {
					sb.WriteString(c.Text)
				}
			}
		}
	}
	if sb.Len() == 0 {
		return "", fmt.Errorf("文生文接口未返回内容（原始响应: %s）", truncateStr(string(data), 200))
	}
	return sb.String(), nil
}

// ---------- 文生图 / 图生图（Images generations API） ----------

// ImageRef 参考图（图生图底图）。URL 为可访问图片 URL 或 data:image/...;base64,... 编码。
// 方舟 images/generations 通过 image 字段做"参考图生图"：多张参考图时传数组，模型据此保持人物主体一致。
type ImageRef struct {
	URL string
}

type volcImageReq struct {
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	Size           string `json:"size"`
	ResponseFormat string `json:"response_format"`
	Watermark      bool   `json:"watermark"`
	Stream         bool   `json:"stream"`
	// 参考图（图生图底图，如角色标准像）。对应方舟 image 字段：
	// 单张传字符串，多张传字符串数组；模型据此保持人物外貌一致。用 any 兼容两种形态。
	Image any `json:"image,omitempty"`
}

type volcImageItem struct {
	URL string `json:"url"`
	B64 string `json:"b64_json"`
}

type volcImageResp struct {
	Data  []volcImageItem `json:"data"`
	Error *volcAPIError   `json:"error"`
}

// GenerateImage 文生图：返回图片原始字节。refImages 为可选参考图（URL 或 base64 data URL），用作图生图底图。
func (v *VolcClient) GenerateImage(prompt, size string, refImages ...string) ([]byte, error) {
	refs := make([]ImageRef, 0, len(refImages))
	for _, u := range refImages {
		if u != "" {
			refs = append(refs, ImageRef{URL: u})
		}
	}
	return v.GenerateImageRefs(prompt, size, refs)
}

// GenerateImageRefs 文生图/参考图生图：返回图片原始字节。
// refs 为参考图（角色标准像等），通过方舟 image 字段传入：单张为字符串，多张为字符串数组。
// seedream 据此保持人物主体/风格一致。response_format 优先 url，失败回退 b64_json。
func (v *VolcClient) GenerateImageRefs(prompt, size string, refs []ImageRef) ([]byte, error) {
	cfg := v.Config()
	if !cfg.Configured() {
		return nil, fmt.Errorf("尚未配置火山引擎 API Key，请到「平台设置」中填写")
	}
	if size == "" {
		size = cfg.ImgSize
	}
	if size == "" {
		size = DefaultVolcImgSize
	}
	size = normalizeVolcSize(size)

	// 组装 image 字段：收集参考图 URL，单张传字符串、多张传数组、无则纯文生图（nil 配合 omitempty 省略）。
	var imageField any
	urls := make([]string, 0, len(refs))
	for _, r := range refs {
		if r.URL != "" {
			urls = append(urls, r.URL)
		}
	}
	switch len(urls) {
	case 0:
		imageField = nil
	case 1:
		imageField = urls[0]
	default:
		imageField = urls
	}

	// 优先 url，失败时回退 b64_json
	data, err := v.generateImageOnce(cfg, prompt, size, "url", imageField)
	if err == nil && len(data) > 0 {
		return data, nil
	}
	b64, err2 := v.generateImageOnce(cfg, prompt, size, "b64_json", imageField)
	if err2 != nil {
		return nil, fmt.Errorf("文生图失败: url: %v; b64: %v", err, err2)
	}
	return b64, nil
}

func (v *VolcClient) generateImageOnce(cfg VolcConfig, prompt, size, format string, image any) ([]byte, error) {
	body, _ := json.Marshal(volcImageReq{
		Model: cfg.ImgModel, Prompt: prompt, Size: size,
		ResponseFormat: format, Watermark: true, Stream: false,
		Image: image,
	})
	req, err := http.NewRequest("POST", cfg.Endpoint("/images/generations"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("接口返回 %d: %s", resp.StatusCode, truncateStr(string(data), 300))
	}
	var out volcImageResp
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("%s %s", out.Error.Code, out.Error.Message)
	}
	if len(out.Data) == 0 {
		return nil, fmt.Errorf("响应中无图片数据")
	}
	item := out.Data[0]
	if item.B64 != "" {
		raw, err := base64.StdEncoding.DecodeString(item.B64)
		if err != nil {
			return nil, fmt.Errorf("解析 base64 图片失败: %w", err)
		}
		return raw, nil
	}
	if item.URL == "" {
		return nil, fmt.Errorf("响应缺少图片 URL")
	}
	// 下载图片
	imgResp, err := v.httpc.Get(item.URL)
	if err != nil {
		return nil, fmt.Errorf("下载图片失败: %w", err)
	}
	defer imgResp.Body.Close()
	if imgResp.StatusCode != 200 {
		return nil, fmt.Errorf("下载图片失败: %d", imgResp.StatusCode)
	}
	return io.ReadAll(imgResp.Body)
}

// ---------- 设置读写 ----------

// GetSetting 读取单个设置
func (v *VolcClient) GetSetting(key, def string) string {
	var s models.Setting
	if v.db.Where("key = ?", key).First(&s).Error == nil && s.Value != "" {
		return s.Value
	}
	return def
}

// SetSetting 写入单个设置
func (v *VolcClient) SetSetting(key, value string) {
	v.db.Save(&models.Setting{Key: key, Value: value})
}

// AllSettings 返回全部平台设置（API Key 打码后返回，避免前端明文回显）
func (v *VolcClient) AllSettings() map[string]string {
	keys := []string{SettingVolcAPIKey, SettingVolcBaseURL, SettingVolcTextModel, SettingVolcImgModel, SettingVolcImgSize,
		SettingAliAPIKey, SettingAliBaseURL, SettingAliTTSModel, SettingAliTTSVoice, SettingAliVoiceMale, SettingAliTTSStyle, SettingAliTTSExtra, "video_concurrency", "video_resolution"}
	defs := []string{"", DefaultVolcBaseURL, DefaultVolcTextModel, DefaultVolcImgModel, DefaultVolcImgSize,
		"", "https://llm-ebg0fg1ejgmvv30a.cn-beijing.maas.aliyuncs.com", "qwen3-tts-flash", "Cherry", "Ethan", "{}", "{}", "4", "720p"}
	out := map[string]string{}
	for i, k := range keys {
		out[k] = v.GetSetting(k, defs[i])
	}
	// API Key 打码：只保留前 4 后 4
	if out[SettingVolcAPIKey] != "" {
		k := out[SettingVolcAPIKey]
		if len(k) > 12 {
			out[SettingVolcAPIKey] = k[:4] + "****" + k[len(k)-4:]
		}
	}
	if out[SettingAliAPIKey] != "" {
		k := out[SettingAliAPIKey]
		if len(k) > 12 {
			out[SettingAliAPIKey] = k[:4] + "****" + k[len(k)-4:]
		}
	}
	return out
}

// UpdateSettings 批量保存设置（API Key 传入原始值；打码形式则保留原值）
func (v *VolcClient) UpdateSettings(m map[string]string) {
	cur := v.GetSetting(SettingVolcAPIKey, "")
	aliCur := v.GetSetting(SettingAliAPIKey, "")
	for k, val := range m {
		switch k {
		case SettingVolcAPIKey:
			// 空值表示前端没有修改密钥；显式清空必须使用单独的管理动作。
			if val == "" || strings.Contains(val, "****") {
				val = cur
			}
		case SettingAliAPIKey:
			if val == "" || strings.Contains(val, "****") {
				val = aliCur
			}
		}
		v.SetSetting(k, val)
	}
}

// TestText 测试文生文接口连通性
func (v *VolcClient) TestText() (string, error) {
	cfg := v.Config()
	if !cfg.Configured() {
		return "", fmt.Errorf("未配置 API Key")
	}
	text, err := v.Chat("你是连接测试助手。", "请只回复两个字：正常")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(text), nil
}

// TestImage 测试文生图接口连通性
func (v *VolcClient) TestImage() (string, error) {
	cfg := v.Config()
	if !cfg.Configured() {
		return "", fmt.Errorf("未配置 API Key")
	}
	data, err := v.GenerateImage("一个红色圆形，纯色背景，简笔画", "1920x1920")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("生成成功，图片 %d 字节", len(data)), nil
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// decodeBase64 解码 base64 数据（复用 encoding/base64）
func decodeBase64(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}
