package service

import (
	"fmt"
	"path/filepath"
	"strings"

	"gorm.io/gorm"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

// MaterialService 素材库：场景生成图片自动入库 + 手动上传管理
type MaterialService struct {
	cfg    *config.Config
	db     *gorm.DB
	remote *RemoteExec
	upload *UploadManager
}

func NewMaterialService(cfg *config.Config, db *gorm.DB, remote *RemoteExec, upload *UploadManager) *MaterialService {
	return &MaterialService{cfg: cfg, db: db, remote: remote, upload: upload}
}

// SaveGeneratedImage 场景画面生成成功后自动入库
func (m *MaterialService) SaveGeneratedImage(sc *models.Scene, inputPath string, size int64) {
	name := filepath.Base(inputPath)
	projectID := sc.ProjectID
	// 每个场景只保留当前画面记录，避免重试生成产生重复素材。
	m.db.Where("scene_id = ? AND source = ?", sc.ID, "scene").Delete(&models.Material{})
	m.db.Create(&models.Material{
		Name:      fmt.Sprintf("%s_%s", sceneTag(sc), name),
		Type:      "image",
		Source:    "scene",
		ProjectID: &projectID,
		SceneID:   &sc.ID,
		Path:      inputPath,
		Prompt:    sc.ImagePrompt,
		Size:      size,
	})
}

// SaveUpload 手动上传素材入库（保存到 input/<materials>/<name>）
func (m *MaterialService) SaveUpload(projectID *uint, ftype, name string, data []byte) (*models.Material, error) {
	taskID := "materials"
	if projectID != nil {
		taskID = fmt.Sprintf("material_p%d", *projectID)
	}
	path, size, err := m.upload.SaveFile(taskID, ftype, name, data)
	if err != nil {
		return nil, err
	}
	mat := models.Material{
		Name: name, Type: ftype, Source: "upload",
		ProjectID: projectID, Path: path, Size: size,
	}
	if err := m.db.Create(&mat).Error; err != nil {
		return nil, err
	}
	return &mat, nil
}

// List 素材列表，支持类型/项目筛选
func (m *MaterialService) List(ftype string, projectID *uint) ([]models.Material, error) {
	var list []models.Material
	q := m.db.Model(&models.Material{})
	if ftype != "" {
		q = q.Where("type = ?", ftype)
	}
	if projectID != nil {
		q = q.Where("project_id = ?", *projectID)
	}
	if err := q.Order("id desc").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// Delete 删除素材（记录 + 文件）
func (m *MaterialService) Delete(id uint) error {
	var mat models.Material
	if err := m.db.First(&mat, id).Error; err != nil {
		return err
	}
	// 删除 input 文件（场景共享素材不物理删除，避免影响场景引用）
	if mat.Source == "upload" {
		full := m.upload.InputDir() + "/" + mat.Path
		if _, err := m.remote.Run(fmt.Sprintf("rm -f '%s'", full)); err != nil {
			return err
		}
	}
	return m.db.Delete(&mat).Error
}

// TaskIDOf 解析素材 input 相对路径的目录段（用于预览 URL）
func TaskIDOf(path string) string {
	if i := strings.Index(path, "/"); i >= 0 {
		return path[:i]
	}
	return ""
}

// FileNameOf 解析素材 input 相对路径的文件名段
func FileNameOf(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func sceneTag(sc *models.Scene) string {
	if sc.Title != "" {
		return strings.TrimSuffix(sc.Title, " ")
	}
	return fmt.Sprintf("场景%d", sc.Order)
}

var _ = fmt.Sprint
