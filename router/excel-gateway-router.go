package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

func SetExcelGatewayRouter(router *gin.Engine) {
	excel := router.Group("/excel")
	excel.Use(middleware.RouteTag("excel-gateway"))
	excel.GET("/healthz", controller.Healthz)
	excel.GET("/shortcuts.json", controller.ShortcutsMock)
	excel.POST("/tmp-key", controller.CreateExcelTmpKey)

	excel.GET("/models", controller.ExcelListModels)
	excel.GET("/v1/models", middleware.RouteTag("excel-gateway"), controller.ExcelListModels)

	excel.GET("/v1/metrics", middleware.RouteTag("excel-gateway"), controller.MetricsMock)
	excel.POST("/v1/metrics", middleware.RouteTag("excel-gateway"), controller.MetricsMock)

	excel.GET("/v2/traces", middleware.RouteTag("excel-gateway"), controller.TracesMock)
	excel.POST("/v2/traces", middleware.RouteTag("excel-gateway"), controller.TracesMock)

	excel.POST("/api/eval/:sdkKey", middleware.RouteTag("excel-gateway"), controller.EvalMock)
	excel.GET("/api/version", middleware.RouteTag("excel-gateway"), controller.VersionMock)
	excel.POST("/api/analytics", middleware.RouteTag("excel-gateway"), controller.AnalyticsMock)
	excel.GET("/api/check-version", middleware.RouteTag("excel-gateway"), controller.CheckVersion)

	// Debug endpoints for the server-tool executors (flag-gated, default
	// off; see design doc §5.6.4). Direct executor invocation for联调/压测.
	toolsRouter := excel.Group("/api/tools")
	toolsRouter.Use(middleware.RouteTag("excel-gateway"))
	toolsRouter.Use(middleware.TokenAuth())
	toolsRouter.POST("/:tool", controller.ExcelToolDebug)

	// Excel relay — sanitizes requests, injects Chinese language instructions,
	// then forwards through the standard Claude relay pipeline.
	// TokenAuth runs BEFORE the adapter so the adapter can scope document-block
	// file lookups to the authenticated user (M2); TokenAuth reads no body.
	excelRouter := excel.Group("/v1")
	excelRouter.Use(middleware.RouteTag("relay"))
	excelRouter.Use(middleware.SystemPerformanceCheck())
	excelRouter.Use(middleware.TokenAuth())
	excelRouter.Use(middleware.ExcelRequestAdapter())
	excelRouter.Use(middleware.ModelRequestRateLimit())
	{
		httpRouter := excelRouter.Group("")
		httpRouter.Use(middleware.Distribute())
		httpRouter.POST("/messages", controller.ExcelMessagesRelay)
	}

	// M2 file service — Anthropic Files API compatible subset (upload / list /
	// download / delete), TokenAuth-scoped to the owner.
	filesRouter := excel.Group("/v1/files")
	filesRouter.Use(middleware.RouteTag("excel-gateway"))
	filesRouter.Use(middleware.TokenAuth())
	filesRouter.POST("", controller.ExcelFileUpload)
	filesRouter.GET("", controller.ExcelFileList)
	filesRouter.GET("/:id", controller.ExcelFileDownload)
	filesRouter.DELETE("/:id", controller.ExcelFileDelete)

	// M4 MCP connector registry + stateless JSON-RPC proxy — the add-in
	// tunnels all connector traffic through the gateway (same-origin), tools
	// execute client-side.
	mcpServers := excel.Group("/v1/mcp_servers")
	mcpServers.Use(middleware.RouteTag("excel-gateway"))
	mcpServers.Use(middleware.TokenAuth())
	mcpServers.GET("", controller.ExcelMCPListModels)

	mcpProxy := excel.Group("/v1/mcp")
	mcpProxy.Use(middleware.RouteTag("excel-gateway"))
	mcpProxy.Use(middleware.TokenAuth())
	mcpProxy.POST("/:server_id", controller.ExcelMCPProxy)

	// Icon-registry stub: the add-in backfills icon_url from here; an empty
	// list skips silently. NOTE: the plugin's URL has no /v1/ or /api/ segment
	// so the overlay leaves the path at the domain root. No auth (public
	// metadata only).
	excelRouter2 := router.Group("/mcp-registry/v0")
	excelRouter2.Use(middleware.RouteTag("excel-gateway"))
	excelRouter2.GET("/servers", controller.ExcelMCPRegistryStub)

	// M5 skill cloud sync — the add-in hardcodes claude.ai-style paths and the
	// overlay leaves /api/oauth/* at the domain root. Same sk- token as the
	// rest of the Excel gateway (TokenAuth).
	skillsRouter := router.Group("/api/oauth/organizations/:org/skills")
	skillsRouter.Use(middleware.RouteTag("excel-gateway"))
	skillsRouter.Use(middleware.TokenAuth())
	skillsRouter.GET("/list-skills", controller.ExcelSkillList)
	skillsRouter.GET("/:skill_id/instructions", controller.ExcelSkillInstructions)
	skillsRouter.GET("/:skill_id/download", controller.ExcelSkillDownload)
	skillsRouter.POST("/upload-skill", controller.ExcelSkillUpload)

	// M5 admin management (classic panel, session auth).
	skillAdmin := router.Group("/api/excel/skills")
	skillAdmin.Use(middleware.RouteTag("excel-gateway"))
	skillAdmin.Use(middleware.AdminAuth())
	skillAdmin.GET("", controller.ExcelSkillAdminList)
	skillAdmin.POST("", middleware.CriticalRateLimit(), controller.ExcelSkillAdminUpload)
	skillAdmin.PUT("/:id", controller.ExcelSkillAdminUpdate)
	skillAdmin.DELETE("/:id", controller.ExcelSkillAdminDelete)
}
