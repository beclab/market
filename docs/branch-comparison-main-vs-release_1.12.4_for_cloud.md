# main 分支 vs release_1.12.4_for_cloud 分支对比分析

## 概述

`release_1.12.4_for_cloud` 分支是从 main 分支的 `ae03426` 提交点分出的，之后没有独立提交。main 分支从分叉点至今新增了 **108 个提交**，涉及 **52 个文件**，净变化 **+3112 / -7011 行**（整体代码量减少约 3900 行）。

**结论：main 分支的迭代对 Public（云端）部署模式的影响是安全可控的。** 所有 `IsPublicEnvironment()` 守卫保持完整，核心架构变更（Pipeline 重构、并发修复、代码清理）在 Public 模式下的行为分支未受破坏。

---

## 1. Public 与 non-Public 运行模式机制

### 1.1 模式判断

通过环境变量 `isPublic` 控制，值为 `"true"` 时为 Public 模式：

```go
// internal/v2/utils/systeminfo.go
func IsPublicEnvironment() bool {
    isPublic := os.Getenv("isPublic")
    return isPublic == "true"
}
```

### 1.2 Public 模式下被禁用/简化的组件

| 组件 | Public 模式行为 | 控制位置 |
|------|----------------|---------|
| **Redis** | 不创建 Redis 客户端 | `cmd/market/v2/main.go:132` |
| **K8s User Watch** | 不启动 | `cmd/market/v2/main.go:247` |
| **Task Module** | 不初始化 | `cmd/market/v2/main.go:247` |
| **History Module** | 不初始化 | `cmd/market/v2/main.go:247` |
| **Payment State Machine** | 不初始化 | `cmd/market/v2/main.go:247` |
| **DataWatcherRepo** | 不初始化（需 Redis） | `appinfomodule.go:206` |
| **DataWatcherState** | Start 时直接返回 | `datawatcher_state.go:296` |
| **DataWatcherUser** | 类似的 Public 守卫 | `datawatcher_user.go` |
| **DataSender (NATS)** | 开发/Public 环境禁用 | `datasender_app.go` |
| **Redis 持久化** | `ForceSync`/`SaveUserDataToRedis` 为 no-op | `cache.go`, `db.go` |
| **Settings (Redis)** | load 返回默认值，save 为 no-op | `settings/manager.go` |
| **SystemEnv CRD Watch** | 跳过 | `settings/systemenv_watcher.go` |
| **启动等待循环** | 不等待 app-service 就绪 | `cmd/market/v2/main.go:154` |
| **用户身份** | 固定返回 `"admin"` | `utils/user.go` |
| **Token** | 固定返回 `"public"` | `utils/user.go` |

### 1.3 Public 模式下仍然运行的组件

- **CacheManager**（内存模式，无 Redis 后端）
- **Syncer** → 远程数据拉取
- **Hydrator** → 数据渲染/补全
- **DataWatcher** → Pending → Latest 转移
- **Pipeline** → 编排以上组件的串行执行
- **StatusCorrectionChecker** → 状态校正
- **HTTP API Server**（端口 8080）
- **Runtime State Service**

---

## 2. main 分支的核心迭代分类

### 2.1 架构重构：Pipeline 串行编排（影响大，风险低）

**新增文件：** `internal/v2/appinfo/pipeline.go`（335 行）

这是最大的架构变更。原来 Syncer、Hydrator、DataWatcherRepo、StatusCorrectionChecker 各自拥有独立的定时循环和触发机制，现在统一由 Pipeline 每 30 秒串行编排：

```
Phase 1: Syncer          → 拉取远程数据
Phase 2: Hydrator        → 处理 Pending 队列（批量并行，默认并发 5）
Phase 3: DataWatcherRepo → 处理 chart-repo 状态变更
Phase 4: StatusCorrection → 修正 app 运行状态
Phase 5: Hash + ForceSync → 统一计算 hash 并同步到 Redis
```

**对 Public 模式的影响：**
- Pipeline 在 Public 模式下**仍然会启动**（由 `EnableHydrator && cacheManager != nil` 控制，两者在 Public 模式下都为 true）
- Phase 1: Syncer 正常执行远程数据拉取
- Phase 2: Hydrator 正常处理 Pending 数据
- Phase 3: DataWatcherRepo 为 nil（Public 不初始化），自动跳过
- Phase 4: StatusCorrectionChecker 正常执行
- Phase 5: Hash 计算正常，`ForceSync` 到 Redis 因 Public 模式会降级为 no-op
- **结论：兼容，无需修改**

### 2.2 并发安全修复（影响中，风险低）

涉及 PR #107-#120，共约 20 个提交，修复了多种数据竞争和死锁问题：

| 修复内容 | 涉及文件 |
|---------|---------|
| `TryLock` → `Lock`/`RLock` | `cache.go`, `syncer.go`, `hydration.go`, `appinfomodule.go`, `taskmodule.go` |
| `atomic.Pointer` 替代裸指针 | `datawatcher_state.go`（taskModule/historyModule 引用）|
| `defer unlock` 保证 panic 安全 | `appinfomodule.go`, `cache.go` |
| RLock 提前释放（避免在锁内做 HTTP I/O）| `cache.go`（`setAppDataInternal`）|
| 防止 double-close channel | `appinfomodule.go` |
| CacheManager 统一锁管理 | `cache.go`（`types.CacheData` 不再自带锁）|

**对 Public 模式的影响：**
- 这些修复全部是底层并发安全改进，不涉及 Public/non-Public 分支逻辑
- CacheManager 在 Public 模式下同样被使用（只是没有 Redis 后端），所以这些修复**直接受益于 Public 模式**
- **结论：完全兼容，且提高了 Public 模式的稳定性**

### 2.3 新增 App 状态与 API 增强（影响中，需关注）

#### 新增状态

在 `datawatcher_state.go` 中新增了多个 app 安装状态的支持：

- `pendingCanceled` — 在 pending 阶段取消
- `installingCanceled` — 安装中取消
- `downloadingCanceled` — 下载中取消
- `downloadingCanceling` — 正在取消下载
- `resuming` — 恢复中

这些是 **app-service 推送的状态字符串**，不是 TaskModule 的 TaskStatus（仍然只有 Pending/Running/Completed/Failed/Canceled）。

**对 Public 模式的影响：**
- `DataWatcherState` 在 Public 模式下是**禁用的**（Start 时直接返回），因此 NATS 状态消息处理不会执行
- 如果云端 app-service 也推送这些新状态，需要确保 NATS 连接不存在或消息能安全被忽略
- **结论：基本无影响**，因为 Public 模式下状态监听是关闭的

#### 新增/修改 API

- `POST /apps/resume` — 恢复暂停的 app
- `POST /apps/stop` — 暂停 app
- `GET /market/statesimple` — 轻量版状态查询
- `POST /apps/{id}/clone` — 克隆 app
- 同步安装支持超时机制 `waitForSyncTask`（30 分钟硬超时 + HTTP 202 fallback）

**对 Public 模式的影响：**
- `resume` 和 `stop` 依赖 app-service（`/app-service/v1/apps/{name}/resume|suspend`），Public 云端如果没有 app-service，调用会失败
- `clone` 依赖 TaskModule，Public 模式下 TaskModule 为 nil，**调用会失败**
- `statesimple` 只依赖 CacheManager，**Public 模式可用**
- 同步安装（`sync: true`）依赖 TaskModule，Public 模式下不可用
- **结论：新增的任务类 API 在 Public 模式下不可用，但这与原来的设计一致（Public 模式从来不支持安装/卸载任务）**

### 2.4 数据结构变更（影响小）

| 变更 | 详情 |
|-----|------|
| `AppStateLatestData` 新增字段 | `Message`, `Reason`（状态消息和原因）|
| `AppInfoUpdate` 新增 `Message` 字段 | NATS 通知携带更多信息 |
| `AppServiceResponse.Spec` 扩展 | 新增 `RawAppName`, `IsSysApp`, `Settings` 结构 |
| 删除废弃类型 | `AcceptedCurrency`, `PriceProducts`, `PriceDeveloper` |
| `rawAppName` 处理 | 从 `setup.go` 和 `datawatcher_state.go` 正确提取 |
| System Source ID | 系统 app 的 sourceID 固定为 `"system"` |

**对 Public 模式的影响：**
- `AppServiceResponse` 结构扩展：Public 模式的 `SetupAppServiceData` 返回固定数据（`admin` 用户 + 空 map），不会真正解析 app-service 响应，**无影响**
- 新增字段是向后兼容的 JSON 扩展，**无影响**
- 废弃类型删除：这些类型在 Public 模式下未被使用，**无影响**
- **结论：完全兼容**

### 2.5 代码清理与删除（影响小）

| 删除内容 | 原因 |
|---------|------|
| `internal/v2/utils/image.go`（801 行）| 图片处理逻辑移至 `datawatcher_repo.go` |
| `internal/v2/utils/format.go`（~454 行缩减）| 大部分格式化函数已废弃，仅保留 `ParseJson` |
| `internal/v2/utils/user.go`（部分缩减）| 清理未使用函数 |
| `Dockerfile.frontend` + `nginx.conf` | 前端容器构建已剥离 |
| `terminus-frontend-image.yml` | 前端 CI workflow 已移除 |
| `AppInfoModule` 多个方法 | `RefreshUserDataStructures`, `GetConfiguredUsers`, `GetCachedUsers`, `CleanupInvalidData`, `GetInvalidDataReport`, `GetDockerImageInfo`, `GetLayerDownloadProgress` |

**对 Public 模式的影响：**
- 删除的工具函数在 Public 模式下未被调用
- `Dockerfile.frontend` 删除：如果云端之前是分别部署前后端容器的，需要确认前端构建方式是否独立维护
- **结论：基本无影响，但需确认前端部署方式**

### 2.6 Dockerfile 变更（需关注）

```diff
- FROM golang:1.23.11-alpine   # 旧：使用 golang 完整镜像
+ FROM alpine:3.23              # 新：使用更小的 alpine 镜像

- CMD ["/opt/app/market", "-v", "2", "--logtostderr"]
+ ENTRYPOINT ["/opt/app/market", "-v", "2", "--logtostderr"]
```

同时 `Dockerfile.server` 重命名为 `Dockerfile`。

**对 Public 模式的影响：**
- 基础镜像从 `golang:1.23.11-alpine` 切换到 `alpine:3.23`，镜像体积大幅减小
- `CMD` 改为 `ENTRYPOINT`，行为上 K8s 部署不受影响（除非 pod spec 中使用 `command` 覆盖）
- 文件重命名可能需要更新 CI/CD pipeline 和 Helm chart 中的 Dockerfile 引用
- **结论：需要在云端部署配置中确认 Dockerfile 路径和 CI workflow**

---

## 3. 风险评估总结

### 3.1 高兼容 / 低风险

| 改动类别 | 风险等级 | 说明 |
|---------|---------|------|
| Pipeline 架构重构 | ⬜ 低 | Public 模式下正常运行，Phase 3 自动跳过，Phase 5 Redis sync 降级为 no-op |
| 并发安全修复 | ⬜ 低 | 全面提升稳定性，对 Public 模式同样受益 |
| 数据结构扩展 | ⬜ 低 | 向后兼容的字段新增 |
| 代码清理/删除 | ⬜ 低 | 移除的代码在 Public 模式下未被使用 |
| App 状态扩展 | ⬜ 低 | DataWatcherState 在 Public 模式下禁用 |
| DEBUG 日志清理 | ⬜ 低 | 仅影响日志输出 |

### 3.2 需关注 / 需验证

| 关注点 | 风险等级 | 行动建议 |
|-------|---------|---------|
| Dockerfile 路径变更 | 🟡 中 | 确认云端 CI/CD 使用的是 `Dockerfile` 而非 `Dockerfile.server` |
| 前端构建删除 | 🟡 中 | 确认云端前端是否有独立的构建和部署流程 |
| 基础镜像变更 | 🟡 中 | 验证 `alpine:3.23` 在云端容器环境中的兼容性 |
| 新增 API（resume/stop/clone） | 🟡 中 | 这些 API 在 Public 模式下依赖 app-service 或 TaskModule，调用会失败，需确认云端前端是否会调用这些接口 |

### 3.3 不影响 Public 模式

| 改动 | 原因 |
|-----|------|
| TaskModule 重构 | Public 模式下 TaskModule 不初始化 |
| HistoryModule 改动 | Public 模式下 HistoryModule 不初始化 |
| Payment 状态机 | Public 模式下不初始化 |
| K8s User Watch | Public 模式下不启动 |
| Redis 持久化改进 | Public 模式下 Redis 操作为 no-op |
| NATS 状态监听增强 | Public 模式下 DataWatcherState 禁用 |

---

## 4. 部署到云端的建议

### 4.1 直接可行

main 分支的代码**可以直接用于 Public（云端）部署**，所有 `IsPublicEnvironment()` 守卫保持完整，不会因为新增的迭代而引入破坏性变化。

### 4.2 部署前检查清单

1. **环境变量**：确保云端部署配置中设置了 `isPublic=true`
2. **Dockerfile**：CI/CD pipeline 应引用 `Dockerfile`（不再是 `Dockerfile.server`）
3. **基础镜像**：验证 `alpine:3.23` 在目标云平台的兼容性
4. **前端部署**：确认前端有独立的构建/部署流程（`Dockerfile.frontend` 和 `nginx.conf` 已从代码库移除）
5. **API 消费端**：确认云端前端不会调用 `resume`/`stop`/`clone` 等需要 TaskModule 的 API
6. **环境变量可选**：新增的 `PIPELINE_HYDRATION_CONCURRENCY` 可调优 Hydration 并发数（默认 5）
7. **GitHub Actions**：`backend-image-bytag.yml` 和 `backend-publish_docker.yml` 中 Dockerfile 引用已更新为 `Dockerfile`

### 4.3 性能预期

main 分支的 Pipeline 架构相比旧版本：
- **更好的资源利用**：串行编排避免了并发竞争，减少锁冲突
- **更可预测的延迟**：30 秒周期，Phase 5 统一 hash 计算，减少重复计算
- **更高的稳定性**：全面的并发安全修复，消除了数据竞争和死锁风险
