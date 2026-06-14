# Paper War — 大规模多人 RTS 设计文档

## 概述

Paper War 是一款服务端权威的大规模多人 RTS 游戏。玩家以小组（Squad）为单位调度作战，每个小组包含一名指挥官和若干作战单位。单位移动遵循鸟群算法（Boid Flocking），所有移动和攻击判定在服务端完成。

### 核心特性

- 2.5D 等距视角，Web 浏览器运行
- Go 服务端，ECS 架构，WebGL 前端渲染
- 大规模多人战争（4-8 人，每方 10+ 小组，总单位 500-1000+）
- 混合同步模型（输入同步 + 状态校正）
- 指挥官综合型角色（阵型中心 + 战术指令）
- Boid 全规则（分离/聚合/对齐/阵型角色分工）
- 自动攻击（范围触发），仅超远程保留弹道
- 复杂地形 + 动态变更（桥梁损毁、关口阻断）
- Flow Field 寻路（支持兵种地形代价矩阵）

---

## 1. 系统架构

### 1.1 整体架构

```
┌─────────────────────────────────────────────────────────────────┐
│                        客户端 (Browser)                         │
│  ┌──────────────┐  ┌──────────┐  ┌───────────────────────────┐ │
│  │ WebGL 渲染   │  │ 输入处理  │  │  状态插值 & 预测          │ │
│  │ (场景)       │  │(鼠标/键盘)│  │  (15Hz→30fps + 服务端校正)│ │
│  └────┬─────────┘  └────┬─────┘  └───────────┬───────────────┘ │
└───────┼─────────────────┼────────────────────┼─────────────────┘
        │                 │ WebSocket          │
┌───────┼─────────────────┼────────────────────┼─────────────────┐
│       │     服务端 (Go)  │                    │                 │
│  ┌────┴─────────────────────────────────────────────────────┐  │
│  │               Game Loop (15Hz tick)                       │  │
│  │  ┌─────────┐ ┌──────────┐ ┌──────────┐ ┌──────────────┐ │  │
│  │  │ 输入     │→│ Boid     │→│ 寻路     │→│ 战斗         │ │  │
│  │  │ 处理     │ │ 移动系统 │ │ 系统     │ │ 判定系统     │ │  │
│  │  └─────────┘ └──────────┘ └──────────┘ └──────────────┘ │  │
│  │  ┌─────────┐ ┌──────────┐ ┌──────────┐ ┌──────────────┐ │  │
│  │  │ 指挥官   │ │ 阵型     │ │ 地形     │ │ 状态广播     │ │  │
│  │  │ AI系统  │ │ 角色系统 │ │ 管理系统 │ │ 系统         │ │  │
│  │  └─────────┘ └──────────┘ └──────────┘ └──────────────┘ │  │
│  └──────────────────────────────────────────────────────────┘  │
│  ┌────────────────────┐  ┌─────────────────────────────────┐  │
│  │ ECS Core           │  │ 数据层                          │  │
│  │ · Entity Manager   │  │ · 地图网格 (TileMap)            │  │
│  │ · Component Pool   │  │ · Flow Field (流场寻路)         │  │
│  │ · System Scheduler │  │ · Spatial Hash (碰撞网格)       │  │
│  └────────────────────┘  └─────────────────────────────────┘  │
└────────────────────────────────────────────────────────────────┘
```

### 1.2 数据流（每 tick）

```
玩家输入 → 输入队列 → 验证 & 执行
                         │
             ┌───────────┼───────────┐
             ▼           ▼           ▼
       指挥官指令    移动指令     攻击指令
             │           │           │
             ▼           ▼           ▼
       ┌─────────┐ ┌─────────┐ ┌─────────┐
       │ Boid    │ │ Flow    │ │ Combat  │
       │ Force   │ │ Field   │ │ Check   │
       │ Calc    │ │ Follow  │ │ & Apply │
       └────┬────┘ └────┬────┘ └────┬────┘
            │           │           │
            └─────┬─────┘───────────┘
                  ▼
            位置/状态更新
                  │
            ┌─────┼─────┐
            ▼           ▼
     状态快照广播   地形变更事件
     (15Hz)        (触发 Flow Field 重算)
```

---

## 2. 定点数方案

所有坐标、速度、力、距离使用 int64 定点数，确保跨平台确定性一致。

```
FractionBits = 12
1 world unit = 4096 (1 << 12)
精度: 1/4096 ≈ 0.00024 world unit
整数部分 52 位，足够覆盖超大地图
角度: int16 (0-3599, 精度 0.1°)
```

核心运算：
- `FixedMul(a, b) = (a * b) >> 12`
- `FixedDiv(a, b) = (a << 12) / b`

---

## 3. ECS Core

### 3.1 Entity 类型

| Entity | 说明 |
|--------|------|
| Squad | 小组，包含一个 Commander + N 个 CombatUnit，维护 Formation 配置 |
| Commander | 指挥官，阵型中心点，携带战术指令，提供士气光环，死亡后小组切换 AI 模式 |
| CombatUnit | 作战单位，Boid 行为体，阵型角色（前排/后排/侧翼） |
| Projectile | 弹道投射物，仅超远程攻击产生 |

### 3.2 Component 定义

**位置与运动：**
```
PositionComponent  { X, Y int64; Angle int16 }
VelocityComponent  { Vx, Vy int64; Speed int64 }
MovementComponent  { ProfileID uint8 }
```

**Boid 行为：**
```
BoidComponent {
  SquadID       uint32
  Role          uint8      // 0=Melee, 1=Ranged, 2=Flanker, 3=Commander
  SeparationW   int64      // 定点数权重
  CohesionW     int64
  AlignmentW    int64
  FormationW    int64
  NeighborRange int64      // 感知半径
}
```

**战斗：**
```
HealthComponent { HP, MaxHP int32; Armor int32; Morale int32 }
AttackComponent {
  Range      int64     // 攻击范围
  Damage     int32
  Cooldown   uint8     // 攻击间隔 (tick 数)
  LastAttack uint32    // 上次攻击 tick
  TargetID   uint32    // 0=无目标
  AttackType uint8     // 0=Melee, 1=Ranged, 2=Artillery(弹道)
}
```

**阵型：**
```
FormationComponent {
  FormationType  uint8   // 0=Line, 1=Wedge, 2=Circle, 3=Scatter
  Spacing        int64
  RoleOffsets    []RoleOffset
}
RoleOffset { Role uint8; DX, DY int64 }
FormationRoleComponent { OffsetX, OffsetY int64; Role uint8 }
```

**指挥官：**
```
CommanderComponent {
  SquadID         uint32
  AuraRadius      int64
  AuraMoraleBonus int32
  TacticalState   uint8   // 0=Follow, 1=Charge, 2=Retreat, 3=Hold
  IsAlive         bool
}
```

**寻路：**
```
PathfindingComponent { TargetX, TargetY int64; FlowFieldID uint32; Stuck bool }
```

**弹道（仅超远程）：**
```
ProjectileComponent {
  X, Y         int64
  DX, DY       int64
  TargetX, Y   int64
  Damage       int32
  ImpactTick   uint32
  SplashRadius int64
}
```

### 3.3 System 执行管线（每 tick）

```
① InputSystem        解析 & 验证玩家指令
② CommanderAISystem   处理指挥官战术决策，无指挥官时触发小组 AI 接管
③ TerrainSystem       处理地形变更事件，标记需重算的 Flow Field 区域
④ FlowFieldSystem     按需计算/更新 Flow Field（惰性更新）
⑤ BoidSystem          并行计算每个单位的 Boid 合力
⑥ MovementSystem      合成 Boid 力 + Flow Field 方向 → 更新位置
⑦ SpatialHashUpdate   将所有单位重新投入空间哈希网格
⑧ CombatSystem        范围检测 → 自动索敌 → 即时攻击判定 → 伤害结算
⑨ ProjectileSystem    更新弹道（仅超远程单位）
⑩ DeathSystem         处理单位死亡，触发指挥官死亡 → 小组 AI 切换
⑪ SnapshotSystem      生成增量状态快照，广播给客户端

可并行: ⑤ BoidSystem, ⑥ MovementSystem, ⑧ CombatSystem
```

---

## 4. Boid 群体行为系统

### 4.1 四层力学模型

| 层级 | 力 | 权重 | 说明 |
|------|-----|------|------|
| L1 硬约束 | 地形碰撞推力 | ∞ | 防止穿墙/穿山 |
| L1 硬约束 | 边界约束力 | ∞ | 限制在地图边界内 |
| L2 战术 | 阵型目标力 | 3.0 | 拉向阵型理想位置 |
| L2 战术 | Flow Field 力 | 2.5 | 跟随流场向目标移动 |
| L2 战术 | 指挥官追随力 | 2.0 | 保持在指挥官附近 |
| L3 Boid | 分离力 Separation | 1.5 | 避免与邻居重叠 |
| L3 Boid | 聚合力 Cohesion | 1.0 | 朝向邻居质心移动 |
| L3 Boid | 对齐力 Alignment | 1.0 | 与邻居对齐速度方向 |
| L4 行为 | 敌人排斥/吸引 | 0.8 | 近战吸引，远程排斥 |
| L4 行为 | 撤退力 | 1.2 | 低士气时触发 |
| L4 行为 | 分散力 | 2.0 | 被 AoE 攻击时触发 |

最终加速度 = Σ(力 × 权重)，限制在 MaxForce 以内。

### 4.2 指挥官"领头鸟"模型

- 指挥官不受 Boid 三力影响，仅受 Flow Field + 玩家指令驱动
- 作战单位以指挥官为中心计算阵型偏移位置：`实际目标 = 指挥官.Position + Rotate(偏移, 指挥官.朝向)`
- 指挥官阵亡时：指挥官追随力消失，阵型力降级（邻近单位聚合），Boid 三力权重提升，CombatAI 切换为自卫模式

### 4.3 阵型角色权重映射

| 角色 | 分离 | 聚合 | 对齐 | 阵型 | 行为特征 |
|------|------|------|------|------|---------|
| 前排肉盾 | 1.2 | 1.5 | 0.8 | 2.5 | 紧密聚合，稳住阵线 |
| 后排远程 | 2.0 | 0.6 | 1.0 | 2.0 | 保持间距，不近身 |
| 侧翼突击 | 0.8 | 0.4 | 1.5 | 1.0 | 分散移动，快速包抄 |
| 指挥官 | N/A | N/A | N/A | N/A | 仅受玩家指令驱动 |

阵型类型：Line(线阵)、Wedge(楔形)、Circle(环形)、Scatter(散开)

### 4.4 性能优化

**Spatial Hash：** 将世界划分为 CellSize = MaxNeighborRange 的网格，邻居查询只检查周围 9 格，从 O(n²) 降到 O(k)。

**并行计算：** 单位按 SquadID 分组，每组一个 goroutine。不同 Squad 的 Boid 计算互不依赖，天然并行。goroutine 总数 ≈ 玩家数 × 每方小组数 ≈ 40-80。

---

## 5. 地形与寻路系统

### 5.1 地图数据

规则网格 TileMap，每格 1 world unit (4096 定点数)。典型地图 256×256。

```
Tile {
  TerrainType  uint8    // 0=平地, 1=道路, 2=浅水, 3=深水, 4=森林,
                        // 5=山地, 6=沼泽, 7=桥梁, 8=城墙, 9=雪地, 10=沙漠
  Elevation    int8     // 高度
  BlockLOS     bool     // 阻挡视线
  Health       int32    // 可破坏物件血量 (0=不可破坏)
  MaxHealth    int32    // 满血值
}
```

可破坏物件规则：
- 桥梁 Health > 0 → TerrainType=7(桥梁)，Health ≤ 0 → TerrainType=3(深水)
- 城墙 Health > 0 → TerrainType=8(城墙)，Health ≤ 0 → TerrainType=0(平地)

```
GameMap {
  Width, Height int32
  Tiles         []Tile              // Width * Height 线性数组
  Profiles      []MovementProfile
  RegionMaps    [][]uint16          // 每个 Profile 一张 RegionMap (Width*Height)
}
```

### 5.2 兵种地形代价矩阵

MoveCost 不存在 Tile 上，改为每种 UnitType 有独立查表。

```
MovementProfile {
  ID           uint8
  TerrainCosts [16]uint8   // 0=不可通行, 1=正常, 2=减速, 3=重度减速
}
```

| UnitType | 平地 | 道路 | 浅水 | 深水 | 森林 | 山地 | 沼泽 | 桥梁 | 城墙 | 雪地 | 沙漠 |
|----------|------|------|------|------|------|------|------|------|------|------|------|
| 步兵 | 1 | 1 | 2 | 0 | 2 | 2 | 3 | 1 | 0 | 2 | 2 |
| 骑兵 | 1 | 1 | 0 | 0 | 3 | 0 | 0 | 1 | 0 | 2 | 1 |
| 弓手 | 1 | 1 | 2 | 0 | 1 | 2 | 3 | 1 | 0 | 2 | 2 |
| 工兵 | 1 | 1 | 1 | 0 | 1 | 1 | 2 | 1 | 1 | 1 | 1 |
| 攻城 | 1 | 1 | 0 | 0 | 3 | 0 | 0 | 2 | 0 | 0 | 1 |
| 指挥官 | 1 | 1 | 2 | 0 | 2 | 2 | 3 | 1 | 0 | 2 | 2 |

### 5.3 Flow Field 寻路

三步计算：
1. **Cost Field：** 从 MovementProfile.TerrainCosts 查表
2. **Integration Field：** 从目标点反向 Dijkstra 扩散，每格 integratedCost = min(邻居) + self.MoveCost
3. **Direction Field：** 每格找 integratedCost 最小的邻居，方向 = self → 邻居的单位向量

**缓存 Key：** `(TargetX, TargetY, ProfileID)` — 同一目标不同兵种有不同 Flow Field。

**缓存管理：**
- LRU + 引用计数回收
- Profile 自动合并相似代价表（步兵/弓手/指挥官 → 同一 Profile）
- 小组移动以指挥官 Profile 计算 Flow Field
- 不可达成员停在最近可通行位置

### 5.4 动态地形变更

触发事件：攻击摧毁桥梁/城墙、技能改变地形等。

处理流程：
1. 更新 TileMap（局部格子）
2. Cost Field 局部更新
3. RegionID 连通性检查（Flood Fill 按受影响的 ProfileGroup 更新）
4. Flow Field 重算策略：
   - 小影响：找到经过变更格子的活跃 Flow Field，立即重算
   - 大影响：标记 dirty，分帧重算（每 tick 最多 2 个），期间单位跟随旧 Flow Field
5. 广播地形变更给所有客户端

### 5.5 Flow Field + Boid 协作

MovementSystem 合成最终移动向量：
1. Flow Field 方向力（大方向向目标）
2. 阵型力（以指挥官为中心的理想偏移）
3. Boid 三力（群体自然感）
4. 地形碰撞排斥力（硬约束，靠近不可通行格子施加排斥）

---

## 6. 战斗系统

### 6.1 即时攻击（近战 + 普通远程）

无弹道实体，CombatSystem 每 tick 直接结算：
1. SpatialHash 查找范围内敌人
2. Cooldown 检查 (CurrentTick - LastAttack >= Cooldown)
3. 距离 ≤ Range → 命中 → finalDamage = Damage - Armor (下限 1)
4. 应用伤害到 Target.HealthComponent

### 6.2 弹道攻击（仅超远程 Artillery）

射程超过全屏的单位使用弹道：
1. 发射时创建 Projectile 实体
2. ProjectileSystem 每 tick 更新位置
3. 到达 ImpactTick 或碰到目标 → 结算伤害
4. 有 SplashRadius 时对范围内所有单位造成伤害
5. 弹道实体数量极少

### 6.3 客户端表现

- 即时攻击：客户端收到伤害事件后自行播放攻击特效 + 受击动画
- 弹道攻击：服务端同步弹道位置，客户端渲染轨迹

---

## 7. 网络同步系统

### 7.1 混合同步模型

- 服务端绝对权威，客户端不能直接改变游戏状态
- 上行：只发玩家输入指令（~10-20 bytes/条）
- 下行：15Hz 广播增量状态快照

### 7.2 指令协议（上行）

```
Header: [uint8 CmdType] [uint32 ClientSeq] [uint32 PredictedTick]

CmdType:
  0x01 MoveSquad       [uint32 SquadID] [int32 TargetX] [int32 TargetY]    ~13B
  0x02 AttackTarget    [uint32 SquadID] [uint32 TargetEntityID]             ~9B
  0x03 AttackGround    [uint32 SquadID] [int32 TargetX] [int32 TargetY]    ~13B
  0x04 ChangeFormation [uint32 SquadID] [uint8 FormationType]               ~6B
  0x05 TacticalOrder   [uint32 SquadID] [uint8 OrderType]                   ~6B
```

服务端验证：SquadID 所有权、目标范围、冷却时间。非法指令静默丢弃。

### 7.3 增量状态快照（下行）

```
SnapshotHeader { Tick uint32; PrevTick uint32; UnitCount uint16; EventCount uint8 }

UnitUpdate {
  EntityID    uint32
  ChangedMask uint8    // 位掩码标记变化字段
  // bit 0: Position(8B), bit 1: Velocity(8B), bit 2: Angle(2B)
  // bit 3: HP(4B), bit 4: TargetID(4B), bit 5: Morale(2B), bit 6: State(1B)
}

Event { EventType uint8; Data []byte }
  // 0=Damage, 1=Death, 2=TerrainChange, 3=CommanderDown, 4=ProjectileSpawn
```

带宽：1000 单位约 40KB/s per 客户端（压缩后）。

### 7.4 客户端预测 + 校正

- 玩家操作后客户端立即用简化 Boid 模拟预演移动
- 收到服务端快照后对比：偏差小 → 插值平滑，偏差大 → 立即校正
- 双缓冲状态（prev/curr），预测历史环形缓冲

### 7.5 视口裁剪

每个客户端只接收：
1. 视口内的己方单位（完整状态）
2. 视口内可见的敌方单位（受战争迷雾影响）
3. 视口外向视口移动的己方单位（位置摘要）
4. 所有己方指挥官（始终同步）

优化后每客户端约 20-40KB/s 下行带宽。

---

## 8. 客户端渲染层

### 8.1 技术栈

- WebGL（底层）：场景渲染（地形/物件/单位/特效）
- Canvas 2D（上层）：UI 覆盖（血条/面板）
- DOM：顶部信息栏、底部控制栏

### 8.2 WebGL 批量渲染

4 个 Batch Pass，总计 4-5 draw calls：
1. 地形瓦片 Batch：视口内所有 Tile 打包为 VBO，1 draw call
2. 地形物件 Batch：桥梁/树木/建筑，Y 排序后打包，1 draw call
3. 作战单位 Sprite Batch：Instanced Rendering + Texture Atlas，1 draw call
4. 特效层：地面特效 + 空中特效 + 粒子系统，1-2 draw calls

核心技术：Instanced Rendering + Texture Atlas（所有纹理打包到 2048×2048 图集，避免纹理切换）。

### 8.3 等距坐标转换

2:1 菱形瓦片（64×32px），标准等距投影：
- WorldToScreen: `sx = (tx - ty) * 32 + offsetX, sy = (tx + ty) * 16 + offsetY`
- ScreenToWorld: 反向计算
- 深度排序：Y 值从小到大，同一 Y 按 X 排序

### 8.4 精灵动画

每个 UnitType 一套精灵表：8 方向 × N 帧 × M 状态（Idle/Move/Attack/Hurt/Death）。客户端即时反馈（选中高亮、目标圆环等）不等服务端确认。

### 8.5 帧率：15Hz → 30fps

- 服务端 15Hz tick（每 66.7ms）
- 客户端 30fps 渲染（每 33.3ms）
- 每两个服务端帧之间插入一个插值帧
- 双缓冲状态线性插值：`render.X = lerp(prev.X, curr.X, t)`
- 异常处理：快照延迟继续插值，丢包外推一帧，大偏差加速校正

### 8.6 UI 布局（上下结构）

```
┌──────────────────────────────────────────────────────────────┐
│  顶部信息栏 (60px)                                            │
│  [玩家名/头像] [金币] [兵力] [分数] [倒计时] [设置]             │
├──────────────────────────────────────────────────────────────┤
│                                                              │
│                游戏主视窗 (WebGL Canvas)                       │
│                  全宽自适应高度                                │
│                                                              │
├──────────────────────────────────────────────────────────────┤
│  底部控制栏 (140px)                                           │
│  ┌────────────────────┬──────────┬────────────────────────┐  │
│  │  选中面板           │  小地图   │  操作面板               │  │
│  │  指挥官/小组信息     │  160×160  │  阵型: 1线 2楔 3环 4散 │  │
│  │  兵力/士气/状态      │          │  战术: Q冲 W撤 E守 R集  │  │
│  └────────────────────┴──────────┴────────────────────────┘  │
└──────────────────────────────────────────────────────────────┘
```

交互方式：
- 左键：选中/框选
- 右键：移动/攻击
- 快捷键 1-4：切换阵型
- 快捷键 Q/W/E/R：战术指令
- 滚轮：缩放
- WASD / 边缘滚动：平移视口

---

## 9. 实施范围与分阶段策略

本项目包含多个独立子系统，建议分阶段实施：

**Phase 1 — 核心引擎**
- ECS Core 框架（Entity Manager / Component Pool / System Scheduler）
- 定点数运算库
- Game Loop（15Hz tick）
- Spatial Hash

**Phase 2 — 移动与寻路**
- Flow Field 寻路系统（含兵种代价矩阵）
- Boid 群体行为系统（四层力学模型）
- MovementSystem（力合成 + 位置更新）
- 阵型系统

**Phase 3 — 战斗与地形**
- CombatSystem（即时攻击）
- ProjectileSystem（弹道攻击）
- HealthComponent / 死亡处理
- 指挥官系统（士气光环 / 战术指令 / 阵亡 AI 接管）
- 动态地形变更（可破坏物件 / Flow Field 重算）

**Phase 4 — 网络同步**
- WebSocket 连接管理
- 输入协议（上行）
- 增量状态快照（下行）
- 客户端预测 + 服务端校正
- 视口裁剪

**Phase 5 — 客户端渲染**
- WebGL 批量渲染引擎
- 等距坐标转换
- 精灵动画系统
- 状态插值（15Hz → 30fps）
- UI 系统（上下布局）

**Phase 6 — 整合与调优**
- 完整游戏流程串联
- 性能调优（Flow Field 缓存 / 带宽压缩）
- 美术资源集成
- 多人对战测试
