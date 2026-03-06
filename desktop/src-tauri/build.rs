use std::env;
use std::path::PathBuf;
use std::process::Command;

fn main() {
    println!("cargo:rerun-if-changed=../../scripts/build_desktop_bridge_binary.py");
    println!("cargo:rerun-if-changed=../package.json");
    println!("cargo:rerun-if-changed=../../client/cmd/agent-flows-bridge/main.go");
    println!("cargo:rerun-if-env-changed=APPLE_CERTIFICATE");
    println!("cargo:rerun-if-env-changed=APPLE_CERTIFICATE_PASSWORD");
    println!("cargo:rerun-if-env-changed=APPLE_SIGNING_IDENTITY");

    build_packaged_bridge_binary();
    tauri_build::build()
}

fn build_packaged_bridge_binary() {
    let manifest_dir =
        PathBuf::from(env::var("CARGO_MANIFEST_DIR").expect("read CARGO_MANIFEST_DIR"));
    let script_path = manifest_dir
        .parent()
        .expect("resolve desktop dir")
        .parent()
        .expect("resolve repo dir")
        .join("scripts")
        .join("build_desktop_bridge_binary.py");

    let status = Command::new("python3")
        .arg(script_path)
        .current_dir(&manifest_dir)
        .status()
        .expect("run build_desktop_bridge_binary.py");

    if !status.success() {
        panic!("build_desktop_bridge_binary.py failed with status {status}");
    }
}
