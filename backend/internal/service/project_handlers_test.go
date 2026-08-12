package service

import (
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"comfyui-console/internal/models"
)

func TestLoadSceneRejectsCrossProjectScene(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Project{}, &models.Scene{}); err != nil {
		t.Fatal(err)
	}
	p1 := models.Project{Title: "p1", Synopsis: "s"}
	p2 := models.Project{Title: "p2", Synopsis: "s"}
	db.Create(&p1)
	db.Create(&p2)
	scene := models.Scene{ProjectID: p2.ID, Order: 1, Status: "pending"}
	db.Create(&scene)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{
		{Key: "id", Value: strconv.FormatUint(uint64(p1.ID), 10)},
		{Key: "sid", Value: strconv.FormatUint(uint64(scene.ID), 10)},
	}
	svc := &Service{DB: db}
	if got, ok := svc.loadScene(c); ok || got != nil {
		t.Fatalf("跨项目场景不应加载成功: %+v", got)
	}
	if w.Code != 404 {
		t.Fatalf("状态码 = %d", w.Code)
	}
}
