package service

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"comfyui-console/internal/models"
)

func TestUpdateSettingsPreservesAPIKeyWhenEmpty(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Setting{}); err != nil {
		t.Fatal(err)
	}
	v := NewVolcClient(db)
	v.SetSetting(SettingVolcAPIKey, "secret-key")
	v.UpdateSettings(map[string]string{
		SettingVolcAPIKey:  "",
		SettingVolcImgSize: "2k",
	})
	if got := v.GetSetting(SettingVolcAPIKey, ""); got != "secret-key" {
		t.Fatalf("空密钥覆盖了已有配置: %q", got)
	}
	if got := v.GetSetting(SettingVolcImgSize, ""); got != "2k" {
		t.Fatalf("其他设置未保存: %q", got)
	}
}
