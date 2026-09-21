# 极性轨迹 · 离线退磁主分量分析工作台

面向地磁实验室定向标本的**离线**工作台：逐级导入交流（AF）/热（TH）退磁结果
（三分量矢量、测量协方差、装样姿态、处理方式），在 **Zijderveld 投影**与
**等面积球坐标（Schmidt 下半球）**中检查分量，对连续处理窗做加权主分量分析，
多个候选窗并排保留，人工取舍与计算结果分层版本化，任一方向都能追溯到原始
级次和坐标变换链。

纯本地运行：Go + SQLite（`modernc.org/sqlite`，无 cgo）+ HTTP + 内联 SVG，
不依赖任何外部网络服务。

## 安装与演示

```bash
go mod download
go test ./... -count=1
go run ./cmd/server --listen 127.0.0.1:5500
# 浏览器打开 http://127.0.0.1:5500，页面标题为「极性轨迹」
```

数据库文件默认 `polaritytrace.db`；空库启动时自动导入三条固定验收 fixture。
其他参数：`--db PATH`（库路径）、`--no-seed`（空库不自动导入）。

重新生成固定 fixture（确定性，提交在 `internal/fixture/data/`）：

```bash
go run ./cmd/genfixtures            # 写入 internal/fixture/data/*.json
```

## 数据口径（坐标与角度约定）

- 右手正交系，所有帧统一轴定义：**X 北 / Y 东 / Z 向下（NED）**。
- 偏角 `D = atan2(Y, X)`，自北顺时针，范围 0–360°；
  倾角 `I = atan2(Z, sqrt(X²+Y²))`，**向下为正**。
- **角度正方向**：沿命名轴满足右手定则。由于 Z 向下，`Rz(+θ)` 在俯视场景中
  表现为顺时针；代码中的 `Rx/Ry/Rz` 均为右手定则旋转矩阵（行列式 +1）。
- 装样姿态：标本 +X 轴为定向岩心轴，走向 `trend`（自北顺时针方位角）与
  倾伏 `plunge`（向下为正）。M→G 变换明确定义为两步乘积

  `R_MG = Rz(trend) · Ry(−plunge)`

  导出时两个因子矩阵分别列出，均可独立审计。
- 倾斜校正：层面倾向方位角 `dip_azimuth`（下坡方向）与倾角 `dip_angle`。
  G→T 为绕走向轴（方位角 = 倾向−90）旋转 `−dip_angle`（Rodrigues 公式）。
- 三帧：`M` 测量系 / `G` 地理系 / `T` 倾斜校正系。
- 协方差随旋转严格传播：`C' = R C Rᵀ`。
- **往返精度**：`Rᵀ(Rv)` 与输入向量逐分量比较；fixture 与服务接口均输出
  `roundtrip_max_abs_err`，固定 fixture 上约为 1e-15（远高于输入精度）。
  页面侧栏展示当前标本的最大往返误差。
- 磁矩单位 mA/m；协方差为 (mA/m)²。fixture 测量噪声 σ≈0.04 mA/分量。

## 拟合口径（PCA）

- 窗内点按处理方式与级次排序；**重复处理强度不做任何静默合并**，每个复测
  （同 treatment/level、不同 rep）是独立行，可各自纳入/排除。
- 加权方式：`uniform` 等权；`inverse_variance` 权重 `1/tr(Σ)`。
- 自由拟合线过加权质心；原点约束拟合线强制过原点。
- 方向为加权散度矩阵最大特征值对应特征向量（Jacobi 分解，全部特征值/向量
  可见）；极性约定使其指向平均剩磁方向，对跖极方向同时返回。
- `MAD = atan(sqrt((λ2+λ3)/λ1))`（Kirschvink / PuffinPlot 口径，度）。
- 端点残差：首、末级次到拟合轴的垂直距离（mA/m）及轴向坐标；自由拟合另给
  拟合轴到原点的垂直偏离 `anchor_residual_ma_m`。
- 置信区间：1000 次固定种子 bootstrap，方向与主方向夹角分布的 95 分位为
  `α95`；只有窗长不足/方差退化等**硬错误**才使重采样判失败。

### 可解释诊断

| 代码 | 级别 | 触发 |
|---|---|---|
| `WINDOW_TOO_SHORT` | error | 窗内点数 < 3 |
| `DEGENERATE_VARIANCE` | error | 散度近零（点云塌成一点），方向/MAD 不可识别 |
| `NEAR_ZERO_MOMENT` | warning | 级次磁矩 < 0.5 mA/m（如终点趋零） |
| `POLARITY_FLIP_NEARBY` | warning | 窗内相邻非零级次夹角 > 120°（极性翻转附近） |
| `REPEATED_LOAD_MISMATCH` | warning | 同强度复测偏离组合 1σ 超过 3 倍，独立复测不合并 |
| `BOOTSTRAP_UNSTABLE` | warning | 超过半数重采样遇到硬错误 |

被排除级次始终带**原因**返回（越窗、处理方式不同、显式勾选排除），不会被
静默丢弃。

## 分层版本化与追溯

- 计算层 `analyses` 只追加：每次重算新增一行，版本号单调递增不复用；
  可通过 `parent_id` 串起候选谱系。
- 人工层 `decisions` 独立版本化：对某候选标记 `candidate / accepted /
  rejected` 并附理由；计算结果不会被人工操作覆盖改写。
- 每个候选保存：帧、处理方式、级次窗、原点约束、加权、bootstrap 种子、
  纳入键序列、`{key, reason}` 排除列表、完整 `FitResult`、创建时间。
- 「追溯谱系」对话框列出原始级次键、排除原因、版本链、请求哈希；
  「导出旋转矩阵」给出 M→目标帧的每一步 3×3 矩阵、角度参数与合成矩阵。

## 验收样例（固定 fixture，确定性生成）

| 标本 | 场景 | 预期 |
|---|---|---|
| `S1-STABLE` | AF 9 级稳定单分量（G 系 D=10°, I=45°） | 10–60 mT 自由拟合 MAD≈0.1°，方向与设计值差 < 1° |
| `S2-ORIGIN` | TH 双组分，低温黏滞组分 350°C 前剥离，末端 600°C 趋零 | 350–600°C 直线沿 D=200/I=−10 到原点，给出 `NEAR_ZERO_MOMENT` |
| `S3-REPLICA` | AF 30 mT 两次独立复测、载荷不一致 | 两行都保留（共 7 点），给 `REPEATED_LOAD_MISMATCH`，可手动排除其一 |

生成规则：轨迹先在 G 系按设计方向+确定性伪噪声构造，再经旋转链逆变换
`Rᵀ` 落到 M 系存储，形成「导入 M → 变换 G/T → 恢复方向」的端到端校验。

## 清空数据库后重新导入复核

页面：右上角「清空数据库」→「重新导入 fixture」。
命令行/接口：

```bash
curl -s -X POST http://127.0.0.1:5500/api/admin/reset    # 清数据，保留库表，帧复位 G
curl -s -X POST http://127.0.0.1:5500/api/admin/reseed   # 重新导入固定 fixture
curl -s http://127.0.0.1:5500/api/runlog                 # 导出运行记录
curl -s http://127.0.0.1:5500/api/fixtures               # 导出当前帧全部数据与候选
```

切换坐标帧（`PUT /api/project/frame`）后重启进程、重新打开同一数据库文件：
候选窗口、被拒绝级次、方向置信区间与人工决策原样恢复（候选保留创建时的
帧，按帧标注）。该场景在 `internal/api/server_test.go` 中自动化覆盖。

## HTTP 摘要

- `GET /api/project`，`PUT /api/project/frame {frame:M|G|T}`
- `GET /api/specimens`，`GET /api/specimens/{id}?frame=M|G|T`
- `POST /api/analyses`（window 支持连续级次范围或显式 keys）
- `GET /api/analyses?specimen_id=`，`GET /api/analyses/{id}`，`DELETE ...`
- `PUT /api/decisions`（accepted/rejected/candidate + 理由）
- `GET /api/specimens/{id}/rotations?frame=`（每步旋转矩阵导出）
- `GET /api/runlog`，`GET /api/fixtures`，`POST /api/admin/reset|reseed`

## 代码结构

```
cmd/server            HTTP 服务入口（嵌入静态页，空库自动播种）
cmd/genfixtures       确定性重建 internal/fixture/data/*.json
internal/geom         右手系旋转链、协方差传播、Jacobi、PCA/MAD/诊断/bootstrap
internal/fixture      三条固定验收标本（embed JSON）
internal/store        SQLite 表结构、只追加版本化、决策层、运行日志
internal/ingest       导入、M→G/T 变换、窗口选择、拟合、谱系与旋转导出
internal/api          HTTP 路由、日志中间件、静态托管
internal/webapp       单页操作界面（SVG：Zijderveld + 等面积网）
```
