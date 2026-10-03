# 厨房台面开孔尺寸复核与返工系统

测量员、工厂、安装人员围绕**客户确认的尺寸图**协作：客户签字后工厂才加工；安装前三方尺寸
并排复核；发现超差先走返工/变更审批，**客户确认后才允许改孔**；费用与责任方单独留痕。

- 后端：Go + Gin + GORM，SQLite（纯 Go 驱动 modernc.org/sqlite，免 CGO）
- 前端：Vue 3（本地 vendor，免构建、免 npm install）
- 图片：multipart 上传，保存在本地 `uploads/`
- 前端通过 `go:embed` 打入二进制，部署只需一个可执行文件

## 一条命令本地启动

```bash
./run.sh
```

脚本会自动准备 Go（本机没有则下载到 `~/go-sdk`）、拉依赖、编译、写入演示数据并启动：

- 地址：http://localhost:8080
- 自定义端口：`PORT=9000 ./run.sh`
- 保留数据重启：`./run.sh --keep`

## 演示账号

| 岗位 | 用户名 | 密码 | 可执行的操作 |
|---|---|---|---|
| 测量员 | `surveyor` | `sur12345` | 建工单、初测、出图/换版、提交客户、复测（保留前后值与原因） |
| 工厂 | `factory` | `fac12345` | 按客户确认版本开工、登记实际开孔并完工、执行**已获客户确认**的返工 |
| 安装人员 | `installer` | `ins12345` | 现场测量（自动判超差）、提返工/变更方案、返工后复核、安装 |
| 客户 | `customer` | `cus12345` | 签字确认图纸、确认/驳回返工方案、最终验收 |

## 四个内置例子（`-seed`）

| 工单 | 场景 | 可核对的规则 |
|---|---|---|
| `O-DEV-01` | 水槽现场实测 765 vs 确认 760（公差 ±2），**超差**待处理 | 未闭环禁止安装；方案未确认工厂不能改孔 |
| `O-NEW-02` | 图纸已提交但**客户尚未确认**，含水槽复测（750×440 → 760×450，附原因） | 未确认开工返回 409；复测前后值与原因保留 |
| `O-VER-03` | 已按 V1 加工完成，测量员试图出 V2（灶具 650→700） | 换版返回 409，新版本不落库，已加工台面不自动套用新尺寸 |
| `O-RWK-04` | 灶具超差 → 方案 → 客户确认 → 工厂返工 → 复核 → 安装 → **已验收** | 全程留痕：确认记录、图纸版本、返工记录、费用责任方 |

## 核对版本与权限处理

```bash
./run.sh                 # 终端 A：启动
./verify.sh              # 终端 B：43 项自动化核对（默认 http://localhost:8080）
```

核对内容包括：未登录 401；跨岗位越权（开工/现场测量/出图/确认/返工/验收/费用）403；
客户未确认不能加工（409）；加工后换版被拦截且不产生新版本（409）；返工未经客户确认
工厂不能执行（409）；复测前后数值与原因保留；返工链闭环后才能安装与验收；图片本地可访问。

## 关键业务规则实现位置

- 客户确认才是加工基线、确认时锁定 `current_version_id`：`handlers_design.go` (`confirmVersion`)
- 未确认禁止开工：`handlers_flow.go` (`startProduction`)
- 加工后禁止换版：`handlers_design.go` (`createVersion` 前置守卫)
- 完工锁定加工版本 `produced_version_id`：`handlers_flow.go` (`finishProduction`)
- 现场测量自动判超差并锁定工单：`handlers_flow.go` (`submitSiteMeasurement`)
- 返工方案必须客户确认工厂才能执行：`handlers_flow.go` (`executeRework`)
- 复测保留前后数值与原因：`handlers_design.go` (`remeasureOpening`)
- 角色权限中间件：`db.go` (`rolesRequired`)，路由分组：`api.go`

## 主要接口

```
POST /api/login
POST /api/uploads                                  图片本地保存
POST /api/orders                                   [测量员]
POST /api/orders/:id/measurements                  [测量员] 初测
POST /api/orders/:id/versions                      [测量员] 出图/换版（已加工 409）
POST /api/versions/:vid/submit|confirm|reject      提交[测量员] / 确认驳回[客户]
POST /api/orders/:id/openings/:slot/remeasure      [测量员] 复测（前后值+原因）
POST /api/orders/:id/production/start|finish       [工厂]
POST /api/orders/:id/site-measurements             [安装人员] 三方对比自动判超差
POST /api/deviations/:did/propose|resolve          [安装人员] 提方案 / 复核闭环
POST /api/reworks/:rid/confirm|reject              [客户]
POST /api/reworks/:rid/execute                     [工厂]（需客户已确认）
POST /api/orders/:id/costs                         [三岗位] 费用与责任方
POST /api/orders/:id/install|accept                安装[安装人员] / 验收[客户]
GET  /api/orders  /api/orders/:id                  工单列表与详情（含时间线）
```

## 数据与文件

- `countertop.db`：SQLite 数据库（AutoMigrate 自动建表）
- `uploads/`：测量图片本地目录，通过 `/uploads/xxx` 访问
- 重新初始化演示数据：`./countertop-app -reset -seed`
