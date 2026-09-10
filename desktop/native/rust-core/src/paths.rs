use std::path::PathBuf;
use std::env;

pub fn get_python_packages_path() -> PathBuf {
    let exe = env::current_exe().unwrap();
    let root = exe.parent().unwrap();

    root.to_path_buf().join("python")
}

/// Config directory next to the running binary (`./config`).
pub fn get_config_path() -> PathBuf {
    let exe = env::current_exe().unwrap();
    let root = exe.parent().unwrap();

    root.to_path_buf().join("config")
}

/// Default permissions policy path used by the Go bridge.
pub fn get_permissions_path() -> PathBuf {
    let exe = env::current_exe().unwrap();
    let root = exe.parent().unwrap();

    root.to_path_buf().join("permissions.json")
}