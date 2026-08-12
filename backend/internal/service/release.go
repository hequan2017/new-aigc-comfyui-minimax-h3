package service

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

// ReleaseInfo 发版信息：编号、时间、功能说明、来源 commit
type ReleaseInfo struct {
	Number   int      `json:"number"`
	Time     string   `json:"time"`
	Commit   string   `json:"commit"`
	Features []string `json:"features"`
}

// HandleGetRelease 读取发版信息（编号/时间/功能说明），无记录时返回默认空值
func (s *Service) HandleGetRelease(c *gin.Context) {
	path := filepath.Join(s.Cfg.Storage.DataDir, "release.json")
	data, err := os.ReadFile(path)
	if err != nil {
		c.JSON(200, ReleaseInfo{Number: 0, Features: []string{}})
		return
	}
	var rel ReleaseInfo
	if err := json.Unmarshal(data, &rel); err != nil {
		c.JSON(200, ReleaseInfo{Number: 0, Features: []string{}})
		return
	}
	if rel.Features == nil {
		rel.Features = []string{}
	}
	c.JSON(200, rel)
}
