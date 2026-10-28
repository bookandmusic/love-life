mod appupdate;

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        // 媒体另存到本机（APP 查看器「保存」）：dialog 弹系统另存为
        // （Android 走 SAF ACTION_CREATE_DOCUMENT，零存储权限），fs 按块写入所选文件。
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_fs::init())
        // 应用内更新薄桥：仅注册 Kotlin 类（AppUpdatePlugin），命令以应用命令形式暴露（不经 ACL）
        .plugin(appupdate::init())
        .invoke_handler(tauri::generate_handler![
            appupdate::download_apk,
            appupdate::cancel_download,
            appupdate::has_cached,
            appupdate::install_apk,
            appupdate::get_version_code,
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
