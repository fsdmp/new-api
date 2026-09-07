# excel-sandbox — code_execution 沙箱部署物

宿主机**不装 Docker**（设计文档 §5.6.3）。沙箱由三样纯文件组成，单次部署四件套一起替换：
网关二进制 + `nsjail` 静态二进制 + `excel-sandbox-rootfs.tar.xz` + 配置。

```
<网关数据目录>/
├── new-api              # 网关二进制
└── sandbox/
    ├── nsjail           # nsjail 静态二进制（单文件，无守护进程）
    └── rootfs/          # 本目录 build-rootfs.sh 产出的目录树
```

## 构建 rootfs（在有 Docker 的机器上，一次性）

```bash
deploy/excel-sandbox/build-rootfs.sh          # 产出 excel-sandbox-rootfs.tar.xz
```

Dockerfile 基于 `python:3.11-slim`，预装 pandas / numpy / openpyxl /
pdfplumber / python-docx / lxml —— 库清单与 `code_execution` 工具 description
**严格一致**（`setting/excel_tools/definitions.go`），改一处必须同步另一处。

## 宿主机部署步骤

1. **获取 nsjail 静态二进制**（任选其一）：
   - 从发行版附件直接下载（推荐）；
   - 或在任何构建机上：
     ```bash
     git clone https://github.com/google/nsjail && cd nsjail
     make statically_linked   # 产物为 nsjail 单文件，scp 到宿主机 sandbox/nsjail
     ```
2. **解压 rootfs**（root 权限，tar 内含设备节点）：
   ```bash
   mkdir -p /path/to/data/sandbox/rootfs
   tar -xJf excel-sandbox-rootfs.tar.xz -C /path/to/data/sandbox/rootfs
   chmod +x /path/to/data/sandbox/nsjail
   ```
3. **内核前提**：非特权 user namespace 必须开启（nsjail 默认 clone_newuser）：
   ```bash
   sysctl kernel.unprivileged_userns_clone   # Debian/Ubuntu 系；应输出 =1
   # 若为 0 且宿主机是 Debian 系：echo 1 > /proc/sys/kernel/unprivileged_userns_clone
   # （RHEL/CentOS 系无此 sysctl，默认由 user_max_user_namespaces 控制，通常已开启）
   ```
4. **手工验证**（应输出 `2`）：
   ```bash
   cd /path/to/data
   echo 'print(1+1)' | ./sandbox/nsjail -Mo --chroot sandbox/rootfs \
     --user 65534 --group 65534 --cwd /tmp --tmpfsmount /tmp:size=32m \
     -- /usr/local/bin/python3 -I -
   ```
5. **网关配置**（后台 运营设置 → excel_tools，或直接写 options）：
   `sandbox.enabled=true`、`sandbox.nsjail_path` / `sandbox.rootfs_path`
   （默认 `sandbox/nsjail`、`sandbox/rootfs`，相对网关工作目录）。

## 隔离参数（与网关 NsjailRunner 保持一致）

`-Mo` 一次性执行 + `--chroot rootfs`（根只读）+ `--tmpfsmount /tmp`（唯一可写区）+
`--user/--group 65534`（nobody）+ 空网络命名空间（禁出网）+ `--time_limit` /
`--rlimit_cpu`（30s 强杀）+ `--rlimit_as 1024` + `--rlimit_nproc 64` +
`--rlimit_fsize 32`。seccomp 用 nsjail 默认策略起步，二期收紧。

## 红线（R4）

容器禁出网、根文件系统只读、一次性执行 —— 任何整改不得放松这三条。
