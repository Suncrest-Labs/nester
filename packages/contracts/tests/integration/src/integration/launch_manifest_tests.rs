#![cfg(test)]

use std::collections::{BTreeMap, BTreeSet};
use std::path::PathBuf;

#[derive(Debug, Default, Clone)]
struct ManifestEntry {
    section: String,
    key: String,
    package: String,
    wasm: String,
    path: String,
    max_wasm_bytes: u64,
    audited_commit: String,
    reason: String,
}

fn workspace_root() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .parent()
        .and_then(|p| p.parent())
        .expect("packages/contracts directory")
        .to_path_buf()
}

fn parse_launch_manifest() -> BTreeMap<String, ManifestEntry> {
    let manifest_path = workspace_root().join("launch-manifest.toml");
    let raw = std::fs::read_to_string(&manifest_path)
        .unwrap_or_else(|e| panic!("failed to read {}: {}", manifest_path.display(), e));

    let mut entries = BTreeMap::new();
    let mut current: Option<ManifestEntry> = None;

    for raw_line in raw.lines() {
        let line = raw_line.split('#').next().unwrap_or("").trim();
        if line.is_empty() {
            continue;
        }
        if line.starts_with('[') && line.ends_with(']') {
            if let Some(entry) = current.take() {
                entries.insert(entry.key.clone(), entry);
            }
            let header = &line[1..line.len() - 1];
            if let Some((sec, key)) = header.split_once('.') {
                if matches!(sec, "contracts" | "adapters" | "excluded") {
                    current = Some(ManifestEntry {
                        section: sec.to_string(),
                        key: key.to_string(),
                        max_wasm_bytes: 131_072,
                        ..Default::default()
                    });
                }
            }
            continue;
        }

        if let Some(entry) = current.as_mut() {
            if let Some((k, v)) = line.split_once('=') {
                let k = k.trim();
                let v = v.trim().trim_matches('"');
                match k {
                    "package" => entry.package = v.to_string(),
                    "wasm" => entry.wasm = v.to_string(),
                    "path" => entry.path = v.to_string(),
                    "max_wasm_bytes" => {
                        entry.max_wasm_bytes = v.parse::<u64>().expect("valid max_wasm_bytes");
                    }
                    "audited_commit" => entry.audited_commit = v.to_string(),
                    "reason" => entry.reason = v.to_string(),
                    _ => {}
                }
            }
        }
    }

    if let Some(entry) = current.take() {
        entries.insert(entry.key.clone(), entry);
    }

    entries
}

#[test]
fn test_launch_manifest_defines_shipping_set_and_excludes_non_launch_contracts() {
    let entries = parse_launch_manifest();

    let expected_shipping: BTreeSet<&str> = [
        "vault",
        "vault_token",
        "vault_factory",
        "treasury",
        "yield_registry",
        "allocation_strategy",
        "nester",
        "access_control",
        "timelock",
        "adapter_blend",
        "adapter_soroswap",
    ]
    .into_iter()
    .collect();

    let must_be_excluded: BTreeSet<&str> = [
        "lp_aggregator",
        "referral",
        "goal_rewards",
        "savings_goal",
        "recurring_deposit",
        "adapter_lending",
        "adapter_pool",
        "rent_escrow",
    ]
    .into_iter()
    .collect();

    for name in &expected_shipping {
        let entry = entries
            .get(*name)
            .unwrap_or_else(|| panic!("missing shipping contract/adapter '{}' in manifest", name));
        assert!(
            entry.section == "contracts" || entry.section == "adapters",
            "{} must be in [contracts] or [adapters], found [{}]",
            name,
            entry.section
        );
        assert!(!entry.package.is_empty(), "{} missing package", name);
        assert!(
            entry.wasm.ends_with(".wasm"),
            "{} missing .wasm artifact",
            name
        );
        assert!(
            entry.audited_commit.len() == 40
                && entry.audited_commit.chars().all(|c| c.is_ascii_hexdigit()),
            "{} must have a 40-char hex audited_commit, got '{}'",
            name,
            entry.audited_commit
        );
        assert!(
            entry.max_wasm_bytes > 0 && entry.max_wasm_bytes <= 131_072,
            "{} max_wasm_bytes must be within 128 KiB limit",
            name
        );
    }

    for name in &must_be_excluded {
        let entry = entries.get(*name).unwrap_or_else(|| {
            panic!(
                "excluded contract '{}' must be documented in manifest",
                name
            )
        });
        assert_eq!(
            entry.section, "excluded",
            "'{}' must never appear in [contracts] or [adapters]",
            name
        );
        assert!(
            !entry.reason.is_empty(),
            "excluded contract '{}' must record an exclusion reason",
            name
        );
    }
}

#[test]
fn test_audit_scope_doc_links_launch_manifest() {
    let repo_root = workspace_root()
        .parent()
        .and_then(|p| p.parent())
        .expect("repo root")
        .to_path_buf();
    let audit_doc_path = repo_root.join("docs/security/contract_audits.md");
    let content = std::fs::read_to_string(&audit_doc_path)
        .unwrap_or_else(|e| panic!("failed to read {}: {}", audit_doc_path.display(), e));
    assert!(
        content.contains("packages/contracts/launch-manifest.toml"),
        "docs/security/contract_audits.md must link packages/contracts/launch-manifest.toml"
    );
}
