# Makefile for new-api

# ============================================================
# 变量定义
# ============================================================
APP_NAME        := fsdm-new-api
VERSION         := $(shell cat VERSION 2>/dev/null | tr -d '[:space:]')
ifeq ($(VERSION),)
VERSION         := v0.0.0
endif
BUILD_TIME      := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
GIT_COMMIT      := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
OUTPUT_DIR      := build
MAIN_PATH       := ./main.go
MODULE_PATH     := github.com/QuantumNous/new-api

# 编译标志
LDFLAGS := -s -w -X '$(MODULE_PATH)/common.Version=$(VERSION)' -X '$(MODULE_PATH)/common.BuildTime=$(BUILD_TIME)' -X '$(MODULE_PATH)/common.GitCommit=$(GIT_COMMIT)'

# Go 编译环境
GO          ?= go
CGO_ENABLED ?= 0

# Docker
DOCKER         ?= docker
DOCKER_IMAGE   := $(APP_NAME)
DOCKER_REGISTRY ?=

# 前端目录
FRONTEND_DIR         := ./web/default
FRONTEND_CLASSIC_DIR := ./web/classic

# 开发环境 Docker Compose
DEV_COMPOSE_FILE     := docker-compose.dev.yml
DEV_POSTGRES_SERVICE := postgres
DEV_BACKEND_SERVICE  := new-api
DEV_POSTGRES_DB      := new-api
DEV_POSTGRES_USER    := root
DEV_SQLITE_PATH      ?= one-api.db

# 颜色输出
GREEN  := \033[0;32m
YELLOW := \033[0;33m
NC     := \033[0m

# ============================================================
# 默认目标
# ============================================================
.PHONY: all
all: build-all-frontends build

# ============================================================
# 前端构建
# ============================================================

.PHONY: build-frontend
build-frontend:
	@echo "$(YELLOW)构建默认前端 (React 19)...$(NC)"
	@cd $(FRONTEND_DIR) && bun install && DISABLE_ESLINT_PLUGIN='true' bun run build
	@echo "$(GREEN)✓ 默认前端构建完成$(NC)"

.PHONY: build-frontend-classic
build-frontend-classic:
	@echo "$(YELLOW)构建经典前端 (React 18)...$(NC)"
	@cd $(FRONTEND_CLASSIC_DIR) && bun install && bun run build
	@echo "$(GREEN)✓ 经典前端构建完成$(NC)"

.PHONY: build-all-frontends
build-all-frontends: build-frontend build-frontend-classic

# ============================================================
# Go 后端构建
# ============================================================

# 本地编译（仅后端）
.PHONY: build
build:
	@mkdir -p $(OUTPUT_DIR)
	@echo "$(YELLOW)编译 $(APP_NAME) ($(VERSION), $(GIT_COMMIT))...$(NC)"
	@CGO_ENABLED=$(CGO_ENABLED) $(GO) build -ldflags "$(LDFLAGS)" -o $(OUTPUT_DIR)/$(APP_NAME) $(MAIN_PATH)
	@echo "$(GREEN)✓ $(OUTPUT_DIR)/$(APP_NAME)$(NC)"

# 完整构建（前端 + 后端）
.PHONY: build-full
build-full: build-all-frontends build

# ============================================================
# 跨平台编译
# ============================================================

.PHONY: build-all
build-all-platforms: build-linux-amd64 build-linux-arm64 build-darwin-amd64 build-darwin-arm64 build-windows-amd64

.PHONY: build-linux-amd64
build-linux-amd64:
	@mkdir -p $(OUTPUT_DIR)/linux_amd64
	@echo "$(YELLOW)编译 Linux AMD64...$(NC)"
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -ldflags "$(LDFLAGS)" -o $(OUTPUT_DIR)/linux_amd64/$(APP_NAME) $(MAIN_PATH)
	@echo "$(GREEN)✓ $(OUTPUT_DIR)/linux_amd64/$(APP_NAME)$(NC)"

.PHONY: build-linux-arm64
build-linux-arm64:
	@mkdir -p $(OUTPUT_DIR)/linux_arm64
	@echo "$(YELLOW)编译 Linux ARM64...$(NC)"
	@CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -ldflags "$(LDFLAGS)" -o $(OUTPUT_DIR)/linux_arm64/$(APP_NAME) $(MAIN_PATH)
	@echo "$(GREEN)✓ $(OUTPUT_DIR)/linux_arm64/$(APP_NAME)$(NC)"

.PHONY: build-darwin-amd64
build-darwin-amd64:
	@mkdir -p $(OUTPUT_DIR)/darwin_amd64
	@echo "$(YELLOW)编译 macOS AMD64...$(NC)"
	@CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GO) build -ldflags "$(LDFLAGS)" -o $(OUTPUT_DIR)/darwin_amd64/$(APP_NAME) $(MAIN_PATH)
	@echo "$(GREEN)✓ $(OUTPUT_DIR)/darwin_amd64/$(APP_NAME)$(NC)"

.PHONY: build-darwin-arm64
build-darwin-arm64:
	@mkdir -p $(OUTPUT_DIR)/darwin_arm64
	@echo "$(YELLOW)编译 macOS ARM64 (Apple Silicon)...$(NC)"
	@CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GO) build -ldflags "$(LDFLAGS)" -o $(OUTPUT_DIR)/darwin_arm64/$(APP_NAME) $(MAIN_PATH)
	@echo "$(GREEN)✓ $(OUTPUT_DIR)/darwin_arm64/$(APP_NAME)$(NC)"

.PHONY: build-windows-amd64
build-windows-amd64:
	@mkdir -p $(OUTPUT_DIR)/windows_amd64
	@echo "$(YELLOW)编译 Windows AMD64...$(NC)"
	@CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -ldflags "$(LDFLAGS)" -o $(OUTPUT_DIR)/windows_amd64/$(APP_NAME).exe $(MAIN_PATH)
	@echo "$(GREEN)✓ $(OUTPUT_DIR)/windows_amd64/$(APP_NAME).exe$(NC)"

# ============================================================
# 打包发布
# ============================================================

# 打包所有平台（前端 + 后端 + 压缩）
.PHONY: package-all
package-all: build-all-frontends build-all-platforms package-compress

# 压缩构建产物为 tar.gz / zip
.PHONY: package-compress
package-compress:
	@echo "$(YELLOW)打包压缩构建产物...$(NC)"
	@cd $(OUTPUT_DIR) && \
		if [ -d linux_amd64 ]; then tar -czf $(APP_NAME)-$(VERSION)-linux-amd64.tar.gz -C linux_amd64 $(APP_NAME); fi && \
		if [ -d linux_arm64 ]; then tar -czf $(APP_NAME)-$(VERSION)-linux-arm64.tar.gz -C linux_arm64 $(APP_NAME); fi && \
		if [ -d darwin_amd64 ]; then tar -czf $(APP_NAME)-$(VERSION)-darwin-amd64.tar.gz -C darwin_amd64 $(APP_NAME); fi && \
		if [ -d darwin_arm64 ]; then tar -czf $(APP_NAME)-$(VERSION)-darwin-arm64.tar.gz -C darwin_arm64 $(APP_NAME); fi && \
		if [ -d windows_amd64 ]; then cd windows_amd64 && zip -q ../$(APP_NAME)-$(VERSION)-windows-amd64.zip $(APP_NAME).exe && cd ..; fi
	@echo "$(GREEN)✓ 压缩包已生成到 $(OUTPUT_DIR)/$(NC)"

# ============================================================
# Docker 构建
# ============================================================

.PHONY: docker-build
docker-build:
	@echo "$(YELLOW)Docker 构建...$(NC)"
	@$(DOCKER) build -t $(DOCKER_REGISTRY)$(DOCKER_IMAGE):$(VERSION) -t $(DOCKER_REGISTRY)$(DOCKER_IMAGE):latest .
	@echo "$(GREEN)✓ $(DOCKER_REGISTRY)$(DOCKER_IMAGE):$(VERSION)$(NC)"

.PHONY: docker-build-no-cache
docker-build-no-cache:
	@echo "$(YELLOW)Docker 构建（无缓存）...$(NC)"
	@$(DOCKER) build --no-cache -t $(DOCKER_REGISTRY)$(DOCKER_IMAGE):$(VERSION) -t $(DOCKER_REGISTRY)$(DOCKER_IMAGE):latest .
	@echo "$(GREEN)✓ $(DOCKER_REGISTRY)$(DOCKER_IMAGE):$(VERSION)$(NC)"

.PHONY: docker-buildx
docker-buildx:
	@echo "$(YELLOW)Docker 多平台构建 (linux/amd64, linux/arm64)...$(NC)"
	@$(DOCKER) buildx build --platform linux/amd64,linux/arm64 \
		-t $(DOCKER_REGISTRY)$(DOCKER_IMAGE):$(VERSION) \
		-t $(DOCKER_REGISTRY)$(DOCKER_IMAGE):latest \
		--push .
	@echo "$(GREEN)✓ 多平台镜像已推送$(NC)"

.PHONY: docker-push
docker-push:
	@echo "$(YELLOW)推送 Docker 镜像...$(NC)"
	@$(DOCKER) push $(DOCKER_REGISTRY)$(DOCKER_IMAGE):$(VERSION)
	@$(DOCKER) push $(DOCKER_REGISTRY)$(DOCKER_IMAGE):latest
	@echo "$(GREEN)✓ 镜像已推送$(NC)"

# ============================================================
# 开发环境
# ============================================================

.PHONY: start-backend
start-backend:
	@echo "$(YELLOW)启动后端 (go run)...$(NC)"
	@cd $(BACKEND_DIR) && $(GO) run main.go &

.PHONY: dev-api
dev-api:
	@echo "$(YELLOW)启动后端服务 (docker compose)...$(NC)"
	@docker compose -f $(DEV_COMPOSE_FILE) up -d

.PHONY: dev-api-rebuild
dev-api-rebuild:
	@echo "$(YELLOW)重新构建并启动后端服务 (docker compose)...$(NC)"
	@docker compose -f $(DEV_COMPOSE_FILE) up -d --build $(DEV_BACKEND_SERVICE)

.PHONY: dev-web
dev-web:
	@echo "$(YELLOW)启动默认前端开发服务器...$(NC)"
	@cd $(FRONTEND_DIR) && bun install && bun run dev

.PHONY: dev-web-classic
dev-web-classic:
	@echo "$(YELLOW)启动经典前端开发服务器...$(NC)"
	@cd $(FRONTEND_CLASSIC_DIR) && bun install && bun run dev

.PHONY: dev
dev: dev-api dev-web

.PHONY: reset-setup
reset-setup:
	@echo "$(YELLOW)重置 setup wizard 状态...$(NC)"
	@if docker compose -f $(DEV_COMPOSE_FILE) ps --services --status running | grep -qx "$(DEV_POSTGRES_SERVICE)"; then \
		echo "检测到运行中的 Docker PostgreSQL，清理 setup 数据..."; \
		docker compose -f $(DEV_COMPOSE_FILE) exec -T $(DEV_POSTGRES_SERVICE) \
			psql -U $(DEV_POSTGRES_USER) -d $(DEV_POSTGRES_DB) \
			-c 'DELETE FROM setups;' \
			-c 'DELETE FROM users WHERE role = 100;' \
			-c "DELETE FROM options WHERE key IN ('SelfUseModeEnabled', 'DemoSiteEnabled');"; \
		echo "重启后端服务..."; \
		docker compose -f $(DEV_COMPOSE_FILE) restart $(DEV_BACKEND_SERVICE); \
	elif db_path="$${SQLITE_PATH:-$(DEV_SQLITE_PATH)}"; db_path="$${db_path%%\?*}"; [ -f "$$db_path" ]; then \
		db_path="$${SQLITE_PATH:-$(DEV_SQLITE_PATH)}"; \
		db_path="$${db_path%%\?*}"; \
		echo "检测到本地 SQLite: $$db_path"; \
		sqlite3 "$$db_path" \
			"DELETE FROM setups; DELETE FROM users WHERE role = 100; DELETE FROM options WHERE key IN ('SelfUseModeEnabled', 'DemoSiteEnabled');"; \
		echo "SQLite 状态已重置，请重启后端进程。"; \
	else \
		echo "未找到运行中的 Docker PostgreSQL 或本地 SQLite 数据库。"; \
		echo "使用 'make dev-api' 启动开发环境，或设置 SQLITE_PATH/DEV_SQLITE_PATH。"; \
		exit 1; \
	fi

# ============================================================
# 运行与测试
# ============================================================

.PHONY: run
run: build
	@./$(OUTPUT_DIR)/$(APP_NAME)

.PHONY: test
test:
	@echo "$(YELLOW)运行测试...$(NC)"
	@$(GO) test -v ./...

.PHONY: test-cover
test-cover:
	@echo "$(YELLOW)运行测试（覆盖率）...$(NC)"
	@$(GO) test -v -coverprofile=coverage.out ./...
	@$(GO) tool cover -func=coverage.out | tail -1
	@echo "$(GREEN)详细报告: go tool cover -html=coverage.out$(NC)"

.PHONY: lint
lint:
	@echo "$(YELLOW)代码检查...$(NC)"
	@$(GO) fmt ./...
	@$(GO) vet ./...

.PHONY: fmt
fmt:
	@echo "$(YELLOW)格式化代码...$(NC)"
	@$(GO) fmt ./...

# ============================================================
# 依赖管理
# ============================================================

.PHONY: deps
deps:
	@echo "$(YELLOW)下载 Go 依赖...$(NC)"
	@$(GO) mod download
	@$(GO) mod tidy

.PHONY: deps-frontend
deps-frontend:
	@echo "$(YELLOW)安装前端依赖...$(NC)"
	@cd $(FRONTEND_DIR) && bun install
	@cd $(FRONTEND_CLASSIC_DIR) && bun install

.PHONY: deps-all
deps-all: deps deps-frontend

# ============================================================
# 清理
# ============================================================

.PHONY: clean
clean:
	@echo "$(YELLOW)清理构建产物...$(NC)"
	@rm -rf $(OUTPUT_DIR)
	@rm -f coverage.out
	@echo "$(GREEN)✓ 已清理$(NC)"

.PHONY: clean-frontend
clean-frontend:
	@echo "$(YELLOW)清理前端构建产物...$(NC)"
	@rm -rf $(FRONTEND_DIR)/dist
	@rm -rf $(FRONTEND_CLASSIC_DIR)/dist
	@echo "$(GREEN)✓ 前端产物已清理$(NC)"

.PHONY: clean-all
clean-all: clean clean-frontend
	@echo "$(YELLOW)清理 node_modules...$(NC)"
	@rm -rf $(FRONTEND_DIR)/node_modules
	@rm -rf $(FRONTEND_CLASSIC_DIR)/node_modules
	@echo "$(GREEN)✓ 全部已清理$(NC)"

# ============================================================
# 版本信息
# ============================================================

.PHONY: version
version:
	@echo "版本:     $(VERSION)"
	@echo "Git:      $(GIT_COMMIT)"
	@echo "构建时间: $(BUILD_TIME)"
	@echo "模块:     $(MODULE_PATH)"

# ============================================================
# 帮助
# ============================================================

.PHONY: help
help:
	@echo ""
	@echo "$(APP_NAME) 构建工具"
	@echo ""
	@echo "$(YELLOW)构建命令:$(NC)"
	@echo "  all                  - 构建前端 + 后端（默认）"
	@echo "  build                - 仅编译后端（本地平台）"
	@echo "  build-full           - 前端 + 后端完整构建"
	@echo "  build-frontend       - 仅构建默认前端 (React 19)"
	@echo "  build-frontend-classic - 仅构建经典前端 (React 18)"
	@echo "  build-all-frontends  - 构建所有前端"
	@echo ""
	@echo "$(YELLOW)跨平台编译:$(NC)"
	@echo "  build-all-platforms  - 编译所有平台"
	@echo "  build-linux-amd64    - 编译 Linux AMD64"
	@echo "  build-linux-arm64    - 编译 Linux ARM64"
	@echo "  build-darwin-amd64   - 编译 macOS AMD64"
	@echo "  build-darwin-arm64   - 编译 macOS ARM64 (Apple Silicon)"
	@echo "  build-windows-amd64  - 编译 Windows AMD64"
	@echo ""
	@echo "$(YELLOW)打包发布:$(NC)"
	@echo "  package-all          - 构建所有平台并打包压缩"
	@echo "  package-compress     - 压缩已有构建产物为 tar.gz/zip"
	@echo ""
	@echo "$(YELLOW)Docker:$(NC)"
	@echo "  docker-build         - Docker 构建"
	@echo "  docker-build-no-cache - Docker 构建（无缓存）"
	@echo "  docker-buildx        - Docker 多平台构建并推送"
	@echo "  docker-push          - 推送 Docker 镜像"
	@echo ""
	@echo "$(YELLOW)开发环境:$(NC)"
	@echo "  dev                  - 启动开发环境 (docker + 前端 dev server)"
	@echo "  dev-api              - 启动后端服务 (docker compose)"
	@echo "  dev-api-rebuild      - 重新构建并启动后端服务"
	@echo "  dev-web              - 启动默认前端 dev server"
	@echo "  dev-web-classic      - 启动经典前端 dev server"
	@echo "  start-backend        - 本地 go run 启动后端"
	@echo "  run                  - 编译并运行"
	@echo "  reset-setup          - 重置 setup wizard 状态"
	@echo ""
	@echo "$(YELLOW)测试与检查:$(NC)"
	@echo "  test                 - 运行测试"
	@echo "  test-cover           - 运行测试（带覆盖率）"
	@echo "  lint                 - 代码检查（fmt + vet）"
	@echo "  fmt                  - 格式化代码"
	@echo ""
	@echo "$(YELLOW)依赖管理:$(NC)"
	@echo "  deps                 - 安装 Go 依赖"
	@echo "  deps-frontend        - 安装前端依赖"
	@echo "  deps-all             - 安装所有依赖"
	@echo ""
	@echo "$(YELLOW)清理:$(NC)"
	@echo "  clean                - 清理构建产物"
	@echo "  clean-frontend       - 清理前端构建产物"
	@echo "  clean-all            - 清理全部（含 node_modules）"
	@echo ""
	@echo "$(YELLOW)其他:$(NC)"
	@echo "  version              - 显示版本信息"
	@echo "  help                 - 显示此帮助"
	@echo ""
	@echo "$(YELLOW)变量:$(NC)"
	@echo "  VERSION=$(VERSION)  CGO_ENABLED=$(CGO_ENABLED)  OUTPUT_DIR=$(OUTPUT_DIR)"
	@echo "  DOCKER_REGISTRY=$(DOCKER_REGISTRY)  DOCKER_IMAGE=$(DOCKER_IMAGE)"
	@echo ""
