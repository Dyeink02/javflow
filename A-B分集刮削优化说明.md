# A/B 分集刮削优化说明

更新时间：2026-07-29

## 目标

`MIDD-820-A` 与 `MIDD-820-B` 是同一部影片的分集文件。它们应该各自保留本地文件名和 sidecar 文件，但详情页、演员、标题、封面、背景等元数据只请求和解析一次。

## 当前实现

### 规范化键

刮削任务先把文件番号拆成：

```text
显示番号：MIDD-820-A / MIDD-820-B
元数据键：MIDD-820
分集标记：A / B
```

元数据键用于本地爬虫记录查找、远端 provider 查询、缓存和并发协调；显示番号只用于 UI、日志和 sidecar 输出路径。

### single-flight 请求合并

同一轮任务中，多个分集同时请求 `MIDD-820` 时只有第一个请求访问 provider，其余请求等待同一个共享结果。请求完成后结果进入内存缓存，后续 A/B 文件直接复用。这样不会因为 A/B 并发而向站点重复发起详情请求。

### 文件输出

共享元数据不会合并文件输出。A、B 仍分别写入：

```text
MIDD-820-A.mp4.nfo
MIDD-820-A.mp4-poster.jpg
MIDD-820-A.mp4-backdrop.jpg
MIDD-820-A.mp4-landscape.jpg

MIDD-820-B.mp4.nfo
MIDD-820-B.mp4-poster.jpg
MIDD-820-B.mp4-backdrop.jpg
MIDD-820-B.mp4-landscape.jpg
```

因此媒体服务器仍能把两个文件显示为两个可播放项目，同时刮削速度和请求量得到改善。

## 失败与取消语义

- 共享请求失败时，等待者收到同一个错误，不会各自重试造成请求风暴。
- 单个文件的写入失败不会污染其它分集的 sidecar 路径。
- 取消任务会释放等待者和 provider 请求的上下文；缓存不会保存不完整结果。
- 日志会区分“首次请求”“复用共享结果”和“等待共享结果”，方便判断是否真的只请求了一次。

## 回归测试

已覆盖：

- A/B 共用一个 provider 查询。
- A/B 各自完成 NFO、封面、背景、横图写入。
- 同一规范键的并发请求只执行一次。
- 取消、失败和缓存命中不产生重复写入。

对应测试位于 `wails-shell/internal/librarymetadata/service_runtime_test.go`，其中验证 `MIDD-820-A/B` 只触发一次 `MIDD-820` provider 搜索。
