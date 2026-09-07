package controller

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service/exceltools"
	"github.com/QuantumNous/new-api/setting/excel_mcp"

	"github.com/gin-gonic/gin"
)

// Excel MCP connector endpoints (M4, docs/excel-mcp-skills-m4m5-design.md
// §2.2). The add-in discovers servers via GET /excel/v1/mcp_servers and
// JSON-RPCs them through POST /excel/v1/mcp/:server_id; tool execution is
// client-side, so no relay/billing changes are involved.

// ExcelMCPListModels serves GET /excel/v1/mcp_servers — the connector
// registry. Auth via TokenAuth (the add-in sends its sk- token).
func ExcelMCPListModels(c *gin.Context) {
	if !excel_mcp.Config().Enabled {
		c.JSON(http.StatusOK, gin.H{"data": []interface{}{}, "next_page": nil})
		return
	}
	c.JSON(http.StatusOK, exceltools.MCPRegistryResponse())
}

// ExcelMCPProxy serves POST /excel/v1/mcp/:server_id — stateless JSON-RPC
// pass-through to the configured upstream (streamable-http MCP).
func ExcelMCPProxy(c *gin.Context) {
	if !excel_mcp.Config().Enabled {
		mcpProxyError(c, http.StatusNotFound, "not_found", "MCP connector service is not enabled")
		return
	}
	server := excel_mcp.FindServer(c.Param("server_id"))
	if server == nil {
		mcpProxyError(c, http.StatusNotFound, "not_found", "no such MCP server")
		return
	}

	body, err := io.ReadAll(io.LimitReader(c.Request.Body, exceltools.MCPBodyBudget()))
	if err != nil {
		mcpProxyError(c, http.StatusBadRequest, "invalid_request", "read body failed")
		return
	}

	// Method sniffing drives the timeout tier and the proxy whitelist; a
	// notification (no id) never gets a result body back, but still forwards.
	method := exceltools.MCPExtractMethod(body)
	if method != "" && !exceltools.MCPAllowedMethod(method) {
		mcpProxyError(c, http.StatusBadRequest, "invalid_request", "method not allowed: "+method)
		return
	}

	release, ok := exceltools.MCPAcquire(c.Request.Context())
	if !ok {
		mcpProxyError(c, http.StatusServiceUnavailable, "overloaded", "MCP proxy concurrency limit reached")
		return
	}
	defer release()

	ctx, cancel := context.WithTimeout(c.Request.Context(), exceltools.MCPProxyTimeout(server, method))
	defer cancel()

	resp, err := exceltools.MCPProxyRequest(
		ctx,
		server,
		bytes.NewReader(body),
		c.GetHeader("mcp-session-id"),
		c.GetHeader("Content-Type"),
		c.GetHeader("Accept"),
	)
	if err != nil {
		// Upstream unreachable — 502 keeps the add-in's failed-server path
		// (it shows the server as failed and offers retry).
		common.SysLog("[excel-mcp] proxy " + server.Id + " (" + method + ") upstream error: " + err.Error())
		mcpProxyError(c, http.StatusBadGateway, "upstream_error", "MCP upstream request failed")
		return
	}
	defer resp.Body.Close()

	// Pass through the upstream session + content type; stream the body.
	if sid := resp.Header.Get("mcp-session-id"); sid != "" {
		c.Header("mcp-session-id", sid)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		c.Header("Content-Type", ct)
	}
	c.Status(resp.StatusCode)
	io.Copy(c.Writer, io.LimitReader(resp.Body, exceltools.MCPBodyBudget()))
}

// mcpProxyError writes an anthropic-style error envelope.
func mcpProxyError(c *gin.Context, status int, errType, message string) {
	c.JSON(status, gin.H{
		"type":  "error",
		"error": gin.H{"type": errType, "message": message},
	})
}

// ExcelMCPRegistryStub serves GET /mcp-registry/v0/servers — the add-in only
// uses this to backfill icon_url; an empty list skips silently.
func ExcelMCPRegistryStub(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"servers": []interface{}{}})
}
