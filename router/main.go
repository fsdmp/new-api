package router

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

func SetRouter(router *gin.Engine, assets ThemeAssets) {
	router.Use(middleware.CORS())
	// 零停机蓝绿发布探针：注册为根级具体路由（先于 SetApiRouter / SetWebRouter）。
	// gin 路由树命中精确静态节点即返回，不会落入 SetWebRouter 的 static.Serve/NoRoute
	// SPA 兜底；且不进 /api 组的限流/gzip，探针零成本。
	router.GET("/healthz", controller.GetHealthz)
	router.GET("/readyz", controller.GetReadyz)
	SetApiRouter(router)
	SetDashboardRouter(router)
	SetRelayRouter(router)
	SetExcelGatewayRouter(router)
	SetVideoRouter(router)
	frontendBaseUrl := os.Getenv("FRONTEND_BASE_URL")
	if common.IsMasterNode && frontendBaseUrl != "" {
		frontendBaseUrl = ""
		common.SysLog("FRONTEND_BASE_URL is ignored on master node")
	}
	if frontendBaseUrl == "" {
		SetWebRouter(router, assets)
	} else {
		frontendBaseUrl = strings.TrimSuffix(frontendBaseUrl, "/")
		router.NoRoute(func(c *gin.Context) {
			c.Set(middleware.RouteTagKey, "web")
			c.Redirect(http.StatusMovedPermanently, fmt.Sprintf("%s%s", frontendBaseUrl, c.Request.RequestURI))
		})
	}
}
