use std::env;
use std::path::PathBuf;
use std::process::Command;

fn env_bool(name: &str, default: bool) -> bool {
    env::var(name)
        .map(|value| matches!(value.to_lowercase().as_str(), "1" | "true" | "yes" | "on"))
        .unwrap_or(default)
}

fn resolve_frontend_index() -> Option<PathBuf> {
    let exe = env::current_exe().ok()?;
    let exe_dir = exe.parent()?;

    let bundled = exe_dir.join("frontend").join("index.html");
    if bundled.exists() {
        return Some(bundled);
    }

    let dev_path = PathBuf::from("frontend").join("dist").join("index.html");
    if dev_path.exists() {
        return Some(dev_path);
    }

    None
}

pub fn launch_ui_if_enabled() {
    if !env_bool("NEURO_UI_LAUNCH", true) {
        return;
    }

    let Some(frontend_index) = resolve_frontend_index() else {
        eprintln!("[warn] UI launch skipped: frontend index.html not found");
        return;
    };

    #[cfg(target_os = "windows")]
    {
        let _ = Command::new("cmd")
            .args(["/C", "start", "", frontend_index.to_string_lossy().as_ref()])
            .spawn();
    }

    #[cfg(target_os = "macos")]
    {
        let _ = Command::new("open").arg(&frontend_index).spawn();
    }

    #[cfg(all(unix, not(target_os = "macos")))]
    {
        let _ = Command::new("xdg-open").arg(&frontend_index).spawn();
    }
}
