import { Box, Button, Checkbox, FormControl, FormLabel, Heading, HStack, Input, Select, Text, VStack } from "@hope-ui/solid"
import { createSignal, For, onMount, Show } from "solid-js"
import { useManageTitle, useRouter } from "~/hooks"
import { handleResp, r } from "~/utils"
import { Resp } from "~/types"

type Health = {
  ready: boolean
  product_version: string
  upstream_commit: string
  frontend_commit: string
  output_root: string
  public_url: string
  task_execution_available: boolean
}

type Storage = { id: number; mount_path: string; driver: string; status: string }
type Task = { id: string; name: string; storage_id: number; source_path: string; output_subdir: string; entry_prefix: string; public_url: string; state: string; config_revision: number }
type Preview = { complete: boolean; directories: number; files: number; candidates: number; items_truncated: boolean; items: Array<{ source_path: string; output_path: string; size: number }>; issues: Array<{ path: string; message: string }> }
type Generation = { run_id: string; generation: number; complete: boolean; status: string; candidates: number; added: number; updated: number; unchanged: number; missing_confirmed: number; failed: number; issues: Array<{ path: string; message: string }> }
type MissingPreview = { generation: number; last_complete_generation: number; total_managed: number; total_missing: number; mass_disappearance: boolean; items_truncated: boolean; items: Array<{ source_path: string; output_path: string; size: number; missing_confirmations: number; output_state: string }> }
type TrashPlan = { plan_id: string; expires_at: string; generation: number; eligible: number; blocked: number; mass_disappearance: boolean }
type TrashRun = { plan_id: string; status: string; committed: number; failed: number }
type TrashItem = { id: string; output_path: string; content_hash: string; trashed_at: string; state: string }
type BrowseResult = { storage_id: number; path: string; complete: boolean; items: Array<{ path: string; name: string; original_name: string; is_dir: boolean; size: number }> }

const Overview = () => {
  useManageTitle("manage.sidemenu.tasks")
  const { to } = useRouter()
  const [health, setHealth] = createSignal<Health>()
  const [loading, setLoading] = createSignal(false)
  const [error, setError] = createSignal("")
  const [storages, setStorages] = createSignal<Storage[]>([])
  const [tasks, setTasks] = createSignal<Task[]>([])
  const [name, setName] = createSignal("")
  const [storageID, setStorageID] = createSignal("")
  const [sourcePath, setSourcePath] = createSignal("/")
  const [outputSubdir, setOutputSubdir] = createSignal("")
  const [entryPrefix, setEntryPrefix] = createSignal("")
  const [saving, setSaving] = createSignal(false)
  const [editing, setEditing] = createSignal<Task>()
  const [previewing, setPreviewing] = createSignal("")
  const [preview, setPreview] = createSignal<Preview>()
  const [generating, setGenerating] = createSignal("")
  const [generation, setGeneration] = createSignal<Generation>()
  const [missingPreviewing, setMissingPreviewing] = createSignal("")
  const [missingPreview, setMissingPreview] = createSignal<MissingPreview>()
  const [missingTask, setMissingTask] = createSignal<Task>()
  const [missingPage, setMissingPage] = createSignal(1)
  const [trashPlan, setTrashPlan] = createSignal<TrashPlan>()
  const [trashExecution, setTrashExecution] = createSignal<TrashRun>()
  const [confirmMassDisappearance, setConfirmMassDisappearance] = createSignal(false)
  const [planningTrash, setPlanningTrash] = createSignal(false)
  const [executingTrash, setExecutingTrash] = createSignal(false)
  const [trashTask, setTrashTask] = createSignal<Task>()
  const [trashItems, setTrashItems] = createSignal<TrashItem[]>([])
  const [trashTotal, setTrashTotal] = createSignal(0)
  const [loadingTrash, setLoadingTrash] = createSignal(false)
  const [browsePath, setBrowsePath] = createSignal("/")
  const [browseItems, setBrowseItems] = createSignal<BrowseResult["items"]>([])
  const [browseReady, setBrowseReady] = createSignal(false)
  const [browseLoading, setBrowseLoading] = createSignal(false)
  const [browseError, setBrowseError] = createSignal("")
  const loadData = async () => {
    const [storageResp, taskResp] = (await Promise.all([
      r.get("/admin/storage/list?page=1&per_page=100") as unknown as Promise<Resp<{ content: Storage[] }>>,
      r.get("/admin/strm/tasks?page=1&per_page=100") as unknown as Promise<Resp<{ content: Task[] }>>,
    ]))
    handleResp(storageResp, (data) => {
      setStorages(data.content)
      if (!storageID() && data.content.length) setStorageID(String(data.content[0].id))
    }, setError)
    handleResp(taskResp, (data) => setTasks(data.content), setError)
  }
  const refresh = async () => {
    setLoading(true)
    setError("")
    try {
      const resp: Resp<Health> = await r.get("/admin/strm/health")
      handleResp(resp, setHealth, (message) => setError(message))
      await loadData()
    } catch {
      setError("无法连接后端，请检查服务状态后重试。")
    } finally {
      // long: 请求失败也要恢复刷新按钮，避免网络异常后管理员只能重载整个页面。
      setLoading(false)
    }
  }
  onMount(refresh)
  const createDraft = async () => {
    setSaving(true)
    setError("")
    setPreview(undefined)
    try {
      const draft = { name: name(), storage_id: Number(storageID()), source_path: sourcePath(), output_subdir: outputSubdir(), entry_prefix: entryPrefix() }
      const resp: Resp<Task> = editing()
        ? await r.put(`/admin/strm/tasks/${editing()!.id}`, { ...draft, config_revision: editing()!.config_revision })
        : await r.post("/admin/strm/tasks", draft)
      handleResp(resp, async () => {
        setName("")
        setOutputSubdir("")
        setEntryPrefix("")
        setEditing(undefined)
        await loadData()
      }, setError)
    } catch {
      setError("无法保存任务草稿，请检查输入和存储状态。")
    } finally {
      setSaving(false)
    }
  }
  const editDraft = async (task: Task) => {
    setError("")
    try {
      const resp: Resp<Task> = await r.get(`/admin/strm/tasks/${task.id}`)
      handleResp(resp, (current) => {
        setEditing(current)
        setName(current.name)
        setStorageID(String(current.storage_id))
        setSourcePath(current.source_path)
        setOutputSubdir(current.output_subdir)
        setEntryPrefix(current.entry_prefix || "")
        setPreview(undefined)
      }, setError)
    } catch {
      setError("无法读取草稿，请刷新任务列表后重试。")
    }
  }
  const deleteDraft = async (task: Task) => {
    setError("")
    try {
      const resp: Resp<unknown> = await r.delete(`/admin/strm/tasks/${task.id}`)
      handleResp(resp, async () => {
        if (editing()?.id === task.id) {
          setEditing(undefined)
          setName("")
          setOutputSubdir("")
          setEntryPrefix("")
        }
        if (preview()) setPreview(undefined)
        await loadData()
      }, setError)
    } catch {
      setError("仅未运行的草稿可以删除。")
    }
  }
  const runPreview = async (task: Task) => {
    setPreviewing(task.id)
    setError("")
    setPreview(undefined)
    try {
      const resp: Resp<Preview> = await r.post(`/admin/strm/tasks/${task.id}/preview`, {})
      handleResp(resp, setPreview, setError)
    } catch {
      setError("预览请求失败。网盘读取异常不会被解释为文件缺失。")
    } finally {
      setPreviewing("")
    }
  }
  const runGenerate = async (task: Task) => {
    setGenerating(task.id)
    setError("")
    setGeneration(undefined)
    setMissingPreview(undefined)
    setMissingTask(undefined)
    setMissingPage(1)
    try {
      const resp: Resp<Generation> = await r.post(`/admin/strm/tasks/${task.id}/generate`, {})
      handleResp(resp, setGeneration, setError)
      await loadData()
    } catch {
      setError("生成请求失败。请检查任务运行记录和目标目录后重试。")
    } finally {
      setGenerating("")
    }
  }
  const runMissingPreview = async (task: Task, page = 1) => {
    setMissingPreviewing(task.id)
    setError("")
    setMissingTask(task)
    setMissingPage(page)
    try {
      const resp: Resp<MissingPreview> = await r.get(`/admin/strm/tasks/${task.id}/missing-preview?page=${page}&per_page=100`)
      handleResp(resp, setMissingPreview, setError)
    } catch {
      setError("缺失预览读取失败，请等待任务结束后重试。")
    } finally {
      setMissingPreviewing("")
    }
  }
  const createTrashPlan = async () => {
    const task = missingTask()
    if (!task) return
    setPlanningTrash(true)
    setError("")
    setTrashPlan(undefined)
    setTrashExecution(undefined)
    setConfirmMassDisappearance(false)
    try {
      const resp: Resp<TrashPlan> = await r.post(`/admin/strm/tasks/${task.id}/trash-preview`, {})
      handleResp(resp, setTrashPlan, setError)
    } catch {
      setError("无法创建回收预览，任务状态可能已变化；请重新扫描缺失项。")
    } finally {
      setPlanningTrash(false)
    }
  }
  const executeTrashPlan = async () => {
    const task = missingTask()
    const plan = trashPlan()
    if (!task || !plan) return
    setExecutingTrash(true)
    setError("")
    try {
      const resp: Resp<TrashRun> = await r.post(`/admin/strm/tasks/${task.id}/trash-preview/${plan.plan_id}/execute`, {
        confirm_mass_disappearance: confirmMassDisappearance(),
      })
      handleResp(resp, (result) => {
        setTrashExecution(result)
        if (result.status === "committed" || result.status === "blocked") setTrashPlan(undefined)
      }, setError)
      await runMissingPreview(task, 1)
      if (trashTask()?.id === task.id) await loadTrash(task)
    } catch {
      setError("回收计划未能执行。预览过期或产物变化时，请重新核对后创建新预览。")
    } finally {
      setExecutingTrash(false)
    }
  }
  const loadTrash = async (task: Task) => {
    setLoadingTrash(true)
    setError("")
    setTrashTask(task)
    try {
      const resp = await r.get(`/admin/strm/tasks/${task.id}/trash?page=1&per_page=100`) as unknown as Resp<{ content: TrashItem[]; total: number }>
      handleResp(resp, (data) => { setTrashItems(data.content); setTrashTotal(data.total) }, setError)
    } catch {
      setError("无法读取回收站，请刷新后重试。")
    } finally {
      setLoadingTrash(false)
    }
  }
  const restoreTrash = async (item: TrashItem) => {
    const task = trashTask()
    if (!task) return
    setError("")
    try {
      const resp: Resp<unknown> = await r.post(`/admin/strm/tasks/${task.id}/trash/${item.id}/restore`, {})
      handleResp(resp, async () => {
        await loadTrash(task)
        await loadData()
      }, setError)
    } catch {
      setError("恢复失败：原输出路径已有文件或回收内容已变化，未覆盖现有文件。")
    }
  }
  const browse = async (path: string) => {
    if (!storageID()) return
    setBrowseLoading(true)
    setBrowseError("")
    try {
      const resp = await r.get(`/admin/strm/browse?storage_id=${storageID()}&path=${encodeURIComponent(path)}`) as unknown as Resp<BrowseResult>
      handleResp(resp, (result) => {
        if (!result.complete) {
          setBrowseError("目录结果不完整，请重试；不完整读取不能作为同步基线。")
          return
        }
        setBrowsePath(result.path)
        setBrowseItems(result.items)
        setBrowseReady(true)
      }, setBrowseError)
    } catch {
      setBrowseError("读取目录失败；账号异常或列表不完整时，不会生成任务基线。")
    } finally {
      setBrowseLoading(false)
    }
  }
  return (
    <VStack alignItems="stretch" spacing="$6" maxW="$4xl">
      <Box>
        <Heading size="xl" mb="$2">STRM任务</Heading>
        <Text>连接网盘、生成本地STRM，再交给Emby或Jellyfin管理和播放。</Text>
      </Box>
      <Show when={error()}><Text role="alert" color="$danger11">{error()}</Text></Show>
      <Box>
        <Heading size="base" mb="$2">准备存储</Heading>
        <Text mb="$4">存储连接、目录读取、任务预览和实际播放属于不同验收阶段。</Text>
        <HStack spacing="$3" wrap="wrap">
          <Button onClick={() => to("/@manage/storages")}>打开存储管理</Button>
          <Button variant="outline" loading={loading()} onClick={refresh}>检查后端</Button>
        </HStack>
      </Box>
      <Box>
        <Heading size="base" mb="$3">{editing() ? "编辑草稿" : "创建草稿"}</Heading>
        <Text mb="$3">保存不会扫描网盘或创建文件。保存后可单独运行只读预览。</Text>
        <Box css={{ display: "grid", "grid-template-columns": "repeat(auto-fit, minmax(min(100%, 15rem), 1fr))", gap: "16px" }}>
          <FormControl>
            <FormLabel for="strm-task-name">任务名称</FormLabel>
            <Input id="strm-task-name" value={name()} onInput={(event) => setName(event.currentTarget.value)} />
          </FormControl>
          <FormControl>
            <FormLabel for="strm-task-storage">网盘存储</FormLabel>
            <Select id="strm-task-storage" value={storageID()} onChange={(value) => {
              setStorageID(value)
              setBrowsePath("/")
              setBrowseItems([])
              setBrowseReady(false)
            }}>
              <For each={storages()}>{(storage) => <option value={storage.id}>{storage.mount_path} · {storage.driver} · {storage.status}</option>}</For>
            </Select>
          </FormControl>
          <FormControl>
            <FormLabel for="strm-task-source">源目录</FormLabel>
            <Input id="strm-task-source" value={sourcePath()} onInput={(event) => setSourcePath(event.currentTarget.value)} />
            <HStack mt="$2" wrap="wrap">
              <Button size="sm" variant="outline" loading={browseLoading()} disabled={!storageID()} onClick={() => browse(browsePath())}>浏览网盘目录</Button>
              <Button size="sm" variant="subtle" disabled={!browseReady()} onClick={() => setSourcePath(browsePath())}>选择当前目录</Button>
            </HStack>
          </FormControl>
          <FormControl>
            <FormLabel for="strm-task-output">library 内独占输出目录</FormLabel>
            <Input id="strm-task-output" value={outputSubdir()} onInput={(event) => setOutputSubdir(event.currentTarget.value)} />
          </FormControl>
          <FormControl>
            <FormLabel for="strm-task-entry-prefix">播放入口地址</FormLabel>
            <Input id="strm-task-entry-prefix" value={entryPrefix()} onInput={(event) => setEntryPrefix(event.currentTarget.value)} placeholder={health()?.public_url || "使用全局配置"} />
          </FormControl>
        </Box>
        <Show when={browseReady() || browseError()}>
          <Box mt="$3" py="$3" borderTop="1px solid var(--hope-colors-neutral6)">
            <HStack mb="$2" wrap="wrap">
              <Text css={{ "overflow-wrap": "anywhere" }}>当前目录：{browsePath()}</Text>
              <Button size="sm" variant="outline" disabled={browsePath() === "/" || browseLoading()} onClick={() => {
                const parent = browsePath().split("/").slice(0, -1).join("/") || "/"
                void browse(parent)
              }}>上一级</Button>
            </HStack>
            <Show when={browseError()}><Text role="alert" color="$danger11">{browseError()}</Text></Show>
            <VStack alignItems="stretch" spacing="$1">
              <For each={browseItems()}>{(item) => (
                item.is_dir
                  ? <Button variant="ghost" justifyContent="start" onClick={() => browse(item.path)}>{item.name}/</Button>
                  : <Text css={{ "overflow-wrap": "anywhere" }}>{item.name} · {item.size} B</Text>
              )}</For>
              <Show when={!browseLoading() && browseItems().length === 0}><Text>此目录没有内容。</Text></Show>
            </VStack>
          </Box>
        </Show>
        <HStack mt="$3">
          <Button loading={saving()} disabled={!name().trim() || !storageID() || !outputSubdir().trim()} onClick={createDraft}>{editing() ? "保存修改" : "保存草稿"}</Button>
          <Show when={editing()}><Button variant="outline" onClick={() => { setEditing(undefined); setName(""); setOutputSubdir(""); setEntryPrefix("") }}>取消编辑</Button></Show>
        </HStack>
      </Box>
      <Box>
        <Heading size="base" mb="$3">任务</Heading>
        <VStack alignItems="stretch" spacing="$2">
          <Show when={!tasks().length}><Text>还没有任务草稿。</Text></Show>
          <For each={tasks()}>{(task) => (
            <HStack justifyContent="space-between" alignItems="center" gap="$3" wrap="wrap" p="$2" borderBottom="1px solid var(--hope-colors-neutral6)">
              <VStack alignItems="start" spacing="$1">
                <Text fontWeight="$semibold">{task.name}</Text>
                <Text css={{ "overflow-wrap": "anywhere" }}>{task.source_path} → library/{task.output_subdir} · {task.state}</Text>
              </VStack>
              <HStack spacing="$2" wrap="wrap">
                <Show when={task.state === "draft"}><Button variant="outline" onClick={() => editDraft(task)}>编辑</Button></Show>
                <Button variant="outline" loading={previewing() === task.id} onClick={() => runPreview(task)}>只读预览</Button>
                <Button loading={generating() === task.id} disabled={!!generating() || !(task.public_url || task.entry_prefix || health()?.public_url)} onClick={() => runGenerate(task)}>{task.state === "draft" ? "扫描并生成" : "再次扫描并生成"}</Button>
                <Show when={task.state === "active"}>
                  <Button variant="outline" loading={missingPreviewing() === task.id} onClick={() => runMissingPreview(task)}>缺失预览</Button>
                  <Button variant="outline" loading={loadingTrash() && trashTask()?.id === task.id} onClick={() => loadTrash(task)}>回收站</Button>
                </Show>
                <Show when={task.state === "draft"}><Button variant="subtle" colorScheme="danger" onClick={() => deleteDraft(task)}>删除草稿</Button></Show>
              </HStack>
            </HStack>
          )}</For>
        </VStack>
      </Box>
      <Show when={preview()}>
        {(result) => (
          <Box>
            <Heading size="base" mb="$2">预览结果{result().complete ? " · 完整" : " · 不完整"}</Heading>
            <Text>已读目录 {result().directories} · 文件 {result().files} · STRM候选 {result().candidates} · 本次只读</Text>
            <Show when={result().items_truncated}><Text>候选较多，当前只展示前1000项；总数仍为完整统计。</Text></Show>
            <For each={result().issues}>{(issue) => <Text role="alert" color="$danger11">{issue.path || "扫描"}：{issue.message}</Text>}</For>
            <VStack mt="$3" alignItems="stretch" spacing="$1">
              <For each={result().items}>{(item) => <Text css={{ "overflow-wrap": "anywhere" }}>{item.source_path} → {item.output_path} · {item.size} B</Text>}</For>
            </VStack>
          </Box>
        )}
      </Show>
      <Show when={generation()}>
        {(result) => (
          <Box>
            <Heading size="base" mb="$2">生成结果 · {result().status === "succeeded" ? "完成" : result().status === "partial" ? "部分完成" : "失败"}</Heading>
            <Text>运行编号 {result().generation} · 扫描完整：{result().complete ? "是" : "否"} · 候选 {result().candidates} · 新建 {result().added} · 更新 {result().updated} · 未变 {result().unchanged} · 两轮缺失 {result().missing_confirmed} · 失败 {result().failed}</Text>
            <For each={result().issues}>{(issue) => <Text role="alert" color="$danger11" css={{ "overflow-wrap": "anywhere" }}>{issue.path || "运行"}：{issue.message}</Text>}</For>
          </Box>
        )}
      </Show>
      <Show when={missingPreview()}>
        {(result) => (
          <Box>
            <Heading size="base" mb="$2">缺失预览 · {result().total_missing} 项</Heading>
            <Text>基线 {result().last_complete_generation} · 已管理 {result().total_managed} 项 · 本页列出连续两轮完整运行未见的文件。此处只供检查，不会回收文件。</Text>
            <Show when={result().mass_disappearance}><Text role="alert" color="$danger11">缺失比例较高，需要进一步复核；当前不会执行回收。</Text></Show>
            <Show when={result().items_truncated}><Text>当前显示前100项，缺失总数仍为 {result().total_missing}。</Text></Show>
            <VStack mt="$3" alignItems="stretch" spacing="$1">
              <For each={result().items}>{(item) => (
                <Text css={{ "overflow-wrap": "anywhere" }}>
                  {item.source_path} → library/{item.output_path} · {item.missing_confirmations} 轮 · {missingOutputLabel(item.output_state)}
                </Text>
              )}</For>
              <Show when={result().total_missing === 0}><Text>当前没有达到两轮确认的缺失文件。</Text></Show>
            </VStack>
            <Show when={result().total_missing > 0}>
              <Button mt="$3" variant="outline" loading={planningTrash()} disabled={!!trashPlan() || result().items_truncated} onClick={createTrashPlan}>创建回收预览</Button>
            </Show>
            <Show when={result().items_truncated}><Text role="alert" color="$warning11">该列表有分页内容；请先检查全部页面。当前版本不会执行不完整列表的回收。</Text></Show>
            <Show when={trashPlan()}>
              {(plan) => (
                <Box mt="$3" py="$3" borderTop="1px solid var(--hope-colors-neutral6)">
                  <Heading size="sm" mb="$2">回收计划已生成</Heading>
                  <Text>运行编号 {plan().generation} · 可回收 {plan().eligible} 项 · 阻塞 {plan().blocked} 项 · 15分钟内有效</Text>
                  <Show when={plan().mass_disappearance}>
                    <FormControl mt="$2">
                      <Checkbox checked={confirmMassDisappearance()} onChange={(event: any) => setConfirmMassDisappearance(event.currentTarget.checked)}>
                        我已复核大批量缺失，确认仅回收本计划列出的未修改文件
                      </Checkbox>
                    </FormControl>
                  </Show>
                  <HStack mt="$2">
                    <Button loading={executingTrash()} disabled={plan().eligible === 0 || (plan().mass_disappearance && !confirmMassDisappearance())} onClick={executeTrashPlan}>执行可回收项</Button>
                  </HStack>
                </Box>
              )}
            </Show>
            <Show when={trashExecution()}>
              {(execution) => <Text role="status" mt="$2">回收结果：{execution().status} · 已回收 {execution().committed} · 未完成 {execution().failed}</Text>}
            </Show>
            <Show when={result().total_missing > 100 && missingTask()}>
              {(task) => (
                <HStack mt="$3">
                  <Button size="sm" variant="outline" disabled={missingPage() <= 1 || !!missingPreviewing()} onClick={() => runMissingPreview(task(), missingPage() - 1)}>上一页</Button>
                  <Text>第 {missingPage()} 页</Text>
                  <Button size="sm" variant="outline" disabled={!result().items_truncated || !!missingPreviewing()} onClick={() => runMissingPreview(task(), missingPage() + 1)}>下一页</Button>
                </HStack>
              )}
            </Show>
          </Box>
        )}
      </Show>
      <Show when={trashTask()}>
        {(task) => (
          <Box>
            <Heading size="base" mb="$2">回收站 · {task().name} · {trashTotal()} 项</Heading>
            <VStack alignItems="stretch" spacing="$1">
              <For each={trashItems()}>{(item) => (
                <HStack justifyContent="space-between" gap="$3" wrap="wrap" py="$2" borderBottom="1px solid var(--hope-colors-neutral6)">
                  <VStack alignItems="start" spacing="$1">
                    <Text css={{ "overflow-wrap": "anywhere" }}>{item.output_path}</Text>
                    <Text>{item.trashed_at} · {item.content_hash.slice(0, 12)}</Text>
                  </VStack>
                  <Button size="sm" variant="outline" onClick={() => restoreTrash(item)}>恢复</Button>
                </HStack>
              )}</For>
              <Show when={!trashItems().length}><Text>{loadingTrash() ? "正在读取回收站…" : "回收站为空。"}</Text></Show>
            </VStack>
          </Box>
        )}
      </Show>
      <Show when={health()}>
        {(value) => (
          <Box>
            <Heading size="base" mb="$3">运行信息</Heading>
            <VStack alignItems="start" spacing="$2">
              <Text>后端：{value().ready ? "已就绪" : "初始化中"}</Text>
              <Text>产品版本：{value().product_version}</Text>
              <Text css={{ "overflow-wrap": "anywhere" }}>输出父目录：{value().output_root}</Text>
              <Text css={{ "overflow-wrap": "anywhere" }}>默认播放入口：{value().public_url || "未配置，可在任务中指定"}</Text>
              <Text css={{ "overflow-wrap": "anywhere" }}>上游提交：{value().upstream_commit}</Text>
              <Text css={{ "overflow-wrap": "anywhere" }}>前端提交：{value().frontend_commit}</Text>
            </VStack>
          </Box>
        )}
      </Show>
    </VStack>
  )
}

function missingOutputLabel(state: string) {
  switch (state) {
    case "ready_for_review": return "本地内容与清单一致"
    case "output_missing": return "本地文件已缺失"
    case "output_modified": return "本地内容已修改"
    case "output_conflict": return "输出路径冲突"
    case "artifact_inactive": return "产物状态非活动"
    default: return "产物清单异常"
  }
}

export default Overview
