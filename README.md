<p align="center">
  <img src="https://raw.githubusercontent.com/DGZSbot/ai-icon/refs/heads/main/WorkBuddy.png" alt="WorkBuddy2API" width="120">
</p>

<h1 align="center">WorkBuddy2API</h1>

<p align="center">
  <b>把 CodeBuddy 账号变成 OpenAI 兼容 API 的多账号网关</b><br>
  OAuth 登录 · 账号池轮转 · 熔断与冷却 · 会话粘性 · 积分补充
</p>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.22.5-00ADD8?logo=go&logoColor=white&style=flat-square">
  <img alt="API" src="https://img.shields.io/badge/API-OpenAI_Compatible-412991?style=flat-square">
  <img alt="Deploy" src="https://img.shields.io/badge/Deploy-Docker_Compose-2496ED?logo=docker&logoColor=white&style=flat-square">
  <img alt="Transport" src="https://img.shields.io/badge/Transport-SSE%20%2F%20Streaming-0DBD8B?style=flat-square">
  <a href="https://t.me/sliverkiss_blog"><img alt="Telegram" src="https://img.shields.io/badge/Telegram-%E9%A2%91%E9%81%93-blue?logo=telegram&logoColor=white&style=flat-square"></a>
</p>

---

## 项目简介

WorkBuddy2API 是一个自托管的 **OpenAI 兼容反向代理网关**，将 ```CodeBuddy``` 账号包装为统一的 `/v1/chat/completions` 服务。

- 官方不提供 OpenAI 形态的开放 API，本项目通过 **OAuth 设备授权**（`login.sh`）获取账号凭证，在网关侧做 token 自动刷新、账号池调度与流量治理；
- 面向 **个人多账号** 场景：多账号共享、单号故障自动换号、冷却 / 熔断防止雪崩、会话粘性保证多轮上下文不跳号；
- 对客户端只暴露 OpenAI 兼容接口，现有 SDK / 前端 / 工具 **零改造接入**。

> ⚠️ 合规须知：本项目是**非官方**网关，使用 ```CodeBuddy``` 账号作为上游，**仅限本人授权账号、本机 / 私有环境测试**。详细边界见[安全与合规](#安全与合规)。

📖 完整文档见 [GitHub Wiki](https://github.com/Sliverkiss/workbuddy2api/wiki)。

## 核心能力

### 账号池治理

- **OAuth 设备授权登录** — `login.sh` 一条命令完成：取授权 URL → 浏览器登录 → token 轮询 → 凭证落盘 → 重启加载，全程无 PKCE（state 由服务端签发），重复执行即可连续添加多账号
- **四因子加权随机选号** — `credits 比例 ×10 + 闲置补偿 + 成功率 ×3 + 快过期积分占比 ×8` 四项加权（`pool.expiring_soon` 窗口内的积分优先消耗，默认 7 天），按权重降序取 **Top-5 候选短名单**，再在短名单内加权抽签（等权重候选先随机打乱防惊群、LRU 兜底覆盖全部候选），兼顾积分多、闲置久、成功率高、快过期积分先用掉的账号
- **防惊群** — 跳过 100ms 内刚被选中的账号，多账号同时待命时不打爆同一台
- **在途租约** — 单账号最大在途请求数（`pool.max_in_flight`）限制并发占用，占满的号不参与选号，避免单号过载
- **账本择优** — 每次成功请求按 `usage.credit` 折算每千 token 单价记入 `(账号, 模型)` 账本，免费 / 便宜的账号优先；观测按 EMA 平滑、6 小时未更新即失效，成本随上游活动实时变化

### 流量治理

- **分级熔断与冷却** — 429 软冷却（600s 起指数退避、封顶 `soft_rate_max`）、404 固定浅冷却、402 / 余额耗尽硬冷却至次日 04:00、连续失败熔断（`breaker_threshold` 触发后指数退避封顶 6h）
- **模型级限流独立冷却** — 6004（该模型使用量超限）只冷却触发调用的模型，切其他模型立即可用；`/status` 透出 `rate_limited_models` 台账
- **状态持久化** — 池状态（积分 / 冷却 / 熔断 / 计数）本地原子落盘 `state.json`，可选镜像至 Upstash Redis，重启后择优恢复

### 请求链路

- **流式 + 非流式** — 出站强制 `stream:true`；SSE 帧按 OpenAI 规范白名单重建；非流式由本地聚合为单响应
- **DeepSeek 思维链注入** — 出站请求体注入 `thinking.type=enabled` + 默认档位，`reasoning_content` 多轮回填，`reasoning_effort` 按模型档位自动降级
- **系统提示词体系** — 默认透传客户端原始 system（`passthrough` 模式，缺省），仅自定义配置 `custom` 时网关用自有提示词替换客户端 system/developer（从源头消除模板句误报）；`passthrough` 模式遇拦截自动降级中性提示词重试
- **会话头族注入** — 出站携带官方客户端会话头族（`X-Conversation-Request-ID` 聚合主键 · `X-Conversation-ID` 透传 · B3 链路），轮转 / 重试 / 路径回退复用同键，后台按对话轮聚合不再碎片化（issue #35）
- **指纹脱敏** — 出站请求体黑名单指纹字段清洗（可开关），与提示词体系两层叠加

### 定时积分任务

- **签到**（09 / 21 点）— 每日签到 + 余额查询，余额恢复自动解冻冷却账号
- **活跃上报**（10 点）— 对话事件连发上报，点亮连登天数、解锁领养前置，回读 streak 自检
- **猫猫旅行**（09 / 21 点）— 独立排程：领养 / 派出 / 领奖闭环推进
- **token 保活**（22 点）— 全账号刷新 token，session 失效连续 3 次才禁用
- **开学季任务**（12 点）— 任务点亮 + claim + 自动抽空抽奖余额，活动下线时自动跳过
- **夜猫子任务**（01 点）— 夜猫窗口（23:00–08:00 CST）内补一次 black_cat 任务

六类任务独立排程、独立开关（`schedule.*_enabled`），互不影响。

### 双域适配

- 同时适配**国内版（CN，`copilot.tencent.com` / `www.codebuddy.cn`）与国际版（Global，`www.workbuddy.ai`）**账号
- 共享同一账号池，由账号 `realm` 或请求模型名前缀（`cn:` / `global:`）决定路由；`global.enabled` 可一键锁死纯 CN 部署
- 国际版支持注册激活、地区完善、一次性 trial 加油包领取（`./trial.sh`）

### 辅助工具

- 积分日报：`./credit.sh`（美化 / `-json`，realm 感知双域）
- 手动签到：`./signin.sh`（批量、幂等不重复计）
- 领养联动 / 任务查询：`scripts/task_runner.py`（成长任务一体机，默认 dry-run）
- 个性化提示词：`prompt.file` 指向自定义提示词文件即整体替换内置默认

## 架构总览

```mermaid
flowchart LR
    Client["客户端 / SDK\nOpenAI 兼容请求"] --> H

    subgraph GWI["WorkBuddy2API 网关 :7863"]
        H["HTTP Handler\n鉴权 · 请求体上限 · 提示词改写 · 轮转"] --> P
        H --> S
        P["账号池\n三因子加权 · 熔断 · 冷却 · 租约"] --> U
        S["会话粘性路由"] -.绑定镜像.-> REDIS
        T["定时调度\n签到 09/21 · 旅行 09/21 · 活跃 10 · 保活 22"] --> P
        U["上游 Client\nChatHTTP 流式 · 短 RPC"]
    end

    P -. "读凭证 (0600)" .-> AUTH[("auths/*.json")]
    P -. "状态镜像" .-> REDIS[("Upstash Redis\n可选")]
    U -->|"chat/completions (SSE)"| CB["CodeBuddy\ncopilot.tencent.com"]
    U -->|"billing / auth / growth"| CB
```

上游请求在出站前经历统一的改写管线（`internal/upstream/payload.go`）：强制 `stream:true`、`developer` 角色归一、tool_choice 归一、DeepSeek 思维链注入、`reasoning_effort` 档位降级、`reasoning_content` 回填、指纹脱敏。

## 快速开始

### 环境要求

- **Docker + Docker Compose**（推荐部署方式，镜像内已含 `app` 低权限用户与全部工具脚本）
- 一个或多个已注册的 CodeBuddy 账号，用于 OAuth 登录
- 宿主机 Go ≥ 1.22（仅源码构建时需要）

### Docker Compose 一键部署

```bash
git clone https://github.com/Sliverkiss/workbuddy2api.git
cd workbuddy2api
cp config.example.json config.json
```

编辑 `config.json`，**至少设置 `api_key`**（`留空 = 不鉴权`，公网部署务必设置）。示例中的 `test_key` 等均为占位符，`config.example.json` 不含任何真实密钥。

```bash
# 登录添加账号（重复执行可加多号）
./login.sh

# 启动服务
docker compose up -d --build

# 健康检查（无可用账号时 503）；service 字段用于确认打到的是本网关
curl -s http://localhost:7863/healthz
# {"healthy":2,"total":3,"service":"workbuddy2api"}
```

`login.sh` 内置授权 URL 获取 + 浏览器登录 + token 轮询 + 首次签到 + `auths/workbuddy-<uid>.json` 落盘 + 容器重启，全程无 PKCE（state 由服务端签发）。账号池在容器启动时用 `auths/` 目录自动对齐，新增凭证文件即自动发现。

### 源码构建

```bash
go build ./...
go vet ./...
go test ./...      # 完整测试套件
go run ./cmd/server -config config.json
```

构建二进制：

```bash
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o wb2api ./cmd/server
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o signin_bin ./cmd/signin
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o login ./cmd/login
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o credit ./cmd/credit
```

### 启动网关（二进制）

`go build` 产物是 `wb2api`（Windows 下为 `wb2api.exe`）。它**必须从项目根目录启动**——会读取当前工作目录下的 `config.json`（用 `-config <路径>` 可换位置）与 `auths/` 凭证目录，默认监听 `:7863`。原生二进制**不热加载**凭证，新增 / 更换账号后需重启网关（见下文「登录」）。

**前台启动**（占住当前终端，Ctrl+C 停止）：

```bash
# Git Bash / Linux
cd /path/to/workbuddy2api
./wb2api                 # 等价于 ./wb2api -config config.json
```

```powershell
# PowerShell（Windows，若 Git Bash 构建产物不带 .exe 后缀则去掉 .exe）
cd D:\path\to\workbuddy2api
.\wb2api.exe
```

**后台启动（推荐，落日志便于排查）**：

```bash
# Git Bash
cd /path/to/workbuddy2api
./wb2api > wb2api.log 2>&1 &
```

```powershell
# PowerShell：Start-Process 非阻塞启动，stdout/stderr 分别落盘
cd D:\path\to\workbuddy2api
Start-Process -FilePath ".\wb2api.exe" -RedirectStandardOutput "wb2api.log" -RedirectStandardError "wb2api.err"
```

**验证启动成功**：

```bash
# 账号池状态（需鉴权，healthy > 0 即账号已加载）
curl -s http://localhost:7863/status -H "Authorization: Bearer your-api-key"
# 或免鉴权的健康检查
curl -s http://localhost:7863/healthz
```

**停止服务**：

```powershell
Stop-Process -Name wb2api          # PowerShell
```
```bash
pkill -f wb2api                     # Git Bash / Linux
```

> ⚠️ `wb2api` 是常驻前台进程：**关闭终端、注销、重启电脑都会让它退出**。进程意外退出后直接重跑上面的启动命令即可，无需重新登录；除非 `/status` 中账号变为 `disabled`（此时需按「登录」重新授权并重启）。

### Windows 下运行工具（无 Linux 环境）

仓库里的 `*.sh` 是 bash 脚本，Windows 无法直接运行；它们只是对仓库内 Go 原生二进制（`login` / `signin_bin` / `credit`）的封装。**在 Windows 上直接用对应的 `.exe` 即可**——`go build` 在 Windows 下产物自带 `.exe` 后缀。PowerShell 中 `curl` 是 `Invoke-WebRequest` 的别名，但 `login.exe` 等是程序本体，正常调用即可。

> 注：若你在 Git Bash 下构建，产物可能不带 `.exe` 后缀，命令里去掉 `.exe` 即可（如 `./login`、`signin_bin`）。

**登录（添加账号）** —— 分两步，中间需你在浏览器完成扫码 / 授权：

```powershell
# 1) 取授权链接（realm 可选 cn / global；不传回落 cn）
login.exe --realm=cn url
#   ↓ 复制终端打印的 https://copilot.tencent.com/login?... 链接，浏览器打开并完成登录

# 2) 登录完成后，轮询 token 并落盘为 auths/workbuddy-<uid>.json（格式与网关读取一致）
#    注意：Windows PowerShell 5.1 默认按系统 OEM 码页（中文系统为 GBK）解码外部程序
#    stdout，而 login.exe 输出 UTF-8，需先切到 UTF-8，否则中文 nickname 会乱码。
$prevEnc = [Console]::OutputEncoding
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new()
$json = login.exe --realm=cn poll | ConvertFrom-Json
[Console]::OutputEncoding = $prevEnc
$uid  = $json.uid
$exp  = [int][datetime]::UtcNow.Subtract([datetime]'1970-01-01').TotalSeconds + [int]$json.expires_in
$auth = @{
    account = @{ uid = $uid; enterpriseId = $json.enterprise_id; nickname = $json.nickname }
    auth    = @{
        accessToken  = $json.access_token
        refreshToken = $json.refresh_token
        expiresAt    = $exp
        domain       = $json.domain
        realm        = 'cn'
    }
} | ConvertTo-Json -Depth 5
New-Item -ItemType Directory -Force -Path auths | Out-Null
[System.IO.File]::WriteAllText("auths/workbuddy-$uid.json", $auth, [System.Text.UTF8Encoding]::new($false))
Write-Host "已保存 auths/workbuddy-$uid.json"
```

> 登录后**必须重启网关**才能加载新账号：原生二进制不热加载凭证（仅在启动时扫描 `auths/`）。停掉 `wb2api` 进程后重新 `.\wb2api.exe -config config.json` 即可，无需像脚本那样 `docker restart`。CN 首次签到可省略，网关内置 09 / 21 点定时签到。

**签到（批量）** —— 遍历 `auths/` 下所有账号执行签到：

```powershell
signin_bin.exe            # 默认目录 auths
# 或指定目录：signin_bin.exe D:\path\to\auths
```

**积分日报** —— 查看各账号积分：

```powershell
credit.exe -pretty       # 人类可读日报（默认）
credit.exe               # 原始 JSON
```

### 验证

```bash
# 模型列表
curl -s http://localhost:7863/v1/models -H "Authorization: Bearer your-api-key"

# 账号状态（汇总 + 每账号详情，disabled 账号透出 disabled_reason）
curl -s http://localhost:7863/status -H "Authorization: Bearer your-api-key"

# 流式聊天
curl -sN http://localhost:7863/v1/chat/completions \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"model":"deepseek-v4.1-flash","messages":[{"role":"user","content":"hi"}],"stream":true}'

# 非流式聊天（本地聚合）
curl -s http://localhost:7863/v1/chat/completions \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"model":"deepseek-v4.1-flash","messages":[{"role":"user","content":"hi"}],"stream":false}'

> **Windows 提示**：PowerShell 中 `curl` 是 `Invoke-WebRequest` 的别名，**必须用 `curl.exe`** 调用真正的 curl 程序；Windows 10 / 11 已内置 curl（cmd 与 PowerShell 均可直接用）。下面给出与上方等价的 Windows 写法。

**CMD（命令提示符）** —— 行尾用 `^` 续行；JSON 内的双引号用 `\"` 转义：

```cmd
:: 流式聊天
curl -sN http://localhost:7863/v1/chat/completions ^
  -H "Authorization: Bearer your-api-key" ^
  -H "Content-Type: application/json" ^
  -d "{\"model\":\"deepseek-v4.1-flash\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}],\"stream\":true}"

:: 非流式聊天（本地聚合）
curl -s http://localhost:7863/v1/chat/completions ^
  -H "Authorization: Bearer your-api-key" ^
  -H "Content-Type: application/json" ^
  -d "{\"model\":\"deepseek-v4.1-flash\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}],\"stream\":false}"
```

**PowerShell** —— **不能**像 bash 那样用 `-d '{...}'` 单引号直接包 JSON 传给 curl。PowerShell 5.1 会把 JSON 开头 `{` 紧跟的前双引号吞掉，导致服务端收到 `{model...` 而报 `invalid character 'm'`（错误码 11101）。两种可靠写法：

写法 A（推荐，零转义 + 多行不依赖续行符）—— 先把 JSON 写成**无 BOM 的 UTF-8** 文件，再用 `-d @文件` 让 curl 读取；多行参数用 PowerShell 数组（**splatting**，`@(...)`）传递，**不依赖反引号续行，复制粘贴混进尾随空格也不会断行**：

```powershell
# 流式聊天
$json = '{"model":"deepseek-v4.1-flash","messages":[{"role":"user","content":"hi"}],"stream":true}'
[System.IO.File]::WriteAllText("$PWD\body.json", $json, [System.Text.UTF8Encoding]::new($false))
$argList = @(
  '-sN'
  'http://localhost:7863/v1/chat/completions'
  '-H', 'Authorization: Bearer your-api-key'
  '-H', 'Content-Type: application/json'
  '-d', "@$PWD\body.json"
)
curl.exe @argList

# 非流式聊天（本地聚合）
$json = '{"model":"deepseek-v4.1-flash","messages":[{"role":"user","content":"hi"}],"stream":false}'
[System.IO.File]::WriteAllText("$PWD\body.json", $json, [System.Text.UTF8Encoding]::new($false))
$argList = @(
  '-s'
  'http://localhost:7863/v1/chat/completions'
  '-H', 'Authorization: Bearer your-api-key'
  '-H', 'Content-Type: application/json'
  '-d', "@$PWD\body.json"
)
curl.exe @argList
```

写法 B（单行，类似 CMD）—— 在 `curl.exe` 后加 `--%` 停止解析，后续引号按 CMD 风格用 `\"` 转义：

```powershell
# 流式聊天
curl.exe --% -sN http://localhost:7863/v1/chat/completions -H "Authorization: Bearer your-api-key" -H "Content-Type: application/json" -d "{\"model\":\"deepseek-v4.1-flash\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}],\"stream\":true}"

# 非流式聊天（本地聚合）
curl.exe --% -s http://localhost:7863/v1/chat/completions -H "Authorization: Bearer your-api-key" -H "Content-Type: application/json" -d "{\"model\":\"deepseek-v4.1-flash\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}],\"stream\":false}"
```


## 安全与合规

### 发布来源与合规边界

- **无预编译 release**：仓库无 Release / tag，产物 = 源码自构建（Dockerfile 多阶段在本地构建时完成）
- 登录 / 签到 / 积分工具：`./login.sh` / `./signin.sh` / `./credit.sh`（Linux / 容器环境）。这三个脚本均为 Go 原生二进制（`login` / `signin_bin` / `credit`）的薄封装，**Windows 无 Linux 环境时无需跑 `.sh`，直接调用对应的 `.exe` 即可**，详见下方「Windows 下运行工具（无 Linux 环境）」。
- **无产物校验和**：`go.sum` 仅约束 Go 模块依赖；Docker 镜像由本地 `docker compose build` 生成，未引用第三方镜像
- 上游 CodeBuddy 属第三方商业产品，本项目是其**非官方 OpenAI 兼容网关**；使用其账号做 API 网关涉及目标平台服务条款与账号风险，作者不对账号封禁、条款违约或使用结果负责

### 授权使用边界

- 仅限**本人授权账号**、本机 / 私有环境测试
- 不得共享、转售、违规分发，或用于违反目标平台条款的用途
- 遵守 CodeBuddy 平台服务条款与所在地法律
- 妥善保管 `auths/`（明文凭证）与网关端口

## 免责声明

本项目（包括但不限于代码、脚本、文档、配置示例及仓库内任何资源，下称「本项目内容」）**仅供个人学习与研究使用**。使用本项目表示您已阅读并接受本声明全部条款；如不同意，请立即停止使用并删除全部相关内容。

**1. 用途限制。** 本项目内容仅可用于个人学习、研究等非商业用途；请勿将本项目用于任何商业目的或牟利行为，请勿违反所属国家 / 地区 / 组织的任何法律法规。本项目不构成对任何软件、服务、平台的使用建议或授权。

**2. 账号与数据责任。** 本项目可能涉及个人账号凭证的获取、存储与使用。您应仅使用本人持有且已获授权的账号，自行确认相关平台的服务条款与允许范围，并自行承担使用、存储凭证（如 `auths/` 中的文件）及调用上游服务所产生的全部责任与风险。本项目不参与、不介入您与任何平台之间的契约关系。

**3. 内容与第三方界限。** 本项目内容中引用的第三方产品、服务、LOGO、图片、文案等，其权利均归各自权利人所有；本项目不保证此类内容的准确性、完整性、合法性，亦不代表支持或推荐任何第三方。如实存在侵权情形，请通过 Issues 告知，经核实后本项目会尽快处理。

**4. 无担保与风险自担。** 本项目内容按「现状」提供，不附带任何明示或默示的担保（包括但不限于适销性、特定用途适用性、准确性、不侵权等）。使用本项目（包括直接或间接）所产生的任何风险与后果（包括但不限于账号异常、数据丢失、服务中断、纠纷或损失），均由使用者自行承担，与本项目及其全部贡献者无关。

**5. 责任限定。** 在任何情况下，本项目及其作者、贡献者均不对任何直接、间接、偶然、特殊或后果性损害承担责任，无论该等损害是否基于合同、侵权或其他法律理论，即使已被告知发生该等损害的可能性。

**6. 修改与分发。** 基于本项目源代码进行的任何修改、衍生均系第三方自发行为，与本项目无关，相应后果由该第三方自行承担。本项目内所有资源文件，禁止任何公众号、自媒体进行任何形式的转载、发布。未经授权，任何组织或个人不得将本项目内容用于转载、发布或再分发。

**7. 条款变更。** 本项目保留随时修改、补充本声明的权利。修改后的声明自发布之日起生效，继续使用本项目即视为接受修订后的声明。本项目所有内容仅供学习和研究使用，请于学习研究完成后及时删除。

## ☕ Coffee

如果这个项目对你有帮助，欢迎请我喝杯咖啡～

<table>
  <tr>
    <td align="center"><b>💰 Solana</b></td>
    <td><code>AZAKF74rTu7UFVSNRzsKV4HHpTwarax6cG8KAh4fP5rQ</code></td>
  </tr>
  <tr>
    <td align="center"><b>💎 Ethereum</b></td>
    <td><code>0x1d418627aD6B043900CBE11fe439759bDF2b5170</code></td>
  </tr>
  <tr>
    <td align="center"><b>₿ Bitcoin</b></td>
    <td><code>bc1q9w7h4j9msyd9q6lhl0398n4s3g8h4vchpqvc2k</code></td>
  </tr>
</table>

## License

本项目采用 [MIT License](LICENSE) 开源协议。

- 在遵守 MIT License 前提下，允许使用、复制、修改、合并本项目源代码
- 再分发（源码或二进制形式）时，须保留原仓库的 MIT 版权声明与许可声明，并在 NOTICE 或 README 中注明原始出处 `https://github.com/Sliverkiss/workbuddy2api`
- 本项目不授予任何上游（CodeBuddy）接口或服务的权利；使用者仍需自行遵守上游服务条款
- 本项目的使用同时受上方**免责声明**约束；如免责声明与 MIT License 存在不一致，以免责声明为准
