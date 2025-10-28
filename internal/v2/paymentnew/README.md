# PaymentNew 模块 - 基于状态机的支付流程重构

## 设计目标

重新设计支付流程，从流程式改为状态机式，更好地反映业务的抽象本质。

## 核心概念

### 五维状态设计

支付流程由五个独立的维度组成，每个维度都有不同的状态值：

#### 维度一：是否需要支付 (`PaymentNeed`)
- `PaymentNeedNotRequired`: 不需要支付（免费应用）
- `PaymentNeedRequired`: 需要支付

#### 维度二：是否已跟开发者同步过VC信息 (`DeveloperSync`)
- `DeveloperSyncNotStarted`: 未开始
- `DeveloperSyncInProgress`: 进行中
- `DeveloperSyncCompleted`: 已完成
- `DeveloperSyncFailed`: 失败

#### 维度三：是否跟larepass同步过签名信息 (`LarePassSync`)
- `LarePassSyncNotStarted`: 未开始
- `LarePassSyncInProgress`: 进行中
- `LarePassSyncCompleted`: 已完成
- `LarePassSyncFailed`: 失败

#### 维度四：签名状态 (`SignatureStatus`)
- `SignatureNotRequired`: 不需要签名
- `SignatureRequired`: 需要签名
- `SignatureRequiredAndSigned`: 需要签名并且已经有签名
- `SignatureRequiredButPending`: 需要签名但还在等待中

#### 维度五：支付状态 (`PaymentStatus`)
- `PaymentNotNotified`: 尚未通知前端
- `PaymentNotificationSent`: 已经通知前端进行支付
- `PaymentFrontendCompleted`: 前端已经支付完成
- `PaymentDeveloperConfirmed`: 开发者已经确认

### 状态机架构

```
PaymentStateMachine
├── states: map[string]*PaymentState  // 状态存储 (key: userid:appid:productid)
├── dataSender: DataSenderInterface    // 数据发送接口
└── settingsManager: SettingsManager   // Redis 等设置管理
```

### 主要事件

1. **start_payment**: 开始支付流程
2. **signature_submitted**: 签名已提交
3. **payment_completed**: 前端支付完成
4. **vc_received**: 收到开发者返回的VC
5. **request_signature**: 请求LarePass签名

## 文件结构

```
paymentnew/
├── types.go           # 状态定义和数据结构
├── utils.go           # 业务功能方法
├── state_machine.go   # 状态机实现
├── api.go             # 对外接口（兼容旧版API）
└── README.md          # 本文档
```

## 关键组件

### types.go
- 定义五个维度的状态类型
- 定义 `PaymentState` 结构
- 定义 `PaymentStateMachine` 结构
- 提供状态判断和转换的基础方法

### utils.go
提供核心业务功能的内部实现：
- `QueryVCFromDeveloper()`: 从开发者服务查询VC
- `NotifyLarePassToSign()`: 通知LarePass客户端签名
- `NotifyFrontendPaymentRequired()`: 通知前端需要支付
- `NotifyFrontendPurchaseCompleted()`: 通知前端购买完成
- `CreateFrontendPaymentData()`: 创建前端支付数据
- `checkIfAppIsPaid()`: 检查应用是否需要支付（内部实现）
- `getPaymentStatus()`: 获取支付状态（内部实现）
- `getProductIDFromAppInfo()`: 从应用信息提取产品ID（内部实现）
- `getPaymentStateFromStore()`: 从内存或Redis获取PaymentState（内部实现）
- `savePaymentStateToStore()`: 保存PaymentState到内存和Redis（内部实现）
- `deletePaymentStateFromStore()`: 删除PaymentState（内部实现）
- `GetAllPaymentStates()`: 获取所有内存中的PaymentState（调试用）

**存储设计**：
- 内存缓存：使用 `paymentStateStore`（线程安全的 map）缓存 PaymentState
- Redis 持久化：使用 `payment:state:userid:appid:productid` 作为 Redis key
- 唯一性：使用 `userid:appid:productid` 作为唯一标识
- 获取策略：先查内存，再查 Redis，都没有则返回错误

注：内部实现方法（小写开头）在 `utils.go` 中，公共接口（大写开头）在 `api.go` 中

### state_machine.go
状态机核心实现（内部方法）：
- `getOrCreateState()`: 获取或创建状态（内部方法）
- `getState()`: 获取状态（内部方法）
- `updateState()`: 更新状态（内部方法）
- `processEvent()`: 处理事件并触发状态转换（内部方法）
- `handleStartPayment()`: 处理开始支付
- `handleSignatureSubmitted()`: 处理签名提交
- `handlePaymentCompleted()`: 处理支付完成
- `handleVCReceived()`: 处理VC接收
- `handleRequestSignature()`: 处理签名请求
- `requestVCFromDeveloper()`: 从开发者请求VC
- `pollForVCFromDeveloper()`: 轮询获取VC
- `storePurchaseInfo()`: 存储购买信息到Redis

### api.go
对外公共接口（兼容旧版API）：
- `InitStateMachine()`: 初始化状态机
- `GetStateMachine()`: 获取状态机实例
- `ProcessAppPaymentStatus()`: 处理应用支付状态（主入口）
- `StartPaymentProcess()`: 开始支付流程
- `RetryPaymentProcess()`: 重试支付流程
- `ProcessSignatureSubmission()`: 处理签名提交
- `StartPaymentPolling()`: 开始支付轮询
- `ListPaymentStates()`: 列出所有状态（调试用）
- `PreprocessAppPaymentData()`: 预处理应用支付数据（验证开发者 RSA 密钥，加载购买信息）

注：`GetPaymentState` 和 `SavePaymentState` 为包内部功能，不暴露在公共 API 中。状态机的所有方法都是内部方法，只能通过 `api.go` 中的公共接口访问。

**设计模式**：
- `api.go`: 提供公共接口（大写开头），供外部调用
- `utils.go`: 提供内部实现（小写开头），供 `api.go` 调用
- `state_machine.go`: 状态机内部实现（小写开头），仅供包内部使用
- 这样设计便于接口管理和实现细节分离

## 使用示例

### 初始化状态机

```go
// 创建状态机实例
sm := NewPaymentStateMachine(dataSender, settingsManager)

// 获取或创建状态
state, err := sm.GetOrCreateState(
    userID,
    appID,
    productID,
    appName,
    sourceID,
    developerName,
)
```

### 处理事件

```go
// 开始支付
err := sm.ProcessEvent(ctx, userID, appID, productID, "start_payment", nil)

// 签名提交
err := sm.ProcessEvent(ctx, userID, appID, productID, "signature_submitted", SignaturePayload{
    JWS:      jws,
    SignBody: signBody,
})

// 支付完成
err := sm.ProcessEvent(ctx, userID, appID, productID, "payment_completed", PaymentPayload{
    TxHash:        txHash,
    SystemChainID: systemChainID,
})
```

## 状态转换逻辑

### 典型流程

1. **初始状态**: `start_payment` 事件
   - 判断是否需要支付
   - 判断是否需要签名
   - 更新相关维度状态

2. **需要签名**: 触发 `request_signature` 事件
   - 通知LarePass进行签名
   - 更新签名状态

3. **签名完成**: `signature_submitted` 事件
   - 保存JWS和SignBody
   - 更新LarePassSync状态
   - 主动请求VC或等待支付

4. **支付完成**: `payment_completed` 事件
   - 保存TxHash和SystemChainID
   - 开始轮询VC

5. **收到VC**: `vc_received` 事件
   - 保存VC
   - 存储到Redis
   - 通知前端购买完成

## 与旧版本的主要区别

### 旧版本 (payment/pay.go)
- **流程式设计**: 按照步骤顺序执行
- **单一状态**: 只有 `not_sign`, `not_pay`, `purchased` 三个状态
- **耦合度高**: 状态和流程耦合在一起

### 新版本 (paymentnew/)
- **状态机式设计**: 基于多个维度独立管理状态
- **五维状态**: 每个维度独立管理，更加灵活
- **解耦设计**: 状态和业务逻辑分离
- **事件驱动**: 通过事件触发状态转换

## 待补充的业务逻辑

当前框架已搭建完成，但以下具体业务逻辑需要后续补充：

1. **状态转换规则**: `PaymentState.CanTransition()` 方法需要实现具体的状态转换规则
2. **业务判断逻辑**: `handleStartPayment()` 等方法中的 TODO 部分
3. **错误处理**: 各个事件处理函数的错误处理策略
4. **重试机制**: 轮询失败后的重试策略
5. **超时处理**: 各阶段超时的处理逻辑

## 下一步工作

1. 实现具体的状态转换规则
2. 完善各个事件处理函数的业务逻辑
3. 添加完善的错误处理和重试机制
4. 编写单元测试
5. 集成到现有系统中

