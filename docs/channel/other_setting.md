# 渠道额外设置说明

该配置用于设置一些额外的渠道参数，可以通过 JSON 对象进行配置。常用设置项包括：

1. force_format
    - 仅用于重新格式化 OpenAI 渠道的 Chat Completions 响应，不处理原生 `/v1/responses`
    - 类型为布尔值，设置为 true 时启用强制格式化

2. responses_compatibility_fix
    - 用于修复原生 `/v1/responses` 的非标准 Item ID，并保护显式 `store:false` 历史的跨渠道重放
    - 类型为布尔值；不填写时默认启用，显式设置为 `false` 时同时关闭请求侧和响应侧兼容修复

3. allow_reasoning_without_encrypted_content
    - 控制显式 `store:false` 请求是否保留缺少可用 `encrypted_content` 的 reasoning item
    - 类型为布尔值；不填写时默认关闭，仅在确认目标上游接受 summary-only reasoning 重放后开启
    - 该字段只影响 reasoning 删除策略，不会关闭 Item ID 修复；`responses_compatibility_fix=false` 时不生效

4. proxy
    - 用于配置网络代理
    - 类型为字符串，支持 `http`、`https`、`socks5` 和 `socks5h` 协议
    - 保存时必须包含协议和主机；仅允许空路径或根路径 `/`，不允许 query 或 fragment
    - SOCKS 代理未填写端口时，运行时使用默认端口 `1080`

5. thinking_to_content
   - 用于标识是否将思考内容`reasoning_content`转换为`<think>`标签拼接到内容中返回
   - 类型为布尔值，设置为 true 时启用思考内容转换

6. max_concurrency
   - 限制该渠道同时在途的中转请求数
   - 类型为非负整数，`0` 或不填写表示不限
   - 多 Key 渠道的所有 Key 共享同一个渠道并发上限

7. rpm_limit
   - 限制该渠道在滚动 60 秒窗口内开始的中转请求数
   - 类型为非负整数，`0` 或不填写表示不限
   - 多 Key 渠道的所有 Key 共享同一个渠道 RPM 上限

--------------------------------------------------------------

## JSON 格式示例

以下是一个示例配置，启用强制格式化并设置了代理地址：

```json
{
    "force_format": true,
    "responses_compatibility_fix": true,
    "allow_reasoning_without_encrypted_content": false,
    "thinking_to_content": true,
    "proxy": "socks5://proxy.example:1080",
    "max_concurrency": 20,
    "rpm_limit": 120
}
```

--------------------------------------------------------------

通过调整上述 JSON 配置中的值，可以灵活控制渠道的额外行为，比如是否进行格式化、使用特定网络代理，以及限制渠道容量。

## Responses 兼容处理顺序

- 每次 relay attempt 在选中目标渠道后，都从不可变的原始 Responses 请求生成独立副本，再读取该渠道当前的兼容设置。一个 attempt 的 reasoning 删除或 Item ID 修复不会污染后续重试。
- 对显式 `store:false` 且数组形式 `input` 的请求，兼容修复先按渠道策略删除或保留无密文 reasoning，再规范化 `function_call`、`message` 和保留的 `reasoning` Item ID。随后才执行协议转换、模型映射和 `param_override`。
- `pass_through_body_enabled=true` 或全局请求体透传开启时，请求体继续原样转发，不执行请求侧兼容修复；渠道响应侧 Item ID 规范化仍由 `responses_compatibility_fix` 独立控制。
- 原生 Responses JSON 和 SSE 响应会在发送客户端前统一规范化 output item 及其 `item_id` 引用。处理保留未知扩展字段、合法 ID 和 `call_id`，且不会伪造 `reasoning.encrypted_content`。

## 渠道容量限制语义

- 容量检查和预留发生在发送上游请求之前。候选渠道达到并发或 RPM 上限时，路由器会先尝试同优先级的其他渠道，再尝试较低优先级或下一个自动分组。
- Token 固定渠道和渠道亲和绑定不会因容量不足而静默改道；绑定渠道满载时直接返回本地容量错误，原亲和绑定保持不变。
- 本地容量跳过不占用上游重试次数，不触发渠道自动禁用，也不会作为上游错误记录。所有候选均满载时返回 `429 channel_capacity_exhausted` 和 `Retry-After`；本功能不提供请求排队。
- 并发租约覆盖流式、非流式、WebSocket 和任务提交的完整上游调用。正常结束、错误、取消和 panic 都会释放；Redis 租约带过期保护，长请求会自动续租。
- RPM 在候选预留后、真正开始调用上游前仍可回退，例如本地请求校验或渠道上下文初始化失败。上游调用一旦开始，RPM 计数不会因成功、失败、取消或重试而回退。
- 启用 Redis 时，并发与 RPM 在所有实例间全局共享，并通过原子脚本同时检查和预留。未配置 Redis 时使用进程内原子限制，每个实例独立计算；Redis 运行时故障会降级到进程内模式并记录告警，此时不再保证跨实例全局上限。
- 修改限制后，新请求立即按新值判断。降低并发上限不会中断现有请求，而是暂停该渠道的新准入，直到在途请求数回落。

## 升级兼容性

`max_concurrency`、`rpm_limit` 和 Responses 兼容字段都直接存放在现有渠道设置 JSON 中，不需要数据库迁移。已有渠道缺省容量字段时保持不限流；缺省 `responses_compatibility_fix` 时启用修复，缺省 `allow_reasoning_without_encrypted_content` 时继续过滤无密文 reasoning。

旧版本会忽略代理地址中的 path、query 和 fragment。为避免升级后中断已有渠道流量，运行时会继续剥离这些遗留后缀，并对同一代理地址每个进程记录一次不含凭证和后缀的警告。该兼容逻辑不会改写数据库；再次保存渠道时必须按上述严格规则修正代理地址。

代理连接使用 30 秒 TCP 拨号超时和 30 秒 KeepAlive；TLS 握手超时为 10 秒。这些超时同样适用于未配置渠道代理的中转请求。
