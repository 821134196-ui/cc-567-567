# 厨房台面开孔尺寸复核与返工系统

测量员、工厂、安装员围绕客户确认的图纸协作：测量提交 → 客户签字确认 → 工厂按确认版加工 → 安装现场三方对比（确认尺寸 / 实际开孔 / 现场测量）→ 发现偏差先定方案、客户确认后再施工，费用与责任方单独记录，全程留痕。

## 启动（一条命令）

```bash
go run .
```

然后浏览器打开 **http://localhost:8080**。首次启动自动建库（`countertop.db`）并写入演示数据；测量图片保存在本地 `uploads/` 目录。

## 演示账号（密码均为 `123456`）

| 账号 | 姓名 | 岗位 | 能做什么 |
|---|---|---|---|
| `surveyor` | 王测 | 测量员 | 建订单、录初测、上传测量图片、重新测量（必填原因）、图纸换版、提交客户确认 |
| `factory` | 李厂 | 工厂 | 客户确认后按图纸加工，录入实际开孔尺寸 |
| `installer` | 赵装 | 安装员 | 录入现场测量、提出返工/变更方案、标记返工完成、安装验收 |
| `customer1`~`customer4` | 张女士/刘先生/王女士/陈先生 | 客户 | 签字确认图纸、审批返工/变更方案（仅限本人订单） |

## 内置演示数据（4 个订单）

| 订单 | 场景 | 看点 |
|---|---|---|
| ORD-2026-001 | **尺寸超差** | 水槽确认 760mm、实际开成 766mm（公差 ±2mm），返工方案待客户批准；未处理前不能验收 |
| ORD-2026-002 | **客户尚未确认** | 图纸已提交待确认，工厂点加工会被拒绝；含一条"重新测量"前后值记录 |
| ORD-2026-003 | **加工后换版** | 已按 v1 加工，之后 v2（水槽右移 20mm）已确认但**不自动套用**，对比基准仍锁 v1 |
| ORD-2026-004 | **返工完成** | 超差 → 提方案 → 客户批准 → 返工完成 → 验收关闭，全流程日志可查 |

## 关键业务规则

- **公差 ±2mm**：确认 vs 实际、确认 vs 现场任一项超差即标红，订单不能验收。
- **先方案后施工**：存在超差时，必须由安装员提出返工/变更方案 → 客户批准 → 执行完成，才能验收；费用与责任方随方案单独记录。
- **版本锁定**：加工以"客户确认版"为准并锁定；加工后再确认的新版本不会自动套用到已加工台面（订单 003 可见警告条）。
- **复测留痕**：同一开孔重新测量保留前后数值与原因（订单 002 可见）。
- **岗位隔离**：接口按角色鉴权，越权返回 403；客户只能操作本人订单。

## 技术栈与结构

- 后端：Go + Gin + GORM + SQLite（纯 Go 驱动 `glebarez/sqlite`，无需 CGO）
- 前端：Vue 3（本地 `static/vue.global.prod.js`，无构建步骤）
- `main.go` 路由 · `models.go` 模型 · `auth.go` 登录与角色中间件 · `handlers.go` 业务规则 · `seed.go` 演示数据 · `static/` 前端

## 主要 API

```
POST /api/login                       登录
GET  /api/orders                      订单列表
GET  /api/orders/:id                  订单详情（含三方对比 comparison）
POST /api/orders                      [测量员] 建订单+初测
POST /api/orders/:id/drawings         [测量员] 图纸换版
PUT  /api/cutouts/:id                 [测量员] 重新测量（必填原因）
POST /api/cutouts/:id/images          [测量员] 上传测量图片
POST /api/drawings/:id/submit         [测量员] 提交客户确认
POST /api/drawings/:id/confirm        [客户]   签字确认（已加工则不套用）
POST /api/orders/:id/fabricate        [工厂]   加工（未确认会被拒）
POST /api/orders/:id/field-measurements [安装员] 现场测量
POST /api/orders/:id/rework-plans     [安装员] 提出返工/变更方案
POST /api/rework-plans/:id/decide     [客户]   批准/拒绝方案
POST /api/rework-plans/:id/complete   [安装员] 返工完成
POST /api/orders/:id/complete         [安装员] 安装验收（超差未处理会被拒）
```
