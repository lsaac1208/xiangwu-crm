# KXOne CRM

基于 Go 的模块化单体架构，实现线索从进入 CRM 到主管派发、销售领取、跟进填写的完整闭环。

## 架构设计

```
internal/
├─ domain/          # 核心领域（纯函数，无外部依赖）
│  ├─ lead/         # 线索状态机、领取逻辑
│  └─ card/         # 卡片生命周期管理
├─ adapter/         # 外部系统适配层
│  ├─ wecom/        # 企业微信 API 封装
│  ├─ repo/         # 数据库访问
│  └─ cache/        # Redis 缓存
├─ usecase/         # 业务用例编排（事务边界）
├─ transport/       # 传输层
│  ├─ http/         # REST API
│  └─ worker/       # 异步任务
```

## 快速开始

### 环境要求

- Go 1.25+
- Node.js 20.19+（前端构建与测试）
- MySQL 8.0+ 或 SQLite（开发环境）
- Redis 6.0+

### 安装 Go（macOS）

```bash
brew install go
```

### 初始化项目

```bash
# 下载依赖
go mod download

# 配置环境变量
cp .env.example .env
# 编辑 .env，设置数据库、JWT 和首次管理员账号

# 启动 Redis
brew services start redis

# 启动服务（SQLite 开发模式）
make dev
```

服务启动后访问 http://localhost:8080/health

详细配置指南请查看 [快速启动文档](./docs/快速启动.md)。

## 开发规范

### 模块职责

- **domain**：纯函数，状态转换逻辑，可独立测试
- **adapter**：封装外部依赖，提供接口抽象
- **usecase**：编排 domain + adapter，管理事务
- **transport**：HTTP/Worker 入口，参数校验

### 状态机设计

线索状态流转采用事件驱动：

```
Event: DispatchRequested  → State: pending_dispatch
Event: AssignedToSales    → State: assigned_pending_claim
Event: ClaimedBySales     → State: claimed
Event: FollowupSubmitted  → State: following
```

### 测试策略

- domain 层：纯函数单元测试（覆盖率 > 80%）
- usecase 层：mock adapter 的集成测试
- transport 层：端到端 API 测试

## 技术栈

- **框架**：Gin（HTTP）+ Asynq（异步任务）
- **数据库**：SQLite（当前生产）+ GORM；保留 MySQL 方言兼容
- **缓存**：Redis（幂等、Token、队列）
- **认证**：账号密码 + HttpOnly Cookie + JWT
- **提醒**：企业微信群机器人 Webhook

## 文档

- [需求文档](./docs/需求文档.md)
- [开发文档](./docs/开发文档.md)
- [技术设计补充](./docs/技术设计补充.md)
