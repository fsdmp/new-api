# Makefile for new-api

# ============================================================
# 变量定义
# ============================================================
APP_NAME        := fsdm-new-api
VERSION         := $(shell cat VERSION 2>/dev/null | tr -d '[:space:]')
ifeq ($(VERSION),)
VERSION         := v$(shell date +%Y%m%d-%H%M)
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
RED    := \033[0;31m
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
# 线上发布（脚本托管 nohup：构建 → 上传 → 原子替换 → 重启 → 健康检查）
# ============================================================
# 服务器配置放在 deploy/release.env（已 gitignore，不会误提交）。
# 首次使用先运行: make setup-release 生成模板并填写。
-include deploy/release.env

SERVER        ?=
REMOTE_DIR    ?=
# 线上二进制名（与本地构建名解耦：现网为 fsdm-new-api3000，带端口后缀）
REMOTE_BIN    ?= $(APP_NAME)
# 启动参数（原样传给二进制，如 --log-dir）
REMOTE_START_ARGS ?=
HEALTH_PORT   ?= 3000
HEALTH_PATH   ?= /api/status
RELEASE_ARCH  ?= amd64
SSH           := ssh
SCP           := scp
SHA256        := $(shell command -v sha256sum >/dev/null 2>&1 && echo sha256sum || echo shasum -a 256)

RELEASE_BIN     := $(OUTPUT_DIR)/linux_$(RELEASE_ARCH)/$(APP_NAME)
RESTART_SCRIPT  := deploy/release-restart.sh
SANDBOX_NSJAIL  ?= deploy/excel-sandbox/nsjail
SANDBOX_TARXZ   ?= deploy/excel-sandbox/excel-sandbox-rootfs.tar.xz

# 发布前置检查：SERVER/REMOTE_DIR 未配置时给出指引并终止
define check-release-env
	@if [ -z "$(SERVER)" ] || [ -z "$(REMOTE_DIR)" ]; then \
		echo "$(RED)✗ 缺少 SERVER / REMOTE_DIR 配置$(NC)"; \
		echo "  运行 make setup-release 生成 deploy/release.env 并填写服务器信息"; \
		exit 1; \
	fi
endef

# 配置守卫目标：作为首个前置依赖，确保在编译前端之前就快速失败
.PHONY: release-env
release-env:
	$(check-release-env)

# 发布链内部依赖顺序必须串行（release-env 先于编译、上传先于重启）
.NOTPARALLEL:

.PHONY: setup-release
setup-release:
	@if [ -f deploy/release.env ]; then \
		echo "$(YELLOW)deploy/release.env 已存在，跳过$(NC)"; \
	else \
		cp deploy/release.env.example deploy/release.env; \
		echo "$(GREEN)✓ 已生成 deploy/release.env，请编辑填写 SERVER / REMOTE_DIR 后重新执行$(NC)"; \
	fi

# 发布构建：两套前端 + Linux 目标架构二进制（前端已 embed，单文件自包含）
# 注：go:embed 同时依赖 web/default/dist 与 web/classic/dist，两套都必须构建，
#     即使线上只用经典前端（classic）也不能跳过默认前端。
.PHONY: release-build
release-build: release-env build-all-frontends build-linux-$(RELEASE_ARCH)
	@echo "$(GREEN)✓ 发布产物 $(RELEASE_BIN) ($(VERSION) $(GIT_COMMIT))$(NC)"

# 上传（不重启）：scp 到 .new → sha256 校验 → 原子替换，旧版本保留为 .prev
# 原子 mv 避免运行中二进制被覆写（Linux Text file busy），并天然支持回滚
.PHONY: upload
upload: release-env release-build
	@echo "$(YELLOW)上传 → $(SERVER):$(REMOTE_DIR)/$(REMOTE_BIN) ...$(NC)"
	@SUM=$$($(SHA256) $(RELEASE_BIN) | awk '{print $$1}'); \
	$(SCP) $(RELEASE_BIN) $(SERVER):$(REMOTE_DIR)/$(REMOTE_BIN).new && \
	$(SSH) $(SERVER) "cd $(REMOTE_DIR) && echo '$$SUM  $(REMOTE_BIN).new' | sha256sum -c -" && \
	$(SSH) $(SERVER) "cd $(REMOTE_DIR) && \
		if [ -f $(REMOTE_BIN) ]; then mv -f $(REMOTE_BIN) $(REMOTE_BIN).prev; fi && \
		mv $(REMOTE_BIN).new $(REMOTE_BIN) && chmod +x $(REMOTE_BIN) && \
		(ls -lh $(REMOTE_BIN)* 2>/dev/null || true)"
	@echo "$(GREEN)✓ 上传完成（未重启；make release 会继续重启 + 健康检查）$(NC)"

# 重启服务：上传 deploy/release-restart.sh（优雅停止 + nohup 拉起）并执行
.PHONY: restart-service
restart-service:
	$(check-release-env)
	@echo "$(YELLOW)重启 $(REMOTE_BIN) ...$(NC)"
	@$(SCP) -q $(RESTART_SCRIPT) $(SERVER):$(REMOTE_DIR)/release-restart.sh
	@$(SSH) $(SERVER) "sh $(REMOTE_DIR)/release-restart.sh $(REMOTE_BIN) $(REMOTE_DIR) $(REMOTE_START_ARGS)"

# 进程存活检查（失败时打印 output.log）
.PHONY: check-process
check-process:
	$(check-release-env)
	@if ! $(SSH) $(SERVER) "pgrep -f '[.]/$(REMOTE_BIN)' >/dev/null"; then \
		echo "$(RED)✗ $(REMOTE_BIN) 进程未运行，最近日志：$(NC)"; \
		$(SSH) $(SERVER) "tail -n 30 $(REMOTE_DIR)/output.log 2>/dev/null"; \
		exit 1; \
	fi

# 健康检查：进程存活 + 轮询健康端点
.PHONY: verify-release
verify-release: check-process
	@echo "$(YELLOW)健康检查 http://127.0.0.1:$(HEALTH_PORT)$(HEALTH_PATH) ...$(NC)"
	@for i in $$(seq 1 15); do \
		if $(SSH) $(SERVER) "curl -fsS --max-time 3 http://127.0.0.1:$(HEALTH_PORT)$(HEALTH_PATH)" >/dev/null 2>&1; then \
			echo "$(GREEN)✓ 服务健康$(NC)"; \
			exit 0; \
		fi; \
		sleep 1; \
	done; \
	echo "$(RED)✗ 健康检查超时，最近日志：$(NC)"; \
	$(SSH) $(SERVER) "tail -n 30 $(REMOTE_DIR)/output.log 2>/dev/null"; \
	exit 1

# 一键发布：上传 → 重启 → 健康检查（任一步失败即终止）
.PHONY: release
release: upload
	@$(MAKE) restart-service
	@$(MAKE) verify-release
	@echo "$(GREEN)✓ 发布完成: $(VERSION) ($(GIT_COMMIT)) → $(SERVER):$(REMOTE_DIR)$(NC)"

# 回滚：.prev 换回并重启；问题现场保留为 .bad
.PHONY: rollback
rollback:
	$(check-release-env)
	@echo "$(YELLOW)回滚到上一版本 ...$(NC)"
	@$(SSH) $(SERVER) "cd $(REMOTE_DIR) && test -f $(REMOTE_BIN).prev || { echo 'no $(REMOTE_BIN).prev'; exit 1; } && \
		mv -f $(REMOTE_BIN) $(REMOTE_BIN).bad && mv $(REMOTE_BIN).prev $(REMOTE_BIN) && chmod +x $(REMOTE_BIN)"
	@$(MAKE) restart-service
	@$(MAKE) verify-release
	@echo "$(GREEN)✓ 已回滚（问题现场保留在 $(REMOTE_DIR)/$(REMOTE_BIN).bad）$(NC)"

# 查看线上状态：进程 + 二进制清单 + 日志尾 + 健康端点响应片段
.PHONY: release-status
release-status:
	$(check-release-env)
	@$(SSH) $(SERVER) "pgrep -af '[.]/$(REMOTE_BIN)' || echo '(进程未运行)'"
	@echo ""
	@$(SSH) $(SERVER) "ls -lh $(REMOTE_DIR)/$(REMOTE_BIN)* 2>/dev/null; echo; tail -n 5 $(REMOTE_DIR)/output.log 2>/dev/null"
	@echo ""
	@$(SSH) $(SERVER) "curl -fsS --max-time 3 http://127.0.0.1:$(HEALTH_PORT)$(HEALTH_PATH) | head -c 300; echo"

# 上传 excel 沙箱组件（nsjail + rootfs tarball）并远端验证（期望输出 2）
# 前置：nsjail 静态二进制放 deploy/excel-sandbox/nsjail（构建方式见同目录 README）；
#       rootfs tarball 在任一 Docker 机器运行 deploy/excel-sandbox/build-rootfs.sh 产出
.PHONY: upload-sandbox
upload-sandbox:
	$(check-release-env)
	@if [ ! -f $(SANDBOX_NSJAIL) ]; then \
		echo "$(RED)✗ 未找到 $(SANDBOX_NSJAIL)（nsjail 静态二进制，构建方式见 deploy/excel-sandbox/README.md）$(NC)"; \
		exit 1; \
	fi
	@if [ ! -f $(SANDBOX_TARXZ) ]; then \
		echo "$(RED)✗ 未找到 $(SANDBOX_TARXZ)（在任一 Docker 机器运行 deploy/excel-sandbox/build-rootfs.sh 产出）$(NC)"; \
		exit 1; \
	fi
	@echo "$(YELLOW)上传沙箱组件 → $(SERVER):$(REMOTE_DIR)/sandbox/ ...$(NC)"
	@$(SSH) $(SERVER) "mkdir -p $(REMOTE_DIR)/sandbox/rootfs"
	@$(SCP) $(SANDBOX_NSJAIL) $(SERVER):$(REMOTE_DIR)/sandbox/nsjail
	@$(SCP) $(SANDBOX_TARXZ) $(SERVER):$(REMOTE_DIR)/sandbox/
	@$(SSH) $(SERVER) "tar -xJf $(REMOTE_DIR)/sandbox/excel-sandbox-rootfs.tar.xz -C $(REMOTE_DIR)/sandbox/rootfs && \
		chmod +x $(REMOTE_DIR)/sandbox/nsjail && \
		ls -l $(REMOTE_DIR)/sandbox/rootfs/usr/local/bin/python3"
	@echo "$(YELLOW)远端验证沙箱（期望输出 2）...$(NC)"
	@echo 'print(1+1)' | $(SSH) $(SERVER) "cd $(REMOTE_DIR) && ./sandbox/nsjail -Mo --chroot sandbox/rootfs \
		--user 65534 --group 65534 --cwd /tmp --tmpfsmount /tmp:size=32m \
		-- /usr/local/bin/python3 -I -"
	@echo "$(GREEN)✓ 沙箱组件就绪；网关后台开启 excel_tools.sandbox.enabled 后生效$(NC)"

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
	@echo "$(YELLOW)线上发布 (脚本托管):$(NC)"
	@echo "  setup-release        - 生成 deploy/release.env 配置模板（首次使用）"
	@echo "  release-build        - 构建发布产物（前端 + Linux 二进制）"
	@echo "  upload               - 上传并原子替换（保留 .prev，不重启）"
	@echo "  release              - 一键发布：上传 + 重启 + 健康检查"
	@echo "  rollback             - 回滚到上一版本（.prev）"
	@echo "  release-status       - 查看线上服务/二进制/健康状态"
	@echo "  upload-sandbox       - 上传 excel 沙箱（nsjail + rootfs）并远端验证"
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
	@echo "  发布: RELEASE_ARCH=$(RELEASE_ARCH) REMOTE_BIN=$(REMOTE_BIN) SERVER=$(SERVER) REMOTE_DIR=$(REMOTE_DIR)"
	@echo ""
