#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        // 媒体另存到本机（APP 查看器「保存」）：dialog 弹系统另存为
        // （Android 走 SAF ACTION_CREATE_DOCUMENT，零存储权限），fs 按块写入所选文件。
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_fs::init())
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
