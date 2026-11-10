// Shared build script referenced by every deployable contract crate via
// `build = "../../build-meta.rs"` in their Cargo.toml.
//
// Writes the version and commit contractmeta! entries to
// $OUT_DIR/contractmeta.rs, which each contract include!s. contractmeta! only
// accepts string literals, so the values are baked in here rather than read
// with env!() at the call site. GIT_COMMIT_HASH is respected as-is when
// already set by CI (e.g. GITHUB_SHA), so builds are reproducible.
fn main() {
    let hash = std::env::var("GIT_COMMIT_HASH").unwrap_or_else(|_| {
        std::process::Command::new("git")
            .args(["rev-parse", "--short", "HEAD"])
            .output()
            .ok()
            .and_then(|o| String::from_utf8(o.stdout).ok())
            .map(|s| s.trim().to_owned())
            .unwrap_or_else(|| "unknown".to_string())
    });
    let version = std::env::var("CARGO_PKG_VERSION").expect("cargo sets CARGO_PKG_VERSION");

    let out_dir = std::env::var("OUT_DIR").expect("cargo sets OUT_DIR");
    let meta = format!(
        "soroban_sdk::contractmeta!(key = \"version\", val = {version:?});\n\
         soroban_sdk::contractmeta!(key = \"commit\", val = {hash:?});\n"
    );
    std::fs::write(std::path::Path::new(&out_dir).join("contractmeta.rs"), meta)
        .expect("write contractmeta.rs");

    println!("cargo:rerun-if-changed=.git/HEAD");
    println!("cargo:rerun-if-env-changed=GIT_COMMIT_HASH");
}
