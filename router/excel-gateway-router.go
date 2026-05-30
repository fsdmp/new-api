package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func SetExcelGatewayRouter(router *gin.Engine) {
	router.GET("/healthz", middleware.RouteTag("excel-gateway"), controller.Healthz)

	router.GET("/v1/metrics", middleware.RouteTag("excel-gateway"), controller.MetricsMock)
	router.POST("/v1/metrics", middleware.RouteTag("excel-gateway"), controller.MetricsMock)

	router.GET("/v2/traces", middleware.RouteTag("excel-gateway"), controller.TracesMock)
	router.POST("/v2/traces", middleware.RouteTag("excel-gateway"), controller.TracesMock)

	router.POST("/api/eval/:sdkKey", middleware.RouteTag("excel-gateway"), controller.EvalMock)
	router.GET("/api/version", middleware.RouteTag("excel-gateway"), controller.VersionMock)
	router.POST("/api/analytics", middleware.RouteTag("excel-gateway"), controller.AnalyticsMock)

	router.GET("/shortcuts.json", middleware.RouteTag("excel-gateway"), controller.ShortcutsMock)

	// Excel relay — sanitizes requests, injects Chinese language instructions,
	// then forwards through the standard Claude relay pipeline.
	excelRouter := router.Group("/v1/excel")
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
