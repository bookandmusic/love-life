package llm

// 思考档位注入（模型级 `thinking` 参数的原生映射）。
//
// 只映射**现役代**参数形态——各家都已换成「档位」制，旧代的固定预算字段（anthropic
// `budget_tokens`、gemini `thinkingBudget`）不再发：
//
//	anthropic：thinking:{type:"adaptive"} + output_config:{effort}
//	gemini：   generationConfig.thinkingConfig.thinkingLevel
//	responses：reasoning:{effort, summary:"auto"}
//	openai-chat：无统一参数，不发
//
// 现役代参数形态一致，故**不按模型名分支**：解析模型名只会引入别名误判
// （判错即 400 或静默失效），不如让不一致的模型自己报错。
// 「这个模型支持哪些档位」是配置面的事实（provider_models.thinking_levels），由 API 侧校验，
// 本文件只负责把**已选中的档位**映射成原生参数——校验与注入分离。
//
// 「关」在现役模型上无法真正关闭思考（anthropic adaptive 恒开、gemini 3 亦然），
// 故映射为**该协议的最低档**：与「把思考压到最低」的意图一致。
//
// 空串 = **调用方未指定档位**（历史配置 / 直连 adapter），一律不注入、不改动其它参数；
// 只有显式 `off` 才走最低档。生产路径经 `store.NormalizeThinking` 归一，恒为 off/low/medium/high。
//
// 档位取值面只在 `store.NormalizeThinking` 一处定义（本文件只做映射，不再列一遍取值）。

// thinkingEffort 档位 → 现役代取值；`off` 归到最低档 `low`。
func thinkingEffort(level string) string {
	switch level {
	case "medium", "high":
		return level
	default: // off / low
		return "low"
	}
}

// applyAnthropicThinking 注入 anthropic 思考参数。
// adaptive 恒开，故这里不带 temperature（该参数已从模型配置面删除，二者互斥、同传即 400）。
func applyAnthropicThinking(body map[string]any, level string) {
	if level == "" {
		return
	}
	body["thinking"] = map[string]any{"type": "adaptive"}
	body["output_config"] = map[string]any{"effort": thinkingEffort(level)}
}

// applyGeminiThinking 注入 gemini thinkingConfig（走 generationConfig，与平铺注入不同路径）。
// 只发 thinkingLevel：它与 thinkingBudget **互斥**，同传即 400。
// includeThoughts 控制是否回思考摘要，故仅非「关」档位请求它。
func applyGeminiThinking(gen map[string]any, level string) {
	if level == "" {
		return
	}
	off := level == "off"
	gen["thinkingConfig"] = map[string]any{
		"thinkingLevel":   thinkingEffort(level),
		"includeThoughts": !off,
	}
}

// applyResponsesThinking 注入 openai-responses 的 reasoning。
// 「关」→ 该协议最低档 `minimal`；其余档位直映并请求摘要（不指定 summary 则不回思考摘要）。
func applyResponsesThinking(body map[string]any, level string) {
	if level == "" {
		return
	}
	off := level == "off"
	effort := thinkingEffort(level)
	if off {
		effort = "minimal"
	}
	r := map[string]any{"effort": effort}
	if !off {
		r["summary"] = "auto"
	}
	body["reasoning"] = r
}
