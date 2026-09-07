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
}
