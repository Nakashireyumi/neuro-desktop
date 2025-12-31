// build.rs
fn main() {
    println!("cargo:rustc-link-search=native=libs"); // Specify the library path
    println!("cargo:rustc-link-lib=static=go_lib"); // Link the static library
}
