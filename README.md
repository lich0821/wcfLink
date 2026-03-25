# wcfLink

`wcfLink` 是一个本地运行的 iLink 微信通道桌面 App。当前版本已经跑通：

- 扫码登录
- 登录状态轮询
- 已登录账号持久化
- iLink `getupdates` 长轮询收消息
- 文本消息接收与落库
- 图片、语音、视频、文件接收与结构化展示
- 图片、语音、视频、文件自动下载到本地目录
- 文本消息发送
- 图片、视频、文件发送
- 回调地址配置
- WebHook 重试与死信
- 本地事件面板
- 桌面端测试发送框
- 本地 HTTP API
- SQLite 本地状态存储

当前仍未实现：

- 语音发送
- Pull 消费确认
- 订阅推送编排

## 技术形态

- 核心服务：Go
- 桌面壳：Wails
- 本地存储：SQLite
- 前端：Vite + Vanilla JS

## 目录

- 桌面入口：[main.go](/Users/chuck/Projs/WeChat/wcfLink/main.go)
- Wails 桥接：[app.go](/Users/chuck/Projs/WeChat/wcfLink/app.go)
- 后台入口：[main.go](/Users/chuck/Projs/WeChat/wcfLink/cmd/wcfLink/main.go)
- 应用装配：[app.go](/Users/chuck/Projs/WeChat/wcfLink/internal/app/app.go)
- iLink 客户端：[client.go](/Users/chuck/Projs/WeChat/wcfLink/internal/ilink/client.go)
- SQLite 存储：[store.go](/Users/chuck/Projs/WeChat/wcfLink/internal/store/store.go)
- HTTP API：[server.go](/Users/chuck/Projs/WeChat/wcfLink/internal/httpapi/server.go)
- 长轮询 worker：[poller.go](/Users/chuck/Projs/WeChat/wcfLink/internal/worker/poller.go)
- 桌面前端：[main.js](/Users/chuck/Projs/WeChat/wcfLink/frontend/src/main.js)

## 运行要求

开发和构建需要：

- Go `1.25+`
- Node.js `20+`
- `wails` CLI `v2`

运行桌面包时不依赖：

- Redis
- PostgreSQL
- 外部浏览器

## 快速开始

### 桌面版构建

安装前端依赖：

```bash
cd frontend
npm install
cd ..
```

构建桌面应用：

```bash
wails build
```

macOS 构建完成后，产物在：

```text
build/bin/wcfLink.app
```

### 桌面版开发运行

```bash
wails dev
```

当前桌面 UI 支持：

- 开始扫码登录
- 已登录态展示账号信息
- 本地退出登录
- 设置监听地址
- 设置回调地址
- 查看并手动重试 WebHook 死信
- 查看事件流
- 在事件流中查看图片、语音、视频、文件的结构化元数据
- 选择本地图片、视频、文件并直接发送
- 发送测试文本消息

### 后台模式构建

如果你只想运行本地 HTTP 服务：

```bash
go build -o ./bin/wcfLink ./cmd/wcfLink
./bin/wcfLink
```

默认监听地址：

```text
127.0.0.1:17890
```

默认状态目录：

```text
./bin/data/
```

默认数据库：

```text
./bin/data/wcfLink.db
```

## 桌面版使用

### 1. 登录

点击“开始扫码登录”后，桌面 App 会直接显示二维码。

扫码确认成功后：

- 账号信息会替换二维码区域
- “开始扫码登录”会禁用
- 可以直接在事件面板看到后续收发记录

### 2. 发送测试消息

右侧“发送测试”面板支持直接发文本：

- `账号 ID` 会在登录后自动填充
- `目标用户 ID` 可手动输入
- `消息内容` 输入后点击发送即可

如果当前联系人已有会话上下文，发送时会自动复用本地缓存的 `context_token`。

### 3. 退出登录

当前实现的是“本地断开”：

- 停止该账号轮询
- 删除本地账号与上下文
- UI 恢复到未登录状态

说明：

- 这不是远端 revoke token
- 当前公开 iLink 协议里没有确认到可用的官方 logout 接口

## HTTP API

桌面版不依赖浏览器，但本地 HTTP API 仍然保留给外部系统集成。

主要接口：

- `GET /health/live`
- `GET /health/ready`
- `POST /api/accounts/login/start`
- `GET /api/accounts/login/status`
- `GET /api/accounts/login/qr`
- `GET /api/accounts`
- `GET /api/events`
- `GET /api/webhooks/dead-letters`
- `GET /api/settings`
- `POST /api/settings`
- `POST /api/messages/send-text`
- `POST /api/messages/send-media`
- `POST /api/webhooks/dead-letters/{id}/retry`

说明：

- `GET /api/events` 中的每条入站事件现在会附带 `items` 数组
- `items` 会输出消息项类型，以及语音转写、文件名、媒体参数、本地下载路径等结构化字段
- 历史媒体事件在首次读取时会懒下载到本地目录

### 发送文本消息

```bash
curl -s -X POST http://127.0.0.1:17890/api/messages/send-text \
  -H 'Content-Type: application/json' \
  -d '{
    "account_id": "xxx@im.bot",
    "to_user_id": "yyy@im.wechat",
    "text": "你好"
  }'
```

## 配置

支持环境变量：

- `WCFLINK_LISTEN_ADDR`
- `WCFLINK_STATE_DIR`
- `WCFLINK_DB_PATH`
- `WCFLINK_BASE_URL`
- `WCFLINK_CDN_BASE_URL`
- `WCFLINK_CHANNEL_VERSION`
- `WCFLINK_MEDIA_DIR`
- `WCFLINK_POLL_TIMEOUT`
- `WCFLINK_LOG_LEVEL`

运行中通过 UI 或 API 保存的设置会写入：

```text
<state-dir>/settings.json
```

当前保存：

- `listen_addr`
- `webhook_url`

媒体文件默认保存到：

```text
<state-dir>/media/
```

## 验证状态

当前已经验证：

- `go build ./...`
- `go build -o ./bin/wcfLink ./cmd/wcfLink`
- `wails build`
- Wails 前端绑定生成
- 桌面包产物生成
- 扫码登录、文本接收、文本发送主链路已跑通
