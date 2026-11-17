import assert from "node:assert/strict"
import { spawn, spawnSync } from "node:child_process"
import { createHash, randomBytes } from "node:crypto"
import { chmodSync, closeSync, existsSync, mkdirSync, mkdtempSync, openSync, readFileSync, realpathSync, statSync, unlinkSync, writeFileSync } from "node:fs"
import { createServer } from "node:net"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { fileURLToPath } from "node:url"

const root = fileURLToPath(new URL("../", import.meta.url))
const docker = process.argv.includes("--docker")
const keepRunning = process.argv.includes("--keep-running")
const fixture = realpathSync(mkdtempSync(join(tmpdir(), "wangpan-strm-smoke-")))
chmodSync(fixture, 0o700)
const data = join(fixture, "data")
const output = join(fixture, "output")
const source = join(fixture, "source")
for (const path of [data, output, source]) mkdirSync(path, { mode: 0o700 })
if (docker) chmodSync(source, 0o755)
writeFileSync(join(source, "电影 #1 百分号%.mkv"), "read-only directory fixture", { mode: 0o644 })
writeFileSync(join(output, "unknown.keep"), "must survive startup and restart", { mode: 0o600 })
const password = randomBytes(24).toString("base64url")
const secretProbe = randomBytes(24).toString("base64url")
const username = "smoke-admin"
const logs = join(fixture, "server.log")
const run = (args) => {
  const result = spawnSync("docker", args, { encoding: "utf8", cwd: root })
  if (result.error) throw result.error
  if (result.status !== 0) throw new Error(`docker ${args[0]} 失败；测试数据保留在 ${fixture}`)
  return (args[0] === "logs" ? result.stdout + result.stderr : result.stdout).trim()
}
const port = await new Promise((resolve, reject) => {
  const server = createServer()
  server.on("error", reject)
  server.listen(0, "127.0.0.1", () => {
    const port = server.address().port
    server.close(() => resolve(port))
  })
})
const url = `http://127.0.0.1:${port}`
const name = `wangpan-strm-smoke-${randomBytes(5).toString("hex")}`
const binary = join(root, "build/strm/darwin-arm64/openlist-strm")
let child
let started = false
let token = ""
const delay = (ms) => new Promise((resolve) => setTimeout(resolve, ms))

async function start() {
  if (docker) {
    run(["start", name])
  } else {
    // long: 隔离所有继承的OpenList配置，测试不能误连用户账号或复用生产数据库。
    const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith("OPENLIST_")))
    const fd = openSync(logs, "a", 0o600)
    child = spawn(binary, ["strm-server", "--data", data, "--log-std", "--debug"], {
      cwd: root, detached: true, stdio: ["ignore", fd, fd],
      env: { ...env, OPENLIST_ADDR: "127.0.0.1", OPENLIST_HTTP_PORT: String(port), OPENLIST_STRM_OUTPUT_ROOT: output, OPENLIST_STRM_PUBLIC_URL: url },
    })
    closeSync(fd)
    child.on("error", () => {})
  }
  started = true
  for (let n = 0; n < 120; n++) {
    try {
      const response = await fetch(`${url}/ready`, { signal: AbortSignal.timeout(1000) })
      if (response.ok && (await response.json()).ready === true) return
    } catch {}
    await delay(250)
  }
  throw new Error(`服务未就绪；私有日志在 ${logs}`)
}

async function stop() {
  if (!started) return
  if (docker) {
    run(["stop", "--time", "35", name])
    writeFileSync(logs, run(["logs", name]), { mode: 0o600 })
  } else {
    // long: 仅终止本轮spawn的进程；保留测试目录，不清理任何未知数据。
    if (child.exitCode === null) {
      await new Promise((resolve, reject) => {
        const timer = setTimeout(() => reject(new Error("关停超时")), 35000)
        child.once("exit", () => { clearTimeout(timer); resolve() })
        child.kill("SIGTERM")
      })
    }
  }
  started = false
}

async function request(path, body, authenticated = true, method) {
  const response = await fetch(url + path, {
    method: method ?? (body === undefined ? "GET" : "POST"),
    headers: { "Content-Type": "application/json", ...(authenticated ? { Authorization: token } : {}) },
    body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(path.endsWith("/generate") ? 900000 : 5000),
  })
  const payload = await response.json()
  return { status: response.status, ...payload }
}
async function success(path, body, authenticated = true, method) {
  const response = await request(path, body, authenticated, method)
  // long: 上游业务错误常用HTTP 200，必须同时验证JSON业务code，不能误报成功。
  assert.equal(response.code, 200, `${path} 业务失败（code ${response.code}）：${response.message} ${JSON.stringify(response.data ?? {})}`)
  return response.data
}
function privateCopy(relative) {
  if (!docker) return join(data, relative)
  const destination = join(fixture, relative)
  run(["cp", `${name}:/opt/openlist/data/${relative}`, destination])
  return destination
}
const hash = (path) => createHash("sha256").update(readFileSync(path)).digest("hex")
const checkNoSecrets = () => {
  const content = readFileSync(logs, "utf8")
  for (const secret of [password, secretProbe, token]) {
    assert.ok(!secret || !content.includes(secret), "运行日志泄露测试凭据")
  }
}

try {
  if (docker) {
    const dataVolume = `${name}-data`
    const outputVolume = `${name}-output`
    for (const volume of [dataVolume, outputVolume]) run(["volume", "create", "--label", "wangpan.scope=m0-smoke", volume])
    run(["create", "--name", name, "--label", "wangpan.scope=m0-smoke", "--platform", "linux/amd64",
      "-p", `127.0.0.1:${port}:5344`, "--health-interval=2s", "--health-start-period=2s",
      "-e", `OPENLIST_STRM_PUBLIC_URL=${url}`,
      "-v", `${dataVolume}:/opt/openlist/data`, "-v", `${outputVolume}:/data/output`,
      "-v", `${source}:/fixture:ro`, "openlist-strm:dev", "strm-server", "--data", "/opt/openlist/data", "--log-std", "--debug"])
    assert.equal(run(["inspect", "--format", "{{.Config.User}}", name]), "65532:65532")
  } else {
    assert.ok(existsSync(binary), "先构建native产物")
  }
  await start()
  assert.equal((await success("/api/public/init_status", undefined, false)).initialized, false)
  await success("/api/public/init/setup", { username, password, site_title: "STRM 开发验证" }, false)
  const secondSetup = await request("/api/public/init/setup", { username, password }, false)
  assert.equal(secondSetup.code, 400, "已初始化系统不能被匿名重新设置")
  token = (await success("/api/auth/login", { username, password }, false)).token
  assert.ok(token)
  assert.equal((await success("/api/me")).role, 2)
  assert.ok([401, 403].includes((await request("/api/admin/strm/health", undefined, false)).code))
  const health = await success("/api/admin/strm/health")
  assert.equal(health.ready, true)
  assert.equal(health.task_execution_available, true)
  assert.equal(health.milestone, "M2")
  const drivers = await success("/api/admin/driver/list")
  assert.ok(JSON.stringify(drivers).includes("Local"))
  assert.ok(!JSON.stringify(drivers).includes("Strm"), "旧STRM同步驱动不能启用")
  const storage = await success("/api/admin/storage/create", {
    mount_path: "/smoke", driver: "Local",
    addition: JSON.stringify({ root_folder_path: docker ? "/fixture" : source, thumbnail: false, show_hidden: true, directory_size: false }),
  })
  assert.equal((await success(`/api/admin/storage/get?id=${storage.id}`)).status, "work")
  const editable = await success(`/api/admin/storage/get?id=${storage.id}`)
  editable.remark = "本地只读集成测试"
  await success("/api/admin/storage/update", editable)
  assert.equal((await success(`/api/admin/storage/get?id=${storage.id}`)).remark, editable.remark)
  const listed = await success("/api/fs/list", { path: "/smoke", page: 1, per_page: 100 })
  assert.ok(listed.content.some((file) => file.name === "电影 #1 百分号%.mkv"))
  const browsed = await success(`/api/admin/strm/browse?storage_id=${storage.id}&path=%2F`, undefined)
  assert.equal(browsed.complete, true)
  assert.ok(browsed.items.some((file) => file.name === "电影 #1 百分号%.mkv"))
  const task = await success("/api/admin/strm/tasks", {
    name: "本地预览", storage_id: storage.id, source_path: "/", output_subdir: "smoke-preview",
  })
  assert.equal(task.state, "draft")
  assert.equal((await success(`/api/admin/strm/tasks/${task.id}/runs?page=1&per_page=20`)).total, 0, "保存草稿不能隐式扫描")
  assert.equal((await request("/api/admin/strm/tasks", {
    name: "嵌套冲突", storage_id: storage.id, source_path: "/", output_subdir: "smoke-preview/nested",
  })).code, 400, "重叠输出目录必须被拒绝")
  const preview = await success(`/api/admin/strm/tasks/${task.id}/preview`, {})
  assert.equal(preview.complete, true)
  assert.equal(preview.read_only, true)
  assert.equal(preview.candidates, 1)
  assert.equal(preview.items[0].output_path, "smoke-preview/电影 #1 百分号%.mkv.strm")
  const runs = await success(`/api/admin/strm/tasks/${task.id}/runs?page=1&per_page=20`)
  assert.equal(runs.total, 1, "预览历史未被持久化")
  assert.equal(runs.content[0].preview, true)
  assert.ok(!existsSync(join(output, "library/smoke-preview")), "预览写入了STRM输出")
  const loadedTask = await success(`/api/admin/strm/tasks/${task.id}`)
  const updatedTask = await success(`/api/admin/strm/tasks/${task.id}`, {
    name: task.name, storage_id: task.storage_id, source_path: task.source_path,
    output_subdir: task.output_subdir, config_revision: loadedTask.config_revision,
  }, true, "PUT")
  assert.equal(updatedTask.config_revision, loadedTask.config_revision + 1)
  assert.equal((await request(`/api/admin/strm/tasks/${task.id}`, {
    name: task.name, storage_id: task.storage_id, source_path: task.source_path,
    output_subdir: task.output_subdir, config_revision: loadedTask.config_revision,
  }, true, "PUT")).code, 400, "过期配置版本必须被拒绝")
  await success(`/api/admin/strm/tasks/${task.id}`, undefined, true, "DELETE")
  const reused = await success("/api/admin/strm/tasks", {
    name: "本地生成", storage_id: storage.id, source_path: "/", output_subdir: "smoke-playback",
  })
  const generated = await success(`/api/admin/strm/tasks/${reused.id}/generate`, {})
  assert.equal(generated.status, "succeeded")
  assert.equal(generated.added, 1)
  assert.equal(generated.generation, 1, "首次生成未返回已预留的运行编号")
  assert.equal(generated.missing_confirmed, 0, "首轮运行不能把文件判作缺失")
  const generatedOutput = join(fixture, "generated.strm")
  const generatedRelative = "library/smoke-playback/电影 #1 百分号%.mkv.strm"
  if (docker) run(["cp", `${name}:/data/output/${generatedRelative}`, generatedOutput])
  else writeFileSync(generatedOutput, readFileSync(join(output, generatedRelative)))
  const entry = readFileSync(generatedOutput, "utf8").trim()
  assert.ok(entry.startsWith(`${url}/api/public/strm/`), "STRM内容未使用签名中间件入口")
  assert.ok(!entry.includes("/smoke/") && !entry.includes("remote"), "STRM内容泄露源路径或远端ID")
  const head = await fetch(entry, { method: "HEAD", signal: AbortSignal.timeout(10000) })
  assert.equal(head.status, 200, "签名播放HEAD未通过")
  const ranged = await fetch(entry, { headers: { Range: "bytes=0-3" }, signal: AbortSignal.timeout(10000) })
  assert.equal(ranged.status, 206, "签名播放Range未返回206")
  assert.equal(await ranged.text(), "read", "签名播放Range返回了错误字节")
  const badSignature = await fetch(entry.replace(/sig=[^&]+/, "sig=invalid"), { signal: AbortSignal.timeout(10000) })
  assert.equal(badSignature.status, 404, "错误签名没有被拒绝")
  assert.equal((await request(`/api/admin/strm/tasks/${reused.id}`, {
    name: "不应成功", storage_id: reused.storage_id, source_path: reused.source_path,
    output_subdir: reused.output_subdir, config_revision: reused.config_revision,
  }, true, "PUT")).code, 400, "已生成任务仍可通过草稿接口修改")
  const repeated = await success(`/api/admin/strm/tasks/${reused.id}/generate`, {})
  assert.equal(repeated.status, "succeeded")
  assert.equal(repeated.unchanged, 1, "重复生成没有识别未变化文件")
  assert.equal(repeated.generation, 2, "重复生成复用了旧运行编号")
  assert.equal(repeated.missing_confirmed, 0, "重新出现的文件仍被计为缺失")
  const sourceFile = join(source, "电影 #1 百分号%.mkv")
  unlinkSync(sourceFile)
  const firstAbsent = await success(`/api/admin/strm/tasks/${reused.id}/generate`, {})
  assert.equal(firstAbsent.status, "succeeded")
  assert.equal(firstAbsent.missing_confirmed, 0, "一次完整缺失就被标记为两轮确认")
  const secondAbsent = await success(`/api/admin/strm/tasks/${reused.id}/generate`, {})
  assert.equal(secondAbsent.missing_confirmed, 1, "连续两轮完整缺失没有进入待预览列表")
  let missing = await success(`/api/admin/strm/tasks/${reused.id}/missing-preview?page=1&per_page=100`)
  assert.equal(missing.total_missing, 1)
  assert.equal(missing.items[0].output_state, "ready_for_review")
  assert.equal(missing.mass_disappearance, false)
  const retainedOutput = join(fixture, "retained.strm")
  if (docker) run(["cp", `${name}:/data/output/${generatedRelative}`, retainedOutput])
  else writeFileSync(retainedOutput, readFileSync(join(output, generatedRelative)))
  assert.ok(readFileSync(retainedOutput, "utf8").includes("/api/public/strm/"), "只读缺失预览误删了STRM产物")
  const trashPlan = await success(`/api/admin/strm/tasks/${reused.id}/trash-preview`, {})
  assert.equal(trashPlan.eligible, 1, "匹配manifest摘要的两轮缺失文件没有进入回收预览")
  assert.equal(trashPlan.mass_disappearance, false)
  const trashResult = await success(`/api/admin/strm/tasks/${reused.id}/trash-preview/${trashPlan.plan_id}/execute`, { confirm_mass_disappearance: false })
  assert.equal(trashResult.status, "committed")
  assert.equal(trashResult.committed, 1)
  const trashList = await success(`/api/admin/strm/tasks/${reused.id}/trash?page=1&per_page=100`)
  assert.equal(trashList.total, 1)
  const trashEntry = trashList.content[0]
  const trashCopy = join(fixture, "trash-copy.strm")
  if (docker) run(["cp", `${name}:/data/output/trash/${trashEntry.trash_path}`, trashCopy])
  else writeFileSync(trashCopy, readFileSync(join(output, "trash", trashEntry.trash_path)))
  assert.equal(readFileSync(trashCopy, "utf8"), readFileSync(retainedOutput, "utf8"), "回收没有保留原始STRM内容")
  assert.equal((await fetch(entry, { signal: AbortSignal.timeout(10000) })).status, 404, "回收后稳定播放入口仍可用")
  writeFileSync(sourceFile, "read-only directory fixture", { mode: 0o644 })
  assert.equal((await request(`/api/admin/strm/tasks/${reused.id}/trash/${trashEntry.id}/restore`, {})).code, 200, "回收项恢复失败")
  const restoredOutput = join(fixture, "restored.strm")
  if (docker) run(["cp", `${name}:/data/output/${generatedRelative}`, restoredOutput])
  else writeFileSync(restoredOutput, readFileSync(join(output, generatedRelative)))
  assert.equal(readFileSync(restoredOutput, "utf8"), readFileSync(retainedOutput, "utf8"), "恢复没有还原原始STRM内容")
  assert.equal((await fetch(entry, { headers: { Range: "bytes=0-3" }, signal: AbortSignal.timeout(10000) })).status, 206, "恢复后稳定播放入口没有恢复")
  unlinkSync(sourceFile)
  await success(`/api/admin/strm/tasks/${reused.id}/generate`, {})
  const secondMissingRun = await success(`/api/admin/strm/tasks/${reused.id}/generate`, {})
  assert.equal(secondMissingRun.missing_confirmed, 1)
  const automaticPlan = await success(`/api/admin/strm/tasks/${reused.id}/trash-preview`, {})
  assert.equal(automaticPlan.eligible, 1)
  const automaticTrash = await success(`/api/admin/strm/tasks/${reused.id}/trash-preview/${automaticPlan.plan_id}/execute`, { confirm_mass_disappearance: false })
  assert.equal(automaticTrash.committed, 1)
  writeFileSync(sourceFile, "read-only directory fixture", { mode: 0o644 })
  const automaticRestore = await success(`/api/admin/strm/tasks/${reused.id}/generate`, {})
  assert.equal(automaticRestore.status, "succeeded", JSON.stringify(automaticRestore))
  assert.equal(automaticRestore.unchanged, 1, "远端文件重现后未自动恢复受管STRM")
  const trashAfterReappearance = await success(`/api/admin/strm/tasks/${reused.id}/trash?page=1&per_page=100`)
  assert.equal(trashAfterReappearance.total, 0, "自动恢复后回收记录仍保持活动")
  const tamperedOutput = join(fixture, "tampered.strm")
  writeFileSync(tamperedOutput, "hand-edited output", { mode: 0o640 })
  if (docker) run(["cp", tamperedOutput, `${name}:/data/output/${generatedRelative}`])
  else writeFileSync(join(output, generatedRelative), readFileSync(tamperedOutput))
  const reappeared = await success(`/api/admin/strm/tasks/${reused.id}/generate`, {})
  assert.equal(reappeared.status, "partial", "被修改的本地产物没有作为冲突保留")
  missing = await success(`/api/admin/strm/tasks/${reused.id}/missing-preview?page=1&per_page=100`)
  assert.equal(missing.total_missing, 0, "文件重新出现后缺失确认未清零")
  const observedOutput = join(fixture, "observed.strm")
  if (docker) run(["cp", `${name}:/data/output/${generatedRelative}`, observedOutput])
  else writeFileSync(observedOutput, readFileSync(join(output, generatedRelative)))
  assert.equal(readFileSync(observedOutput, "utf8"), "hand-edited output", "源文件重现时覆盖了管理员修改的STRM")
  for (const path of ["/api/fs/remove", "/api/fs/rename", "/api/seed/offline_download", "/api/admin/task/upload/clear_done"]) {
    assert.equal((await request(path, {})).status, 403, `${path} 未被产品门禁阻止`)
  }
  const signedResponse = await fetch(`${url}/d/test?sign=${secretProbe}&refresh_token=${secretProbe}`, { signal: AbortSignal.timeout(5000) })
  assert.equal(signedResponse.status, 403)
  const html = await fetch(url).then((response) => response.text())
  assert.ok(html.includes("<html") && html.includes("/assets/"), "Go二进制未提供内嵌前端")
  const script = html.match(/src="([^"]+\.js)"/)
  assert.ok(script)
  assert.equal((await fetch(new URL(script[1], url))).status, 200)
  if (docker) {
    run(["exec", name, "/opt/openlist/openlist-strm", "healthcheck"])
    for (let n = 0; n < 20 && run(["inspect", "--format", "{{.State.Health.Status}}", name]) !== "healthy"; n++) await delay(500)
    assert.equal(run(["inspect", "--format", "{{.State.Health.Status}}", name]), "healthy")
  }
  const keyHash = hash(privateCopy("strm-playback.key"))
  assert.equal(statSync(privateCopy("config.json")).mode & 0o777, 0o600)
  assert.equal(statSync(privateCopy("data.db")).mode & 0o777, 0o600)
  assert.equal(statSync(privateCopy("strm-playback.key")).mode & 0o777, 0o600)
  if (!docker) assert.equal(statSync(data).mode & 0o777, 0o700)
  await stop()
  checkNoSecrets()
  await start()
  assert.equal((await success("/api/public/init_status", undefined, false)).initialized, true)
  token = (await success("/api/auth/login", { username, password }, false)).token
  assert.equal(hash(privateCopy("strm-playback.key")), keyHash, "重启改变了播放密钥")
  const storages = await success("/api/admin/storage/list?page=1&per_page=100")
  assert.equal(storages.total, 1, "重启未保留存储配置")
  assert.equal((await success("/api/fs/list", { path: "/smoke", page: 1, per_page: 100 })).total, 1)
  if (!docker) assert.equal(readFileSync(join(output, "unknown.keep"), "utf8"), "must survive startup and restart")
  const evidence = { mode: docker ? "docker-linux-amd64" : "native-darwin-arm64", url, container: docker ? name : null,
    pid: !docker && keepRunning ? child.pid : null, fixture, ready: true,
    checks: ["初始化与登录", "管理员门禁", "Local只读目录", "严格浏览", "草稿保存不扫描", "重叠输出拒绝", "只读预览与历史", "预览不写产物", "配置版本冲突", "草稿删除与输出复用", "Local完整扫描生成", "完整基线与运行编号", "连续两轮缺失预览", "受管产物回收", "回收后播放入口撤销", "无覆盖恢复与播放恢复", "远端重现自动恢复", "重新出现清零缺失状态", "修改产物冲突保护", "签名播放GET/HEAD/Range", "错误签名拒绝", "重复生成幂等", "已生成任务锁定", "旧写入接口403", "前端内嵌资源", "私有文件600", "重启保留账号/存储/播放密钥", "日志无测试凭据"],
    realCloudTested: false, taskExecutionAvailable: true, realMediaServerTested: false }
  writeFileSync(join(fixture, "evidence.json"), JSON.stringify(evidence, null, 2) + "\n", { mode: 0o600 })
  if (keepRunning) {
    // long: 浏览器复核凭据只存在私有夹具文件，不进入终端或构建制品。
    writeFileSync(join(fixture, "browser-fixture.json"), JSON.stringify({ url, username, password }) + "\n", { mode: 0o600 })
    if (!docker) child.unref()
  } else {
    await stop()
    checkNoSecrets()
  }
  console.log(JSON.stringify(evidence, null, 2))
} catch (error) {
  await stop().catch(() => {})
  console.error(`${error.message}；测试夹具保留：${fixture}`)
  process.exitCode = 1
}
