# 极性轨迹 · 古地磁退磁离线工作台

面向定向标本逐级退磁数据（交流 AF 或热退磁 TH）的本地离线分析台。研究者在
Zijderveld 正交投影与等角度球面投影上检查三分量，选择**连续处理窗、原点约束
与加权方式**，对窗内向量做特征分解（Kirschvink PCA），同时**并排保留多个
候选窗**——任何一次筛选都不会被当作唯一解释。

- 语言/运行时：Go（标准库 `net/http` + `encoding/json`，无前端构建链）
- 存储：SQLite（纯 Go 驱动 `modernc.org/sqlite`，无需 CGO）
- 图形：服务端直接生成 SVG
- 页面：原生 HTML/CSS/JS，静态资源通过 `go:embed` 打包进单一二进制

## 快速开始

```sh
go mod download
go test ./... -count=1
go run ./cmd/server --listen 127.0.0.1:5500
```

浏览器打开 <http://127.0.0.1:5500>，页眉应显示 **“极性轨迹”**。首次启动在数据库
为空时自动导入三条固定 fixture（共 32 个测量级次）。数据库文件默认
`./paleobench.db`，可用 `--db` 指定；`--auto-import=false` 可关闭自动导入。

## 数据口径

每个级次（`steps` 一行）保存：

| 字段 | 含义 |
| --- | --- |
| `seq` | 测量顺序（1 起），窗口与排除均按 seq 引用 |
| `kind`,`level` | `AF`（峰值场，mT）或 `TH`（温度，°C） |
| `x,y,z` | 三分量磁矩，**标本测量坐标系**，单位以 fixture 为准 |
| `cov_xx..cov_zz` | 测量协方差（3×3 对称，标本系），用于加权与诊断 |
| `azimuth/plunge/roll` | 该级次的装样姿态（见下），允许逐级不同 |
| `note` | 备注，例如复测批次 |

- **重复处理强度不合并**：同一 `kind+level` 的多行是独立复测，全部进入特征
  分解；若同级复测向量相对平均磁矩差异超过 20%，给出 `DUPLICATE_ANOMALOUS_LOAD`
  警告（fixture DUP-1 的 40 mT 两级相差约 53%）。
- **特征分解**：对加权散布矩阵做循环 Jacobi 对称特征分解（确定性、无第三方数值
  依赖）。主特征向量即拟合方向；MAD 采用 Kirschvink (1980)
  `MAD = asin(sqrt((λ2+λ3)/λ1))`（度）。
- **原点约束**：`free` 为过加权质心的自由直线；`anchored` 强制过地理原点。
  ORIGIN-1 的高温尾在锚定下 MAD 明显更小，用于演示“末端趋近原点”时约束选择
  的影响——两个结果并排保留，不互相覆盖。
- **加权方式**：`none` 等权；`isotropic` 用 `3/tr(Cov)`；`mahalanobis` 先以
  各向同性权求初值，再按 `1/(uᵀ Cov u)` 沿当前拟合轴迭代 4 次。
- **端点残差**：窗口首、末级点到拟合直线的正交距离（与磁矩同单位）。
- **方向置信区间**：以候选种子（默认 1164）驱动的 PCG + 有放回 bootstrap
 （2000 次，可复现），报告与主方向夹角的第 95 百分位（半角，度）。少于
  3 级时返回 `CI_INSUFFICIENT` 而不是伪造区间。

### 诊断码（均为可解释中文提示）

| code | 触发条件 |
| --- | --- |
| `WINDOW_TOO_SHORT` | 窗内少于 2 个级次（error） |
| `VARIANCE_DEGENERATE` | 协方差迹/方向方差非正，或最大特征值无优势（error/warning） |
| `NEAR_ZERO_MOMENT` | 级次磁矩 < `1e-3`（ORIGIN-1 的 600°C 末级） |
| `POLARITY_FLIP` | 沿拟合轴投影在相邻级次间变号且量级超过中位磁矩 15% |
| `DUPLICATE_LEVEL` | 同一处理强度存在多条独立复测（info） |
| `DUPLICATE_ANOMALOUS_LOAD` | 同级复测向量差异 > 20% 平均磁矩（warning） |
| `CI_INSUFFICIENT` / `CI_UNSTABLE` | bootstrap 不可用或多数重采样退化 |

## 坐标与旋转约定（右手系、角度正方向）

所有坐标帧均为右手正交系：

- `spec` 标本系：X 为标本参考标记，Y 在标记面内自 X 顺时针 90°，Z 沿标本轴向下。
- `geo` 地理系：**X=北，Y=东，Z=向下（NED）**。
- `tilt` 倾斜校正系：层面恢复水平后的地层坐标。

角度一律 **度**，矩阵中转弧度；正方向遵循右手定则——绕 Z 轴正向旋转时 +X 转向
+Y（沿轴向原点看为逆时针）。

变换链按内旋顺序构造并**逐节点导出**：

1. `X(roll)`：绕标本 +X 滚转；
2. `Y(plunge)`：绕 Y 倾伏（+X 向下）；
3. `Z(azimuth)`：绕地理 Z 方位（自北顺时针的装样方位角）；
4. 仅在目标帧为 `tilt` 时追加绕走向轴的 `untilt`（角度 = −Dip，Rodrigues）。

统一约定 `v_target = R · v_source`，因此逆变换即转置。往返校验
（`internal/geomag/frame_test.go`）对姿态+倾斜的完整链要求
`‖Rᵀ(Rv) − v‖ < 1e-12`，同时验证列向量单位正交且 `e0×e1=e2`（右手系）。

任一候选窗都可通过 `GET /api/candidate/{id}/chain?download=1` 导出**每个原始级
次**的旋转链（各步角度、轴、3×3 矩阵与合成矩阵），因此页面上的任一方向都可
追溯到原始级次和完整坐标变换链。

## 分层版本化与人工取舍

- `candidates`：多个候选窗并排保存（窗口/帧/约束/加权/手动排除/种子）。
- `analyses`：每次计算追加一行**不可变**版本（`spec_json`、`result_json`、
  `chain_json`），`POST /api/candidate/{id}/refit` 只追加，不改写历史；
  `GET /api/candidate/{id}/versions` 查看全部版本。
- `decisions`：人工采纳/否决与理由是**独立于计算结果的一层**，不会回算覆盖。
- 候选窗记录自己的坐标帧快照；即使随后切换项目坐标帧，重开项目后该候选仍在其
  原始帧中按相同种子重放——被拒绝级次、方向与置信区间原样恢复
  （测试 `TestFrameSwitchRestoresCandidateExactly`）。

## 验收 fixture

`internal/fixtures/fixtures.json` 为随仓库提交的固定数据，由带固定 PCG 种子的
生成器产生，可用 `PBENCH_REGEN=1 go test ./internal/fixtures/ -run TestRegenerateFixture -count=1`
重放再生成：

1. **STABLE-1 稳定单分量**：AF 0–100 mT，点近似共线，自由拟合 MAD ≈ 2.3°。
2. **ORIGIN-1 末端趋近原点**：TH 20–600°C，低温有黏滞剩磁叠加，高温尾向原点
   收敛；含 600°C 近零末级。锚定拟合比自由拟合更紧。
3. **DUP-1 重复级异载荷**：40 mT 有两次独立复测（run A / run B），run B 重装
   异常，产生异载荷警告；两次复测均保留。

## 清空数据库后的重放复核

页面上「清空数据库」→「重新导入 fixture」，或用 API：

```sh
curl -X POST http://127.0.0.1:5500/api/reset
curl -X POST http://127.0.0.1:5500/api/import
```

重导入逐向量、逐协方差幂等（同 `project_id+code` upsert 并重建级次），自动化
测试 `TestWipeAndReimportReplays` 对比清空前后的完整矢量签名。

## 运行记录

所有创建、拟合、取舍、帧切换、导入、清空动作写入 `run_log` 表：

```sh
curl -o run-log.json "http://127.0.0.1:5500/api/runlog?download=1"
```

## HTTP API 摘要

| 方法 路径 | 作用 |
| --- | --- |
| `GET /api/project` | 项目、当前帧、标本列表 |
| `POST /api/frame` `{frame:spec|geo|tilt}` | 切换项目坐标帧 |
| `POST /api/import` / `POST /api/reset` | 重导入 / 清空 |
| `GET /api/specimen/{id}` | 级次、变换后矢量、投影点、全部候选 |
| `POST /api/specimen/{id}/candidates` | 新增候选并计算 v1 |
| `POST /api/candidate/{id}/refit` | 追加一个不可变计算版本 |
| `POST /api/candidate/{id}/decision` | 写人工取舍层 |
| `GET /api/candidate/{id}/versions` | 计算版本 |
| `GET /api/candidate/{id}/chain?download=1` | 导出逐级旋转矩阵 |
| `GET /api/runlog?download=1` | 导出运行记录 |
| `GET /api/specimen/{id}/plot/zijderveld.svg` | Zijderveld SVG |
| `GET /api/specimen/{id}/plot/stereonet.svg` | 球面投影 SVG |

## 目录结构

```
cmd/server            服务入口（embed 静态页、参数、优雅退出）
internal/geomag       向量/旋转/特征分解/PCA/诊断/投影（纯算法，可独立测试）
internal/fixtures     固定验收数据与确定性生成器
internal/store        SQLite schema 与版本化存储
internal/web          服务编排、HTTP 路由、SVG 绘制、端到端测试
internal/web/webroot  操作页面（随二进制嵌入）
```
