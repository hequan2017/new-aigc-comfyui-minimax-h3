package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"comfyui-console/internal/models"
)

// AliyunTTS 阿里云百炼 TTS（后期配音）：DashScope 原生协议
// POST {base}/api/v1/services/aigc/multimodal-generation/generation
// body: {"model":"qwen3-tts-flash","input":{"text":"..."},"parameters":{"voice":"Cherry","format":"mp3","language_type":"Chinese"}}
// 返回 {"output":{"audio":{"url":"..."}}}
type AliyunTTS struct {
	db    *gorm.DB
	httpc *http.Client
}

func NewAliyunTTS(db *gorm.DB) *AliyunTTS {
	return &AliyunTTS{db: db, httpc: &http.Client{Timeout: 120 * time.Second}}
}

// AliConfig 阿里云 TTS 配置
type AliConfig struct {
	APIKey    string            `json:"api_key"`
	BaseURL   string            `json:"base_url"`
	Model     string            `json:"model"`
	Voice     string            `json:"voice"`       // 默认音色
	VoiceMale string            `json:"voice_male"`  // 男声默认音色
	VoiceMap  map[string]string `json:"voice_map"`
	Extra     map[string]any    `json:"extra"`
}

func (a *AliyunTTS) get(key, def string) string {
	var s models.Setting
	if a.db != nil && a.db.Where("key = ?", key).First(&s).Error == nil && s.Value != "" {
		return s.Value
	}
	return def
}

// Config 读取阿里云 TTS 配置（实时读库）
func (a *AliyunTTS) Config() AliConfig {
	cfg := AliConfig{
		APIKey:    a.get(SettingAliAPIKey, ""),
		BaseURL:   a.get(SettingAliBaseURL, "https://llm-ebg0fg1ejgmvv30a.cn-beijing.maas.aliyuncs.com"),
		Model:     a.get(SettingAliTTSModel, "qwen3-tts-flash"),
		Voice:     a.get(SettingAliTTSVoice, "Cherry"),
		VoiceMale: a.get(SettingAliVoiceMale, "Ethan"),
		VoiceMap:  map[string]string{},
	}
	if raw := a.get(SettingAliTTSStyle, ""); raw != "" {
		_ = json.Unmarshal([]byte(raw), &cfg.VoiceMap)
	}
	if raw := a.get(SettingAliTTSExtra, ""); raw != "" {
		_ = json.Unmarshal([]byte(raw), &cfg.Extra)
	}
	return cfg
}

func (c AliConfig) Configured() bool {
	return c.APIKey != ""
}

// isMaleCharacter 根据角色名/身份判断性别（男角色关键词）
func isMaleCharacter(character string) bool {
	s := strings.ToLower(strings.TrimSpace(character))
	for _, kw := range []string{"男主", "男", "皇帝", "太子", "王爷", "皇上", "帝", "公子", "少爷", "叔", "哥", "爹", "父", "爷", "师傅", "太监", "侍卫", "将军", "丞相", "太傅", "员外", "道士", "和尚", "king", "prince", "mr", "boy", "male"} {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}

// VoiceFor 返回角色音色：角色映射优先 → 按性别（男/女）→ 默认音色
func (c AliConfig) VoiceFor(character string) string {
	if character != "" {
		if v, ok := c.VoiceMap[strings.TrimSpace(character)]; ok && v != "" {
			return v
		}
		if isMaleCharacter(character) && c.VoiceMale != "" {
			return c.VoiceMale
		}
	}
	return c.Voice
}

// TextToSpeech 文本转语音：返回 mp3 字节。voice 为空用默认音色。
func (a *AliyunTTS) TextToSpeech(text, character, voice string) ([]byte, error) {
	cfg := a.Config()
	if !cfg.Configured() {
		return nil, fmt.Errorf("尚未配置阿里云 TTS API Key，请到「平台设置」填写")
	}
	if voice == "" {
		voice = cfg.VoiceFor(character)
	}
	if voice == "" {
		return nil, fmt.Errorf("未配置 TTS 音色，请在「平台设置」填写默认音色或角色音色映射")
	}
	model := cfg.Model
	if model == "" {
		model = "qwen3-tts-flash"
	}
	params := map[string]any{
		"voice": voice, "format": "mp3", "language_type": "Chinese",
	}
	for k, v := range cfg.Extra {
		params[k] = v
	}
	body, _ := json.Marshal(map[string]any{
		"model":      model,
		"input":      map[string]any{"text": text},
		"parameters": params,
	})
	url := strings.TrimRight(cfg.BaseURL, "/") + "/api/v1/services/aigc/multimodal-generation/generation"
	req, err := http.NewRequest("POST", url, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用阿里云 TTS 失败: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("阿里云 TTS 返回 %d: %s", resp.StatusCode, truncateStr(string(data), 300))
	}
	var out struct {
		Output struct {
			Audio struct {
				URL  string `json:"url"`
				Data string `json:"data"`
			} `json:"audio"`
			FinishReason string `json:"finish_reason"`
		} `json:"output"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("解析阿里云 TTS 响应失败: %w", err)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("阿里云 TTS 错误: %s %s", out.Error.Code, out.Error.Message)
	}
	if out.Output.Audio.Data != "" {
		// base64 数据
		raw, err := decodeBase64(out.Output.Audio.Data)
		if err == nil && len(raw) > 0 {
			return raw, nil
		}
	}
	if out.Output.Audio.URL == "" {
		return nil, fmt.Errorf("阿里云 TTS 响应缺少音频 URL")
	}
	// 下载音频
	audioResp, err := a.httpc.Get(out.Output.Audio.URL)
	if err != nil {
		return nil, fmt.Errorf("下载音频失败: %w", err)
	}
	defer audioResp.Body.Close()
	if audioResp.StatusCode != 200 {
		return nil, fmt.Errorf("下载音频失败: %d", audioResp.StatusCode)
	}
	return io.ReadAll(audioResp.Body)
}

// TestTTS 测试 TTS 连通性
func (a *AliyunTTS) TestTTS() (string, error) {
	data, err := a.TextToSpeech("你好，这是配音连通性测试。", "", "")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("合成成功，音频 %d 字节", len(data)), nil
}
