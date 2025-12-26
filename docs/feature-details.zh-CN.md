# 功能细节文档

[English](./feature-details.md)

## 概述

本文档记录 Market 系统中各个功能模块的实现细节、问题修复和优化改进。

## Pending 状态消息延迟处理机制

### 问题描述

在 `datawatcher_state` 模块中，当接收到 app-service 发出的状态变化推送时，系统需要通过一套逻辑来找到与之匹配的 source 信息。存在一个竞态条件问题：

如果安装操作的第一条消息是 `pending` 状态，并且这个消息是在安装接口请求中就被发送并被接收到的，此时找到的 source 有可能不是这次的安装 source，而是上一次的。

**原因分析**：
1. `pending` 状态不在失败/取消状态列表中，会参与缓存查找
2. 缓存查找逻辑按 `statusTime` 降序排序，选择最新的记录
3. 当新安装开始时，任务可能还在 `pendingTasks` 队列中，尚未执行到 `AppInstall`，因此还没有 `OpID`
4. 如果上一次安装的 `pending` 状态的 `statusTime` 比当前消息的 `CreateTime` 更新或相同，缓存查找会匹配到上一次的 source
5. 后备机制（通过 `OpID` 或 `appName+user` 查找）也可能失效，因为新任务可能还没有 `OpID`，或者 `GetLatestTaskByAppNameAndUser` 可能返回上一次的任务

### 解决方案

实现了延迟处理机制，对于 `pending` 状态的消息，先检查是否有未完成的安装任务，如果有则延迟处理：

**实现位置**：
- `internal/v2/appinfo/datawatcher_state.go`
- `internal/v2/task/taskmodule.go`

**关键改动**：

1. **TaskModule 新增方法**：
   - `HasPendingOrRunningInstallTask(appName, user string) (hasTask bool, lockAcquired bool)`
   - 使用 `TryRLock` 非阻塞方式检查是否有未完成的安装/克隆任务
   - 返回两个值：是否有任务，以及是否成功获取锁

2. **DataWatcherState 延迟处理机制**：
   - 添加 `DelayedMessage` 结构体存储延迟消息
   - 添加 `delayedMessages` 队列和后台处理 goroutine
   - `processDelayedMessages()` 每秒检查一次延迟队列
   - `processDelayedMessagesBatch()` 处理到期的延迟消息

3. **handleMessage 特殊处理**：
   - 对于 `pending` 状态的安装操作消息，先检查是否有未完成的安装任务
   - 如果无法获取锁，延迟处理（避免阻塞 NATS 消息处理）
   - 如果有未完成的任务，延迟处理消息
   - 延迟消息每 2 秒重试一次，最多重试 10 次（约 20 秒）

