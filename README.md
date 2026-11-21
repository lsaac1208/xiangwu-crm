# 襄五独立 CRM（xiangwu-crm）

面向招生/销售团队的线索管理系统：线索从广告平台、官网表单或手工录入进入 CRM，经主管派发、邀约师/谈单师领取与跟进，直至转化或关闭，并提供团队与渠道统计看板。

> 本仓库是脱敏后的公开快照：已移除生产服务器地址、域名、运维证据、真实人员姓名与历史提交记录；示例域名使用 `example.com`，示例 IP 使用文档保留段 `203.0.113.x`。

## 功能概览

- **线索全流程**：录入、去重、分配、领取、转派、跟进、预约、公海与回收。
- **角色权限**：管理员、主管、调度员、邀约师、谈单师。
- **平台接入**：百度、腾讯、小红书、抖音（飞鱼）线索回调，支持按账户校验签名；官网表单与统一网关入口。
- **来源渠道**：渠道来源可配置并在新建线索时必选。
- **统计看板**：团队、渠道与跟进监督视图。
- **通知**：新线索通过企业微信群机器人提醒。
- **账号安全**：账号密码登录，认证信息通过 HttpOnly Cookie 承载；敏感字段加密存储。

## 目录

```
apps/kxone/        Go 后端（API、worker）与 CRM Web
apps/kxone/web/    Vue 前端
apps/kxone/docs/   需求、设计与维护文档
contracts/         线索网关接口契约
deploy/            Nginx / systemd / 环境变量示例
scripts/           开发、备份与健康检查脚本
```

## 本地开发

环境：Go（见 `apps/kxone/go.mod`）、Node.js 20.19+、Redis。

```bash
cp .env.example .env     # 按需修改密钥与初始管理员账号
make install
make check               # Go 测试 + 前端单元测试
make build
```

首次启动使用 `CRM_BOOTSTRAP_USERNAME` / `CRM_BOOTSTRAP_PASSWORD` 创建管理员。更多配置见 [apps/kxone/README.md](apps/kxone/README.md) 与 [apps/kxone/docs](apps/kxone/docs)。

## 说明

- 所有密钥、令牌均为占位符，部署前必须替换。
- 浏览器视觉回归基线图未随快照公开，首次运行 Playwright 视觉用例需自行生成基线。
- 尚未选择开源许可证；使用或再分发前请先与仓库所有者确认。
