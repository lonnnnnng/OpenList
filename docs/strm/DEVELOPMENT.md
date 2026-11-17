# 本地开发与运行

> 作者：long｜日期：2026-11-17｜当前交付：可启动产品；Local夹具已覆盖手动生成与签名代理播放，真实网盘和媒体服务器仍未验收。

## 构建

在仓库根目录执行，工具精确版本见 `strm-build.json`。不运行上游带清理、sudo或浮动前端下载的build.sh。

```zsh
# long: 前端frozen安装、完整类型检查、产品Vite构建、嵌入ZIP与Go二进制由同一入口管理。
node scripts/build-strm.mjs --target native
# long: 已安装固定依赖后可跳过安装，但仍检查锁文件/工具版本并运行lint与build。
node scripts/build-strm.mjs --skip-install --target linux/amd64
```

原生开发目标为macOS arm64，首发发布目标为Linux amd64、CGO=0。二进制、SHA256SUMS、build-info和许可证位于 `build/strm/<系统>-<架构>/`。固定构建时间取后端基线提交时间，实际测试日期另记在验收报告；工作树有改动时版本明确标记dirty。

## 本机启动

```zsh
# long: 开发仅监听回环地址；独立数据目录不可指向现有OpenList或生产数据库。
OPENLIST_ADDR=127.0.0.1 OPENLIST_HTTP_PORT=5344 \
  build/strm/darwin-arm64/openlist-strm strm-server --data ./data-strm --log-std
```

首次通过Web向导创建管理员，之后进入STRM任务页。可以维护一期候选网盘配置、创建草稿、显式浏览目录、运行只读预览，并手动生成本地 `.strm`；Local只用于本地测试。生成前必须完整扫描，任一目录读取失败时本轮零写入；每次运行使用单调递增编号，只有完整成功才推进缺失判断基线，安全处理的文件记录最近出现编号并清零缺失确认数。连续两次完整成功运行未见的文件进入分页缺失预览。管理员可创建15分钟有效的回收计划，执行时会持有任务互斥锁，并复核任务revision/generation、缺失总数和产物摘要；大批量缺失还要单独确认。回收在同一受限输出根内移到 `trash/`，回收项停用播放入口，支持不覆盖的手动恢复；源文件重新出现时自动恢复匹配且未修改的产物。进程重启会释放中断计划的任务锁并将计划转成可重试状态，执行重试会按动作路径和摘要核对两侧文件。当前不做永久清理或定时运行。

生成的 `.strm` 指向本服务的签名播放入口；播放时服务端重新核对存储绑定、刷新源对象并通过代理传输，可处理 GET、HEAD 和 Range。必须设置 `OPENLIST_STRM_PUBLIC_URL` 为 Emby/Jellyfin 容器或主机可访问的中间件基址，例如 `http://openlist-strm:5344`；不要填写浏览器专用地址、回环地址或带凭据的URL。也可通过任务 API 的 `entry_prefix` 为单个任务指定基址。稳定播放密钥与数据库需一起备份，轮换密钥会使已有入口失效。

配置默认位于私有数据目录，目录700、配置/DB/播放密钥600。`OPENLIST_STRM_OUTPUT_ROOT`可指定输出父目录，内部建立 `library` 和 `trash`。媒体服务器只挂载 `library/`；读取文件需要访问中间件 `OPENLIST_STRM_PUBLIC_URL`。容器网络中应使用Emby/Jellyfin能解析并访问的服务名或宿主机地址，并确保该地址实际转发到中间件监听端口。

`--dev`在产品命令中拒绝；`--debug`也不会输出产品SQL参数、管理员密码或完整配置。环境值运行时覆盖配置，不自动成为持久设置；需要长期使用的值应写入私有配置或部署环境配置。

## 单容器开发

```zsh
# long: Dockerfile只打包上一步固定构建的静态二进制及许可，不在镜像内启动Node或下载浮动前端。
docker build --platform linux/amd64 -f Dockerfile.strm -t openlist-strm:dev .
docker compose -f compose.strm.yml up -d
```

开发Compose使用独立named volumes，监听 `127.0.0.1:5344`；业务用户65532。Apple Silicon通过Docker仿真运行首发amd64产物。正式家庭服务器部署需要另核对宿主目录、共享组、媒体卷和端口，不直接复用开发卷。

媒体服务器只挂载输出 `library`；私有数据与trash不能交给媒体库扫库。不要使用 `down -v` 或删除私有目录来“重置”已有实例：账号、manifest与播放密钥必须一起备份。

## 可重复集成验证

```zsh
# long: 每次生成私有临时数据、本地只读源和随机测试凭据，不读取真实网盘/生产配置。
node scripts/smoke-strm.mjs
node scripts/smoke-strm.mjs --docker
# long: 浏览器复核时保留本机服务；凭据只写入临时目录browser-fixture.json，不打印到终端。
node scripts/smoke-strm.mjs --keep-running
```

smoke校验首次初始化、禁止重复匿名初始化、管理员认证、候选驱动列表、Local目录读取/编辑、只读预览、完整扫描生成、签名播放GET/HEAD/Range、错误签名拒绝、重复生成幂等、已生成任务锁定、旧写入接口403、内嵌前端资源、私有文件权限、重启保留和测试凭据日志遮挡。HTTP 200还要核对JSON业务code。

测试默认停止自己启动的进程/容器，保留私有夹具、证据、容器与卷以便复核，不自动清理。`--keep-running`用于本机浏览器演示，进程PID和地址在证据文件中；停止前核对PID与该夹具目录。Range smoke验证的是Local夹具经签名入口代理传输，不代表真实网盘或Emby/Jellyfin客户端播放已经通过。

上游及M0回归入口：

```zsh
go test ./internal/db/... ./internal/transcode/... ./internal/media/... \
  ./internal/fs/... ./server/handles/... ./server/common/... ./pkg/torrent/... \
  ./internal/product/... ./internal/strm/... ./internal/op/... \
  ./server/middlewares/... ./scripts/pack-strm/...
```

正式CI与发布管线、真实网盘和媒体服务器验收、任务动作恢复、缺失回收和数据库升级/回滚演练仍按后续路线图交付。
