# 新建 Agent 接线

本模块提供列表右上角和空状态入口、机器选择、目录浏览与最近目录、启动器搜索、Agent 类型、权限模式、思考强度和可选首句。创建成功后通过现有 `Navigator` 打开返回的 `ChatRoute`，并刷新列表。选择按 hub 配对作用域和机器保存在 DataStore。

App 已通过 `AgentsWiringModule` 和 `AgentsEntries` 提供 `LocalAgentCreationService`，偏好与操作记录使用当前配对的 `listPreferences`。构造 `HttpAgentCreationService(scope, baseUrl, token, client, isCurrent)`，其中 `scope` 使用当前 `HubScope.path`，地址和 HTTP client 来自当前已验证、已钉扎的连接路由，`isCurrent` 检查配对代次和路由仍有效。每条路由保持实例稳定；换配对或连接路由时更换实例，避免沿用旧凭据和旧地址。未提供服务时入口显示尚未接线的说明。

当前服务端 `POST /api/v1/sessions` 直接调用 `h.startSession`，没有包装 `h.withClientOp("start", h.startSession)`。需要服务端车道补这一处，才能在丢失回执后通过 `GET /api/v1/ops/{id}` 恢复启动结果。不能仅凭全局 `client_ops` 能力位认定启动接口也支持幂等。本车道没有修改服务端。

客户端在 POST 前持久保存 UUID 和完整参数，重复点击、重开页面和进程重启都复用这份记录。重试只查询该 UUID 的回执，不重复 POST；`pending` 和 `unknown` 保持表单锁定，只有确定的失败回执才允许改参数后再创建。旧 hub 查不到回执时保持“待确认”，需要到机器上核对，不提供会再次启动的重发按钮。回执随配对作用域隔离。

Launcher responses identify their agent with `agent` (node launchers) or `agent_type` (configured launchers); absent means Claude Code. The form lists only what the computer returns and never builds a runtime command itself.

验证范围为 `:feature:agents` 编译及新建状态测试、`:core:hub` 传输测试。App 接线已补齐，未做模拟器截图或真实 hub 验收。按钮标签单行，颜色使用 Material 主题，深浅色实际显示待真机验收。

## 异步新建与跟随模型配置（2026-11-02）

- Claude starts with `async: true` when a `MessageSender` is available. The hub's `starting` session is adopted before opening its chat; the first message travels through the normal sender, with its client id saved in `AgentStartOperation.firstMessageId` before starting. Reconciliation and process restoration reuse that id. A failed enqueue keeps the operation for another receipt check. Without a sender, the start remains synchronous. Codex is always synchronous: a supplied first message goes in `prompt`; it can be omitted only when that computer's launcher advertises `prompt_optional`.
- `AgentCreationService.newAgentModel()` 读 hub 的 `agent.new` 用途：有的话默认「跟随模型配置」（空 launcher），记住的启动器不再自动带上；手选一个启动器是本次覆盖。旧 hub（404）照旧默认记住的或第一个启动器；读失败时不预选，要手选或重新加载。
- `NewAgentRoute` 打开同一张表单的页面版（`NewAgentDestination`），预填机器、目录（那台机器在线才选中）和第一条消息；开好后先清掉启动记录再离开页面（页面出栈会清掉它的 ViewModel 作用域）。

## Codex before its first message (#4999)

A waiting row carries `awaiting_first_message` and stays idle. Its second line explains the wait; its empty chat names the computer and accepts the first message here or in the terminal. Start/send receipts live in `SessionsRepository.adoptedSessions` as chat metadata, separately from the filtered list window and its counts. Both the ordinary HTTP sender and its composer decorator handle the first send's returned thread and fill its machine id from the request. `startedThreads` binds that reply to the exact waiting session that received the message; an earlier receipt from a reused pane cannot redirect a new waiting page. The chat replaces its route, keeping Back's destination.

A terminal message replaces the waiting row with a live Codex thread in the same computer's pane. The open page follows that pane only in the already-loaded session rows. If a filter or page window excludes the thread, it keeps the waiting page without fetching extra pages, changing the filter or guessing from a directory. Refusals preserve the message for manual retry, and an unconfirmed first delivery stays under review.
