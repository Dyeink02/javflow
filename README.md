# JavFlow

**不需要 Docker、不需要命令列 — Windows 上一键安装的 JAV 媒体库自动化工作流工具。**
番号抓取 · 视频整理 · 女优订阅 · 媒体库刮削 · 演员头像自动缓存

[![Platform](https://img.shields.io/badge/platform-Windows%2010%2F11-0078D6.svg)](https://github.com/javflow-team/javflow)
[![GitHub Release](https://img.shields.io/github/v/release/javflow-team/javflow)](https://github.com/Dyeink02/javflow/releases/tag/beta)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](https://github.com/javflow-team/javflow/blob/main/LICENSE)

简体中文

> **这不只是又一个「把资料喂给 Jellyfin 的刮削器」。** JavFlow 负责从抓片、整理、订阅新片到生成 Emby / Jellyfin 兼容的媒体库，把一整套重复劳动串成一条自动流水线。你按一次按钮，剩下的交给它。

核心由四个工作区组成：**🕷 爬虫** → **📁 视频整理** → **📌 订阅** → **🎬 媒体库刮削**。

**100% 本地运行** — 不收集数据、不上传任何文件信息，网络请求仅用于刮削公开元数据。

---

## 规格速览

| 项目 | 内容 |
| --- | --- |
| **平台** | Windows 10 / 11（64-bit） |
| **安装** | 下载 `javflow.exe`，双击运行，**免 Docker、免命令列** |
| **抓取来源** | JavBus 为主，支持镜像地址自动 fallback |
| **磁力处理** | 自动去重、按关键词排除、输出 `magnet-links.txt` |
| **视频整理** | 按番号识别、去广告片段、去重保留最高画质、自动重命名 |
| **订阅更新** | 追踪女优 / 片商 / 系列，一键更新并生成新片磁力 |
| **媒体库输出** | 生成 NFO + poster + backdrop + landscape，兼容 **Emby / Jellyfin** |
| **演员头像** | 全局缓存在库根 `.actors` 目录，全库复用，无需重复下载 |
| **防封锁** | 代理状态监控、镜像地址自动切换、TLS 握手失败 fallback |
| **过滤系统** | 发布日期、女优数量、指定番号阈值三重过滤 |
| **授权** | MIT |

---


## 安装

### 推荐方式：下载 EXE

从 [GitHub Releases](https://github.com/Dyeink02/javflow/releases/tag/beta) 下载：

| 平台 | 文件 |
| --- | --- |
| **Windows x64** | `javflow.exe` |

下载后直接双击运行，无需安装。

首次启动建议先进入 **设置** 页面配置：

- 代理地址（如需）
- 媒体库根目录
- 输出目录
- 反封锁镜像地址

---

## 核心功能

### 🕷 爬虫

从 JavBus 等来源抓取影片元数据与磁力链接。

- **多页批量抓取**：设置起始页与总页数，自动翻页。
- **磁力过滤**：按关键词排除、按画质/大小排序。
- **日期过滤**：只保留指定日期之后的影片。
- ** actress / 番号过滤**：按女优数量或指定番号阈值排除。
- **输出产物**：
  - `filmData.json`：完整元数据
  - `magnet-links.txt`：过滤后的磁力链接
  - `filtered-film-codes.json`：被过滤的番号及原因
  - `crawl-profile.json`：本次抓取摘要

### 📁 视频整理

把杂乱的视频文件整理成规范的媒体库结构。

- **番号识别**：支持多种命名格式，单-digit 番号自动补齐（如 `APGH-4` → `APGH-004`）。
- **去广告**：内置规则 + AI 广告检测（默认关闭）。
- **去重**：同一番号多个版本时，自动保留最大/最高画质文件，其余移入 `_to_delete`。
- **重命名**：按 `番号-标题` 或自定义规则重命名，保留已有后缀（如 `-A`、`-B`）。
- **非递归扫描**：只处理第一层目录，避免误动子目录。

### 📌 订阅

追踪你喜欢的女优、片商或系列，自动发现新片。

- **订阅地址**：输入 JavBus 女优页、片商页或系列页 URL。
- **一键更新**：抓取订阅源新片并生成磁力。
- **修改 / 删除**：管理订阅列表。
- **自动刷新**：爬虫完成后自动更新整理、刮削、订阅状态。

### 🎬 媒体库刮削

为已有影片生成 Emby / Jellyfin 兼容的元数据与图片。

- **NFO 生成**：包含标题、演员、标签、发行日期等。
- **图片下载**：cover、backdrop、landscape，失败时自动 fallback。
- **演员头像**：下载到库根 `.actors/演员名.jpg`，供媒体库全局读取。
- **STRM 支持**：可配合 Alist 等网盘生成 `.strm` 文件。

### 🛡 防封锁

- **代理状态监控**：每 15 秒刷新一次，失败时 3 秒重试并显示倒计时。
- **镜像 fallback**：主站 TLS 握手失败或被墙时，自动切换到备用镜像地址。
- **浏览器 fallback**：Cloudflare 等反爬场景自动弹浏览器手动验证。

---

## 常见问题（FAQ）

**JavFlow 和 Jellyfin / Emby 有什么不同？**

Jellyfin / Emby 是媒体服务器，负责播放。JavFlow 是上游工具，负责抓资料、整理文件、生成 NFO 和图片。刮削完成后，你的媒体库就能被 Jellyfin / Emby 正确识别。

**JavFlow 会删除我的文件吗？**

不会主动删除。视频整理的去重功能会把较小版本移入 `_to_delete` 目录，由你决定是否最终删除。

**演员头像在 Emby 里不显示怎么办？**

JavFlow 会把头像放在库根 `.actors/` 目录。Emby 需要刷新整个库的元数据，并确保「本地图片提取器」或类似选项已启用。如果 Emby 版本不支持 `.actors`，未来版本会加入 Emby API 自动刷新功能。

**为什么抓取时提示 TLS 握手失败？**

目标站被墙或证书异常。在设置里添加反封锁镜像地址，程序会自动 fallback。

**可以只生成 NFO 不动原文件吗？**

可以。媒体库刮削功能只读取原文件信息，生成 NFO 和图片，不搬移原文件。

---

## 开发者指南

### 技术架构

| 层级 | 技术 |
| --- | --- |
| **Backend** | Go 1.25+ |
| **Frontend** | 原生 HTML / JS / CSS |
| **Desktop** | Wails v2 |
| **Testing** | Go testing + Mocha |

### 从源码构建

**前置需求**：Go 1.25+、Node.js 18+、Wails CLI

```bash
git clone https://github.com/javflow-team/javflow.git
cd javflow
npm install
npm run build:desktop-frontend
npm run sync:wails-frontend
cd wails-shell
wails build -platform windows/amd64 -ldflags "-s -w"
```

### 运行测试

```bash
# Go 后端测试
cd wails-shell
go test ./...

# Node 侧载测试
cd ..
npm test
```

### 目录结构

```
javflow/
├── wails-shell/              # Wails 桌面应用（Go 后端）
│   ├── internal/             # 内部模块
│   │   ├── crawlrunner/      # 爬虫执行引擎
│   │   ├── crawlfetch/       # 页面抓取
│   │   ├── crawltask/        # 任务管理
│   │   ├── organizer/        # 视频整理
│   │   ├── subcrawl/         # 订阅抓取
│   │   ├── subcrawlv2/       # 订阅抓取 v2
│   │   ├── librarymetadata/  # 媒体库刮削
│   │   └── bridge/           # 前后端桥接 API
│   ├── frontend/desktop/     # 预打包前端资源
│   └── app.go                # Wails 入口
├── desktop/                  # 前端源码
│   ├── renderer/             # 渲染器 JS / HTML / CSS
│   └── common/               # 公共 helper / 文本
├── src/                      # Node.js / TypeScript 侧载核心（Cloudflare 绕过等）
├── dist/                     # TypeScript 编译输出（由 npm run build 生成）
├── test/                     # Node 侧测试
└── package.json              # 脚本与依赖
```

---

## 致谢

JavFlow 使用并感谢以下开源项目：

- **[Wails](https://wails.io/)** - 跨平台桌面应用框架
- **[Go](https://go.dev/)** - 后端语言
- **[Puppeteer](https://pptr.dev/)** - 早期版本使用（保留在 src/）

---

## License

MIT License

---

⚠️ 免责声明

本项目仅供个人学习研究使用，请使用者遵守：

- 尊重网站服务条款
- 合理控制请求频率
- 不用于商业目的

使用本项目造成的任何后果由使用者自行承担。
