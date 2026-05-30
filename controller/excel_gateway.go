package controller

import (
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
)

func Healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func MetricsMock(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"partialSuccess": gin.H{}})
}

func TracesMock(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"partialSuccess": gin.H{}})
}

func EvalMock(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"features": gin.H{}})
}

func VersionMock(c *gin.Context) {
	sha := "unknown"
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" {
				sha = s.Value
				break
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"sha": sha})
}

func AnalyticsMock(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func ShortcutsMock(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{})
}
