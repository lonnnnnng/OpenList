# 上游来源与集成记录

> 作者：long｜更新：2026-11-16（北京时间）｜产品分支：`feat/strm-middleware`

## 固定来源

| 内容 | 固定值 |
| --- | --- |
| 后端基线 | `OpenListTeam/OpenList`，`de1cdb3a054628f32c410a79571848c052ce7cc2` |
| Fork | `https://github.com/lonnnnnng/OpenList` |
| 前端来源 | `https://github.com/OpenListTeam/OpenList-Frontend.git` |
| 前端提交 | `9a57baf8690dbdb934d64995159d257b4b416ec8` |
| 前端导入方式 | `git archive` 导入 `web/`，不含嵌套Git仓库；保留MIT许可、patches与pnpm锁文件 |
| Go / Node / pnpm | `go1.27.4` / `v26.12.1` / `11.28.11` |
| 前端锁文件SHA256 | `c7123f89089b44545c728be2bdcf92a4cb528d17028b817a357c4489df4f0974` |
| 产品许可 | 后端沿用AGPL-3.0；前端上游MIT许可保留；自建功能全部免费 |

运行镜像摘要和其他构建值统一记录在根目录 `strm-build.json`。脚本核对工具版本、锁文件摘要和Dockerfile镜像，拒绝自动追随上游main/latest/rolling。M0本地工作树尚未提交，制品记录 `dirty:true`；只有记录来源和补丁才能完整重建本次开发版本。

## 必要集成改动

| 上游位置 | 产品行为与保留边界 |
| --- | --- |
| `cmd/server.go`、`cmd/strm.go` | 新专用 `strm-server` 命令，默认独立data-strm，拒绝上游dev内存库/默认密码；产品关停预算30秒 |
| `internal/conf/config.go`、`strm.go` | 产品配置命名空间；专用默认5344；强制关闭S3/FTP/SFTP/MCP和前端CDN |
| `internal/bootstrap/config.go`、`db.go`、`log.go` | 固定真实路径、私有权限、连接前限制SQLite、新表迁移、启动前日志脱敏、禁止产品Debug SQL/凭据日志 |
| `internal/bootstrap/data/user.go` | 初始管理员密码不进入产品日志；沿用初始化向导和原管理员认证 |
| `internal/bootstrap/run.go`、`task.go` | 不初始化离线工具；旧任务管理器不恢复、不启动worker、不清空temp；初始化和关停产品运行态 |
| `internal/transcode/manager.go` | 产品不启动上游转码worker；媒体服务器自身仍可转码 |
| `drivers/strm/hook.go`、`internal/media/scanner.go`、`strm.go` | 产品禁止旧STRM输出、清理及媒体扫描；原上游模式保留 |
| `internal/op/storage.go` | 创建/更新/启用/初始化前限制驱动；编辑留空保留凭据；产品初始化panic不记录授权响应 |
| `drivers/aliyundrive_open/util.go` | 所有模式都不再打印轮换前后的刷新令牌，只记录刷新事件 |
| `server/router.go`、`server/middlewares/strm_policy.go` | 集中旧API白名单、认证链内管理员门禁、关闭协议与旧写入入口、专用就绪和管理健康接口 |
| `server/handles/driver.go`、`storage.go`、`setting.go` | 限制驱动元数据/表单字段；不回显存储凭据；复制公开设置后追加产品信息 |
| `public/public.go`、`product.go` | 产品build tag内嵌独立ZIP；不覆盖/清理上游public/dist |

新业务集中在 `internal/strm/`、`internal/product/`、`internal/model/strm.go`、`internal/db/strm_migrate.go`、`server/strm_routes.go`。数据库按既有表前缀追加业务表，不删除原表；更高产品schema拒绝降级。

## 前端补丁

- 保留SolidJS与Hope UI，沿用认证、请求封装和动态存储表单；固定工具链运行全前端TypeScript检查。
- 修正固定上游的Monaco/Artplayer插件类型、图片Props导出、归档分支缩窄；新增SchemaItem适配，不弱化tsconfig或全局判别联合。
- 产品默认中文，管理菜单保留任务概览、存储、管理员资料及2FA；隐藏管理页面同样经管理员守卫。
- 产品首页与登录默认进入任务概览，禁用访客/LDAP/SSO/WebAuthn入口；不在浏览器保存密码。
- 登录/初始化使用内嵌图标；不加载远程Logo。动态存储表单不展示旧云写入与协议字段，凭据默认遮挡，已有凭据留空保留，单独勾选清除。
- M0概览明确显示任务预览/生成仍在开发，不伪造同步成功、播放状态或进度。

## 后续升级步骤

1. 固定新上游提交，在独立升级窗口比较以上集成位置与驱动认证/缓存变更。
2. 同步前端之前比较接口、字段与TS类型，再重新核验锁文件和许可；不只更新SHA记录。
3. 跑上游相关回归、新业务测试、双平台构建、容器smoke和UI验证。
4. 驱动存在只表示底座具备候选能力；四盘真实播放、取消、分页、Range等按里程碑分别验收。

M0未引入Cron依赖；在M3实际接入调度前锁定版本。严格源读取、缓存失效与取消适配在M1扫描器实现时验证，不能把M0的Local目录smoke当作网盘扫描完整性或播放验收。
