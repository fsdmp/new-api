package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func SetExcelGatewayRouter(router *gin.Engine) {
	excel := router.Group("/excel")
	excel.Use(middleware.RouteTag("excel-gateway"))
	excel.GET("/healthz", controller.Healthz)
	excel.GET("/shortcuts.json", controller.ShortcutsMock)

	excel.GET("/models", controller.ExcelListModels)
	excel.GET("/v1/models", middleware.RouteTag("excel-gateway"), controller.ExcelListModels)

	excel.GET("/v1/metrics", middleware.RouteTag("excel-gateway"), controller.MetricsMock)
	excel.POST("/v1/metrics", middleware.RouteTag("excel-gateway"), controller.MetricsMock)

	excel.GET("/v2/traces", middleware.RouteTag("excel-gateway"), controller.TracesMock)
	excel.POST("/v2/traces", middleware.RouteTag("excel-gateway"), controller.TracesMock)

	excel.POST("/api/eval/:sdkKey", middleware.RouteTag("excel-gateway"), controller.EvalMock)
	excel.GET("/api/version", middleware.RouteTag("excel-gateway"), controller.VersionMock)
	excel.POST("/api/analytics", middleware.RouteTag("excel-gateway"), controller.AnalyticsMock)

	// Excel relay — sanitizes requests, injects Chinese language instructions,
	// then forwards through the standard Claude relay pipeline.
	excelRouter := excel.Group("/v1")
	excelRouter.Use(middleware.RouteTag("relay"))
	excelRouter.Use(middleware.SystemPerformanceCheck())
	excelRouter.Use(middleware.ExcelRequestAdapter())
	excelRouter.Use(middleware.TokenAuth())
	excelRouter.Use(middleware.ModelRequestRateLimit())
	{
		httpRouter := excelRouter.Group("")
		httpRouter.Use(middleware.Distribute())
		httpRouter.POST("/messages", func(c *gin.Context) {
			controller.Relay(c, types.RelayFormatClaude)
		})
	}
}
