<template>
  <div class="page">
    <nav class="tabs">
      <RouterLink to="/integrations/webhooks" class="tab" active-class="active">通知渠道</RouterLink>
      <RouterLink to="/integrations/map" class="tab" active-class="active">地图配置</RouterLink>
    </nav>
    <RouterView />
  </div>
</template>
