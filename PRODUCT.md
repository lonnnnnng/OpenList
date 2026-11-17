# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Stack

后端由用户Fork OpenList，当前为Go项目。固定来源的OpenList-Frontend源码已导入同仓web目录，延续SolidJS与Hope UI。前端编译后内嵌单个Go业务进程，运行时不启动Node服务。

## Users

管理员自行维护网盘与同步任务，使用中文管理界面。家人通过现有Emby/Jellyfin看片，不需要中间件管理账号。中文优先、仅管理员使用已由用户确认。

## Product Purpose

把多款网盘中的媒体接入Emby/Jellyfin：读取目录、生成本地STRM、提供可持续使用的播放入口并同步本地产物变化。用户要求开源免费、可自行维护、整合成一个中间件，选择在Fork OpenList基础上继续开发。

## Operating Context

macOS arm64用于开发，首发部署目标为Docker linux/amd64。M0已进行独立本机与非root容器验证。已有家庭服务器OpenList/Jellyfin信息属于外层运维文档记录；正式部署前仍核对实际端口、目录和媒体卷。

## Capabilities and Constraints

- 必须开源免费，不依赖自有Pro授权服务器，不设置收费高级功能。
- 网盘驱动与取流基础复用OpenList，自建任务、可靠扫描、受管输出、安全删除和管理UI。
- 原PRD一期盘为夸克/阿里/百度/UC；扩展盘与302能力路线以审核后的方案为准。
- 同步任务只读网盘；远端删除后的本地同步不意味着授权任务删除云端文件。
- 软件免费不保证网盘免费高速或任意客户端直连；不得把Direct Play等同绕过服务器。
- 一期管理界面中文优先、仅管理员；播放/删除/凭据/前端组织默认值和完整路线图已由用户审核并要求开始实施。

## Evidence on Hand

实现方案：[docs/strm/IMPLEMENTATION-PLAN.md](docs/strm/IMPLEMENTATION-PLAN.md)。路线图：[docs/strm/ROADMAP.md](docs/strm/ROADMAP.md)。M0产品双平台构建、相关回归、本机/容器集成及中文骨架UI验证已通过；任务生成、双服务器和真实四盘播放仍未验收，不能伪造成功状态或资源指标。

## Product Principles

- 保留上游驱动结构，集中维护自有媒体业务，明确每次升级的来源与代价。
- 先建立能播放的单盘闭环，再完善可靠同步和多盘兼容。
- 只有完整成功的扫描能作为缺失判断依据，未知文件与不完整结果不参与删除。
- 同步、刷新、取消、回收和恢复分别表达真实状态，让管理员知道发生了什么和如何继续。
- 管理与播放鉴权分离，凭据和完整签名链接不出现在常规日志。

## Open Decisions

产品名称、独立视觉规范尚未选择；M0保留导入前端的视觉体系，不创建新的DESIGN.md。实现方案与一期默认值已审核，正式部署目标在实际部署前核对。
