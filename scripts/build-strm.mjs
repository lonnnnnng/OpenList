import { spawnSync } from "node:child_process"
import { createHash } from "node:crypto"
import { cpSync, mkdirSync, readFileSync, writeFileSync } from "node:fs"
import { dirname, join } from "node:path"
import { fileURLToPath } from "node:url"

const root = fileURLToPath(new URL("../", import.meta.url))
const manifest = JSON.parse(readFileSync(join(root, "strm-build.json"), "utf8"))
const currentLockHash = createHash("sha256").update(readFileSync(join(root,"web/pnpm-lock.yaml"))).digest("hex")
if (currentLockHash !== manifest.frontendLockSHA256) throw new Error("前端锁文件与构建来源记录不一致，请先审核依赖变更")
if (!readFileSync(join(root,"Dockerfile.strm"),"utf8").includes(manifest.runtimeImage)) throw new Error("运行镜像与构建来源记录不一致")
const targetArg = process.argv.indexOf("--target")
const target = targetArg < 0 ? "native" : process.argv[targetArg + 1]
if (!["native", "linux/amd64"].includes(target)) throw new Error("--target 只支持 native 或 linux/amd64")
if (process.version !== manifest.nodeVersion) throw new Error(`请使用Node ${manifest.nodeVersion}，当前为${process.version}`)

// long: 命令参数直接传数组，不经过Shell拼接；工具版本不匹配时失败，不能悄悄改锁文件。
const run = (command, args, options = {}) => {
  const result = spawnSync(command, args, {
    cwd: root, env: { ...process.env, GOTOOLCHAIN: "local", HUSKY: "0" },
    stdio: "inherit", ...options,
  })
  if (result.error) throw result.error
  if (result.status !== 0) throw new Error(`${command} 执行失败，退出码 ${result.status}`)
  return result.stdout?.trim()
}
const capture = (command, args) => run(command, args, { stdio: "pipe", encoding: "utf8" })
const goVersion = capture("go", ["env", "GOVERSION"])
if (goVersion !== manifest.goVersion) throw new Error(`请使用Go ${manifest.goVersion}，当前为${goVersion}`)
const pnpm = ["--yes", `pnpm@${manifest.pnpmVersion}`, "--dir", "web"]
if (!process.argv.includes("--skip-install")) run("npx", [...pnpm, "install", "--frozen-lockfile"])
run("npx", [...pnpm, "run", "lint"])
run("npx", [...pnpm, "run", "build"], {
  env: { ...process.env, HUSKY: "0", VITE_STRM_MODE: "true", VITE_LITE: "true" },
})

const epoch = capture("git", ["show", "-s", "--format=%ct", manifest.backendBase])
const builtAt = new Date(Number(epoch) * 1000).toISOString()
// long: 独立ZIP只替换本产品生成文件，保留上游dist及未知文件；排序和固定时间使重复构建可比较。
run("go", ["run", "-mod=readonly", "./scripts/pack-strm", "-source", "web/dist", "-output", "public/strm-bundle.zip", "-epoch", epoch])
const nativeOS = capture("go", ["env", "GOOS"])
const nativeArch = capture("go", ["env", "GOARCH"])
const [os, arch] = target === "native" ? [nativeOS, nativeArch] : ["linux", "amd64"]
const outputDir = join(root, "build", "strm", `${os}-${arch}`)
mkdirSync(outputDir, { recursive: true })
const output = join(outputDir, "openlist-strm")
const head = capture("git", ["rev-parse", "HEAD"])
const dirty = Boolean(capture("git", ["status", "--porcelain", "--untracked-files=normal"]))
const linker = "github.com/OpenListTeam/OpenList/v4/internal/conf"
const flags = ["-s", "-w", ...Object.entries({
  BuiltAt: builtAt, GitCommit: head + (dirty ? "-dirty" : ""),
  Version: `main-${manifest.backendBase.slice(0, 8)}`,
  WebVersion: `strm-${manifest.frontendCommit.slice(0, 8)}`,
  ProductVersion: manifest.productVersion, FrontendCommit: manifest.frontendCommit,
  UpstreamCommit: manifest.backendBase,
}).map(([key, value]) => `-X=${linker}.${key}=${value}`)].join(" ")
run("go", ["build", "-mod=readonly", "-trimpath", "-tags=jsoniter,strm_product", `-ldflags=${flags}`, "-o", output, "."], {
  env: { ...process.env, GOTOOLCHAIN: "local", CGO_ENABLED: "0", GOOS: os, GOARCH: arch },
})
const sha256 = (path) => createHash("sha256").update(readFileSync(path)).digest("hex")
writeFileSync(join(outputDir, "SHA256SUMS"), `${sha256(output)}  openlist-strm\n`)
writeFileSync(join(outputDir, "build-info.json"), JSON.stringify({
  ...manifest, head, dirty, builtAt, target: `${os}/${arch}`, cgo: false,
  frontendBundleSHA256: sha256(join(root, "public/strm-bundle.zip")),
  binarySHA256: sha256(output),
}, null, 2) + "\n")
for (const relative of ["opt/openlist/data", "data/output/library", "data/output/trash"]) {
  const directory = join(root, "build/strm/runtime", relative)
  mkdirSync(directory, { recursive: true, mode: relative.endsWith("data") ? 0o700 : 0o750 })
  writeFileSync(join(directory, ".keep"), "", { mode: 0o600 })
}
cpSync(join(root, "LICENSE"), join(outputDir, "LICENSE-backend"))
cpSync(join(root, "web/LICENSE"), join(outputDir, "LICENSE-frontend"))
console.log(`构建完成：${output}`)
