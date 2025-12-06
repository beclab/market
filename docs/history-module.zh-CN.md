# History 模块

> Language: [English](./history-module.md) | **简体中文**

## 概述

History 模块负责存储、查询和管理应用历史记录。该模块使用 PostgreSQL 数据库进行数据存储，并提供自动清理旧记录的功能。

## 功能特性

1. **存储历史记录** - 存储各种应用操作和系统事件
2. **条件查询** - 按类型、应用、时间范围等条件查询历史记录
3. **自动清理** - 定期清理超过 1 个月的旧记录
4. **分页支持** - 支持大数据集的分页查询
5. **健康检查** - 提供数据库连接健康检查功能

## 数据结构

### HistoryRecord
```go
type HistoryRecord struct {
    ID       int64       `json:"id" db:"id"`             // Primary key
    Type     HistoryType `json:"type" db:"type"`         // Record type (enum)
    Message  string      `json:"message" db:"message"`   // Description message
    Time     int64       `json:"time" db:"time"`         // Unix timestamp
    App      string      `json:"app" db:"app"`           // Application name
    Account  string      `json:"account" db:"account"`   // Account name
    Extended string      `json:"extended" db:"extended"` // JSON string for additional data
}
```

### HistoryType（记录类型）
预定义的记录类型包括:
- `ACTION_INSTALL` - 应用安装操作
- `ACTION_UNINSTALL` - 应用卸载操作
- `ACTION_CANCEL` - 应用操作取消
- `ACTION_UPGRADE` - 应用升级操作
- `SYSTEM_INSTALL_SUCCEED` - 系统安装成功
- `SYSTEM_INSTALL_FAILED` - 系统安装失败
- `action` - 通用操作类型
- `system` - 通用系统类型

### QueryCondition
```go
type QueryCondition struct {
    Type      HistoryType `json:"type,omitempty"`        // Filter by type
    App       string      `json:"app,omitempty"`         // Filter by app
    Account   string      `json:"account,omitempty"`     // Filter by account
    StartTime int64       `json:"start_time,omitempty"`  // Time range start
    EndTime   int64       `json:"end_time,omitempty"`    // Time range end
    Limit     int         `json:"limit,omitempty"`       // Max records to return
    Offset    int         `json:"offset,omitempty"`      // Pagination offset
}
```

## 数据库表结构

模块自动创建以下数据库表:

```sql
CREATE TABLE IF NOT EXISTS history_records (
    id BIGSERIAL PRIMARY KEY,
    type VARCHAR(100) NOT NULL,
    message TEXT NOT NULL,
    time BIGINT NOT NULL,
    app VARCHAR(255) NOT NULL,
    account VARCHAR(255) NOT NULL DEFAULT '',
    extended TEXT DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 为常用查询字段创建索引以提高性能
CREATE INDEX IF NOT EXISTS idx_history_type ON history_records(type);
CREATE INDEX IF NOT EXISTS idx_history_app ON history_records(app);
CREATE INDEX IF NOT EXISTS idx_history_account ON history_records(account);
CREATE INDEX IF NOT EXISTS idx_history_time ON history_records(time);
CREATE INDEX IF NOT EXISTS idx_history_created_at ON history_records(created_at);
```

## 自动清理功能

模块自动启动后台清理例程，定期删除超过 1 个月的旧记录:

- 默认每 24 小时运行一次清理
- 删除 `time` 字段小于当前时间减去 1 个月的记录
- 清理间隔可通过 `HISTORY_CLEANUP_INTERVAL_HOURS` 环境变量配置

## 扩展记录类型

你可以轻松添加新的记录类型（下面是示例，当前代码中未必已经定义这些常量）:

```go
const (
    TypeActionRestart   HistoryType = "ACTION_RESTART"
    TypeSystemBackup    HistoryType = "SYSTEM_BACKUP"
    TypeSystemRestore   HistoryType = "SYSTEM_RESTORE"
)
```

## 注意事项

1. Extended 字段应使用有效的 JSON 格式；如果不是合法 JSON，模块会记录 Warning 日志但仍然以原始字符串存储
2. 在公共环境（由 `utils.IsPublicEnvironment()` 判断）下不会初始化 History 模块，避免在某些部署场景中不必要的数据库连接


