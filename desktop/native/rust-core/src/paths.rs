use std::path::PathBuf;
use std::env;

pub fn get_python_packages_path() -> PathBuf {
    let exe = env::current_exe().unwrap();
    let root = exe.parent().unwrap();

    root.to_path_buf().join("python")
}

// pub fn get_config_path() -> PathBuf {
//     let exe = env::current_exe().unwrap();
//     let root = exe.parent().unwrap();

//     root.to_path_buf().join("config")
// }