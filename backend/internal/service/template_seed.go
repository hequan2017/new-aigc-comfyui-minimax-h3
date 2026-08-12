package service

import (
	"embed"
	"encoding/json"
	"log"

	"gorm.io/gorm"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

//go:embed templates/*.json
var templateFS embed.FS

type templateFile struct {
	Name         string                   `json:"name"`
	Code         string                   `json:"code"`
	Description  string                   `json:"description"`
	Inputs       []map[string]any         `json:"inputs"`
	Workflow     map[string]map[string]any `json:"workflow"`
}

// InitSystemTemplates 初始化内置模板到 DB
func InitSystemTemplates(db *gorm.DB) error {
	entries, err := templateFS.ReadDir("templates")
	if err != nil {
		return err
	}
	for _, e := range entries {
		data, err := templateFS.ReadFile("templates/" + e.Name())
		if err != nil {
			return err
		}
		var tf templateFile
		if err := json.Unmarshal(data, &tf); err != nil {
			return err
		}
		inputsJSON, _ := json.Marshal(tf.Inputs)
		workflowJSON, _ := json.Marshal(tf.Workflow)

		var existing models.Template
		err = db.Where("code = ?", tf.Code).First(&existing).Error
		if err == gorm.ErrRecordNotFound {
			tpl := models.Template{
				Name:         tf.Name,
				Code:         tf.Code,
				Description:  tf.Description,
				WorkflowJSON: string(workflowJSON),
				InputsJSON:   string(inputsJSON),
				IsSystem:     true,
				Enabled:      true,
			}
			if err := db.Create(&tpl).Error; err != nil {
				return err
			}
			log.Printf("[templates] seeded %s", tf.Code)
		} else if err == nil {
			// 更新系统模板
			db.Model(&existing).Updates(map[string]any{
				"name": tf.Name, "description": tf.Description,
				"workflow_json": string(workflowJSON), "inputs_json": string(inputsJSON),
			})
		}
	}
	return nil
}

// UploadManager 素材文件管理
type UploadManager struct {
	cfg    *config.Config
	db     *gorm.DB
	remote *RemoteExec
}

func NewUploadManager(cfg *config.Config, db *gorm.DB, remote *RemoteExec) *UploadManager {
	return &UploadManager{cfg: cfg, db: db, remote: remote}
}

// InputDir 返回 ComfyUI 共享 input 目录
func (m *UploadManager) InputDir() string {
	return m.cfg.Comfy.ComfyDir + "/input"
}

// SaveFile 保存上传文件到 ComfyUI input 目录（远程模式走 SFTP），返回 input 相对路径 (taskId/filename)
func (m *UploadManager) SaveFile(taskID, ftype, filename string, data []byte) (string, int64, error) {
	path := taskID + "/" + filename
	if err := m.remote.WriteFile(m.InputDir()+"/"+path, data, 0o644); err != nil {
		return "", 0, err
	}
	rec := models.UploadFile{
		TaskID: taskID,
		Type:   ftype,
		Name:   filename,
		Path:   path,
		Size:   int64(len(data)),
	}
	m.db.Create(&rec)
	return path, int64(len(data)), nil
}
