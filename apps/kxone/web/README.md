# KXOne H5 前端

企业微信线索管理系统的移动端 H5 应用。

## 技术栈

- Vue 3 + Vite
- Vant UI 组件库
- Vue Router + Pinia
- Axios
- 企业微信 JSSDK

## 快速开始

```bash
# 安装依赖
npm install

# 启动开发服务器
npm run dev

# 构建生产版本
npm run build
```

开发服务器默认运行在 `http://127.0.0.1:5173`

## 项目结构

```
src/
├── api/              # API 接口封装
│   ├── index.js      # Axios 配置
│   ├── lead.js       # 线索相关接口
│   └── followup.js   # 跟进相关接口
├── components/       # 公共组件
│   ├── LeadCard.vue  # 线索卡片
│   └── StatusBadge.vue # 状态标签
├── views/            # 页面组件
│   ├── LeadList.vue    # 线索列表
│   ├── LeadDetail.vue  # 线索详情
│   └── AddFollowup.vue # 添加跟进
├── stores/           # Pinia 状态管理
│   └── user.js       # 用户状态
├── utils/            # 工具函数
│   ├── date.js       # 时间格式化
│   └── wecom.js      # 企业微信 SDK
├── router/           # 路由配置
├── assets/           # 静态资源
├── App.vue           # 根组件
└── main.js           # 入口文件
```

## 核心功能

- **线索列表**：支持状态筛选、下拉刷新、分页加载
- **线索详情**：展示客户信息、跟进记录、操作按钮
- **认领线索**：销售人员快速认领待分配线索
- **添加跟进**：记录跟进内容、更新线索状态
- **企业微信集成**：自动获取用户身份、原生体验

## API 代理配置

开发环境通过 Vite 代理转发 `/api` 请求到后端服务器（默认 `http://localhost:8080`）。

生产环境需在 Nginx 或网关层配置路由。