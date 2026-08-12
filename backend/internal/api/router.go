package api

import (
	"strings"

	"github.com/gin-gonic/gin"

	"comfyui-console/internal/config"
	"comfyui-console/internal/service"
	"comfyui-console/internal/static"
)

type Router struct {
	cfg *config.Config
	svc *service.Service
}

func NewRouter(cfg *config.Config, svc *service.Service) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	r.GET("/api/health", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
	r.GET("/api/release", svc.HandleGetRelease)

	// 实例管理
	r.GET("/api/instances", svc.HandleListInstances)
	r.POST("/api/instances/:id/start", svc.HandleInstanceStart)
	r.POST("/api/instances/:id/stop", svc.HandleInstanceStop)
	r.POST("/api/instances/:id/restart", svc.HandleInstanceRestart)
	r.POST("/api/instances/start-all", svc.HandleStartAll)
	r.POST("/api/instances/stop-all", svc.HandleStopAll)
	r.POST("/api/instances/restart-all", svc.HandleRestartAll)

	// GPU 监控
	r.GET("/api/gpus", svc.HandleListGPUs)

	// 模板
	r.GET("/api/templates", svc.HandleListTemplates)

	// 任务
	r.GET("/api/tasks", svc.HandleListTasks)
	r.DELETE("/api/tasks", svc.HandleClearTasks)
	r.GET("/api/tasks/:id", svc.HandleGetTask)
	r.POST("/api/tasks", svc.HandleCreateTask)
	r.POST("/api/tasks/:id/cancel", svc.HandleCancelTask)
	r.POST("/api/tasks/:id/requeue", svc.HandleRequeueTask)
	r.POST("/api/tasks/cancel-all", svc.HandleCancelAllTasks)
	r.POST("/api/tasks/:id/rerun", svc.HandleRerunTask)

	// 文件
	r.POST("/api/upload", svc.HandleUpload)
	r.GET("/api/output/:gpu/*path", svc.HandleOutputFile)
	r.GET("/api/media/:gpu/*path", svc.HandleMediaInfo)

	// 实时推送
	r.GET("/api/ws", svc.HandleWS)

	// 平台设置（火山引擎）
	r.GET("/api/settings", svc.HandleGetSettings)
	r.PUT("/api/settings", svc.HandleUpdateSettings)
	r.POST("/api/settings/test-text", svc.HandleTestText)
	r.POST("/api/settings/test-image", svc.HandleTestImage)
	r.POST("/api/settings/test-tts", svc.HandleTestTTS)

	// 漫剧项目
	r.GET("/api/projects", svc.HandleListProjects)
	r.POST("/api/projects", svc.HandleCreateProject)
	r.GET("/api/projects/:id", svc.HandleGetProject)
	r.PUT("/api/projects/:id", svc.HandleUpdateProject)
	r.DELETE("/api/projects/:id", svc.HandleDeleteProject)
	r.POST("/api/projects/:id/script", svc.HandleGenerateScript)
	r.POST("/api/projects/:id/script/render", svc.HandleRenderScriptFromText)
	r.POST("/api/projects/:id/script/expand", svc.HandleExpandScript)
	r.POST("/api/projects/:id/plan", svc.HandleGeneratePlan)
	r.PUT("/api/projects/:id/plan/episodes", svc.HandleUpdatePlanEpisodes)
	r.POST("/api/projects/:id/generate", svc.HandleGenerateProject)
	r.POST("/api/projects/:id/images", svc.HandleGenerateAllImages)
	r.POST("/api/projects/:id/videos", svc.HandleGenerateAllVideos)
	r.PATCH("/api/projects/:id/scenes/:sid", svc.HandleUpdateScene)
	r.POST("/api/projects/:id/scenes/:sid/image", svc.HandleGenerateSceneImage)
	r.POST("/api/projects/:id/scenes/:sid/video", svc.HandleGenerateSceneVideo)
	r.POST("/api/projects/:id/scenes/:sid/video/cancel", svc.HandleCancelSceneVideo)
	r.POST("/api/projects/:id/merge", svc.HandleCreateMerge)
	r.POST("/api/projects/:id/merge-all", svc.HandleCreateAllMerges)
	r.GET("/api/projects/:id/merges", svc.HandleListMerges)
	// 对白配音与字幕
	r.GET("/api/projects/:id/scenes/:sid/dialogues", svc.HandleListSceneDialogues)
	r.POST("/api/projects/:id/scenes/:sid/dub", svc.HandleGenerateSceneDub)
	r.POST("/api/projects/:id/dub", svc.HandleGenerateProjectDub)
	r.GET("/api/projects/:id/srt", svc.HandleEpisodeSRT)
	// 剪辑台
	r.GET("/api/projects/:id/editor", svc.HandleEditorData)
	r.PUT("/api/projects/:id/dialogues/:did", svc.HandleUpdateDialogue)
	r.POST("/api/projects/:id/dialogues/:did/dub", svc.HandleRedubDialogue)
	r.PUT("/api/projects/:id/editor/order", svc.HandleReorderScenes)
	r.PATCH("/api/projects/:id/scenes/:sid/duration", svc.HandleUpdateSceneDuration)
	// 角色资产
	r.GET("/api/projects/:id/characters", svc.HandleListCharacters)
	r.POST("/api/projects/:id/characters", svc.HandleCreateCharacter)
	r.POST("/api/projects/:id/characters/portraits", svc.HandleGenerateAllPortraits)
	r.PUT("/api/projects/:id/characters/:cid", svc.HandleUpdateCharacter)
	r.DELETE("/api/projects/:id/characters/:cid", svc.HandleDeleteCharacter)
	r.POST("/api/projects/:id/characters/:cid/portrait", svc.HandleGenerateCharacterPortrait)
	r.POST("/api/projects/:id/characters/:cid/portrait/upload", svc.HandleUploadCharacterPortrait)
	r.GET("/api/input/:taskid/*path", svc.HandleInputFile)

	// 素材库
	r.GET("/api/materials", svc.HandleListMaterials)
	r.POST("/api/materials", svc.HandleUploadMaterial)
	r.DELETE("/api/materials/:id", svc.HandleDeleteMaterial)

	// 前端静态资源 (SPA)
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(404, gin.H{"error": "not found"})
			return
		}
		static.Handler().ServeHTTP(c.Writer, c.Request)
	})

	return r
}
