// 应用内更新（GitHub Releases 直连 → 前台服务后台下载 → 安装），设计见客户端设计「应用内更新」：
// 本文件只是「薄桥」：应用命令 → Kotlin（AppUpdatePlugin / UpdateDownloadService）。
// 下载引擎、断点续传、sha256 校验、前台服务、系统通知、安装器交接全部在 Kotlin 侧
// （HttpURLConnection + MessageDigest 平台原生能力，避免 Rust 交叉编译引入 C 依赖）。
// 桥本身无 invoke command（无 ACL 权限面，capabilities 零改动）；前端调的是下面的应用命令（不经 ACL）。

#[cfg(mobile)]
use std::sync::OnceLock;
#[cfg(mobile)]
use tauri::plugin::PluginHandle;
#[cfg(mobile)]
use tauri::Wry;

#[cfg(mobile)]
/// Kotlin 桥（setup 时注册）。桌面平台不存在，相关命令返回明确错误。
static BRIDGE: OnceLock<PluginHandle<Wry>> = OnceLock::new();

/// 薄桥插件：仅注册 Kotlin 类，无 invoke 面。
pub fn init() -> tauri::plugin::TauriPlugin<Wry> {
    tauri::plugin::Builder::new("appupdate")
        .setup(|_app, api| {
            #[cfg(target_os = "android")]
            {
                let handle =
                    api.register_android_plugin("com.bookandmusic.lovelife", "AppUpdatePlugin")?;
                let _ = BRIDGE.set(handle);
            }
            Ok(())
        })
        .build()
}

#[cfg(mobile)]
async fn bridge() -> Result<&'static PluginHandle<Wry>, String> {
    BRIDGE
        .get()
        .ok_or_else(|| "更新桥未就绪，请重启应用".to_string())
}

/// 启动后台下载（Kotlin 前台服务线程执行；进度/完成经 onProgress Channel 直回前端）。
#[tauri::command]
pub async fn download_apk(
    url: String,
    expected_sha256: String,
    file_name: String,
    version_name: String,
    on_progress: tauri::ipc::Channel,
) -> Result<(), String> {
    #[cfg(mobile)]
    {
        let bridge = bridge().await?;
        bridge
            .run_mobile_plugin_async::<serde_json::Value>(
                "startDownload",
                serde_json::json!({
                    "url": url,
                    "expectedSha256": expected_sha256,
                    "fileName": file_name,
                    "versionName": version_name,
                    "onProgress": on_progress,
                }),
            )
            .await
            .map_err(|e| format!("启动下载失败: {e}"))?;
        Ok(())
    }
    #[cfg(not(mobile))]
    {
        let _ = (&url, &expected_sha256, &file_name, &version_name, &on_progress);
        Err("更新功能仅支持 Android".to_string())
    }
}

#[tauri::command]
pub async fn cancel_download() -> Result<(), String> {
    #[cfg(mobile)]
    {
        let bridge = bridge().await?;
        bridge
            .run_mobile_plugin_async::<serde_json::Value>("cancelDownload", serde_json::json!({}))
            .await
            .map_err(|e| format!("取消失败: {e}"))?;
        Ok(())
    }
    #[cfg(not(mobile))]
    {
        Err("更新功能仅支持 Android".to_string())
    }
}

/// 冷启动恢复：成品 APK 是否已在缓存（存在返回绝对路径）。
#[tauri::command]
pub async fn has_cached(file_name: String) -> Result<Option<String>, String> {
    #[cfg(mobile)]
    {
        let bridge = bridge().await?;
        let r: serde_json::Value = bridge
            .run_mobile_plugin_async("hasCached", serde_json::json!({ "fileName": file_name }))
            .await
            .map_err(|e| format!("读取缓存失败: {e}"))?;
        Ok(r.get("path")
            .and_then(|v| v.as_str())
            .map(String::from))
    }
    #[cfg(not(mobile))]
    {
        let _ = file_name;
        Err("更新功能仅支持 Android".to_string())
    }
}

/// 交给系统安装器（含未知来源授权引导）。needsPermission=true 时授权回来重试。
#[derive(serde::Serialize)]
pub struct InstallOutcome {
    pub needs_permission: bool,
}

#[tauri::command]
pub async fn install_apk(path: String) -> Result<InstallOutcome, String> {
    #[cfg(mobile)]
    {
        let bridge = bridge().await?;
        let r: serde_json::Value = bridge
            .run_mobile_plugin_async("installApk", serde_json::json!({ "path": path }))
            .await
            .map_err(|e| format!("安装交接失败: {e}"))?;
        Ok(InstallOutcome {
            needs_permission: r
                .get("needsPermission")
                .and_then(|v| v.as_bool())
                .unwrap_or(false),
        })
    }
    #[cfg(not(mobile))]
    {
        let _ = path;
        Err("更新功能仅支持 Android".to_string())
    }
}

/// 本机 versionCode（与 GitHub tag 换算值比较用）。
#[tauri::command]
pub async fn get_version_code() -> Result<u64, String> {
    #[cfg(mobile)]
    {
        let bridge = bridge().await?;
        let r: serde_json::Value = bridge
            .run_mobile_plugin_async("getVersionCode", serde_json::json!({}))
            .await
            .map_err(|e| format!("读取版本失败: {e}"))?;
        Ok(r.get("versionCode").and_then(|v| v.as_u64()).unwrap_or(0))
    }
    #[cfg(not(mobile))]
    {
        Err("更新功能仅支持 Android".to_string())
    }
}
