#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
new-api 运营数据统计报告生成器
================================
连接 MySQL（只读 SELECT），统计用户增长/留存/用量/渠道/Token，
渲染为自包含 HTML 报告（内嵌 SVG 图表，无外部依赖，可离线打开）。
不包含任何金额、余额类财务数据。

用法:
    python3 scripts/db_report.py                     # 使用 ~/.newapi_db_report.conf
    python3 scripts/db_report.py -c my.conf -o out.html
    NEWAPI_DB_HOST=... NEWAPI_DB_PORT=3306 NEWAPI_DB_USER=readonly \
      NEWAPI_DB_PASSWORD=... NEWAPI_DB_NAME=new-api python3 scripts/db_report.py

配置文件格式 (INI):
    [db]
    host = rm-xxx.mysql.rds.aliyuncs.com
    port = 3306
    user = readonly
    password = xxx
    name = new-api
"""
import argparse
import configparser
import datetime
import html
import os
import subprocess
import sys

# ----------------------------------------------------------------------------
# 配置与查询
# ----------------------------------------------------------------------------

def load_config(path):
    cfg = {"host": "", "port": "3306", "user": "", "password": "", "name": "new-api"}
    env_map = {
        "host": "NEWAPI_DB_HOST", "port": "NEWAPI_DB_PORT", "user": "NEWAPI_DB_USER",
        "password": "NEWAPI_DB_PASSWORD", "name": "NEWAPI_DB_NAME",
    }
    for k, e in env_map.items():
        if os.environ.get(e):
            cfg[k] = os.environ[e]
    if path and os.path.exists(path):
        cp = configparser.ConfigParser()
        cp.read(path)
        if cp.has_section("db"):
            for k in cfg:
                if cp.has_option("db", k):
                    cfg[k] = cp.get("db", k)
    missing = [k for k in ("host", "user", "password", "name") if not cfg[k]]
    if missing:
        sys.exit(f"[缺配置] {missing}。请提供 -c 配置文件或 NEWAPI_DB_* 环境变量。")
    return cfg


def query(cfg, sql):
    """运行只读查询，返回 list[dict]。NULL -> None。"""
    cmd = [
        "mysql", f"-h{cfg['host']}", f"-P{cfg['port']}", f"-u{cfg['user']}",
        f"-D{cfg['name']}", "--batch", "--raw", "--connect-timeout=10",
    ]
    env = dict(os.environ, MYSQL_PWD=cfg["password"])
    p = subprocess.run(cmd, input=sql, capture_output=True, text=True, env=env)
    if p.returncode != 0:
        sys.exit(f"[SQL 失败] {p.stderr.strip()[:500]}\n--- SQL ---\n{sql[:300]}")
    lines = [ln for ln in p.stdout.split("\n") if ln != ""]
    if not lines:
        return []
    header = lines[0].split("\t")
    rows = []
    for ln in lines[1:]:
        vals = ln.split("\t")
        row = {}
        for i, col in enumerate(header):
            v = vals[i] if i < len(vals) else ""
            row[col] = None if v == "NULL" else v
        rows.append(row)
    return rows


def num(v, default=0.0):
    if v is None:
        return default
    try:
        return float(v)
    except (TypeError, ValueError):
        return default


# ----------------------------------------------------------------------------
# 统计 SQL（全部只读）
# ----------------------------------------------------------------------------

SQLS = {
    "kpi": """
        SELECT (SELECT COUNT(*) FROM users WHERE deleted_at IS NULL) AS total_users,
               (SELECT COUNT(*) FROM (
                   SELECT u.id FROM users u
                   WHERE u.deleted_at IS NULL AND EXISTS
                     (SELECT 1 FROM logs l WHERE l.type=2 AND l.user_id=u.id)) t) AS ever_active,
               (SELECT COUNT(*) FROM logs WHERE type=2) AS total_reqs,
               (SELECT IFNULL(SUM(prompt_tokens+completion_tokens),0) FROM logs WHERE type=2) AS total_tokens,
               (SELECT COUNT(DISTINCT user_id) FROM logs
                 WHERE type=2 AND created_at >= UNIX_TIMESTAMP(DATE_FORMAT(NOW(),'%Y-%m-01 00:00:00'))) AS mau_this_month""",
    "range": """
        SELECT FROM_UNIXTIME(MIN(created_at)) AS first_log, FROM_UNIXTIME(MAX(created_at)) AS last_log FROM logs""",
    "week_users": """
        SELECT DATE_FORMAT(FROM_UNIXTIME(created_at),'%x-W%v') AS wk, COUNT(*) AS new_users
        FROM users WHERE deleted_at IS NULL GROUP BY wk ORDER BY MIN(created_at)""",
    "week_dau": """
        SELECT DATE_FORMAT(d,'%x-W%v') AS wk, COUNT(*) AS days_covered, ROUND(AVG(c),1) AS avg_dau
        FROM (SELECT DATE(FROM_UNIXTIME(created_at)) d, COUNT(DISTINCT user_id) c
              FROM logs WHERE type=2 GROUP BY d) t GROUP BY wk ORDER BY wk""",
    "week_consume": """
        SELECT DATE_FORMAT(FROM_UNIXTIME(created_at),'%x-W%v') AS wk, COUNT(*) AS reqs,
               COUNT(DISTINCT user_id) AS act_users, SUM(prompt_tokens+completion_tokens) AS tokens
        FROM logs WHERE type=2 GROUP BY wk ORDER BY MIN(created_at)""",
    "dau_recent": """
        SELECT DATE(FROM_UNIXTIME(created_at)) AS d, COUNT(DISTINCT user_id) AS dau, COUNT(*) AS reqs
        FROM logs WHERE type=2 AND created_at > UNIX_TIMESTAMP()-14*86400
        GROUP BY d ORDER BY d""",
    "month": """
        SELECT DATE_FORMAT(FROM_UNIXTIME(created_at),'%Y-%m') AS m, COUNT(DISTINCT user_id) AS mau,
               COUNT(*) AS reqs, SUM(quota) AS quota
        FROM logs WHERE type=2 GROUP BY m""",
    "retention": """
        WITH act AS (SELECT DISTINCT user_id, DATE(FROM_UNIXTIME(created_at)) d FROM logs WHERE type=2),
        reg AS (SELECT id, DATE(FROM_UNIXTIME(created_at)) rd FROM users WHERE deleted_at IS NULL)
        SELECT DATE_FORMAT(r.rd,'%x-W%v') AS reg_week, COUNT(DISTINCT r.id) AS new_users,
          COUNT(DISTINCT CASE WHEN DATEDIFF(a.d,r.rd) BETWEEN 0 AND 0 THEN r.id END) AS d0,
          COUNT(DISTINCT CASE WHEN DATEDIFF(a.d,r.rd) BETWEEN 1 AND 1 THEN r.id END) AS d1,
          COUNT(DISTINCT CASE WHEN DATEDIFF(a.d,r.rd) BETWEEN 2 AND 7 THEN r.id END) AS d2_7,
          COUNT(DISTINCT CASE WHEN DATEDIFF(a.d,r.rd) BETWEEN 8 AND 30 THEN r.id END) AS d8_30
        FROM reg r LEFT JOIN act a ON a.user_id=r.id GROUP BY reg_week ORDER BY MIN(r.rd)""",
    "models": """
        SELECT model_name, COUNT(*) AS reqs,
               SUM(prompt_tokens+completion_tokens) AS tokens, ROUND(AVG(use_time),0) AS avg_use_time,
               SUM(type=5) AS errs
        FROM logs WHERE type IN (2,5) GROUP BY model_name ORDER BY COUNT(*) DESC LIMIT 12""",
    "top_users": """
        SELECT u.id, u.username, COUNT(*) AS reqs,
               MAX(DATE(FROM_UNIXTIME(l.created_at))) AS last_active
        FROM logs l JOIN users u ON u.id=l.user_id WHERE l.type=2
        GROUP BY u.id, u.username ORDER BY COUNT(*) DESC LIMIT 10""",
    "concentration": """
        SELECT ROUND(SUM(CASE WHEN rn<=3 THEN cnt ELSE 0 END)/SUM(cnt)*100,1) AS top3_pct,
               ROUND(SUM(CASE WHEN rn<=10 THEN cnt ELSE 0 END)/SUM(cnt)*100,1) AS top10_pct
        FROM (SELECT user_id, COUNT(*) cnt, ROW_NUMBER() OVER (ORDER BY COUNT(*) DESC) rn
              FROM logs WHERE type=2 GROUP BY user_id) t""",
    "depth": """
        SELECT
          (SELECT ROUND(AVG(cnt),1) FROM (SELECT user_id, COUNT(*) cnt FROM logs WHERE type=2 GROUP BY user_id) t1) AS avg_reqs_per_user,
          (SELECT ROUND(AVG(cnt),1) FROM (SELECT user_id, DATE(FROM_UNIXTIME(created_at)) d, COUNT(*) cnt
             FROM logs WHERE type=2 GROUP BY user_id, d) t2) AS avg_reqs_per_active_day,
          (SELECT ROUND(AVG(m),1) FROM (SELECT user_id, COUNT(DISTINCT model_name) m
             FROM logs WHERE type=2 GROUP BY user_id) t3) AS avg_models,
          (SELECT COUNT(*) FROM (SELECT user_id FROM logs WHERE type=2
             GROUP BY user_id HAVING COUNT(DISTINCT model_name)=1) t4) AS single_model_users,
          (SELECT ROUND(AVG(dd),1) FROM (SELECT user_id, COUNT(DISTINCT DATE(FROM_UNIXTIME(created_at))) dd
             FROM logs WHERE type=2 GROUP BY user_id) t5) AS avg_active_days,
          (SELECT ROUND(AVG(prompt_tokens+completion_tokens),0) FROM logs WHERE type=2) AS avg_tokens_per_req,
          (SELECT ROUND(100*SUM(CASE WHEN prompt_tokens>10000 THEN 1 ELSE 0 END)/COUNT(*),1)
             FROM logs WHERE type=2) AS long_ctx_pct""",
    "depth_reqs": """
        SELECT CASE WHEN cnt<=5 THEN '1-5次' WHEN cnt<=20 THEN '6-20次' WHEN cnt<=100 THEN '21-100次'
                    WHEN cnt<=500 THEN '101-500次' ELSE '>500次' END AS bucket, COUNT(*) AS users
        FROM (SELECT user_id, COUNT(*) cnt FROM logs WHERE type=2 GROUP BY user_id) t GROUP BY bucket""",
    "depth_days": """
        SELECT CASE WHEN d<=1 THEN '1天' WHEN d<=5 THEN '2-5天' WHEN d<=15 THEN '6-15天'
                    WHEN d<=30 THEN '16-30天' ELSE '>30天' END AS bucket, COUNT(*) AS users
        FROM (SELECT user_id, COUNT(DISTINCT DATE(FROM_UNIXTIME(created_at))) d FROM logs WHERE type=2 GROUP BY user_id) t
        GROUP BY bucket""",
    "sleepy": """
        SELECT COUNT(*) AS sleepy_users
        FROM users u WHERE u.deleted_at IS NULL
          AND EXISTS (SELECT 1 FROM logs l WHERE l.type=2 AND l.user_id=u.id)
          AND NOT EXISTS (SELECT 1 FROM logs l WHERE l.type=2 AND l.user_id=u.id
            AND l.created_at>UNIX_TIMESTAMP()-14*86400)""",
    "hours": """
        SELECT HOUR(FROM_UNIXTIME(created_at)) AS h, COUNT(*) AS reqs
        FROM logs WHERE type=2 GROUP BY h ORDER BY h""",
    "channels": """
        SELECT c.id, c.name, c.status, COUNT(CASE WHEN l.type=2 THEN 1 END) AS ok_reqs,
               COUNT(CASE WHEN l.type=5 THEN 1 END) AS err_reqs,
               ROUND(AVG(CASE WHEN l.type=2 THEN l.use_time END),0) AS avg_use_time
        FROM channels c LEFT JOIN logs l ON l.channel_id=c.id AND l.type IN (2,5)
        GROUP BY c.id, c.name, c.status ORDER BY COUNT(CASE WHEN l.type=2 THEN 1 END) DESC""",
    "tokens": """
        SELECT COUNT(*) AS total, SUM(status=1) AS enabled,
               SUM(CASE WHEN accessed_time>UNIX_TIMESTAMP()-7*86400 THEN 1 ELSE 0 END) AS active_7d,
               SUM(CASE WHEN accessed_time=0 OR accessed_time<UNIX_TIMESTAMP()-30*86400 THEN 1 ELSE 0 END) AS zombie
        FROM tokens WHERE deleted_at IS NULL""",
    "top_tokens": """
        SELECT name, IFNULL(FROM_UNIXTIME(NULLIF(accessed_time,0)),'从未使用') AS last_used
        FROM tokens WHERE deleted_at IS NULL ORDER BY accessed_time DESC LIMIT 10""",
    "ip_risk": """
        SELECT ip, COUNT(DISTINCT user_id) AS users, COUNT(*) AS reqs
        FROM logs WHERE type=2 AND IFNULL(ip,'')<>'' GROUP BY ip
        HAVING users>1 ORDER BY users DESC, reqs DESC LIMIT 10""",
}

# ----------------------------------------------------------------------------
# SVG 图表（遵循 dataviz 规范：2px 线宽 / ≤24px 柱 / 4px 数据端圆角 / hairline 网格）
# ----------------------------------------------------------------------------

def nice_ticks(vmax, ticks=4):
    if vmax <= 0:
        return [0, 1]
    raw = vmax / ticks
    mag = 10 ** int(f"{vmax:.6e}".split("e")[1]) if vmax >= 1 else 1
    for m in (1, 2, 2.5, 5, 10):
        step = m * mag
        if step >= raw:
            break
    top = step
    while top < vmax:
        top += step
    return [round(top * i / ticks, 4) for i in range(ticks + 1)]


def esc(s):
    return html.escape(str(s), quote=True)


def svg_line(chart_id, labels, series, h=280, w=880, fmt="{:.0f}", area=False):
    """多序列折线图。series = [(name, css_color, values)]"""
    pad_l, pad_r, pad_t, pad_b = 56, 24, 18, 40
    pw, ph = w - pad_l - pad_r, h - pad_t - pad_b
    all_v = [v for _, _, vals in series for v in vals]
    vmax = max(all_v) if all_v else 1
    ticks = nice_ticks(vmax)
    ymax = ticks[-1]
    n = len(labels)
    xi = lambda i: pad_l + (pw * i / (n - 1) if n > 1 else pw / 2)
    yi = lambda v: pad_t + ph - ph * v / ymax
    parts = [f'<svg viewBox="0 0 {w} {h}" role="img" class="chart">']
    # gridlines + y ticks
    for t in ticks:
        y = yi(t)
        parts.append(f'<line x1="{pad_l}" y1="{y:.1f}" x2="{w-pad_r}" y2="{y:.1f}" class="grid"/>')
        parts.append(f'<text x="{pad_l-8}" y="{y+4:.1f}" class="tick" text-anchor="end">{fmt.format(t)}</text>')
    # x labels (抽稀)
    step = max(1, (n + 8) // 9)
    for i, lb in enumerate(labels):
        if i % step == 0 or i == n - 1:
            parts.append(f'<text x="{xi(i):.1f}" y="{h-12}" class="tick" text-anchor="middle">{esc(lb)}</text>')
    # baseline
    parts.append(f'<line x1="{pad_l}" y1="{yi(0):.1f}" x2="{w-pad_r}" y2="{yi(0):.1f}" class="axis"/>')
    # series
    for name, slot, vals in series:
        pts = " ".join(f"{xi(i):.1f},{yi(v):.1f}" for i, v in enumerate(vals))
        if area:
            parts.append(f'<polygon points="{pad_l},{yi(0):.1f} {pts} {xi(len(vals)-1):.1f},{yi(0):.1f}" class="f{slot}" opacity="0.10"/>')
        parts.append(f'<polyline points="{pts}" class="l{slot}"/>')
        # endpoint marker + selective direct label (只标末点)
        lx, ly = xi(len(vals) - 1), yi(vals[-1])
        tip = f"{esc(name)} · {esc(labels[-1])} · {fmt.format(vals[-1])}"
        parts.append(f'<circle cx="{lx:.1f}" cy="{ly:.1f}" r="4" class="hit f{slot} ring" data-tip="{tip}"/>')
        parts.append(f'<text x="{lx-10:.1f}" y="{ly-10:.1f}" class="dlabel" text-anchor="end">{fmt.format(vals[-1])}</text>')
        # hover hit-paths per point
        for i, v in enumerate(vals):
            t2 = f"{esc(name)} · {esc(labels[i])} · {fmt.format(v)}"
            parts.append(f'<circle cx="{xi(i):.1f}" cy="{yi(v):.1f}" r="12" fill="transparent" data-tip="{t2}" class="hit"/>')
    parts.append("</svg>")
    return "".join(parts)


def svg_hbar(chart_id, items, fmt="{:.0f}", w=880, bar_h=20):
    """横向条形图（排行）。items = [(label, value)]，单序列 slot1。"""
    row_h = max(30, bar_h + 12)
    h = row_h * len(items) + 24
    label_w = 190
    pad_r = 70
    pw = w - label_w - pad_r
    vmax = max(v for _, v in items) if items else 1
    ticks = nice_ticks(vmax)
    parts = [f'<svg viewBox="0 0 {w} {h}" role="img" class="chart">']
    for t in ticks:
        x = label_w + pw * t / ticks[-1]
        parts.append(f'<line x1="{x:.1f}" y1="6" x2="{x:.1f}" y2="{h-18}" class="grid"/>')
        parts.append(f'<text x="{x:.1f}" y="{h-4}" class="tick" text-anchor="middle">{fmt.format(t)}</text>')
    for i, (lb, v) in enumerate(items):
        y = 6 + i * row_h
        bw = max(0.0, pw * v / ticks[-1])
        r = min(4, bw)
        parts.append(f'<text x="{label_w-10}" y="{y+bar_h*0.72:.0f}" class="cat" text-anchor="end">{esc(lb if len(str(lb))<=24 else str(lb)[:23]+"…")}</text>')
        # 数据端 4px 圆角，baseline 端平方
        parts.append(f'<path d="M {label_w} {y} h {bw-r:.1f} a {r} {r} 0 0 1 {r} {r} v {bar_h-2*r} a {r} {r} 0 0 1 -{r} {r} h -{bw-r:.1f} Z" class="hit f1" data-tip="{esc(lb)} · {fmt.format(v)}"/>')
        parts.append(f'<text x="{label_w+bw+8:.1f}" y="{y+bar_h*0.72:.0f}" class="dlabel">{fmt.format(v)}</text>')
    parts.append("</svg>")
    return "".join(parts)


def svg_vbar(labels, values, fmt="{:.0f}", w=880, h=260, bar_w=20):
    """竖柱图（时段分布等）。单序列 slot1。"""
    pad_l, pad_r, pad_t, pad_b = 46, 16, 16, 34
    pw, ph = w - pad_l - pad_r, h - pad_t - pad_b
    vmax = max(values) if values else 1
    ticks = nice_ticks(vmax)
    ymax = ticks[-1]
    n = len(values)
    band = pw / max(1, n)
    parts = [f'<svg viewBox="0 0 {w} {h}" role="img" class="chart">']
    for t in ticks:
        y = pad_t + ph - ph * t / ymax
        parts.append(f'<line x1="{pad_l}" y1="{y:.1f}" x2="{w-pad_r}" y2="{y:.1f}" class="grid"/>')
        parts.append(f'<text x="{pad_l-8}" y="{y+4:.1f}" class="tick" text-anchor="end">{fmt.format(t)}</text>')
    parts.append(f'<line x1="{pad_l}" y1="{pad_t+ph:.1f}" x2="{w-pad_r}" y2="{pad_t+ph:.1f}" class="axis"/>')
    for i, v in enumerate(values):
        x = pad_l + band * i + (band - bar_w) / 2
        bh = ph * v / ymax
        r = min(4, bh)
        y = pad_t + ph - bh
        tip = f"{esc(labels[i])} · {fmt.format(v)}"
        parts.append(f'<path d="M {x:.1f} {pad_t+ph:.1f} v -{bh-r:.1f} a {r} {r} 0 0 1 {r} -{r} h {bar_w-2*r} a {r} {r} 0 0 1 {r} {r} v {bh-r:.1f} Z" class="hit f1" data-tip="{tip}"/>')
        if i % max(1, n // 12) == 0:
            parts.append(f'<text x="{x+bar_w/2:.1f}" y="{h-12}" class="tick" text-anchor="middle">{esc(labels[i])}</text>')
    parts.append("</svg>")
    return "".join(parts)

# 留存热力表：sequential blue（单一 hue light→dark，0% 递归于底色）
SEQ_RAMP = ["#eef4fd", "#cde2fb", "#b7d3f6", "#9ec5f4", "#86b6ef", "#6da7ec",
            "#5598e7", "#3987e5", "#2a78d6", "#256abf", "#1c5cab", "#184f95"]


def heat_color(pct):
    if pct is None:
        return "", "muted"
    idx = int(round(pct / 100 * (len(SEQ_RAMP) - 1)))
    c = SEQ_RAMP[max(0, min(len(SEQ_RAMP) - 1, idx))]
    # 亮底用墨字，深底用白字（按索引 6 为界）
    ink = "#ffffff" if idx >= 6 else "#0b0b0b"
    return c, ink

# ----------------------------------------------------------------------------
# HTML 组装
# ----------------------------------------------------------------------------

CSS = """
:root { color-scheme: light; }
.viz-root {
  --surface-1:#fcfcfb; --page:#f9f9f7; --ink:#0b0b0b; --ink-2:#52514e; --muted:#898781;
  --grid:#e1e0d9; --axis:#c3c2b7; --border:rgba(11,11,11,.10);
  --s1:#2a78d6; --s2:#eb6834; --s3:#1baf7a;
  --good:#0ca30c; --warn:#fab219; --crit:#d03b3b; --good-text:#006300;
  background:var(--page); color:var(--ink);
  font-family:system-ui,-apple-system,"Segoe UI","PingFang SC","Microsoft YaHei",sans-serif;
  margin:0; padding:24px clamp(16px,4vw,40px) 60px;
}
@media (prefers-color-scheme: dark) {
  :root:where(:not([data-theme="light"])) .viz-root { color-scheme:dark;
    --surface-1:#1a1a19; --page:#0d0d0d; --ink:#ffffff; --ink-2:#c3c2b7; --muted:#898781;
    --grid:#2c2c2a; --axis:#383835; --border:rgba(255,255,255,.10);
    --s1:#3987e5; --s2:#d95926; --s3:#199e70; --good-text:#0ca30c; }
}
:root[data-theme="dark"] .viz-root { color-scheme:dark;
  --surface-1:#1a1a19; --page:#0d0d0d; --ink:#ffffff; --ink-2:#c3c2b7; --muted:#898781;
  --grid:#2c2c2a; --axis:#383835; --border:rgba(255,255,255,.10);
  --s1:#3987e5; --s2:#d95926; --s3:#199e70; --good-text:#0ca30c; }
:root[data-theme="light"] .viz-root { color-scheme:light;
  --surface-1:#fcfcfb; --page:#f9f9f7; --ink:#0b0b0b; --ink-2:#52514e; --muted:#898781;
  --grid:#e1e0d9; --axis:#c3c2b7; --border:rgba(11,11,11,.10);
  --s1:#2a78d6; --s2:#eb6834; --s3:#1baf7a; --good-text:#006300; }
.wrap { max-width:960px; margin:0 auto; }
h1 { font-size:22px; font-weight:650; margin:8px 0 2px; }
.sub { color:var(--ink-2); font-size:13px; margin-bottom:20px; }
h2 { font-size:15px; font-weight:650; margin:34px 0 4px; }
h2 .note { font-weight:400; color:var(--muted); font-size:12px; margin-left:8px; }
.card { background:var(--surface-1); border:1px solid var(--border); border-radius:12px; padding:16px 18px; margin:12px 0; }
.kpis { display:grid; grid-template-columns:repeat(auto-fit,minmax(150px,1fr)); gap:10px; }
.tile { background:var(--surface-1); border:1px solid var(--border); border-radius:12px; padding:14px 16px; }
.tile .lb { font-size:12px; color:var(--ink-2); }
.tile .v { font-size:26px; font-weight:600; margin:3px 0 1px; }
.tile .d { font-size:12px; color:var(--muted); }
.tile .d.up { color:var(--good-text); }
.legend { display:flex; gap:18px; font-size:12px; color:var(--ink-2); margin:2px 0 8px; flex-wrap:wrap; }
.legend .sw { display:inline-block; width:10px; height:10px; border-radius:50%; margin-right:6px; vertical-align:-1px; }
.legend .sw1 { background:var(--s1); } .legend .sw2 { background:var(--s2); } .legend .sw3 { background:var(--s3); }
.chart { width:100%; height:auto; display:block; }
.chart .f1 { fill:var(--s1); } .chart .f2 { fill:var(--s2); } .chart .f3 { fill:var(--s3); }
.chart .l1 { stroke:var(--s1); } .chart .l2 { stroke:var(--s2); } .chart .l3 { stroke:var(--s3); }
.chart .l1, .chart .l2, .chart .l3 { fill:none; stroke-width:2; stroke-linejoin:round; stroke-linecap:round; }
.chart .ring { stroke:var(--surface-1); stroke-width:2; }
.chart .grid { stroke:var(--grid); stroke-width:1; }
.chart .axis { stroke:var(--axis); stroke-width:1; }
.chart .tick { fill:var(--muted); font-size:11px; font-variant-numeric:tabular-nums; }
.chart .cat  { fill:var(--ink-2); font-size:11.5px; }
.chart .dlabel { fill:var(--ink-2); font-size:11px; font-variant-numeric:tabular-nums; }
.chart .hit { cursor:default; }
table { border-collapse:collapse; width:100%; font-size:12.5px; font-variant-numeric:tabular-nums; }
th { text-align:left; color:var(--ink-2); font-weight:600; padding:6px 10px; border-bottom:1px solid var(--axis); white-space:nowrap; }
td { padding:6px 10px; border-bottom:1px solid var(--grid); white-space:nowrap; }
tr:last-child td { border-bottom:none; }
td.r, th.r { text-align:right; }
details.tbl { margin-top:10px; }
details.tbl summary { font-size:12px; color:var(--muted); cursor:pointer; user-select:none; }
.retention td.c { text-align:center; padding:6px 4px; border-radius:6px; font-weight:600; }
.retention td.dim { color:var(--muted); text-align:center; font-weight:400; }
.pill { display:inline-block; font-size:11px; border-radius:99px; padding:1px 9px; border:1px solid var(--axis); }
.pill.ok { color:var(--good-text); border-color:var(--good); }
.pill.bad { color:var(--crit); border-color:var(--crit); }
.pill.warn { color:var(--ink-2); border-color:var(--warn); }
#tip { position:fixed; pointer-events:none; background:var(--surface-1); color:var(--ink);
  border:1px solid var(--border); border-radius:8px; padding:6px 10px; font-size:12px;
  box-shadow:0 4px 14px rgba(0,0,0,.12); z-index:9; display:none; max-width:280px; }
.toggle { position:fixed; top:12px; right:14px; z-index:10; background:var(--surface-1);
  color:var(--ink-2); border:1px solid var(--border); border-radius:8px; padding:4px 10px; font-size:12px; cursor:pointer; }
.foot { color:var(--muted); font-size:12px; margin-top:36px; line-height:1.7; }
.grid2 { display:grid; grid-template-columns:1fr; gap:0; }
@media (min-width:760px){ .grid2 { grid-template-columns:1fr 1fr; gap:12px; } }
"""

JS = """
(function(){
  var tip = document.getElementById('tip');
  document.addEventListener('mouseover', function(e){
    var t = e.target.closest('[data-tip]');
    if(t){ tip.textContent = t.getAttribute('data-tip'); tip.style.display='block'; }
  });
  document.addEventListener('mouseout', function(e){
    if(e.target.closest && e.target.closest('[data-tip]')) tip.style.display='none';
  });
  document.addEventListener('mousemove', function(e){
    if(tip.style.display==='block'){
      var x = e.clientX+14, y = e.clientY+14;
      var r = tip.getBoundingClientRect();
      if(x+r.width > innerWidth-8) x = e.clientX-r.width-10;
      if(y+r.height > innerHeight-8) y = e.clientY-r.height-10;
      tip.style.left = x+'px'; tip.style.top = y+'px';
    }
  });
  var btn = document.getElementById('themeToggle');
  var root = document.documentElement;
  var saved = null;
  try { saved = localStorage.getItem('report-theme'); } catch(e){}
  if(saved) root.setAttribute('data-theme', saved);
  btn.addEventListener('click', function(){
    var cur = root.getAttribute('data-theme') ||
      (matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
    var next = cur === 'dark' ? 'light' : 'dark';
    root.setAttribute('data-theme', next);
    try { localStorage.setItem('report-theme', next); } catch(e){}
  });
})();
"""


def table_html(headers, rows, right_cols=()):
    th = "".join(f'<th class="{"r" if i in right_cols else ""}">{esc(h)}</th>' for i, h in enumerate(headers))
    trs = []
    for r in rows:
        tds = []
        for i, c in enumerate(r):
            tds.append(f'<td class="{"r" if i in right_cols else ""}">{c if str(c).startswith("<") else esc(c)}</td>')
        trs.append("<tr>" + "".join(tds) + "</tr>")
    return f'<table><thead><tr>{th}</tr></thead><tbody>{"".join(trs)}</tbody></table>'


def details_table(headers, rows, right_cols=(), title="查看数据表"):
    return (f'<details class="tbl"><summary>{esc(title)}</summary>'
            + table_html(headers, rows, right_cols) + "</details>")


def tile(label, value, delta="", delta_up=None):
    dc = ' class="d up"' if delta_up else ' class="d"'
    d = f'<div{dc}>{delta}</div>' if delta else '<div class="d"></div>'
    return f'<div class="tile"><div class="lb">{esc(label)}</div><div class="v">{value}</div>{d}</div>'


def legend_html(items):
    sw = "".join(f'<span><span class="sw sw{slot}"></span>{esc(n)}</span>' for n, slot in items)
    return f'<div class="legend">{sw}</div>'


def pct(a, b):
    return round(100.0 * a / b, 1) if b else 0.0

# ----------------------------------------------------------------------------
# 主流程
# ----------------------------------------------------------------------------

def build_report(D, gen_time):
    S = []  # sections html
    k = D["kpi"][0]
    rng = D["range"][0]
    tk = D["tokens"][0]
    total_users = int(num(k["total_users"]))
    ever_active = int(num(k["ever_active"]))

    # --- KPI ---
    tiles = [
        tile("总用户", f"{total_users:,}", f"曾消费 {ever_active} 人（{pct(ever_active, total_users)}%）"),
        tile("累计 API 请求", f"{int(num(k['total_reqs'])):,}", f"累计 tokens {num(k['total_tokens'])/1e6:.1f}M"),
        tile("本月活跃用户 MAU", f"{int(num(k['mau_this_month'])):,}", f"数据截至 {esc(rng['last_log'] or '')}"),
        tile("7 天活跃 Token", f"{int(num(tk['active_7d']))} / {int(num(tk['total']))}", f"僵尸 Token {int(num(tk['zombie']))} 个（30 天未用）"),
        tile("沉睡用户", f"{int(num(D['sleepy'][0]['sleepy_users']))}", "曾消费但近 14 天未活跃"),
    ]
    S.append(f'<div class="kpis">{"".join(tiles)}</div>')

    # --- 增长与活跃 ---
    wu = D["week_users"]; wd = {r["wk"]: r for r in D["week_dau"]}
    weeks = [r["wk"] for r in wu]
    reg_vals = [num(r["new_users"]) for r in wu]
    dau_vals = [num(wd[w]["avg_dau"]) if w in wd else 0 for w in weeks]
    S.append('<h2>增长与活跃<span class="note">按 ISO 周 · 周均 DAU = 日活跃用户数周平均</span></h2>')
    S.append('<div class="card">' + legend_html([("周新增注册", "1"), ("周均 DAU", "2")])
             + svg_line("growth", weeks, [("周新增注册", "1", reg_vals), ("周均 DAU", "2", dau_vals)], fmt="{:.0f}")
             + details_table(["周", "新增注册", "周均DAU", "覆盖天数"],
                             [[r["wk"], r["new_users"], wd[r["wk"]]["avg_dau"] if r["wk"] in wd else "—",
                               wd[r["wk"]]["days_covered"] if r["wk"] in wd else "—"] for r in wu],
                             right_cols=(1, 2, 3)) + "</div>")
    dr = D["dau_recent"]
    S.append('<div class="card">' + svg_vbar([str(r["d"])[5:] for r in dr], [num(r["dau"]) for r in dr], fmt="{:.0f}")
             + details_table(["日期", "DAU", "请求数"], [[r["d"], r["dau"], r["reqs"]] for r in dr], right_cols=(1, 2),
                             title="查看数据表（近 14 天日活）") + "</div>")

    # --- 消费 ---
    wc = D["week_consume"]
    cw = [r["wk"] for r in wc]
    S.append('<h2>用量趋势<span class="note">按周 · tokens 单位为百万</span></h2>')
    S.append('<div class="grid2">')
    S.append('<div class="card">' + svg_line("tokens", cw, [("周 tokens（百万）", "1", [num(r["tokens"]) / 1e6 for r in wc])],
                                             fmt="{:.0f}", area=True)
             + details_table(["周", "tokens", "活跃用户"], [[r["wk"], f"{int(num(r['tokens'])):,}", r["act_users"]] for r in wc], right_cols=(1, 2)) + "</div>")
    S.append('<div class="card">' + svg_line("reqs", cw, [("周请求数", "3", [num(r["reqs"]) for r in wc])], fmt="{:.0f}")
             + details_table(["周", "请求数", "活跃用户"], [[r["wk"], r["reqs"], r["act_users"]] for r in wc], right_cols=(1, 2)) + "</div>")
    S.append("</div>")
    hrs = {int(num(r["h"])): num(r["reqs"]) for r in D["hours"]}
    S.append('<div class="card">' + svg_vbar([f"{h:02d}时" for h in range(24)], [hrs.get(h, 0) for h in range(24)], fmt="{:,}".replace(",", "_")).replace("_", ",")
             + details_table(["小时", "请求数"], [[f"{h:02d}:00", f"{int(hrs.get(h,0)):,}"] for h in range(24)], right_cols=(1,),
                             title="查看数据表（24 小时请求分布）") + "</div>")

    # --- 模型排行 ---
    md = D["models"][:10]
    S.append('<h2>模型用量排行<span class="note">Top 10 · 含错误率</span></h2>')
    S.append('<div class="card">' + svg_hbar("models", [(r["model_name"], num(r["reqs"]) - num(r["errs"])) for r in md], fmt="{:,}")
             + details_table(["模型", "成功请求", "tokens", "错误数", "错误率", "平均 use_time"],
                             [[r["model_name"], f"{int(num(r['reqs'])-num(r['errs'])):,}",
                               f"{int(num(r['tokens'])):,}", int(num(r["errs"])),
                               f"{pct(num(r['errs']), num(r['reqs']))}%", r["avg_use_time"]] for r in D["models"]],
                             right_cols=(1, 2, 3, 4, 5)) + "</div>")

    # --- 留存 ---
    S.append('<h2>用户留存<span class="note">按注册周 · 列为注册后窗口内活跃人数（率）</span></h2>')
    head = ["注册周", "新用户", "当日 D0", "次日 D1", "第2–7天", "第8–30天"]
    trs = []
    for r in D["retention"]:
        n = int(num(r["new_users"]))
        cells = [f'<td class="r">{n}</td>']
        for col in ("d0", "d1", "d2_7", "d8_30"):
            v = int(num(r[col]))
            p = pct(v, n)
            c, ink = heat_color(p)
            cells.append(f'<td class="c" style="background:{c};color:{ink}">{v}<br><span style="font-weight:400;font-size:10.5px">{p}%</span></td>')
        trs.append(f'<tr><td>{esc(r["reg_week"])}</td>' + "".join(cells) + "</tr>")
    S.append('<div class="card"><table class="retention"><thead><tr>' +
             "".join(f'<th class="{"r" if i==1 else ""}">{esc(h)}</th>' for i, h in enumerate(head)) +
             f'</tr></thead><tbody>{"".join(trs)}</tbody></table>'
             + '<div class="legend" style="margin-top:10px"><span style="color:var(--muted)">颜色深浅 = 留存率（浅→深 = 低→高）；近期注册周的第 8–30 天窗口尚未走完，数值会随时间补齐</span></div></div>')

    # --- 用户使用深度 ---
    dp = D["depth"][0]
    S.append(f'<h2>用户使用深度<span class="note">口径：曾消费用户 {ever_active} 人</span></h2>')
    tiles3 = [
        tile("人均请求数", f"{num(dp['avg_reqs_per_user']):,.0f}", "全部历史累计"),
        tile("活跃日日均请求", f"{num(dp['avg_reqs_per_active_day']):,.1f}", "单用户单活跃日平均调用"),
        tile("户均活跃天数", f"{num(dp['avg_active_days']):,.1f}", "整个数据期内有调用的天数"),
        tile("户均模型数", f"{num(dp['avg_models']):,.1f}", f"仅用 1 个模型的 {int(num(dp['single_model_users']))} 人（{pct(num(dp['single_model_users']), ever_active)}%）"),
        tile("平均单次 tokens", f"{num(dp['avg_tokens_per_req']):,.0f}", f"长上下文（prompt>1万）占 {num(dp['long_ctx_pct'])}%"),
    ]
    S.append(f'<div class="kpis">{"".join(tiles3)}</div>')
    REQ_ORDER = ["1-5次", "6-20次", "21-100次", "101-500次", ">500次"]
    DAY_ORDER = ["1天", "2-5天", "6-15天", "16-30天", ">30天"]
    rb = {r["bucket"]: int(num(r["users"])) for r in D["depth_reqs"]}
    dbk = {r["bucket"]: int(num(r["users"])) for r in D["depth_days"]}
    S.append('<div class="grid2">')
    S.append('<div class="card"><div style="font-size:12px;color:var(--ink-2);margin-bottom:6px">按累计请求数分层（人数）</div>'
             + svg_hbar("dreq", [(o, rb.get(o, 0)) for o in REQ_ORDER], fmt="{:,}")
             + details_table(["累计请求数", "用户数"], [[o, f"{rb.get(o,0):,}"] for o in REQ_ORDER], right_cols=(1,)) + "</div>")
    S.append('<div class="card"><div style="font-size:12px;color:var(--ink-2);margin-bottom:6px">按历史活跃天数分层（人数）</div>'
             + svg_hbar("dday", [(o, dbk.get(o, 0)) for o in DAY_ORDER], fmt="{:,}")
             + details_table(["活跃天数", "用户数"], [[o, f"{dbk.get(o,0):,}"] for o in DAY_ORDER], right_cols=(1,)) + "</div>")
    S.append("</div>")
    S.append('<div class="card">' + svg_line("intensity", cw,
             [("周人均请求（活跃用户平均调用次数）", "2",
               [num(r["reqs"]) / num(r["act_users"]) if num(r["act_users"]) else 0 for r in wc])], fmt="{:.0f}")
             + details_table(["周", "活跃用户", "人均请求"],
                             [[r["wk"], r["act_users"],
                               f"{num(r['reqs'])/num(r['act_users']):.1f}" if num(r["act_users"]) else "—"] for r in wc],
                             right_cols=(1, 2)) + "</div>")

    # --- 渠道 ---
    ch_rows = []
    for r in D["channels"]:
        ok, err = int(num(r["ok_reqs"])), int(num(r["err_reqs"]))
        er = pct(err, ok + err)
        pill = '<span class="pill ok">启用</span>' if r["status"] == "1" else '<span class="pill bad">停用</span>'
        ch_rows.append([r["name"], pill, f"{ok:,}", f"{err:,}", f"{er}%", r["avg_use_time"] or "—"])
    S.append('<h2>渠道质量</h2>')
    S.append('<div class="card">' + table_html(["渠道", "状态", "成功请求", "错误请求", "错误率", "平均 use_time"], ch_rows, right_cols=(2, 3, 4, 5)) + "</div>")

    # --- 用户结构 ---
    conc_row = D["concentration"][0] if D["concentration"] else {"top3_pct": 0, "top10_pct": 0}
    S.append('<h2>用户结构</h2>')
    tu = D["top_users"]
    S.append('<div class="card">' + svg_hbar("topu", [(r["username"], num(r["reqs"])) for r in tu], fmt="{:,}")
             + details_table(["#", "用户", "请求数", "最后活跃"],
                             [[i + 1, r["username"], f"{int(num(r['reqs'])):,}", r["last_active"]] for i, r in enumerate(tu)],
                             right_cols=(2,)) + "</div>")
    tiles2 = [
        tile("Top3 用户请求占比", f"{num(conc_row['top3_pct'])}%", "用量集中度"),
        tile("Top10 用户请求占比", f"{num(conc_row['top10_pct'])}%", "用量集中度"),
    ]
    S.append(f'<div class="kpis">{"".join(tiles2)}</div>')
    S.append('<div class="card">' + table_html(["最近使用的 Token", "最后使用"],
                                               [[r["name"], r["last_used"]] for r in D["top_tokens"]],
                                               right_cols=(1,)) + "</div>")

    # --- 风控 ---
    ip_rows = [[r["ip"], r["users"], f"{int(num(r['reqs'])):,}"] for r in D["ip_risk"]]
    S.append('<h2>风控观察</h2>')
    S.append('<div class="card">' + (table_html(["共用 IP", "关联用户数", "请求数"], ip_rows, right_cols=(1, 2)) if ip_rows
                                     else '<span style="color:var(--muted);font-size:13px">未发现多用户共用 IP（或 ip 字段未记录）。</span>')
             + "</div>")

    body = "".join(S)
    doc = f"""<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>new-api 运营数据报告</title>
<style>{CSS}</style>
</head>
<body class="viz-root">
<button class="toggle" id="themeToggle">切换深浅色</button>
<div id="tip"></div>
<div class="wrap">
<h1>new-api 运营数据报告</h1>
<div class="sub">生成时间 {esc(gen_time)} · 数据范围 {esc(rng['first_log'] or '—')} ~ {esc(rng['last_log'] or '—')} · 只读统计，不影响业务</div>
{body}
<div class="foot">
口径说明：活跃 = 当日存在 type=2 消费日志的独立用户；留存列 = 注册后窗口内任一天活跃的人数/该周新用户数；
沉睡 = 曾消费但近 14 天无消费；用量集中度按请求数计。金额与余额类财务数据不在此报告展示。
图表悬停可查看数值；每张图均附数据表。use_time 为系统原样记录。
</div>
</div>
<script>{JS}</script>
</body>
</html>"""
    return doc


def main():
    ap = argparse.ArgumentParser(description="new-api 经营数据统计报告")
    ap.add_argument("-c", "--conf", default=os.path.expanduser("~/.newapi_db_report.conf"))
    ap.add_argument("-o", "--output", default="")
    ap.add_argument("--open", action="store_true", help="生成后用系统浏览器打开")
    args = ap.parse_args()

    cfg = load_config(args.conf)
    print(f"[连接] {cfg['user']}@{cfg['host']}:{cfg['port']}/{cfg['name']}")
    D = {}
    for name, sql in SQLS.items():
        D[name] = query(cfg, sql)
        print(f"  · {name}: {len(D[name])} 行")
    gen_time = datetime.datetime.now().strftime("%Y-%m-%d %H:%M")
    doc = build_report(D, gen_time)
    out = args.output or f"db_report_{datetime.datetime.now():%Y%m%d_%H%M}.html"
    with open(out, "w", encoding="utf-8") as f:
        f.write(doc)
    print(f"[完成] {os.path.abspath(out)}")
    if args.open:
        subprocess.run(["open", out])


if __name__ == "__main__":
    main()
