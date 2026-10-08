<template>
  <div class="page">
    <nav class="tabs">
      <RouterLink to="/ai/providers" class="tab" active-class="active">供应商</RouterLink>
      <RouterLink to="/ai/chat-chain" class="tab" active-class="active">对话模型链</RouterLink>
      <RouterLink to="/ai/embedding-chain" class="tab" active-class="active">嵌入模型链</RouterLink>
      <RouterLink to="/ai/prompts" class="tab" active-class="active">提示词与风格</RouterLink>
      <RouterLink to="/ai/tools" class="tab" active-class="active">MCP 工具</RouterLink>
      <RouterLink to="/ai/tokens" class="tab" active-class="active">访问令牌</RouterLink>
    </nav>
    <RouterView />
  </div>
</template>
