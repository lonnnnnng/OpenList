import { dict as en } from "../en/entry"

const quarkFields = {
  cookie: "账号 Cookie", root_folder_id: "根目录 ID", order_by: "排序字段", order_direction: "排序方向",
  order_bys: { none: "默认", file_type: "文件类型", file_name: "文件名", updated_at: "更新时间" },
  order_directions: { asc: "升序", desc: "降序" }, only_list_video_file: "只返回视频文件",
  use_transcoding_address: "使用网盘转码地址", "use_transcoding_address-tips": "开启后读取网盘转码流；不代表源文件原画。首轮播放验收建议关闭。",
}

// long: 复用未改动模块的英文fallback，一期管理、登录、存储操作使用本地中文词典，不在线抓翻译。
export const dict = {
  ...en,
  index: { lang: "简体中文" },
  global: {
    ...en.global,
    refresh: "刷新", add: "添加", edit: "编辑", delete: "删除", save: "保存",
    update: "更新", copy: "复制", copied: "已复制", cancel: "取消", confirm: "确认",
    choose: "选择", close: "关闭", back: "返回", name: "名称", operations: "操作",
    yes: "是", no: "否", clear: "清空", ok: "确定", disable: "停用", enable: "启用",
    save_success: "保存成功", update_success: "更新成功", delete_success: "删除成功",
    choose_folder: "选择目录", choose_or_input_path: "选择或输入目录",
    delete_confirm: "确定删除 [{{name}}] 的配置吗？", go_to_storages: "前往存储管理",
  },
  login: {
    ...en.login, login_to: "登录", "username-tips": "管理员用户名", "password-tips": "登录密码",
    "otp-tips": "两步验证码", remember: "记住用户名", forget: "忘记密码？", clear: "清空",
    login: "登录", success: "登录成功", use_guest: "访客访问",
  },
  init: {
    ...en.init, setup_to: "设置", title: "初始化网盘中间件", "username-tips": "管理员用户名",
    "password-tips": "设置管理员密码", "confirm_password-tips": "确认密码", "site_title-tips": "站点名称",
    setup: "完成初始化", success: "初始化成功，请登录", failed: "初始化失败",
    password_mismatch: "两次密码不一致", password_too_short: "密码至少4个字符",
    creating_account: "正在创建管理员账号", waiting_storage: "正在等待初始化完成",
    storage_ready: "已就绪", env_check: "环境检查", env_local: "本地 / 容器",
    env_check_ready: "环境已就绪", env_check_refresh: "重新检查", next: "下一步", back: "返回",
    step_env: "检查环境", step_account: "创建管理员", step_done: "完成初始化",
    finalizing: "正在完成初始化", finalizing_tip: "请保持页面打开，等待初始化结束",
    done_title: "初始化完成", done_subtitle: "现在可以登录并连接网盘。任务预览与生成仍在开发。",
    done_username: "管理员用户名", done_site_url: "站点地址", done_go_login: "前往登录",
    done_go_home: "打开首页", error_title: "初始化失败", error_code: "错误码",
  },
  manage: {
    ...en.manage, title: "网盘中间件管理", no_permission: "此页面仅供管理员使用",
    not_admin: "请使用管理员账号登录", logout_success: "已退出登录",
    sidemenu: { ...en.manage.sidemenu, profile: "管理员", storages: "存储管理", tasks: "STRM任务" },
  },
  users: {
    ...en.users,
    update_profile: "管理员资料", change_username: "管理员用户名", change_password: "新密码",
    "change_password-tips": "留空保留当前密码。", confirm_password: "确认新密码",
    "confirm_password-tips": "再次输入新密码；不修改时留空。", confirm_password_not_same: "两次密码不一致",
    update_profile_success: "管理员资料已更新，请重新登录", enable_2fa: "开启两步验证",
    scan_qr: "使用验证器扫描二维码", or_manual: "或手动输入密钥", input_code: "输入六位验证码", verify: "确认开启",
  },
  drivers: {
    ...en.drivers,
    drivers: { ...en.drivers.drivers, Quark: "夸克网盘", UC: "UC网盘", AliyundriveOpen: "阿里云盘 Open", BaiduNetdisk: "百度网盘", Local: "本地测试目录" },
    Quark: { ...en.drivers.Quark, ...quarkFields }, UC: { ...en.drivers.UC, ...quarkFields },
    AliyundriveOpen: {
      ...en.drivers.AliyundriveOpen, drive_type: "网盘空间", drive_types: { default: "默认", resource: "资源库", backup: "备份盘" },
      root_folder_id: "根目录 ID", refresh_token: "刷新令牌", order_by: "排序字段", order_direction: "排序方向",
      order_bys: { name: "文件名", size: "大小", updated_at: "更新时间", created_at: "创建时间" },
      order_directions: { ASC: "升序", DESC: "降序" }, use_online_api: "使用在线授权刷新服务",
      alipan_type: "授权模式", alipan_types: { default: "默认 Open", alipanTV: "TV模式（待验收）" }, api_url_address: "授权刷新服务地址",
      client_id: "自有应用 Client ID", client_secret: "自有应用 Client Secret", "client_id-tips": "使用自有应用时填写。", "client_secret-tips": "使用自有应用时填写。",
      callback_concurrency: "账号代理并发", "callback_concurrency-tips": "同一阿里账号共用的代理下载并发限制。",
    },
    BaiduNetdisk: {
      ...en.drivers.BaiduNetdisk, root_folder_path: "根目录路径", order_by: "排序字段", order_direction: "排序方向",
      order_bys: { name: "文件名", size: "大小", time: "更新时间" }, order_directions: { asc: "升序", desc: "降序" },
      download_api: "下载模式", download_apis: { official: "官方（一期验收模式）", crack: "其他模式（待验收）", crack_video: "视频模式（待验收）" },
      use_online_api: "使用在线授权刷新服务", api_url_address: "授权刷新服务地址", client_id: "自有应用 Client ID", client_secret: "自有应用 Client Secret",
      refresh_token: "OAuth刷新令牌", custom_crack_ua: "下载 User-Agent", only_list_video_file: "只返回视频文件",
    },
    Local: { ...en.drivers.Local, root_folder_path: "本地测试根目录", thumbnail: "缩略图", show_hidden: "显示隐藏文件", directory_size: "计算目录大小", "directory_size-tips": "大目录会增加本机磁盘读取，测试时建议关闭。" },
  },
  storages: {
    ...en.storages,
    common: {
      ...en.storages.common, mount_path: "挂载路径", "mount_path-tips": "此账号的唯一挂载路径",
      driver: "驱动", order: "顺序", "order-tips": "存储列表的排序顺序。", status: "状态", remark: "备注", usage: "用量",
      cache_expiration: "目录缓存（分钟）", "cache_expiration-tips": "供目录浏览使用；同步扫描会强制刷新并核对完整性。", web_proxy: "Web代理", proxy_range: "Range代理",
      down_proxy_url: "外部代理地址", enable_sign: "下载签名", disable_index: "不建立搜索索引",
      webdav_policy: "WebDAV策略", export: "导出", import: "导入",
    },
    other: { ...en.storages.other, load_all: "重新加载存储", start_load_success: "已开始加载",
      filter_by_driver: "按驱动筛选", table_layout: "表格布局" },
    table_fields: { ...en.storages.table_fields, status: { work: "已连接", disabled: "已停用" } },
  },
}
