# excel-sandbox rootfs
#
# NOT deployed to the production host. This image exists only to BUILD the
# sandbox rootfs once on any machine that has Docker; the exported directory
# tree is shipped as excel-sandbox-rootfs.tar.xz and extracted on the gateway
# host next to the nsjail binary (design doc §5.6.3 — 宿主机不装 Docker).
#
# The preinstalled package list MUST stay in sync with the code_execution
# tool description in setting/excel_tools/definitions.go:
# pandas / numpy / openpyxl / pdfplumber / python-docx / lxml
FROM python:3.11-slim

RUN pip install --no-cache-dir \
        pandas \
        numpy \
        openpyxl \
        pdfplumber \
        python-docx \
        lxml \
    # nsjail runs /usr/local/bin/python3 (python:3.11-slim layout); add the
    # conventional /usr/bin alias for interactive debugging on the host.
    && ln -sf /usr/local/bin/python3 /usr/bin/python3 \
    && mkdir -p /tmp
